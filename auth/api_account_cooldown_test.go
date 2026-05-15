package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex2api/database"
)

func newTestAPIAccount(id int64) *Account {
	return &Account{
		DBID:         id,
		UpstreamType: UpstreamOpenAIResponses,
		BaseURL:      "https://api.example.com",
		APIKey:       "sk-test",
		PlanType:     "api",
		Status:       StatusReady,
		HealthTier:   HealthTierHealthy,
	}
}

func newAPIAccountCooldownTestStore() *Store {
	return NewStore(nil, nil, &database.SystemSettings{
		MaxConcurrency:                         4,
		TestConcurrency:                        1,
		TestModel:                              "gpt-5.4",
		APIAccountCircuitBreakerEnabled:        true,
		APIAccountFailureRateThreshold:         80,
		APIAccountFailureMinSamples:            5,
		APIAccountCooldownMinutes:              2,
		APIAccountRecoveryProbeIntervalMinutes: 1,
		APIAccountRecoveryProbeSuccesses:       1,
		APIAccountRecoveryDirectHealthy:        true,
		APIAccountRecoveryGuardMinutes:         1,
		BackgroundRefreshIntervalMinutes:       2,
		UsageProbeMaxAgeMinutes:                10,
		RecoveryProbeIntervalMinutes:           30,
		MaxRateLimitRetries:                    1,
	})
}

func TestAPIAccountServerFailuresCooldownOnlyAfterThreshold(t *testing.T) {
	store := newAPIAccountCooldownTestStore()
	acc := newTestAPIAccount(1)
	store.AddAccount(acc)

	for i := 0; i < 4; i++ {
		store.ReportRequestFailure(acc, "server", 0)
		if acc.HasActiveCooldown() {
			t.Fatalf("failure %d unexpectedly put account into cooldown", i+1)
		}
	}

	store.ReportRequestFailure(acc, "server", 0)

	reason, until := acc.GetCooldownSnapshot()
	if reason != "api_account_failure_rate" {
		t.Fatalf("CooldownReason = %q, want api_account_failure_rate", reason)
	}
	if !until.After(time.Now()) {
		t.Fatalf("CooldownUtil = %v, want future time", until)
	}
	if got := store.Next(); got != nil {
		store.Release(got)
		t.Fatalf("Next() returned cooled account %d, want nil", got.DBID)
	}
}

func TestAPIAccountClientFailureDoesNotTriggerFailureRateCooldown(t *testing.T) {
	store := newAPIAccountCooldownTestStore()
	acc := newTestAPIAccount(1)
	store.AddAccount(acc)

	for i := 0; i < 5; i++ {
		store.ReportRequestFailure(acc, "client", 0)
	}

	if acc.HasActiveCooldown() {
		t.Fatal("client failures should not trigger API account failure-rate cooldown")
	}
}

func TestAPIAccountUnauthorizedUsesShortCooldownAndRecoveryProbe(t *testing.T) {
	store := newAPIAccountCooldownTestStore()
	acc := newTestAPIAccount(1)
	store.AddAccount(acc)

	store.MarkCooldown(acc, 24*time.Hour, "unauthorized")

	reason, until := acc.GetCooldownSnapshot()
	if reason != "unauthorized" {
		t.Fatalf("CooldownReason = %q, want unauthorized", reason)
	}
	remaining := time.Until(until)
	if remaining <= 0 || remaining > 3*time.Minute {
		t.Fatalf("unauthorized API cooldown remaining = %s, want short API cooldown", remaining)
	}
	if status := acc.RuntimeStatus(); status != "unauthorized" {
		t.Fatalf("RuntimeStatus() = %q, want unauthorized cooldown reason", status)
	}
	if tier := acc.GetHealthTier(); tier != string(HealthTierRisky) {
		t.Fatalf("HealthTier = %s, want %s", tier, HealthTierRisky)
	}
	if !acc.NeedsRecoveryProbe(time.Minute) {
		t.Fatal("API account should be eligible for recovery probe while cooldown is active")
	}
}

func TestAPIAccountRecoverFromProbeRestoresHealthyWithGuard(t *testing.T) {
	store := newAPIAccountCooldownTestStore()
	store.SetMaxConcurrency(4)
	acc := newTestAPIAccount(1)
	store.AddAccount(acc)

	store.MarkCooldown(acc, 2*time.Minute, "api_account_failure_rate")
	if !store.MarkRecoveryProbeSuccess(acc) {
		t.Fatal("MarkRecoveryProbeSuccess should recover after one success by default")
	}

	reason, until := acc.GetCooldownSnapshot()
	if reason != "" || !until.IsZero() {
		t.Fatalf("cooldown snapshot = (%q, %v), want cleared", reason, until)
	}
	if tier := acc.GetHealthTier(); tier != string(HealthTierHealthy) {
		t.Fatalf("HealthTier = %s, want %s", tier, HealthTierHealthy)
	}
	if acc.FailureStreak != 0 || acc.RecentResultsCnt != 0 {
		t.Fatalf("failure counters not reset: streak=%d samples=%d", acc.FailureStreak, acc.RecentResultsCnt)
	}

	got := store.Next()
	if got == nil {
		t.Fatal("Next() returned nil after probe recovery")
	}
	if got.DBID != acc.DBID {
		t.Fatalf("Next() returned account %d, want %d", got.DBID, acc.DBID)
	}
	defer store.Release(got)

	if gotAgain := store.Next(); gotAgain != nil {
		store.Release(gotAgain)
		t.Fatalf("recovery guard allowed concurrent acquire of account %d", gotAgain.DBID)
	}
}

