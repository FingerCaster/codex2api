---
doc_type: feature-ff-note
feature: disable-messages-endpoint
date: 2026-05-18
requirement:
tags: [config, proxy, messages]
---

## 做了什么
新增 `CODEX_DISABLE_V1_MESSAGES` 环境变量，允许部署方显式禁用 Anthropic 兼容的 `/v1/messages` 与 `/messages` 转发路由；默认值保持不禁用，兼容现有部署。

## 改了哪些
- `config/config.go` — 读取 `CODEX_DISABLE_V1_MESSAGES` 到核心配置。
- `proxy/handler.go` — 注册路由时按配置跳过 messages 端点。
- `main.go` — 禁用时不在启动 banner 中打印 `/v1/messages`。
- `README.md`、`README.zh-CN.md`、`docs/CONFIGURATION.md`、`.env.example`、`.env.sqlite.example` — 补充新环境变量说明。
- `config/config_test.go`、`proxy/handler_test.go` — 覆盖配置解析、默认注册和禁用后不注册。

## 怎么验证的
已通过 Docker Go 环境执行 `gofmt`，并运行 `go test ./config ./proxy` 与 `go test ./...`，全部通过。
