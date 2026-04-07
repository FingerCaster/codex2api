package proxy

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/codex2api/auth"
)

// ExecuteGenericOpenAIRequest 向 OpenAI 兼容上游发送请求（base_url + api_key）
func ExecuteGenericOpenAIRequest(ctx context.Context, account *auth.Account, endpointPath string, requestBody []byte, proxyOverride string, stream bool, downstreamHeaders http.Header) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	baseURL, apiKey, extraHeaders := account.GenericUpstream()
	if baseURL == "" || apiKey == "" {
		return nil, ErrNoAvailableAccount()
	}

	account.Mu().RLock()
	proxyURL := account.ProxyURL
	account.Mu().RUnlock()
	if proxyOverride != "" {
		proxyURL = proxyOverride
	}

	targetURL := strings.TrimRight(baseURL, "/") + endpointPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, ErrInternalError("创建请求失败", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	// 透传兼容头，避免丢失客户端期望的 OpenAI 兼容能力开关。
	for _, header := range []string{"OpenAI-Beta", "OpenAI-Organization"} {
		if value := strings.TrimSpace(downstreamHeaders.Get(header)); value != "" {
			req.Header.Set(header, value)
		}
	}
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}

	client := getPooledClient(account, proxyURL)
	resp, err := client.Do(req)
	if err != nil {
		if shouldRecyclePooledClient(err) {
			recyclePooledClient(account, proxyURL)
		}
		return nil, ErrUpstream(0, "请求上游失败", err)
	}

	return resp, nil
}
