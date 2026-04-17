package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/codex2api/auth"
	"github.com/codex2api/database"
	"github.com/codex2api/proxy"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// testEvent SSE 测试事件
type testEvent struct {
	Type    string `json:"type"`              // test_start | content | test_complete | error
	Text    string `json:"text,omitempty"`    // 内容文本
	Model   string `json:"model,omitempty"`   // 测试模型
	Success bool   `json:"success,omitempty"` // 是否成功
	Error   string `json:"error,omitempty"`   // 错误信息
}

// TestConnection 测试账号连接（SSE 流式返回）
// GET /api/admin/accounts/:id/test
func (h *Handler) TestConnection(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的账号 ID"})
		return
	}

	// 查找运行时账号
	account := h.store.FindByID(id)
	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "账号不在运行时池中"})
		return
	}

	// 检查 access_token 是否可用
	account.Mu().RLock()
	hasToken := account.AccessToken != ""
	account.Mu().RUnlock()

	if !hasToken && !account.IsAPIKeyProvider() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "账号没有可用的 Access Token，请先刷新"})
		return
	}

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	testModel := h.store.GetTestModel()

	// 发送 test_start
	sendTestEvent(c, testEvent{Type: "test_start", Model: testModel})

	// 构建最小测试请求体（参考 sub2api createOpenAITestPayload）
	payload := buildTestPayload(testModel)
	estimatedPromptTokens := estimateSimpleTokens(string(payload))

	// 发送请求
	start := time.Now()
	var resp *http.Response
	var reqErr error
	if account.IsAPIKeyProvider() {
		resp, reqErr = proxy.ExecuteGenericOpenAIRequest(c.Request.Context(), account, "/v1/responses", payload, "", true, nil)
	} else {
		resp, reqErr = proxy.ExecuteRequest(c.Request.Context(), account, payload, "", "", "", nil, nil)
	}
	if reqErr != nil {
		sendTestEvent(c, testEvent{Type: "error", Error: fmt.Sprintf("请求失败: %s", reqErr.Error())})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		proxy.SyncCodexUsageState(h.store, account, resp)
		errBody, _ := io.ReadAll(resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			h.store.MarkCooldown(account, 24*time.Hour, "unauthorized")
		case http.StatusTooManyRequests:
			proxy.Apply429Cooldown(h.store, account, errBody, resp)
		}
		sendTestEvent(c, testEvent{Type: "error", Error: fmt.Sprintf("上游返回 %d: %s", resp.StatusCode, truncate(string(errBody), 500))})
		return
	}

	usageState := proxy.SyncCodexUsageState(h.store, account, resp)

	// 解析 SSE 流
	hasContent := false
	var promptTokens int
	var completionTokens int
	var totalTokens int
	var inputTokens int
	var outputTokens int
	var reasoningTokens int
	var cachedTokens int
	_ = proxy.ReadSSEStream(resp.Body, func(data []byte) bool {
		eventType := gjson.GetBytes(data, "type").String()

		switch eventType {
		case "response.output_text.delta":
			delta := gjson.GetBytes(data, "delta").String()
			if delta != "" {
				hasContent = true
				sendTestEvent(c, testEvent{Type: "content", Text: delta})
			}
		case "response.completed":
				usage := gjson.GetBytes(data, "response.usage")
				if usage.Exists() {
					promptTokens = int(usage.Get("prompt_tokens").Int())
				completionTokens = int(usage.Get("completion_tokens").Int())
				totalTokens = int(usage.Get("total_tokens").Int())
				inputTokens = int(usage.Get("input_tokens").Int())
				outputTokens = int(usage.Get("output_tokens").Int())
				reasoningTokens = int(usage.Get("output_tokens_details.reasoning_tokens").Int())
				cachedTokens = int(usage.Get("input_tokens_details.cached_tokens").Int())

				if promptTokens == 0 {
					promptTokens = inputTokens
				}
				if completionTokens == 0 {
					completionTokens = outputTokens
				}
				if inputTokens == 0 {
					inputTokens = promptTokens
				}
				if outputTokens == 0 {
					outputTokens = completionTokens
				}
					if totalTokens == 0 {
						totalTokens = inputTokens + outputTokens
					}
				}
				// 测试成功即重置冷却状态，用量限制由调度器自行判断
				if !usageState.Premium5hRateLimited && (!usageState.HasUsage7d || usageState.UsagePct7d < 100) {
					h.store.ClearCooldown(account)
				}
			// 如果上游未返回用量头，清除旧的用量缓存，避免显示过期数据
			if !usageState.HasUsage7d && !usageState.HasUsage5h {
				account.ClearUsageCache()
			}
			duration := time.Since(start).Milliseconds()
			if totalTokens == 0 && hasContent {
				inputTokens = estimatedPromptTokens
				promptTokens = estimatedPromptTokens
				outputTokens = 1
				completionTokens = 1
				totalTokens = inputTokens + outputTokens
			}
			if h.db != nil {
				_ = h.db.InsertUsageLog(context.Background(), &database.UsageLogInput{
					AccountID:        account.ID(),
					Endpoint:         "/api/admin/accounts/test",
					Model:            testModel,
					PromptTokens:     promptTokens,
					CompletionTokens: completionTokens,
					TotalTokens:      totalTokens,
					StatusCode:       http.StatusOK,
					DurationMs:       int(duration),
					InputTokens:      inputTokens,
					OutputTokens:     outputTokens,
					ReasoningTokens:  reasoningTokens,
					CachedTokens:     cachedTokens,
					InboundEndpoint:  "/api/admin/accounts/test",
					UpstreamEndpoint: "/v1/responses",
					Stream:           true,
				})
			}
			sendTestEvent(c, testEvent{
				Type: "content",
				Text: fmt.Sprintf("\n\n--- 耗时 %dms ---", duration),
			})
			sendTestEvent(c, testEvent{Type: "test_complete", Success: true})
			return false
		case "response.failed":
			errMsg := gjson.GetBytes(data, "response.status_details.error.message").String()
			if errMsg == "" {
				errMsg = "上游返回 response.failed"
			}
			sendTestEvent(c, testEvent{Type: "error", Error: errMsg})
			return false
		}
		return true
	})

	if !hasContent {
		sendTestEvent(c, testEvent{Type: "error", Error: "未收到模型输出"})
	}
}

