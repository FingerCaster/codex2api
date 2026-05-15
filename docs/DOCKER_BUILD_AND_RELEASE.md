# Docker 构建与发布流程

本文档记录每次修改代码后，如何重新构建并发布 Docker 镜像。

当前发布镜像：

```text
daybreakx/codex2api
```

推荐每次发布两个标签：

```text
daybreakx/codex2api:latest
daybreakx/codex2api:<当前 git 短 SHA>
```

`latest` 方便服务器直接升级，短 SHA 标签方便回滚和定位版本。

---

## 1. 前置条件

本机需要：

```text
Docker Desktop
Docker buildx
Docker Hub 登录权限
```

检查 Docker：

```powershell
docker --version
docker buildx version
docker buildx ls
```

登录 Docker Hub：

```powershell
docker login
```

如果后续 `--push` 报 `unauthorized` 或 `denied`，通常是没有登录、登录账号不是 `daybreakx`，或没有 `daybreakx/codex2api` 仓库权限。

---

## 2. 修改代码后的推荐流程

先确认当前改动：

```powershell
git status --short
```

建议先跑本地检查：

```powershell
go test ./...
```

如果改过前端，建议额外跑：

```powershell
cd frontend
npm ci
npm run typecheck
npm run build
cd ..
```

说明：Docker 构建时会重新执行前端 `npm ci` 和 `npm run build`，本地先跑是为了更早发现错误。

---

## 3. 本地单架构验证构建

在正式推送多架构镜像前，可以先构建当前电脑常用的 `linux/amd64` 镜像：

```powershell
docker buildx build --platform linux/amd64 -t codex2api:local --load .
```

SQLite 模式冒烟测试：

```powershell
docker run -d --rm `
  --name codex2api-smoke `
  -p 18080:8080 `
  -e CODEX_PORT=8080 `
  -e DATABASE_DRIVER=sqlite `
  -e CACHE_DRIVER=memory `
  -e DATABASE_PATH=/data/codex2api.db `
  -e IMAGE_ASSET_DIR=/data/images `
  -e BOOTSTRAP_ALLOWED_CIDR=172.17.0.1/32 `
  -e LOG_DISABLED=true `
  -v codex2api-smoke-data:/data `
  codex2api:local

Start-Sleep -Seconds 3
docker logs --tail 80 codex2api-smoke
Invoke-WebRequest -UseBasicParsing -Uri http://127.0.0.1:18080/ -TimeoutSec 10
```

清理测试容器和测试卷：

```powershell
docker stop codex2api-smoke
docker volume rm codex2api-smoke-data
```

看到 HTTP `200`，并且日志里有 `Codex2API v2 已启动`，说明镜像能正常启动。

如果冒烟测试后要打开 `/admin/` 做首次初始化，`BOOTSTRAP_ALLOWED_CIDR` 需要包含 Docker 转发进容器后的客户端地址。本机默认 bridge 常见为 `172.17.0.1/32`；如果 Docker 网络不同，可用 `docker inspect <容器名>` 查看网关后调整。

---

## 4. 发布多架构镜像

发布 `linux/amd64` 和 `linux/arm64`：

```powershell
$rev = git rev-parse --short HEAD

docker buildx build `
  --platform linux/amd64,linux/arm64 `
  --build-arg BUILD_VERSION=$rev `
  -t daybreakx/codex2api:latest `
  -t daybreakx/codex2api:$rev `
  --push .
```

说明：

- `linux/amd64` 覆盖大多数 x86 VPS。
- `linux/arm64` 覆盖 ARM VPS。
- `--push` 会直接上传到 Docker Hub。
- 多架构镜像不能用 `--load` 一次性载入本地 Docker，只能推送到仓库或输出到文件。

---

## 5. 验证远端镜像

推送完成后检查 manifest：

```powershell
docker buildx imagetools inspect daybreakx/codex2api:latest
```

需要看到：

```text
Platform: linux/amd64
Platform: linux/arm64
```

也可以检查本次版本标签：

