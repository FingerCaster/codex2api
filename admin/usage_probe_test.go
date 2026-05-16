package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codex2api/auth"
	"github.com/codex2api/database"
)

func newAPIProbeTestHandler(statusCode int, body string) (*Handler, *auth.Account, func()) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))

	store := auth.NewStore(nil, nil, &database.SystemSettings{
		MaxConcurrency:                         2,
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
	account := &auth.Account{
		DBID:           11,
		UpstreamType:   auth.UpstreamOpenAIResponses,
		BaseURL:        server.URL,
		APIKey:         "sk-test",
		Status:         auth.StatusCooldown,
		CooldownReason: "api_account_failure_rate",
		PlanType:       "api",
		HealthTier:     auth.HealthTierRisky,
	}
	store.AddAccount(account)

	return &Handler{store: store}, account, server.Close
}

func TestProbeUsageSnapshotOpenAIResponsesSuccessLeavesRecoveryToCaller(t *testing.T) {
	handler, account, cleanup := newAPIProbeTestHandler(http.StatusOK, `{"id":"resp_123"}`)
	defer cleanup()

	if err := handler.ProbeUsageSnapshot(context.Background(), account); err != nil {
		t.Fatalf("ProbeUsageSnapshot returned error: %v", err)
	}

	reason, _ := account.GetCooldownSnapshot()
	if reason != "api_account_failure_rate" {
		t.Fatalf("ProbeUsageSnapshot cleared cooldown reason %q, want recovery caller to clear it", reason)
	}
	if account.FailureStreak != 0 {
		t.Fatalf("FailureStreak = %d, want 0 after successful probe", account.FailureStreak)
	}
}

func TestProbeUsageSnapshotOpenAIResponsesQuotaCountsAsOtherError(t *testing.T) {
	handler, account, cleanup := newAPIProbeTestHandler(http.StatusPaymentRequired, `{"error":{"message":"billing quota exhausted"}}`)
	defer cleanup()

	handler.store.ClearCooldown(account)
	if err := handler.ProbeUsageSnapshot(context.Background(), account); err == nil {
		t.Fatal("ProbeUsageSnapshot should return error for quota-limited probe response")
	}

	if account.HasActiveCooldown() {
		t.Fatal("single quota probe response should not directly put API account into cooldown")
	}
	if account.LastOtherErrorAt.IsZero() {
		t.Fatal("quota probe response should be counted as other_error")
	}
}

func TestProbeUsageSnapshotOpenAIResponsesUnauthorizedDoesNotRecover(t *testing.T) {
	handler, account, cleanup := newAPIProbeTestHandler(http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`)
	defer cleanup()

	if err := handler.ProbeUsageSnapshot(context.Background(), account); err == nil {
		t.Fatal("ProbeUsageSnapshot should return error for unauthorized probe response")
	}

	reason, until := account.GetCooldownSnapshot()
	if reason != "unauthorized" {
		t.Fatalf("CooldownReason = %q, want unauthorized", reason)
	}
	if until.IsZero() {
		t.Fatalf("CooldownUtil = %v, want future time", until)
	}
	if account.RecoveryProbeSuccesses != 0 {
		t.Fatalf("RecoveryProbeSuccesses = %d, want 0", account.RecoveryProbeSuccesses)
	}
}

func TestProbeUsageSnapshotOpenAIResponsesServerFailureDoesNotImmediateCooldown(t *testing.T) {
	handler, account, cleanup := newAPIProbeTestHandler(http.StatusBadGateway, `{"error":{"message":"temporary upstream failure"}}`)
	defer cleanup()

	handler.store.ClearCooldown(account)
	if err := handler.ProbeUsageSnapshot(context.Background(), account); err == nil {
		t.Fatal("ProbeUsageSnapshot should return error for 5xx probe response")
	}

	if account.HasActiveCooldown() {
		t.Fatal("single 5xx probe should not immediately put API account into cooldown")
	}
}

func TestAPIAccountConnectionTestUnauthorizedUsesShortCooldown(t *testing.T) {
	handler, account, cleanup := newAPIProbeTestHandler(http.StatusOK, `{"id":"resp_123"}`)
	defer cleanup()

	handler.store.ClearCooldown(account)
	handler.applyAPIAccountConnectionTestFailure(account, http.StatusUnauthorized, []byte(`{"error":{"message":"bad key"}}`), nil, "gpt-5.4", 0)

	reason, until := account.GetCooldownSnapshot()
	if reason != "unauthorized" {
		t.Fatalf("CooldownReason = %q, want unauthorized", reason)
	}
	remaining := until.Sub(time.Now())
	if remaining <= 0 || remaining > 3*time.Minute {
		t.Fatalf("unauthorized connection test cooldown remaining = %s, want short API cooldown", remaining)
	}
	if !account.NeedsRecoveryProbe(time.Minute) {
		t.Fatal("API account should stay eligible for recovery probe after connection-test unauthorized")
	}
	if account.RuntimeStatus() != "unauthorized" {
		t.Fatalf("RuntimeStatus = %q, want unauthorized", account.RuntimeStatus())
	}
}

func TestAPIAccountConnectionTestForbiddenQuotaCountsAsOtherError(t *testing.T) {
	handler, account, cleanup := newAPIProbeTestHandler(http.StatusOK, `{"id":"resp_123"}`)
	defer cleanup()

	handler.store.ClearCooldown(account)
	handled, rateLimited := handler.applyAPIAccountConnectionTestFailure(account, http.StatusForbidden, []byte(`{"error":{"message":"billing quota exhausted"}}`), nil, "gpt-5.4", 0)
	if !handled || rateLimited {
		t.Fatalf("handled=%t rateLimited=%t, want true/false", handled, rateLimited)
	}

	if account.HasActiveCooldown() {
		t.Fatal("single forbidden quota response should not directly put API account into cooldown")
	}
	if account.LastOtherErrorAt.IsZero() {
		t.Fatal("forbidden quota response should be counted as other_error")
	}
}
