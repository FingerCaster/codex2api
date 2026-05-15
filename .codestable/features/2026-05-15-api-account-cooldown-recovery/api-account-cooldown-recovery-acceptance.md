# API Account Cooldown Recovery 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-05-15
> 关联方案 doc：`.codestable/features/2026-05-15-api-account-cooldown-recovery/api-account-cooldown-recovery-design.md`

## 1. 接口契约核对

- [x] 系统设置接口新增 `api_account_circuit_breaker_enabled`、`api_account_failure_rate_threshold`、`api_account_failure_min_samples`、`api_account_cooldown_minutes`、`api_account_recovery_probe_interval_minutes`、`api_account_recovery_probe_successes`、`api_account_recovery_direct_healthy`、`api_account_recovery_guard_minutes`：`database.SystemSettings`、admin settings DTO、默认 settings、前端 `SystemSettings` 均已接入。
- [x] 名词层变化落地：`auth.Account` 新增 `RecoveryProbeSuccesses` 和 `RecoveryGuardUntil`；`auth.Store` 新增 API 账号熔断/恢复配置读写方法。
- [x] 流程图落点核对：失败记录在 `ReportRequestFailure`；账号级冷却在 `MarkCooldown`；模型级 429 在 `Apply429Cooldown` / `MarkModelCooldown`；恢复探测在 `NeedsRecoveryProbe`、`ProbeUsageSnapshot`、`MarkRecoveryProbeSuccess`、`RecoverAccountFromProbe`。

## 2. 行为与决策核对

- [x] 401/明确额度/订阅问题触发 API 账号短冷却：`applyCooldownForModel` 与 API recovery probe 分支均使用 API 账号冷却时长。
- [x] 429 分流未改变：`Apply429Cooldown` 仍按决策写账号级或模型级 cooldown。
- [x] 5xx/timeout/transport 不单次判死：`ReportRequestFailure` 只在最近失败率满足阈值时标记 `api_account_failure_rate`。
- [x] 冷却账号不参与正常调度：`Account.IsAvailable` 与 FastScheduler snapshot 均继续排除 active cooldown。
- [x] 恢复探测成功后清理 cooldown、失败计数和 runtime cache：`RecoverAccountFromProbe` 统一处理。
- [x] 恢复保护窗口落地：`recoveryGuardedConcurrencyLimit` 同时用于普通调度快照和 FastScheduler 快照。
- [x] 挂载点反向核对：本 feature 引用集中在 `auth.Store`、admin settings/probe、database settings、proxy 错误冷却、前端 settings；与 design 第 2.3 节一致。

## 3. 验收场景核对

- [x] S1 冷却账号不进调度池：`TestAPIAccountServerFailuresCooldownOnlyAfterThreshold` 覆盖普通调度，`TestFastSchedulerHonorsAPIAccountRecoveryGuard` 覆盖 FastScheduler。
- [x] S2 少量 server 失败只降权不 cooldown：`TestAPIAccountServerFailuresCooldownOnlyAfterThreshold` 前 4 次失败断言未冷却，`TestProbeUsageSnapshotOpenAIResponsesServerFailureDoesNotImmediateCooldown` 覆盖 probe 5xx。
- [x] S3 高失败率触发短冷却：`TestAPIAccountServerFailuresCooldownOnlyAfterThreshold` 断言 reason 为 `api_account_failure_rate`。
- [x] S4 429 模型级分流不回退：既有 `proxy` 429 tests 继续通过。
- [x] S5 API 账号 probe 成功后由恢复路径清理 cooldown：`TestProbeUsageSnapshotOpenAIResponsesSuccessLeavesRecoveryToCaller` 与 `TestAPIAccountRecoverFromProbeRestoresHealthyWithGuard` 覆盖。
- [x] S6 恢复后 healthy + 并发保护：`TestAPIAccountRecoverFromProbeRestoresHealthyWithGuard` 覆盖普通调度，`TestFastSchedulerHonorsAPIAccountRecoveryGuard` 覆盖 FastScheduler。
- [x] S7 设置可读写：`TestSQLiteSystemSettingsPersistsSessionAffinityTTL` 已扩展 API 账号设置字段。
- [x] 前端验证：`npm run typecheck` 与 `npm run build` 通过。

## 4. 术语一致性

- API 账号、账号级冷却、模型级冷却、恢复探测、恢复保护窗口均使用 design 第 0 节术语。
- 未新增 design 外独立状态机；兼容旧 `type=api_key` 泛用上游时仍归入 API 账号执行语义。

## 5. 架构归并

- [x] `.codestable/architecture/ARCHITECTURE.md` 已补充核心概念、模块索引、关键架构决定和硬边界。

## 6. requirement 回写

- [x] 本 feature 未从独立 requirement 文档起头；需求边界已在 feature design 与 architecture 中落档，无单独 requirement 回写。

## 7. roadmap 回写

- [x] 非 roadmap 起头；frontmatter 中 `roadmap` / `roadmap_item` 为空，无 roadmap 回写。

## 8. attention.md 候选盘点

- [x] 候选：本机 PowerShell PATH 中没有 `go`，本次 Go 格式化/测试通过 Docker `golang:1.26.3-alpine` 执行。是否写入 `attention.md` 可由用户后续决定。

## 9. 遗留

- 后续优化点：`auth/store.go`、`proxy/handler.go` 仍偏大，后续可单独走 `cs-refactor` 拆分 scheduler/cooldown/probe 与 HTTP 错误分类。
- 已知限制：恢复探测是轻量请求，仍依赖上游能接受当前全局测试模型；空模型列表按“不限制模型”处理。
- 验证命令：
  - `docker run --rm -v ${PWD}:/app -v codex2api-gomod:/go/pkg/mod -v codex2api-gocache:/root/.cache/go-build -w /app golang:1.26.3-alpine go test ./...`
  - `npm run typecheck`
  - `npm run build`
