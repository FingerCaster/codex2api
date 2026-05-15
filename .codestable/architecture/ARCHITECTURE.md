# codex2api 架构总入口

> 状态：骨架（待填充）
> 创建日期：2026-05-15

## 1. 项目简介

codex2api 是一个 Go 后端 + 前端管理台项目，用于管理上游账号、API Key、调度、代理、用量记录和兼容接口。

## 2. 核心概念 / 术语表

- **账号运行态**：运行时账号由 `auth.Account` 表示，调度状态集中在 `Status`、`HealthTier`、`SchedulerScore`、`DispatchScore`、`DynamicConcurrencyLimit`、`CooldownUtil` 和 `CooldownReason` 上。
- **API 账号**：OpenAI-compatible / Responses API 上游账号，凭 `BaseURL + APIKey` 调度；`upstream_type=openai_responses` 与兼容的 `type=api_key` 都复用同一套 API 账号执行路径。
- **账号级冷却**：账号进入 `StatusCooldown` 后，在冷却结束前不进入正常请求池；原因保存在 `cooldown_reason`，包括 `unauthorized`、`quota_unavailable`、`subscription_unavailable`、`api_account_failure_rate` 等。
- **模型级冷却**：账号仅对某个模型短暂不可用，存储在 `account_model_cooldowns`，调度时通过模型过滤器排除，不改变账号整体运行态。
- **恢复探测**：后台轻量请求，绕过正常调度池，用于确认冷却账号是否可恢复。
- **恢复保护窗口**：API 账号探测恢复后短时间内把动态并发限制为 1，避免刚恢复即吃满流量。

## 3. 子系统 / 模块索引

- `auth.Store`：账号池、调度分、health tier、账号级/模型级 cooldown、恢复探测状态和 FastScheduler 的内存协调层。
- `admin.Handler`：系统设置读写、批量测试、连接测试和后台用量/恢复探测入口。
- `proxy.Handler`：请求转发与上游错误分类入口，负责把 HTTP 状态码和错误体映射到账号级或模型级冷却。
- `database.DB`：`system_settings`、`accounts.cooldown_*`、`account_model_cooldowns` 和使用日志等持久化。
- `frontend/src/pages/Settings.tsx`：系统设置 UI，暴露 API 账号熔断、冷却和恢复探测参数。

## 4. 关键架构决定

- 账号健康不新增第二套状态机；API 账号短冷却直接复用 `Account.Status/HealthTier/CooldownUtil/CooldownReason`，调度器和 FastScheduler 都从同一份运行态读取。
- 429 的分流保持两层：明确账号额度/窗口问题进入账号级冷却，未知模型拥塞优先写入 `account_model_cooldowns`。
- 普通 `5xx/timeout/transport` 失败先通过 `ReportRequestFailure` 降低调度分和健康层级，只有最近滑动窗口失败率达到配置阈值才进入 `api_account_failure_rate` 短冷却。
- API 账号恢复探测使用 Responses API 轻量测试 payload；成功后通过 Store 统一恢复，清除 cooldown、失败计数和缓存，并按配置恢复到 healthy 或 warm。
- 恢复后的低并发保护是调度层约束，通过 `RecoveryGuardUntil` 影响动态并发上限，不作为独立账号状态展示。

## 5. 已知约束 / 硬边界

- API 账号短冷却默认是谨慎、短周期策略：失败率阈值 80%，最少样本 20，冷却 2 分钟，探测间隔 1 分钟，探测成功 1 次恢复，恢复后保护 1 分钟。
- API 账号 cooldown 期间不参与正常调度，但仍允许后台 recovery probe 按账号专属间隔探测。
- OAuth / RT 账号的 banned 恢复探测仍要求 refresh token，且遵守原有冷却结束前不探测的约束。
- `account_model_cooldowns` 表和模型级冷却主键保持不变；新增机制不能把模型级 429 全部升级成账号级问题。
