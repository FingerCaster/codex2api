package database

import (
	"context"
	"database/sql"
	"encoding/json"
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

func TestSQLiteAccountsEnabledDefaultsAndCanToggle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	id, err := db.InsertAccount(ctx, "test", "rt", "")
	if err != nil {
		t.Fatalf("InsertAccount 返回错误: %v", err)
	}

	rows, err := db.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive 返回错误: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListActive 返回 %d 条，want 1", len(rows))
	}
	if !rows[0].Enabled {
		t.Fatal("new account Enabled = false, want true")
	}

	if err := db.SetAccountEnabled(ctx, id, false); err != nil {
		t.Fatalf("SetAccountEnabled 返回错误: %v", err)
	}
	rows, err = db.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive 返回错误: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListActive 返回 %d 条，want 1", len(rows))
	}
	if rows[0].Enabled {
		t.Fatal("disabled account Enabled = true, want false")
	}

	if err := db.SetAccountEnabled(ctx, id+1, false); err != sql.ErrNoRows {
		t.Fatalf("SetAccountEnabled missing account error = %v, want sql.ErrNoRows", err)
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

	for _, name := range []string{"api_key_id", "api_key_name", "api_key_masked", "image_count", "image_width", "image_height", "image_bytes", "image_format", "image_size", "effective_model", "account_billed", "user_billed", "is_retry_attempt", "attempt_index", "upstream_error_kind", "error_message"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("usage_logs 缺少列 %q", name)
		}
	}
}

func TestUsageLogModeErrorsSkipsSuccessfulLogs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()
	db.SetUsageLogConfig(UsageLogModeErrors, 10, 5)

	ctx := context.Background()
	if err := db.InsertUsageLog(ctx, &UsageLogInput{
		AccountID:  1,
		Endpoint:   "/v1/responses",
		Model:      "gpt-5.4",
		StatusCode: 200,
	}); err != nil {
		t.Fatalf("InsertUsageLog success 返回错误: %v", err)
	}
	if err := db.InsertUsageLog(ctx, &UsageLogInput{
		AccountID:    1,
		Endpoint:     "/v1/responses",
		Model:        "gpt-5.4",
		StatusCode:   500,
		ErrorMessage: "upstream failed",
	}); err != nil {
		t.Fatalf("InsertUsageLog error 返回错误: %v", err)
	}
	db.flushLogs()

	logs, err := db.ListRecentUsageLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentUsageLogs 返回错误: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	if logs[0].StatusCode != 500 {
		t.Fatalf("StatusCode = %d, want 500", logs[0].StatusCode)
	}
}

