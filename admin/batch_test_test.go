package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codex2api/auth"
	"github.com/codex2api/database"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func TestShouldMarkBatchTestAccountError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       []byte
		want       bool
	}{
		{
			name:       "forbidden is account scoped",
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"code":"unsupported_country_region_territory"}}`),
			want:       true,
		},
		{
			name:       "payment required deactivated workspace is account scoped",
			statusCode: http.StatusPaymentRequired,
			body:       []byte(`{"detail":{"code":"deactivated_workspace"}}`),
			want:       true,
		},
		{
			name:       "invalid grant bad request is account scoped",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error":"invalid_grant"}`),
			want:       true,
		},
		{
			name:       "model version bad request is global",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"detail":"The 'gpt-5.5' model requires a newer version of Codex"}`),
			want:       false,
		},
		{
			name:       "server error is not marked as account error",
			statusCode: http.StatusBadGateway,
			body:       []byte(`bad gateway`),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldMarkBatchTestAccountError(tt.statusCode, tt.body); got != tt.want {
				t.Fatalf("shouldMarkBatchTestAccountError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveBatchTestAccountsDefaultsToAllAccounts(t *testing.T) {
	store := auth.NewStore(nil, nil, nil)
	store.AddAccount(&auth.Account{DBID: 1, AccessToken: "token-1", Status: auth.StatusReady})
	store.AddAccount(&auth.Account{DBID: 2, AccessToken: "token-2", Status: auth.StatusReady})

	accounts, missing := resolveBatchTestAccounts(store, nil)
	if missing != 0 {
		t.Fatalf("missing = %d, want 0", missing)
	}
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d, want 2", len(accounts))
	}
}

func TestResolveBatchTestAccountsUsesSelectedIDs(t *testing.T) {
	store := auth.NewStore(nil, nil, nil)
	store.AddAccount(&auth.Account{DBID: 1, AccessToken: "token-1", Status: auth.StatusReady})
	store.AddAccount(&auth.Account{DBID: 2, AccessToken: "token-2", Status: auth.StatusReady})

	ids := []int64{2, 99, 2, 1}
	accounts, missing := resolveBatchTestAccounts(store, &ids)
	if missing != 1 {
		t.Fatalf("missing = %d, want 1", missing)
	}
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d, want 2", len(accounts))
	}
	if accounts[0].DBID != 2 || accounts[1].DBID != 1 {
		t.Fatalf("account order = [%d, %d], want [2, 1]", accounts[0].DBID, accounts[1].DBID)
	}
}

func TestBatchTestUsesGenericProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotPath string
	var gotAuth string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_test","status":"completed","output_text":"hello","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	store := auth.NewStore(nil, nil, &database.SystemSettings{MaxConcurrency: 2, TestConcurrency: 1, TestModel: "gpt-5.4"})
	store.AddAccount(&auth.Account{
		DBID:         9,
		Type:         "api_key",
		BaseURL:      upstream.URL,
		APIKey:       "upstream-key",
		ProviderName: "generic",
		Status:       auth.StatusReady,
	})

	handler := &Handler{store: store}
	router := gin.New()
	router.POST("/api/admin/accounts/batch-test", handler.BatchTest)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/accounts/batch-test", strings.NewReader(`{"ids":[9]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var decoded map[string]int
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded["success"] != 1 || decoded["failed"] != 0 {
		t.Fatalf("response = %v, want success=1 failed=0", decoded)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("upstream path = %q, want /v1/responses", gotPath)
	}
	if gotAuth != "Bearer upstream-key" {
		t.Fatalf("Authorization = %q, want Bearer upstream-key", gotAuth)
	}
	if got := gjson.GetBytes(gotBody, "model").String(); got != "gpt-5.4" {
		t.Fatalf("generic test body model = %q, want gpt-5.4; body=%s", got, string(gotBody))
	}
}
