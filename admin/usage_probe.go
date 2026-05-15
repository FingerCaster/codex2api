package admin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/codex2api/auth"
	"github.com/codex2api/proxy"
)

// ProbeUsageSnapshot 主动发送最小探针请求刷新账号用量
func (h *Handler) ProbeUsageSnapshot(ctx context.Context, account *auth.Account) error {
	if account == nil {
		return nil
	}

	if account.IsOpenAIResponsesAPI() {
		return h.probeOpenAIResponsesAPIAccount(ctx, account)
	}

	account.Mu().RLock()
	hasToken := account.AccessToken != ""
	account.Mu().RUnlock()
	if !hasToken {
		return nil
	}

	payload := buildTestPayload(h.store.GetTestModel())
	resp, err := proxy.ExecuteRequest(ctx, account, payload, "", h.store.ResolveProxyForAccount(account), "", nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	usageState := proxy.SyncCodexUsageState(h.store, account, resp)

	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		h.store.ReportRequestSuccess(account, 0)
		// 只有用量未耗尽时才重置状态
		if !usageState.Premium5hRateLimited && (!usageState.HasUsage7d || usageState.UsagePct7d < 100) {
			h.store.ClearCooldown(account)
		}
		return nil
	case http.StatusUnauthorized:
		h.store.ReportRequestFailure(account, "client", 0)
		h.store.MarkCooldown(account, 24*time.Hour, "unauthorized")
		return nil
	case http.StatusTooManyRequests:
		h.store.ReportRequestFailure(account, "client", 0)
		proxy.Apply429Cooldown(h.store, account, nil, resp, h.store.GetTestModel())
		return nil
	default:
		if resp.StatusCode >= 500 {
			h.store.ReportRequestFailure(account, "server", 0)
		} else if resp.StatusCode >= 400 {
			h.store.ReportRequestFailure(account, "client", 0)
		}
		return fmt.Errorf("探针返回状态 %d", resp.StatusCode)
	}
}

func (h *Handler) probeOpenAIResponsesAPIAccount(ctx context.Context, account *auth.Account) error {
	payload := buildTestPayload(h.store.GetTestModel())
	resp, err := proxy.ExecuteOpenAIResponsesRequest(ctx, account, payload, h.store.ResolveProxyForAccount(account), nil)
	if err != nil {
		h.store.ReportRequestFailure(account, apiProbeTransportFailureKind(err), 0)
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		h.store.ReportRequestSuccess(account, 0)
		return nil
	case http.StatusUnauthorized:
		h.store.ReportRequestFailure(account, "unauthorized", 0)
		h.store.MarkCooldown(account, h.store.GetAPIAccountCooldown(), "unauthorized")
		return fmt.Errorf("API 账号探针返回状态 %d", resp.StatusCode)
	case http.StatusTooManyRequests:
		h.store.ReportRequestFailure(account, "client", 0)
		proxy.Apply429Cooldown(h.store, account, body, resp, h.store.GetTestModel())
		return fmt.Errorf("API 账号探针返回状态 %d", resp.StatusCode)
	case http.StatusPaymentRequired, http.StatusForbidden:
		h.store.ReportRequestFailure(account, "client", 0)
		reason := "quota_unavailable"
		if proxy.IsDeactivatedWorkspaceError(body) {
			reason = "subscription_unavailable"
		} else if resp.StatusCode == http.StatusForbidden && !apiProbeBodyLooksQuotaLimited(body) {
			reason = "unauthorized"
		}
		h.store.MarkCooldown(account, h.store.GetAPIAccountCooldown(), reason)
		return fmt.Errorf("API 账号探针返回状态 %d", resp.StatusCode)
	default:
		if resp.StatusCode >= 500 {
			h.store.ReportRequestFailure(account, "server", 0)
		} else if resp.StatusCode >= 400 {
			h.store.ReportRequestFailure(account, "client", 0)
		}
		return fmt.Errorf("API 账号探针返回状态 %d", resp.StatusCode)
	}
}

func apiProbeTransportFailureKind(err error) string {
	if err == nil {
		return "transport"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") {
		return "timeout"
	}
	return "transport"
}

func apiProbeBodyLooksQuotaLimited(body []byte) bool {
	msg := strings.ToLower(string(body))
	for _, needle := range []string{"quota", "credit", "billing", "subscription", "insufficient", "exhausted", "limit"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
