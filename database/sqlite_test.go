package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNewSQLiteInitializesFreshDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	if got := db.Driver(); got != "sqlite" {
		t.Fatalf("Driver() = %q, want %q", got, "sqlite")
	}
}

func TestSQLiteUsageLogsHasAPIKeyColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	columns, err := db.sqliteTableColumns(context.Background(), "usage_logs")
	if err != nil {
		t.Fatalf("sqliteTableColumns 返回错误: %v", err)
	}

	for _, name := range []string{"api_key_id", "api_key_name", "api_key_masked"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("usage_logs 缺少列 %q", name)
		}
	}
}

func TestUsageLogsFilterByAPIKeyID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	targetAPIKeyID := int64(7)

	logs := []*UsageLogInput{
		{
			AccountID:    1,
			Endpoint:     "/v1/chat/completions",
			Model:        "gpt-5.4",
			StatusCode:   200,
			DurationMs:   120,
			APIKeyID:     targetAPIKeyID,
			APIKeyName:   "Team A",
			APIKeyMasked: "sk-a****...****1111",
		},
		{
			AccountID:    1,
			Endpoint:     "/v1/responses",
			Model:        "gpt-5.4",
			StatusCode:   200,
			DurationMs:   220,
			APIKeyID:     targetAPIKeyID,
			APIKeyName:   "Team A",
			APIKeyMasked: "sk-a****...****1111",
		},
		{
			AccountID:    2,
			Endpoint:     "/v1/responses",
			Model:        "gpt-5.4-mini",
			StatusCode:   200,
			DurationMs:   320,
			APIKeyID:     8,
			APIKeyName:   "Team B",
			APIKeyMasked: "sk-b****...****2222",
		},
	}

	for _, usageLog := range logs {
		if err := db.InsertUsageLog(ctx, usageLog); err != nil {
			t.Fatalf("InsertUsageLog 返回错误: %v", err)
		}
	}
	db.flushLogs()

	recentLogs, err := db.ListRecentUsageLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentUsageLogs 返回错误: %v", err)
	}
	if len(recentLogs) != len(logs) {
		t.Fatalf("recentLogs 长度 = %d, want %d", len(recentLogs), len(logs))
	}

	foundSnapshot := false
	for _, usageLog := range recentLogs {
		if usageLog.APIKeyID == targetAPIKeyID {
			foundSnapshot = true
			if usageLog.APIKeyName != "Team A" {
				t.Fatalf("APIKeyName = %q, want %q", usageLog.APIKeyName, "Team A")
			}
			if usageLog.APIKeyMasked != "sk-a****...****1111" {
				t.Fatalf("APIKeyMasked = %q, want %q", usageLog.APIKeyMasked, "sk-a****...****1111")
			}
		}
	}
	if !foundSnapshot {
		t.Fatal("未找到带 API 密钥快照的最近日志")
	}

	page, err := db.ListUsageLogsByTimeRangePaged(ctx, UsageLogFilter{
		Start:    now.Add(-1 * time.Hour),
		End:      now.Add(1 * time.Hour),
		Page:     1,
		PageSize: 10,
		APIKeyID: &targetAPIKeyID,
	})
	if err != nil {
		t.Fatalf("ListUsageLogsByTimeRangePaged 返回错误: %v", err)
	}

	if page.Total != 2 {
		t.Fatalf("page.Total = %d, want %d", page.Total, 2)
	}
	if len(page.Logs) != 2 {
		t.Fatalf("len(page.Logs) = %d, want %d", len(page.Logs), 2)
	}
	for _, usageLog := range page.Logs {
		if usageLog.APIKeyID != targetAPIKeyID {
			t.Fatalf("APIKeyID = %d, want %d", usageLog.APIKeyID, targetAPIKeyID)
		}
		if usageLog.APIKeyName != "Team A" {
			t.Fatalf("APIKeyName = %q, want %q", usageLog.APIKeyName, "Team A")
		}
	}
}

func TestGetUsageStatsByAPIKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	targetAPIKeyID := int64(7)

	logs := []*UsageLogInput{
		{
			AccountID:        1,
			Endpoint:         "/v1/responses",
			InboundEndpoint:  "/v1/responses",
			Model:            "gpt-5.4",
			StatusCode:       200,
			DurationMs:       100,
			PromptTokens:     40,
			CompletionTokens: 60,
			TotalTokens:      100,
			CachedTokens:     10,
			APIKeyID:         targetAPIKeyID,
			APIKeyName:       "Team A",
		},
		{
			AccountID:        1,
			Endpoint:         "/v1/responses",
			Model:            "gpt-5.4-mini",
			StatusCode:       401,
			DurationMs:       200,
			PromptTokens:     4,
			CompletionTokens: 6,
			TotalTokens:      10,
			APIKeyID:         targetAPIKeyID,
			APIKeyName:       "Team A",
		},
		{
			AccountID:        2,
			Endpoint:         "/v1/chat/completions",
			Model:            "gpt-5.4",
			StatusCode:       200,
			DurationMs:       300,
			PromptTokens:     80,
			CompletionTokens: 120,
			TotalTokens:      200,
			CachedTokens:     20,
			APIKeyID:         8,
			APIKeyName:       "Team B",
		},
	}

	for _, usageLog := range logs {
		if err := db.InsertUsageLog(ctx, usageLog); err != nil {
			t.Fatalf("InsertUsageLog 返回错误: %v", err)
		}
	}
	db.flushLogs()

	stats, err := db.GetUsageStatsByAPIKey(ctx, &targetAPIKeyID)
	if err != nil {
		t.Fatalf("GetUsageStatsByAPIKey 返回错误: %v", err)
	}

	if stats.TotalRequests != 2 {
		t.Fatalf("TotalRequests = %d, want %d", stats.TotalRequests, 2)
	}
	if stats.TotalTokens != 110 {
		t.Fatalf("TotalTokens = %d, want %d", stats.TotalTokens, 110)
	}
	if stats.TotalPrompt != 44 {
		t.Fatalf("TotalPrompt = %d, want %d", stats.TotalPrompt, 44)
	}
	if stats.TotalCompletion != 66 {
		t.Fatalf("TotalCompletion = %d, want %d", stats.TotalCompletion, 66)
	}
	if stats.TotalCachedTokens != 10 {
		t.Fatalf("TotalCachedTokens = %d, want %d", stats.TotalCachedTokens, 10)
	}
	if stats.TodayRequests != 2 {
		t.Fatalf("TodayRequests = %d, want %d", stats.TodayRequests, 2)
	}
	if stats.TodayTokens != 110 {
		t.Fatalf("TodayTokens = %d, want %d", stats.TodayTokens, 110)
	}
	if stats.RPM != 2 {
		t.Fatalf("RPM = %v, want %v", stats.RPM, float64(2))
	}
	if stats.TPM != 110 {
		t.Fatalf("TPM = %v, want %v", stats.TPM, float64(110))
	}
	if stats.ErrorRate != 50 {
		t.Fatalf("ErrorRate = %v, want %v", stats.ErrorRate, float64(50))
	}

	if err := db.ClearUsageLogs(ctx); err != nil {
		t.Fatalf("ClearUsageLogs 返回错误: %v", err)
	}

	filteredAfterClear, err := db.GetUsageStatsByAPIKey(ctx, &targetAPIKeyID)
	if err != nil {
		t.Fatalf("clear 后 GetUsageStatsByAPIKey 返回错误: %v", err)
	}
	if filteredAfterClear.TotalRequests != 0 || filteredAfterClear.TotalTokens != 0 {
		t.Fatalf("clear 后按 key 统计应归零: requests=%d tokens=%d", filteredAfterClear.TotalRequests, filteredAfterClear.TotalTokens)
	}

	overallAfterClear, err := db.GetUsageStats(ctx)
	if err != nil {
		t.Fatalf("clear 后 GetUsageStats 返回错误: %v", err)
	}
	if overallAfterClear.TotalRequests != 3 {
		t.Fatalf("clear 后 TotalRequests = %d, want %d", overallAfterClear.TotalRequests, 3)
	}
	if overallAfterClear.TotalTokens != 310 {
		t.Fatalf("clear 后 TotalTokens = %d, want %d", overallAfterClear.TotalTokens, 310)
	}
}

