---
doc_type: feature-ff-note
feature: disable-messages-endpoint
date: 2026-05-18
requirement:
tags: [config, proxy, messages]
---

## 做了什么
新增 `CODEX_DISABLE_V1_MESSAGES` 环境变量与系统设置页开关，允许部署方禁用 Anthropic 兼容的 `/v1/messages` 与 `/messages` 转发；默认值保持不禁用，兼容现有部署。

## 改了哪些
- `config/config.go` — 读取 `CODEX_DISABLE_V1_MESSAGES` 到核心配置。
- `database/*`、`auth/store.go`、`admin/handler.go` — 持久化并暴露 `disable_v1_messages` 运行时设置。
- `proxy/handler.go`、`proxy/handler_anthropic.go` — 根据运行时设置阻断 messages 转发。
- `frontend/src/pages/Settings.tsx`、`frontend/src/types.ts`、`frontend/src/locales/*` — 在系统设置页增加开关。
- `main.go` — 禁用时不在启动 banner 中打印 `/v1/messages`。
- `README.md`、`README.zh-CN.md`、`docs/CONFIGURATION.md`、`.env.example`、`.env.sqlite.example` — 补充新环境变量说明。
- `config/config_test.go`、`database/sqlite_test.go`、`proxy/handler_test.go` — 覆盖配置解析、持久化读取、默认注册和禁用后本地拒绝转发。

## 怎么验证的
已通过 Docker Go 环境执行 `gofmt`，并运行 `go test ./database ./auth ./admin ./proxy` 与 `go test ./...`，全部通过；前端已运行 `npm run typecheck` 和 `npm run build`。
