---
doc_type: feature-ff-note
feature: usage-ranking
date: 2026-05-16
requirement:
tags: [usage, api-key, ranking, frontend]
---

## 做了什么
在使用统计下面新增使用排行榜，按 API Key 展示当前自然日、周、月的消耗排行，并支持按密钥名称、尾号或 ID 搜索单个 key 的消耗。

## 改了哪些
- `database/postgres.go` — 新增 API Key 周期排行榜聚合，排除 499，按用户计费、Token、请求数排序。
- `admin/handler.go` — 新增 `/api/admin/usage/ranking` 管理接口，解析 day/week/month 和搜索、limit 参数。
- `frontend/src/pages/UsageRanking.tsx` — 新增排行榜页面，包含日榜/周榜/月榜切换、搜索、汇总卡片和排行表格。
- `frontend/src/components/Layout.tsx` / `frontend/src/App.tsx` — 在使用统计下方接入菜单和路由。

## 怎么验证的
已跑 Docker Go 全量测试、前端 typecheck、前端生产构建，并用浏览器打开 `/admin/usage/ranking` 验证菜单和页面路由。
