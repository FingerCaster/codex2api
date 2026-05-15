# DaybreakX Docker 镜像部署指南

本文档面向已经发布到 Docker Hub 的镜像：

```text
daybreakx/codex2api:latest
```

镜像由项目根目录的 `Dockerfile` 构建，目标运行端口为 `8080`。推荐发布为多架构镜像，覆盖常见 VPS：

```text
linux/amd64
linux/arm64
```

---

## 适用场景

| 部署方式 | 适合场景 | 依赖 |
| --- | --- | --- |
| SQLite 单容器 | 小内存 VPS、个人使用、朋友直接拉取运行 | 只需要 Docker |
| Docker Compose SQLite | 需要固定 compose 文件和命名卷 | Docker + Docker Compose |
| Docker Compose 标准版 | 长期生产、账号多、请求量较高 | PostgreSQL + Redis |

小 VPS 优先使用 SQLite 单容器或 SQLite Compose，避免在服务器上编译镜像，也避免 PostgreSQL 和 Redis 占用额外内存。

---

## 方式一：SQLite 单容器部署

这是最省资源的部署方式，适合让朋友在 VPS 上直接拉取镜像运行。

```bash
docker pull daybreakx/codex2api:latest

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

访问地址：

```text
http://服务器IP:8080/admin/
```

首次访问管理后台时，如果没有设置 `ADMIN_SECRET`，页面会进入首次初始化流程，用浏览器设置管理密钥。

`BOOTSTRAP_ALLOWED_CIDR` 用于限制谁可以执行首次初始化。远程 VPS 建议填当前访问管理台的公网 IP，例如 `1.2.3.4/32`；本机 Docker Desktop / 默认 bridge 冒烟测试常见值是 `172.17.0.1/32`。如果已设置 `ADMIN_SECRET`，则不需要通过页面执行首次初始化。

### 带管理密钥启动

如果希望启动时直接指定管理后台密钥：

```bash
docker run -d \
  --name codex2api \
  -p 8080:8080 \
  -e CODEX_PORT=8080 \
  -e DATABASE_DRIVER=sqlite \
  -e CACHE_DRIVER=memory \
  -e DATABASE_PATH=/data/codex2api.db \
  -e IMAGE_ASSET_DIR=/data/images \
  -e ADMIN_SECRET='替换成强密码' \
  -e TZ=Asia/Shanghai \
  -v codex2api-data:/data \
  --restart unless-stopped \
  daybreakx/codex2api:latest
```

---

## 方式二：Docker Compose SQLite 部署

创建 `docker-compose.yml`：

```yaml
name: codex2api-sqlite

services:
  codex2api:
    image: daybreakx/codex2api:latest
    container_name: codex2api
    ports:
      - "${BIND_HOST:-0.0.0.0}:${CODEX_PORT:-8080}:${CODEX_PORT:-8080}"
    environment:
      CODEX_PORT: ${CODEX_PORT:-8080}
      DATABASE_DRIVER: sqlite
      CACHE_DRIVER: memory
      DATABASE_PATH: /data/codex2api.db
      IMAGE_ASSET_DIR: /data/images
      ADMIN_SECRET: ${ADMIN_SECRET:-}
      BOOTSTRAP_ALLOWED_CIDR: ${BOOTSTRAP_ALLOWED_CIDR:-}
      TZ: ${TZ:-Asia/Shanghai}
    volumes:
      - sqlite-data:/data
    restart: unless-stopped

volumes:
  sqlite-data:
    name: codex2api_sqlite_data
```

可选 `.env`：

```env
CODEX_PORT=8080
BIND_HOST=0.0.0.0
ADMIN_SECRET=
BOOTSTRAP_ALLOWED_CIDR=
TZ=Asia/Shanghai
```

启动：

```bash
docker compose pull
docker compose up -d
docker compose logs -f codex2api
```

升级：

```bash
docker compose pull
docker compose up -d
```

---

## 方式三：Docker Compose 标准版

标准版会同时启动 PostgreSQL 和 Redis，适合长期生产环境。

创建 `docker-compose.yml`：

```yaml
name: codex2api