func TestAPIAccountRecoveryProbeRequiresConfiguredSuccessCount(t *testing.T) {
	store := newAPIAccountCooldownTestStore()
	store.SetAPIAccountRecoveryProbeSuccesses(2)
	acc := newTestAPIAccount(1)
	store.AddAccount(acc)

	store.MarkCooldown(acc, 2*time.Minute, "api_account_failure_rate")

	acc.TryBeginRecoveryProbe()
	store.ReportRequestSuccess(acc, 0)
	acc.FinishRecoveryProbe()
	if store.MarkRecoveryProbeSuccess(acc) {
		t.Fatal("first recovery probe success should not recover account")
	}
	if !acc.HasActiveCooldown() {
		t.Fatal("account cooldown cleared before required probe successes")
	}
	if acc.RecoveryProbeSuccesses != 1 {
		t.Fatalf("RecoveryProbeSuccesses = %d, want 1", acc.RecoveryProbeSuccesses)
	}

	acc.TryBeginRecoveryProbe()
	store.ReportRequestSuccess(acc, 0)
	acc.FinishRecoveryProbe()
	if !store.MarkRecoveryProbeSuccess(acc) {
		t.Fatal("second recovery probe success should recover account")
	}
	if acc.HasActiveCooldown() {
		t.Fatal("account should leave cooldown after required probe successes")
	}
	if acc.RecoveryProbeSuccesses != 0 {
		t.Fatalf("RecoveryProbeSuccesses = %d, want reset to 0", acc.RecoveryProbeSuccesses)
	}
}

func TestAPIAccountRecoveryProbeDoesNotNeedOAuthRefresh(t *testing.T) {
	store := newAPIAccountCooldownTestStore()
	store.SetUsageProbeFunc(func(_ context.Context, account *Account) error {
		if account.DBID != 1 {
			t.Fatalf("probe account = %d, want 1", account.DBID)
		}
		return nil
	})
	acc := newTestAPIAccount(1)
	store.AddAccount(acc)
	store.MarkCooldown(acc, 2*time.Minute, "api_account_failure_rate")

	store.parallelRecoveryProbe(context.Background())

	if acc.HasActiveCooldown() {
		t.Fatal("API recovery probe should run without OAuth refresh_token")
	}
	if tier := acc.GetHealthTier(); tier != string(HealthTierHealthy) {
		t.Fatalf("HealthTier = %s, want %s", tier, HealthTierHealthy)
	}
}

func TestCodexRecoveryProbeIgnoresAPIAccountSuccessCount(t *testing.T) {
	store := newAPIAccountCooldownTestStore()
	store.SetAPIAccountRecoveryProbeSuccesses(2)
	acc := &Account{
		DBID:         2,
		RefreshToken: "rt",
		AccessToken:  "at",
		Status:       StatusReady,
		HealthTier:   HealthTierBanned,
	}
	store.AddAccount(acc)

	if !store.MarkRecoveryProbeSuccess(acc) {
		t.Fatal("Codex account recovery should not require API account success count")
	}
	if tier := acc.GetHealthTier(); tier != string(HealthTierHealthy) {
		t.Fatalf("HealthTier = %s, want %s", tier, HealthTierHealthy)
	}
}

func TestFastSchedulerHonorsAPIAccountRecoveryGuard(t *testing.T) {
	store := newAPIAccountCooldownTestStore()
	store.SetFastSchedulerEnabled(true)
	acc := newTestAPIAccount(1)
	store.AddAccount(acc)

	store.RecoverAccountFromProbe(acc, time.Now())

	first := store.Next()
	if first == nil {
		t.Fatal("fast scheduler Next() returned nil")
	}
	if first.DBID != acc.DBID {
		t.Fatalf("fast scheduler returned account %d, want %d", first.DBID, acc.DBID)
	}
	defer store.Release(first)

	second := store.Next()
	if second != nil {
		store.Release(second)
		t.Fatalf("fast scheduler ignored recovery guard and returned account %d", second.DBID)
	}
	if active := atomic.LoadInt64(&acc.ActiveRequests); active != 1 {
		t.Fatalf("ActiveRequests = %d, want 1 while first acquire is held", active)
	}
}
