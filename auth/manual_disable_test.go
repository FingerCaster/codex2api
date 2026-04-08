package auth

import (
	"sync/atomic"
	"testing"
)

func TestAccountManualDisabledBlocksAvailability(t *testing.T) {
	acc := &Account{
		AccessToken: "token",
		Status:      StatusReady,
	}
	if !acc.IsAvailable() {
		t.Fatal("expected account to be available before manual disable")
	}

	atomic.StoreInt32(&acc.ManualDisabled, 1)

	if acc.IsAvailable() {
		t.Fatal("expected manually disabled account to be unavailable")
	}
	if got := acc.RuntimeStatus(); got != "disabled" {
		t.Fatalf("RuntimeStatus() = %q, want %q", got, "disabled")
	}
}

func TestStoreSetManualDisabled(t *testing.T) {
	acc := &Account{DBID: 42, AccessToken: "token", Status: StatusReady}
	store := &Store{
		accounts: []*Account{acc},
	}

	if ok := store.SetManualDisabled(42, true); !ok {
		t.Fatal("SetManualDisabled() = false, want true")
	}
	if atomic.LoadInt32(&acc.ManualDisabled) != 1 {
		t.Fatal("expected ManualDisabled flag to be set")
	}

	if ok := store.SetManualDisabled(42, false); !ok {
		t.Fatal("SetManualDisabled() = false, want true when enabling")
	}
	if atomic.LoadInt32(&acc.ManualDisabled) != 0 {
		t.Fatal("expected ManualDisabled flag to be cleared")
	}
}