func TestUsageErrorSummaryAndFilters(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	for _, usageLog := range []*UsageLogInput{
		{
			AccountID:         1,
			Endpoint:          "/v1/responses",
			InboundEndpoint:   "/v1/responses",
			UpstreamEndpoint:  "/backend-api/codex/responses",
			Model:             "gpt-5.4",
			StatusCode:        500,
			DurationMs:        1200,
			IsRetryAttempt:    true,
			AttemptIndex:      1,
			UpstreamErrorKind: "upstream_timeout",
			ErrorMessage:      "upstream timeout",
		},
		{
			AccountID:         2,
			Endpoint:          "/v1/messages",
			InboundEndpoint:   "/v1/messages",
			Model:             "claude-sonnet-4.5",
			StatusCode:        401,
			DurationMs:        80,
			UpstreamErrorKind: "unauthorized",
			ErrorMessage:      "invalid access token",
		},
		{
			AccountID:    3,
			Endpoint:     "/v1/responses",
			Model:        "gpt-5.4",
			StatusCode:   499,
			DurationMs:   30,
			ErrorMessage: "client canceled",
		},
		{
			AccountID:  4,
			Endpoint:   "/v1/responses",
			Model:      "gpt-5.4",
			StatusCode: 200,
			DurationMs: 90,
		},
	} {
		if err := db.InsertUsageLog(ctx, usageLog); err != nil {
			t.Fatalf("InsertUsageLog 返回错误: %v", err)
		}
	}
	db.flushLogs()

	now := time.Now()
	filter := UsageLogFilter{
		Start:           now.Add(-1 * time.Hour),
		End:             now.Add(1 * time.Hour),
		Page:            1,
		PageSize:        10,
		ErrorOnly:       true,
		IncludeCanceled: true,
	}
	page, err := db.ListUsageLogsByTimeRangePaged(ctx, filter)
	if err != nil {
		t.Fatalf("ListUsageLogsByTimeRangePaged 返回错误: %v", err)
	}
	if page.Total != 3 {
		t.Fatalf("page.Total = %d, want 3", page.Total)
	}

	foundRetry := false
	for _, usageLog := range page.Logs {
		if usageLog.UpstreamErrorKind == "upstream_timeout" {
			foundRetry = true
			if !usageLog.IsRetryAttempt {
				t.Fatal("IsRetryAttempt = false, want true")
			}
			if usageLog.AttemptIndex != 1 {
				t.Fatalf("AttemptIndex = %d, want 1", usageLog.AttemptIndex)
			}
		}
	}
	if !foundRetry {
		t.Fatal("未找到 upstream_timeout 错误日志")
	}

	summary, err := db.GetUsageErrorSummary(ctx, filter)
	if err != nil {
		t.Fatalf("GetUsageErrorSummary 返回错误: %v", err)
	}
	if summary.TotalErrors != 3 {
		t.Fatalf("TotalErrors = %d, want 3", summary.TotalErrors)
	}
	if summary.Status5xx != 1 || summary.Unauthorized != 1 || summary.Canceled != 1 || summary.Timeouts != 1 || summary.RetryAttempts != 1 {
		t.Fatalf("summary = %+v, want one 5xx/401/499/timeout/retry", summary)
	}

	charts, err := db.GetChartAggregation(ctx, filter.Start, filter.End, 5)
	if err != nil {
		t.Fatalf("GetChartAggregation 返回错误: %v", err)
	}
	var chart4xx, chart5xx int64
	for _, point := range charts.Timeline {
		chart4xx += point.Errors4xx
		chart5xx += point.Errors5xx
	}
	if chart4xx != 1 || chart5xx != 1 {
		t.Fatalf("chart errors = 4xx:%d 5xx:%d, want 1/1", chart4xx, chart5xx)
	}

	filter.StatusFamily = "5xx"
	page, err = db.ListUsageLogsByTimeRangePaged(ctx, filter)
	if err != nil {
		t.Fatalf("ListUsageLogsByTimeRangePaged status family 返回错误: %v", err)
	}
	if page.Total != 1 || len(page.Logs) != 1 || page.Logs[0].StatusCode != 500 {
		t.Fatalf("5xx page = total %d len %d first %+v", page.Total, len(page.Logs), page.Logs)
	}
}

func TestUsageLogModeOffSkipsAllLogs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()
	db.SetUsageLogConfig(UsageLogModeOff, 10, 5)

	ctx := context.Background()
	if err := db.InsertUsageLog(ctx, &UsageLogInput{
		AccountID:  1,
		Endpoint:   "/v1/responses",
		Model:      "gpt-5.4",
		StatusCode: 500,
	}); err != nil {
		t.Fatalf("InsertUsageLog 返回错误: %v", err)
	}
	db.flushLogs()

	logs, err := db.ListRecentUsageLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentUsageLogs 返回错误: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("len(logs) = %d, want 0", len(logs))
	}
}

func TestSQLiteModelCooldownPersistence(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	resetAt := time.Now().Add(15 * time.Minute).Truncate(time.Second)
	if err := db.SetModelCooldown(ctx, 42, "gpt-5.4", "model_capacity", resetAt); err != nil {
		t.Fatalf("SetModelCooldown 返回错误: %v", err)
	}

	rows, err := db.ListActiveModelCooldowns(ctx)
	if err != nil {
		t.Fatalf("ListActiveModelCooldowns 返回错误: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListActiveModelCooldowns 返回 %d 条，want 1", len(rows))
	}
	if rows[0].AccountID != 42 || rows[0].Model != "gpt-5.4" || rows[0].Reason != "model_capacity" {
		t.Fatalf("cooldown row = %#v", rows[0])
	}

	if err := db.ClearModelCooldown(ctx, 42, "gpt-5.4"); err != nil {
		t.Fatalf("ClearModelCooldown 返回错误: %v", err)
	}
	rows, err = db.ListActiveModelCooldowns(ctx)
	if err != nil {
		t.Fatalf("ListActiveModelCooldowns 返回错误: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListActiveModelCooldowns 返回 %d 条，want 0", len(rows))
	}
}

