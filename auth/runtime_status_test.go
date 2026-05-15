package auth

import (
	"testing"
	"time"
)

func TestRuntimeStatusShowsRefreshingForRTWithoutAccessToken(t *testing.T) {
	acc := &Account{
		RefreshToken: "rt-test",
		Status:       StatusReady,
	}

	if got := acc.RuntimeStatus(); got != "refreshing" {
		t.Fatalf("RuntimeStatus() = %q, want refreshing", got)
	}
}

func TestRuntimeStatusKeepsErrorForFailedRTAccount(t *testing.T) {
	acc := &Account{
		RefreshToken: "rt-test",
		Status:       StatusError,
		ErrorMsg:     "invalid_grant",
	}

	if got := acc.RuntimeStatus(); got != "error" {
		t.Fatalf("RuntimeStatus() = %q, want error", got)
	}
}

func TestMarkErrorAndClearCooldownRoundTrip(t *testing.T) {
	store := NewStore(nil, nil, nil)
	acc := &Account{
		DBID:        1,
		AccessToken: "at-test",
		Status:      StatusReady,
	}

	store.MarkError(acc, "batch test failed")
	if got := acc.RuntimeStatus(); got != "error" {
		t.Fatalf("RuntimeStatus() after MarkError = %q, want error", got)
	}

	store.ClearCooldown(acc)
	if got := acc.RuntimeStatus(); got != "active" {
		t.Fatalf("RuntimeStatus() after ClearCooldown = %q, want active", got)
	}
}

func TestForceAccountHealthyClearsWarmSignals(t *testing.T) {
	store := NewStore(nil, nil, nil)
	now := time.Now()
	acc := &Account{
		DBID:               1,
		AccessToken:        "at-test",
		Status:             StatusCooldown,
		HealthTier:         HealthTierWarm,
		CooldownUtil:       now.Add(time.Hour),
		CooldownReason:     "rate_limited",
		LastFailureAt:      now,
		LastUnauthorizedAt: now,
		FailureStreak:      3,
	}

	store.ForceAccountHealthy(acc)

	if got := acc.GetHealthTier(); got != string(HealthTierHealthy) {
		t.Fatalf("GetHealthTier() = %q, want %q", got, HealthTierHealthy)
	}
	if got := acc.RuntimeStatus(); got != "active" {
		t.Fatalf("RuntimeStatus() = %q, want active", got)
	}
	_, cooldownUntil := acc.GetCooldownSnapshot()
	if !cooldownUntil.IsZero() {
		t.Fatalf("cooldown_until should be cleared")
	}
	if reason := acc.GetCooldownReason(); reason != "" {
		t.Fatalf("GetCooldownReason() = %q, want empty", reason)
	}
}
