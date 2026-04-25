package database

import "testing"

func TestAccountEmailFromRawCredentialsFallsBackToProviderName(t *testing.T) {
	raw := `{"provider_name":"9527","base_url":"https://api.9527code.com"}`

	got := accountEmailFromRawCredentials(raw)
	if got != "9527" {
		t.Fatalf("accountEmailFromRawCredentials() = %q, want %q", got, "9527")
	}
}