func TestAccountRequestCountsSeparateRetryAttempts(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	logs := []*UsageLogInput{
		{AccountID: 7, Endpoint: "/v1/responses", Model: "gpt-5.4", StatusCode: 200},
		{AccountID: 7, Endpoint: "/v1/responses", Model: "gpt-5.4", StatusCode: 429, IsRetryAttempt: true, AttemptIndex: 1, UpstreamErrorKind: "model_capacity"},
		{AccountID: 7, Endpoint: "/v1/responses", Model: "gpt-5.4", StatusCode: 500, IsRetryAttempt: false, AttemptIndex: 2, UpstreamErrorKind: "server"},
	}
	for _, usageLog := range logs {
		if err := db.InsertUsageLog(ctx, usageLog); err != nil {
			t.Fatalf("InsertUsageLog 返回错误: %v", err)
		}
	}
	db.flushLogs()

	counts, err := db.GetAccountRequestCounts(ctx)
	if err != nil {
		t.Fatalf("GetAccountRequestCounts 返回错误: %v", err)
	}
	got := counts[7]
	if got == nil {
		t.Fatal("account 7 counts missing")
	}
	if got.SuccessCount != 1 || got.ErrorCount != 1 || got.RetryErrorCount != 1 || got.RateLimitAttemptCount != 1 {
		t.Fatalf("counts = %#v, want success=1 error=1 retry=1 rateLimit=1", got)
	}
}

func TestSQLiteUsageStatsBaselineHasBillingColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	columns, err := db.sqliteTableColumns(context.Background(), "usage_stats_baseline")
	if err != nil {
		t.Fatalf("sqliteTableColumns 返回错误: %v", err)
	}

	for _, name := range []string{"account_billed", "user_billed"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("usage_stats_baseline 缺少列 %q", name)
		}
	}
}

func TestUsageLogsPersistEffectiveModel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.InsertUsageLog(ctx, &UsageLogInput{
		AccountID:        1,
		Endpoint:         "/v1/messages",
		InboundEndpoint:  "/v1/messages",
		UpstreamEndpoint: "/v1/responses",
		Model:            "claude-haiku-4-5-20251001",
		EffectiveModel:   "gpt-5.4",
		StatusCode:       200,
		ReasoningEffort:  "high",
	}); err != nil {
		t.Fatalf("InsertUsageLog 返回错误: %v", err)
	}
	db.flushLogs()

	logs, err := db.ListRecentUsageLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentUsageLogs 返回错误: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	if logs[0].Model != "claude-haiku-4-5-20251001" {
		t.Fatalf("Model = %q, want claude-haiku-4-5-20251001", logs[0].Model)
	}
	if logs[0].EffectiveModel != "gpt-5.4" {
		t.Fatalf("EffectiveModel = %q, want gpt-5.4", logs[0].EffectiveModel)
	}
	if logs[0].ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort = %q, want high", logs[0].ReasoningEffort)
	}
}

func TestUsageLogsPersistImageMetadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.InsertUsageLog(ctx, &UsageLogInput{
		AccountID:        1,
		Endpoint:         "/v1/images/generations",
		InboundEndpoint:  "/v1/images/generations",
		UpstreamEndpoint: "/v1/responses",
		Model:            "gpt-image-2-4k",
		StatusCode:       200,
		DurationMs:       1200,
		ImageCount:       1,
		ImageWidth:       3840,
		ImageHeight:      2160,
		ImageBytes:       2457600,
		ImageFormat:      "png",
		ImageSize:        "3840x2160",
	}); err != nil {
		t.Fatalf("InsertUsageLog 返回错误: %v", err)
	}
	db.flushLogs()

	logs, err := db.ListRecentUsageLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentUsageLogs 返回错误: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	got := logs[0]
	if got.ImageCount != 1 || got.ImageWidth != 3840 || got.ImageHeight != 2160 || got.ImageBytes != 2457600 || got.ImageFormat != "png" || got.ImageSize != "3840x2160" {
		t.Fatalf("image metadata = %#v", got)
	}
}

func TestUsageLogsReturnBillingFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.InsertUsageLog(ctx, &UsageLogInput{
		AccountID:        1,
		Endpoint:         "/v1/responses",
		InboundEndpoint:  "/v1/responses",
		UpstreamEndpoint: "/v1/responses",
		Model:            "gpt-5.5",
		StatusCode:       200,
		InputTokens:      476,
		OutputTokens:     252,
		TotalTokens:      728,
		ServiceTier:      "default",
	}); err != nil {
		t.Fatalf("InsertUsageLog 返回错误: %v", err)
	}
	db.flushLogs()

	logs, err := db.ListRecentUsageLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentUsageLogs 返回错误: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}

	got := logs[0]
	want := calculateCost(476, 252, 0, "gpt-5.5", "default")
	if got.AccountBilled != want || got.UserBilled != want {
		t.Fatalf("billing = account %.12f user %.12f, want %.12f", got.AccountBilled, got.UserBilled, want)
	}
	if got.InputCost <= 0 || got.OutputCost <= 0 || got.TotalCost != want {
		t.Fatalf("billing breakdown = input %.12f output %.12f total %.12f, want total %.12f", got.InputCost, got.OutputCost, got.TotalCost, want)
	}
}

func TestUsageLogsReturnErrorMessage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.InsertUsageLog(ctx, &UsageLogInput{
		AccountID:    1,
		Endpoint:     "/v1/responses",
		Model:        "gpt-5.4",
		StatusCode:   429,
		ErrorMessage: "rate_limit_exceeded · Too many requests",
	}); err != nil {
		t.Fatalf("InsertUsageLog 返回错误: %v", err)
	}
	db.flushLogs()

	logs, err := db.ListRecentUsageLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentUsageLogs 返回错误: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	if got := logs[0].ErrorMessage; got != "rate_limit_exceeded · Too many requests" {
		t.Fatalf("ErrorMessage = %q", got)
	}
}

func TestUsageStatsIncludeBillingTotals(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	for _, usageLog := range []*UsageLogInput{
		{
			AccountID:    1,
			Endpoint:     "/v1/responses",
			Model:        "gpt-5.5",
			StatusCode:   200,
			InputTokens:  1000,
			OutputTokens: 500,
			TotalTokens:  1500,
		},
		{
			AccountID:    1,
			Endpoint:     "/v1/responses",
			Model:        "gpt-5.5",
			StatusCode:   499,
			InputTokens:  1000,
			OutputTokens: 500,
			TotalTokens:  1500,
		},
	} {
		if err := db.InsertUsageLog(ctx, usageLog); err != nil {
			t.Fatalf("InsertUsageLog 返回错误: %v", err)
		}
	}
	db.flushLogs()

	stats, err := db.GetUsageStats(ctx)
	if err != nil {
		t.Fatalf("GetUsageStats 返回错误: %v", err)
	}

	want := calculateCost(1000, 500, 0, "gpt-5.5", "")
	if stats.TotalAccountBilled != want || stats.TotalUserBilled != want {
		t.Fatalf("total billing = account %.12f user %.12f, want %.12f", stats.TotalAccountBilled, stats.TotalUserBilled, want)
	}
	if stats.TodayAccountBilled != want || stats.TodayUserBilled != want {
		t.Fatalf("today billing = account %.12f user %.12f, want %.12f", stats.TodayAccountBilled, stats.TodayUserBilled, want)
	}
}