services:
  codex2api:
    image: daybreakx/codex2api:latest
    container_name: codex2api
    ports:
      - "${BIND_HOST:-0.0.0.0}:${CODEX_PORT:-8080}:${CODEX_PORT:-8080}"
    environment:
      CODEX_PORT: ${CODEX_PORT:-8080}
      DATABASE_DRIVER: postgres
      DATABASE_HOST: postgres
      DATABASE_PORT: 5432
      DATABASE_USER: ${DATABASE_USER:-codex2api}
      DATABASE_PASSWORD: ${DATABASE_PASSWORD:-codex2api}
      DATABASE_NAME: ${DATABASE_NAME:-codex2api}
      CACHE_DRIVER: redis
      REDIS_ADDR: redis:6379
      IMAGE_ASSET_DIR: /data/images
      ADMIN_SECRET: ${ADMIN_SECRET:-}
      BOOTSTRAP_ALLOWED_CIDR: ${BOOTSTRAP_ALLOWED_CIDR:-}
      TZ: ${TZ:-Asia/Shanghai}
    volumes:
      - image-assets:/data
      - ./logs:/app/logs
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    restart: unless-stopped
    networks:
      - codex2api-net

  postgres:
    image: postgres:18-alpine
    container_name: codex2api-postgres
    environment:
      POSTGRES_USER: ${DATABASE_USER:-codex2api}
      POSTGRES_PASSWORD: ${DATABASE_PASSWORD:-codex2api}
      POSTGRES_DB: ${DATABASE_NAME:-codex2api}
      PGDATA: /var/lib/postgresql/data
      TZ: ${TZ:-Asia/Shanghai}
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${DATABASE_USER:-codex2api}"]
      interval: 5s
      timeout: 3s
      retries: 5
    restart: unless-stopped
    networks:
      - codex2api-net

  redis:
    image: redis:7-alpine
    container_name: codex2api-redis
    command: redis-server --appendonly yes
    volumes:
      - redisdata:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5
    restart: unless-stopped
    networks:
      - codex2api-net

networks:
  codex2api-net:
    driver: bridge

volumes:
  pgdata:
    name: codex2api_pgdata
  redisdata:
    name: codex2api_redisdata
  image-assets:
    name: codex2api_image_assets
```

建议 `.env`：

```env
CODEX_PORT=8080
BIND_HOST=0.0.0.0
ADMIN_SECRET=替换成强密码
BOOTSTRAP_ALLOWED_CIDR=
DATABASE_USER=codex2api
DATABASE_PASSWORD=替换成数据库强密码
DATABASE_NAME=codex2api
TZ=Asia/Shanghai
```

启动：

```bash
docker compose pull
docker compose up -d
docker compose ps
docker compose logs -f codex2api
```

---

## 常用运维命令

查看日志：

```bash
docker logs -f codex2api
```

重启：

```bash
docker restart codex2api
```

停止并删除容器，但保留数据卷：

```bash
docker rm -f codex2api
```

SQLite 数据备份：

```bash
docker run --rm \
  -v codex2api-data:/data \
  -v "$PWD":/backup \
  alpine:3.19 \
  cp /data/codex2api.db /backup/codex2api.db.backup
```

注意：不要随意执行 `docker volume rm codex2api-data` 或 `docker compose down -v`，这会删除持久化数据。

---

## 发布多架构镜像

在本地 Docker Desktop 或已配置 buildx 的机器上执行：

```powershell
docker login

$rev = git rev-parse --short HEAD
docker buildx build `
  --platform linux/amd64,linux/arm64 `
  --build-arg BUILD_VERSION=$rev `
  -t daybreakx/codex2api:latest `
  -t daybreakx/codex2api:$rev `
  --push .
```

验证 manifest：

```powershell
docker buildx imagetools inspect daybreakx/codex2api:latest
```

如果只想本地验证单架构构建：

```powershell
docker buildx build --platform linux/amd64 -t codex2api:local --load .
```

---

## 端口和反向代理

默认容器端口是 `8080`。如果使用 Nginx 或 Caddy 做反向代理，建议只绑定本机：

```env
BIND_HOST=127.0.0.1
CODEX_PORT=8080
```

管理后台：

```text
http://服务器IP:8080/admin/
```

健康检查：

```text
http://服务器IP:8080/health
```

OpenAI 兼容入口：

```text
http://服务器IP:8080/v1
```