```powershell
$rev = git rev-parse --short HEAD
docker buildx imagetools inspect daybreakx/codex2api:$rev
```

---

## 6. 朋友 VPS 更新方式

如果朋友用的是 `docker run` 单容器 SQLite 部署：

```bash
docker pull daybreakx/codex2api:latest
docker rm -f codex2api

docker run -d \
  --name codex2api \
  -p 8080:8080 \
  -e CODEX_PORT=8080 \
  -e DATABASE_DRIVER=sqlite \
  -e CACHE_DRIVER=memory \
  -e DATABASE_PATH=/data/codex2api.db \
  -e IMAGE_ASSET_DIR=/data/images \
  -e BOOTSTRAP_ALLOWED_CIDR='你的公网IP/32' \
  -e TZ=Asia/Shanghai \
  -v codex2api-data:/data \
  --restart unless-stopped \
  daybreakx/codex2api:latest
```

数据在 `codex2api-data` 卷里，`docker rm -f codex2api` 只删除容器，不会删除数据卷。

如果已经用 `ADMIN_SECRET` 直接启动，可以不设置 `BOOTSTRAP_ALLOWED_CIDR`。如果希望朋友首次打开网页自行初始化，把这里改成对方当前公网 IP，例如 `1.2.3.4/32`。

如果朋友用 Docker Compose：

```bash
docker compose pull
docker compose up -d
docker compose logs -f codex2api
```

---

## 7. 回滚到指定版本

先查看你发布时记录的短 SHA，例如：

```text
daybreakx/codex2api:626e925
```

`docker run` 部署回滚：

```bash
docker pull daybreakx/codex2api:626e925
docker rm -f codex2api

docker run -d \
  --name codex2api \
  -p 8080:8080 \
  -e CODEX_PORT=8080 \
  -e DATABASE_DRIVER=sqlite \
  -e CACHE_DRIVER=memory \
  -e DATABASE_PATH=/data/codex2api.db \
  -e IMAGE_ASSET_DIR=/data/images \
  -e BOOTSTRAP_ALLOWED_CIDR='你的公网IP/32' \
  -e TZ=Asia/Shanghai \
  -v codex2api-data:/data \
  --restart unless-stopped \
  daybreakx/codex2api:626e925
```

Compose 部署回滚时，把 compose 文件里的镜像改成指定版本：

```yaml
image: daybreakx/codex2api:626e925
```

然后：

```bash
docker compose pull
docker compose up -d
```

---

## 8. 常见问题

### 推送时报 unauthorized

重新登录：

```powershell
docker login
```

确认登录账号有 `daybreakx/codex2api` 推送权限。

### VPS 还是旧版本

`latest` 标签在服务器上不会自动更新，必须执行：

```bash
docker pull daybreakx/codex2api:latest
docker rm -f codex2api
```

然后重新 `docker run`，或使用：

```bash
docker compose pull
docker compose up -d
```

### ARM 机器拉错架构

Docker 会按服务器架构自动选择 manifest。可以在 VPS 上检查：

```bash
uname -m
docker image inspect daybreakx/codex2api:latest --format '{{.Architecture}}'
```

### 构建很慢

第一次构建会下载 Node、Go、npm 包和 Go 模块，之后 Docker build cache 会明显加速。不要在小内存 VPS 上构建，直接在本机构建并推送镜像。

### 想减少 attestation unknown/unknown 条目

Docker buildx 默认可能推送 provenance attestation，所以 `imagetools inspect` 里可能出现 `unknown/unknown` 的 attestation manifest。这不影响 VPS 拉取运行。若想关闭，可发布时加：

```powershell
--provenance=false
```

示例：

```powershell
$rev = git rev-parse --short HEAD

docker buildx build `
  --platform linux/amd64,linux/arm64 `
  --provenance=false `
  --build-arg BUILD_VERSION=$rev `
  -t daybreakx/codex2api:latest `
  -t daybreakx/codex2api:$rev `
  --push .
```