func TestGetUsageStatsByFilter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	targetAPIKeyID := int64(7)

	logs := []*UsageLogInput{
		{
			AccountID:        1,
			Endpoint:         "/v1/responses",
			InboundEndpoint:  "/v1/responses",
			Model:            "gpt-5.4",
			StatusCode:       200,
			DurationMs:       120,
			PromptTokens:     50,
			CompletionTokens: 30,
			TotalTokens:      80,
			APIKeyID:         targetAPIKeyID,
			APIKeyName:       "Team A",
		},
		{
			AccountID:        1,
			Endpoint:         "/v1/responses",
			InboundEndpoint:  "/v1/responses",
			Model:            "gpt-5.4-mini",
			StatusCode:       500,
			DurationMs:       240,
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			APIKeyID:         targetAPIKeyID,
			APIKeyName:       "Team A",
		},
		{
			AccountID:        2,
			Endpoint:         "/v1/chat/completions",
			InboundEndpoint:  "/v1/chat/completions",
			Model:            "gpt-5.4-mini",
			StatusCode:       200,
			DurationMs:       360,
			PromptTokens:     90,
			CompletionTokens: 60,
			TotalTokens:      150,
			APIKeyID:         8,
			APIKeyName:       "Team B",
		},
	}

	for _, usageLog := range logs {
		if err := db.InsertUsageLog(ctx, usageLog); err != nil {
			t.Fatalf("InsertUsageLog 返回错误: %v", err)
		}
	}
	db.flushLogs()

	stats, err := db.GetUsageStatsByFilter(ctx, UsageLogFilter{
		Model:    "gpt-5.4",
		Endpoint: "/v1/responses",
		APIKeyID: &targetAPIKeyID,
	})
	if err != nil {
		t.Fatalf("GetUsageStatsByFilter 返回错误: %v", err)
	}

	if stats.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want %d", stats.TotalRequests, 1)
	}
	if stats.TotalTokens != 80 {
		t.Fatalf("TotalTokens = %d, want %d", stats.TotalTokens, 80)
	}
	if stats.TotalPrompt != 50 {
		t.Fatalf("TotalPrompt = %d, want %d", stats.TotalPrompt, 50)
	}
	if stats.TotalCompletion != 30 {
		t.Fatalf("TotalCompletion = %d, want %d", stats.TotalCompletion, 30)
	}
	if stats.ErrorRate != 0 {
		t.Fatalf("ErrorRate = %v, want %v", stats.ErrorRate, float64(0))
	}
	if stats.AvgDurationMs != 120 {
		t.Fatalf("AvgDurationMs = %v, want %v", stats.AvgDurationMs, float64(120))
	}
	if stats.RPM != 0 || stats.TPM != 0 {
		t.Fatalf("RPM/TPM = %v/%v, want 0/0 without explicit time range", stats.RPM, stats.TPM)
	}
}

func TestSQLiteTimeRangeQueriesHandleOffsetWindows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.InsertUsageLog(ctx, &UsageLogInput{
		AccountID:       1,
		Endpoint:        "/v1/responses",
		Model:           "gpt-5.4",
		StatusCode:      200,
		DurationMs:      1234,
		InputTokens:     100,
		OutputTokens:    20,
		TotalTokens:     120,
		CachedTokens:    80,
		ServiceTier:     "default",
		ReasoningEffort: "high",
	}); err != nil {
		t.Fatalf("InsertUsageLog 返回错误: %v", err)
	}
	db.flushLogs()

	var latestRaw interface{}
	if err := db.conn.QueryRowContext(ctx, `SELECT created_at FROM usage_logs ORDER BY id DESC LIMIT 1`).Scan(&latestRaw); err != nil {
		t.Fatalf("查询最新 created_at 返回错误: %v", err)
	}
	latestUTC, err := parseDBTimeValue(latestRaw)
	if err != nil {
		t.Fatalf("parseDBTimeValue 返回错误: %v", err)
	}

	localZone := time.FixedZone("CST", 8*60*60)
	start := latestUTC.In(localZone).Add(-30 * time.Minute)
	end := latestUTC.In(localZone).Add(30 * time.Minute)

	page, err := db.ListUsageLogsByTimeRangePaged(ctx, UsageLogFilter{
		Start:    start,
		End:      end,
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListUsageLogsByTimeRangePaged 返回错误: %v", err)
	}
	if page.Total != 1 || len(page.Logs) != 1 {
		t.Fatalf("offset 窗口日志查询结果异常: total=%d len=%d, want 1/1", page.Total, len(page.Logs))
	}

	agg, err := db.GetChartAggregation(ctx, start, end, 5)
	if err != nil {
		t.Fatalf("GetChartAggregation 返回错误: %v", err)
	}
	if len(agg.Timeline) != 1 {
		t.Fatalf("offset 窗口图表聚合结果异常: len=%d, want 1", len(agg.Timeline))
	}
}
