# Attention

本文件是 CodeStable 技能启动必读的项目注意事项入口。所有 CodeStable 子技能开始工作前必须读取它。

## 项目碎片知识

<!-- cs-note managed: 用 cs-note 维护，新条目按下面分节追加 -->

### 编译与构建

### 运行与本地起服务

### 测试

- 当前 Windows/PowerShell 环境 PATH 中没有 `go`；Go 格式化和测试可用 Docker 执行，例如 `docker run --rm -v ${PWD}:/app -v codex2api-gomod:/go/pkg/mod -v codex2api-gocache:/root/.cache/go-build -w /app golang:1.26.3-alpine go test ./...`。

### 命令与脚本陷阱

### 路径与目录约定

### 环境变量与凭证

### 其他