func TestSoftDeleteAccountMarksDeletedStatus(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	id, err := db.InsertAccount(ctx, "delete-me", "rt-delete-me", "")
	if err != nil {
		t.Fatalf("InsertAccount 返回错误: %v", err)
	}
	if err := db.SoftDeleteAccount(ctx, id); err != nil {
		t.Fatalf("SoftDeleteAccount 返回错误: %v", err)
	}

	active, err := db.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive 返回错误: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("ListActive 返回 %d 条，want 0", len(active))
	}
	if _, err := db.GetAccountByID(ctx, id); err == nil {
		t.Fatal("GetAccountByID 应该排除已删除账号")
	}

	var status string
	var errorMessage string
	var deletedAt sql.NullString
	if err := db.conn.QueryRowContext(ctx, `SELECT status, error_message, deleted_at FROM accounts WHERE id = $1`, id).Scan(&status, &errorMessage, &deletedAt); err != nil {
		t.Fatalf("查询账号状态返回错误: %v", err)
	}
	if status != "deleted" {
		t.Fatalf("status = %q, want deleted", status)
	}
	if errorMessage != "" {
		t.Fatalf("error_message = %q, want empty", errorMessage)
	}
	if !deletedAt.Valid || deletedAt.String == "" {
		t.Fatal("deleted_at 未写入")
	}
}

func TestSQLiteMigratesLegacyDeletedAccounts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")
	ctx := context.Background()

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	id, err := db.InsertAccount(ctx, "legacy-delete", "rt-legacy-delete", "")
	if err != nil {
		t.Fatalf("InsertAccount 返回错误: %v", err)
	}
	if err := db.SetError(ctx, id, "deleted"); err != nil {
		t.Fatalf("SetError 返回错误: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close 返回错误: %v", err)
	}

	db, err = New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	var status string
	var errorMessage string
	var deletedAt sql.NullString
	if err := db.conn.QueryRowContext(ctx, `SELECT status, error_message, deleted_at FROM accounts WHERE id = $1`, id).Scan(&status, &errorMessage, &deletedAt); err != nil {
		t.Fatalf("查询迁移后账号返回错误: %v", err)
	}
	if status != "deleted" {
		t.Fatalf("status = %q, want deleted", status)
	}
	if errorMessage != "" {
		t.Fatalf("error_message = %q, want empty", errorMessage)
	}
	if !deletedAt.Valid || deletedAt.String == "" {
		t.Fatal("deleted_at 未迁移")
	}
}

func TestListActiveIncludesErrorAccounts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	id, err := db.InsertAccount(ctx, "error-account", "rt-error", "")
	if err != nil {
		t.Fatalf("InsertAccount 返回错误: %v", err)
	}
	if err := db.SetError(ctx, id, "batch test failed"); err != nil {
		t.Fatalf("SetError 返回错误: %v", err)
	}

	rows, err := db.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive 返回错误: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListActive 返回 %d 条，want 1", len(rows))
	}
	if rows[0].Status != "error" {
		t.Fatalf("status = %q, want error", rows[0].Status)
	}
	if rows[0].ErrorMessage != "batch test failed" {
		t.Fatalf("error_message = %q, want batch test failed", rows[0].ErrorMessage)
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

	accountCredentials := []string{
		`{"email":"plus@example.com","plan_type":"plus"}`,
		`{"email":"free@example.com","plan_type":"free"}`,
	}
	for idx, credentials := range accountCredentials {
		if _, err := db.conn.ExecContext(ctx, `INSERT INTO accounts (id, name, credentials, proxy_url) VALUES ($1, $2, $3, '')`, idx+1, "test", credentials); err != nil {
			t.Fatalf("插入测试账号返回错误: %v", err)
		}
	}

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
			if usageLog.AccountPlanType != "plus" {
				t.Fatalf("AccountPlanType = %q, want %q", usageLog.AccountPlanType, "plus")
			}
			if usageLog.AccountEmail != "plus@example.com" {
				t.Fatalf("AccountEmail = %q, want %q", usageLog.AccountEmail, "plus@example.com")
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
		if usageLog.AccountPlanType != "plus" {
			t.Fatalf("AccountPlanType = %q, want %q", usageLog.AccountPlanType, "plus")
		}
	}
}

