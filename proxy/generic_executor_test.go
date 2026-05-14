package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codex2api/auth"
)

func TestApplyOpenAICompatibleUserAgentPreservesDownstream(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	applyOpenAICompatibleUserAgent(req, http.Header{
		"User-Agent": []string{"codex_cli_rs/0.128.0 (Windows 10.0.26120; x86_64) WindowsTerminal"},
	})

	if got := req.Header.Get("User-Agent"); got != "codex_cli_rs/0.128.0 (Windows 10.0.26120; x86_64) WindowsTerminal" {
		t.Fatalf("User-Agent = %q", got)
	}
}

func TestApplyOpenAICompatibleUserAgentFallsBackToMinimalCodexCLI(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	applyOpenAICompatibleUserAgent(req, http.Header{})

	if got := req.Header.Get("User-Agent"); got != MinimalCodexCLIUserAgentForHeaders() {
		t.Fatalf("User-Agent = %q, want %q", got, MinimalCodexCLIUserAgentForHeaders())
	}
}

func TestExecuteGenericOpenAIRequestForwardsUserAgent(t *testing.T) {
	var gotUserAgent string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	account := &auth.Account{
		DBID:         42,
		UpstreamType: auth.UpstreamOpenAIResponses,
		BaseURL:      server.URL,
		APIKey:       "sk-test",
	}

	resp, err := ExecuteGenericOpenAIRequest(
		context.Background(),
		account,
		"/v1/responses",
		[]byte(`{"model":"gpt-5.4"}`),
		"",
		false,
		http.Header{"User-Agent": []string{"codex_cli_rs/0.128.0 (Ubuntu 24.04; x86_64) kitty/0.35.2"}},
	)
	if err != nil {
		t.Fatalf("ExecuteGenericOpenAIRequest() error = %v", err)
	}
	defer resp.Body.Close()

	if gotUserAgent != "codex_cli_rs/0.128.0 (Ubuntu 24.04; x86_64) kitty/0.35.2" {
		t.Fatalf("forwarded User-Agent = %q", gotUserAgent)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestExecuteOpenAIResponsesRequestFallsBackToMinimalCodexCLIUserAgent(t *testing.T) {
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_123"}`))
	}))
	defer server.Close()

	account := &auth.Account{
		DBID:         7,
		UpstreamType: auth.UpstreamOpenAIResponses,
		BaseURL:      server.URL,
		APIKey:       "sk-test",
	}

	resp, err := ExecuteOpenAIResponsesRequest(
		context.Background(),
		account,
		[]byte(`{"model":"gpt-5.4"}`),
		"",
		http.Header{"OpenAI-Project": []string{"proj_123"}},
	)
	if err != nil {
		t.Fatalf("ExecuteOpenAIResponsesRequest() error = %v", err)
	}
	defer resp.Body.Close()

	if gotUserAgent != MinimalCodexCLIUserAgentForHeaders() {
		t.Fatalf("User-Agent = %q, want %q", gotUserAgent, MinimalCodexCLIUserAgentForHeaders())
	}
}
