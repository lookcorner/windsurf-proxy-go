package reuse

import "testing"

func TestCheckoutRequiresMatchingAccount(t *testing.T) {
	fingerprint := "fp-account-match"
	Checkin(fingerprint, Entry{
		InstanceName: "local",
		AccountID:    "acct_a",
		CascadeID:    "cascade-1",
	})

	if entry := Checkout(fingerprint, "local", "acct_b"); entry != nil {
		t.Fatalf("Checkout() returned %+v for mismatched account", entry)
	}

	entry := Checkout(fingerprint, "local", "acct_a")
	if entry == nil {
		t.Fatalf("Checkout() returned nil for matching account")
	}
	if entry.CascadeID != "cascade-1" {
		t.Fatalf("CascadeID = %q, want cascade-1", entry.CascadeID)
	}
}