func TestUsageLogsMarshalAccountPlanType(t *testing.T) {
	logEntry := &UsageLog{
		ID:              1,
		AccountEmail:    "plus@example.com",
		AccountPlanType: "plus",
	}

	data, err := json.Marshal(logEntry)
	if err != nil {
		t.Fatalf("json.Marshal 返回错误: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal 返回错误: %v", err)
	}
	if got := decoded["account_plan_type"]; got != "plus" {
		t.Fatalf("account_plan_type = %v, want %q", got, "plus")
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

func TestSQLiteAPIKeysEnabledToggle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	firstID, err := db.InsertAPIKey(ctx, "alpha", "sk-alpha-12345678901234567890")
	if err != nil {
		t.Fatalf("InsertAPIKey(alpha) 返回错误: %v", err)
	}
	_, err = db.InsertAPIKey(ctx, "beta", "sk-beta-12345678901234567890")
	if err != nil {
		t.Fatalf("InsertAPIKey(beta) 返回错误: %v", err)
	}

	keys, err := db.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys 返回错误: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want %d", len(keys), 2)
	}
	for _, key := range keys {
		if !key.Enabled {
			t.Fatalf("新建 API key 默认应为启用状态: %+v", key)
		}
	}

	values, err := db.GetAllAPIKeyValues(ctx)
	if err != nil {
		t.Fatalf("GetAllAPIKeyValues 返回错误: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("len(values) = %d, want %d", len(values), 2)
	}

	if err := db.UpdateAPIKeyEnabled(ctx, firstID, false); err != nil {
		t.Fatalf("UpdateAPIKeyEnabled(false) 返回错误: %v", err)
	}

	keys, err = db.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("禁用后 ListAPIKeys 返回错误: %v", err)
	}
	if !keys[1].Enabled {
		t.Fatalf("未禁用的 key 不应受影响: %+v", keys[1])
	}
	if keys[0].Enabled {
		t.Fatalf("禁用后的 key 仍显示为 enabled=true: %+v", keys[0])
	}

	values, err = db.GetAllAPIKeyValues(ctx)
	if err != nil {
		t.Fatalf("禁用后 GetAllAPIKeyValues 返回错误: %v", err)
	}
	if len(values) != 1 || values[0] != "sk-beta-12345678901234567890" {
		t.Fatalf("禁用后鉴权 key 过滤异常: values=%v", values)
	}
}

func TestSQLiteUsageLogsTimeRangeUsesUTCStorage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "codex2api.db")

	db, err := New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("New(sqlite) 返回错误: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	createdUTC := time.Date(2026, 4, 23, 20, 6, 0, 0, time.UTC)
	if _, err := db.conn.ExecContext(ctx, `
		INSERT INTO usage_logs (
			account_id, endpoint, inbound_endpoint, upstream_endpoint, model,
			status_code, total_tokens, input_tokens, output_tokens, created_at
		)
		VALUES (1, '/v1/images/generations', '/v1/images/generations', '/v1/responses', 'gpt-image-2',
			200, 1790, 34, 1756, $1)
	`, sqliteTimeParam(createdUTC)); err != nil {
		t.Fatalf("insert usage log 返回错误: %v", err)
	}

	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	localCreated := createdUTC.In(shanghai)
	page, err := db.ListUsageLogsByTimeRangePaged(ctx, UsageLogFilter{
		Start:    localCreated.Add(-1 * time.Hour),
		End:      localCreated.Add(1 * time.Hour),
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListUsageLogsByTimeRangePaged 返回错误: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("page.Total = %d, want %d", page.Total, 1)
	}
	if len(page.Logs) != 1 {
		t.Fatalf("len(page.Logs) = %d, want %d", len(page.Logs), 1)
	}
	if got := page.Logs[0].InboundEndpoint; got != "/v1/images/generations" {
		t.Fatalf("InboundEndpoint = %q, want /v1/images/generations", got)
	}
	if got := page.Logs[0].Model; got != "gpt-image-2" {
		t.Fatalf("Model = %q, want gpt-image-2", got)
	}
}