// buildTestPayload 构建最小测试请求体
func buildTestPayload(model string) []byte {
	payload := []byte(`{}`)
	payload, _ = sjson.SetBytes(payload, "model", model)
	payload, _ = sjson.SetBytes(payload, "input", []map[string]any{
		{
			"role": "user",
			"content": []map[string]any{
				{
					"type": "input_text",
					"text": "Say hello in one sentence.",
				},
			},
		},
	})
	payload, _ = sjson.SetBytes(payload, "stream", true)
	payload, _ = sjson.SetBytes(payload, "store", false)
	payload, _ = sjson.SetBytes(payload, "instructions", "You are a helpful assistant. Reply briefly.")
	return payload
}

// sendTestEvent 发送 SSE 事件
func sendTestEvent(c *gin.Context, event testEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("序列化测试事件失败: %v", err)
		return
	}
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
		log.Printf("写入 SSE 事件失败: %v", err)
		return
	}
	c.Writer.Flush()
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func estimateSimpleTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	count := utf8.RuneCountInString(text) / 4
	if count < 1 {
		count = 1
	}
	return count
}

// BatchTest 批量测试所有账号连接
// POST /api/admin/accounts/batch-test
func (h *Handler) BatchTest(c *gin.Context) {
	accounts := h.store.Accounts()
	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{"total": 0, "success": 0, "failed": 0, "banned": 0, "rate_limited": 0})
		return
	}

	testModel := h.store.GetTestModel()
	payload := buildTestPayload(testModel)
	concurrency := h.store.GetTestConcurrency()

	var (
		successCount   int64
		failedCount    int64
		bannedCount    int64
		rateLimitCount int64
		wg             sync.WaitGroup
		sem            = make(chan struct{}, concurrency)
	)

	for _, account := range accounts {
		// 跳过没有 token 的账号
		account.Mu().RLock()
		hasToken := account.AccessToken != ""
		account.Mu().RUnlock()
		if !hasToken {
			atomic.AddInt64(&failedCount, 1)
			continue
		}

		wg.Add(1)
		go func(acc *auth.Account) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			resp, err := proxy.ExecuteRequest(context.Background(), acc, payload, "", "", "", nil, nil)
			if err != nil {
				atomic.AddInt64(&failedCount, 1)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)

			switch resp.StatusCode {
			case http.StatusOK:
				usageState := proxy.SyncCodexUsageState(h.store, acc, resp)
				// 测试成功即重置冷却状态，用量限制由调度器自行判断
				if !usageState.Premium5hRateLimited && (!usageState.HasUsage7d || usageState.UsagePct7d < 100) {
					h.store.ClearCooldown(acc)
				}
				atomic.AddInt64(&successCount, 1)
			case http.StatusUnauthorized:
				proxy.SyncCodexUsageState(h.store, acc, resp)
				h.store.MarkCooldown(acc, 24*time.Hour, "unauthorized")
				atomic.AddInt64(&bannedCount, 1)
			case http.StatusTooManyRequests:
				proxy.SyncCodexUsageState(h.store, acc, resp)
				proxy.Apply429Cooldown(h.store, acc, body, resp)
				atomic.AddInt64(&rateLimitCount, 1)
			default:
				atomic.AddInt64(&failedCount, 1)
			}
		}(account)
	}

	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"total":        len(accounts),
		"success":      successCount,
		"failed":       failedCount,
		"banned":       bannedCount,
		"rate_limited": rateLimitCount,
	})
}
