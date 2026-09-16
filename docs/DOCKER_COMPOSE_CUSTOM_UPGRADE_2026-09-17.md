# 从已上线的上游版本切换到二开版：Docker Compose 操作指南

适用对象：当前已用 Docker Compose 运行 Sub2API，要保留用户、API Key、余额、订阅、账号和配置，切换到本仓库的月卡日额度提前重置版本。

本指南配合 [本轮审查报告](REVIEW3_2026-09-17.md) 使用。当前生产版本、服务器路径、PostgreSQL 主版本尚未提供，文中的服务器值必须以第 1 步采集结果为准。命令中的服务器操作使用 **Linux Bash**；本机打包使用 **Windows PowerShell**。逐节执行，遇到错误停止，不要整篇一次粘贴运行。

切换策略：沿用原来的 Compose 项目、全部配置文件、环境变量、数据卷、PostgreSQL、Redis 和反向代理，通过一个很小的覆盖文件更换 `sub2api` 镜像。**不要用本仓库的新 Compose 文件覆盖线上原文件**，尤其不要顺带把数据库镜像改成另一个主版本。升级后的程序会自动执行 SQL 迁移。

## 1. 记录线上实际部署

SSH 登录服务器。服务器命令需要管理 Docker、读取配置并保留备份文件的 UID/GID；以下按 root Bash 编写，非 root SSH 用户先通过 `sudo -i` 进入该会话，已经是 root 则直接继续。后续服务器代码块保持在同一会话中执行：

```bash
set -euo pipefail
umask 077
docker compose version
docker ps --format 'table {{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Ports}}'
```

从输出选择当前应用容器；下面 `sub2api` 是常见名字，若不同请替换：

```bash
APP_CID=$(docker inspect --format '{{.Id}}' sub2api)
PROJECT=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$APP_CID")
DEPLOY_DIR=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$APP_CID")
CONFIG_FILES=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$APP_CID")
printf '项目：%s\n目录：%s\n配置文件：%s\n' "$PROJECT" "$DEPLOY_DIR" "$CONFIG_FILES"
test -n "$PROJECT" && test -d "$DEPLOY_DIR"
cd "$DEPLOY_DIR"

# 保留原来全部 -f 文件及其顺序，不要只留第一个文件。
IFS=',' read -r -a FILES <<< "$CONFIG_FILES"
DC=(docker compose --project-directory "$DEPLOY_DIR" -p "$PROJECT")
for f in "${FILES[@]}"; do test -f "$f"; DC+=(-f "$f"); done
# 若原部署用了额外 --env-file，在此加入相同参数，例如：
# DC+=(--env-file /opt/sub2api/prod.env)
"${DC[@]}" config --quiet
"${DC[@]}" ps
test "$("${DC[@]}" ps -q sub2api)" = "$APP_CID"

OLD_IMAGE_ID=$(docker inspect --format '{{.Image}}' "$APP_CID")
docker image inspect "$OLD_IMAGE_ID" --format '旧镜像={{.Id}} 平台={{.Os}}/{{.Architecture}}'
docker inspect "$APP_CID" --format '{{range .Mounts}}{{println .Type .Source "->" .Destination}}{{end}}'
docker exec "$APP_CID" /app/sub2api -version
docker exec "$APP_CID" date
```

若 `-version` 在特别旧的版本不存在，通过管理页面或启动日志记录版本。检查 `/app/data` 确实持久化。保留 `.env`、所有外部挂载的 `config.yaml`、JWT/TOTP/其他加密密钥和数据库连接设置；不要重新生成密钥，不要公开发送 `docker inspect` 的完整环境或 `compose config` 的内容。

以下步骤假定数据库服务名为 `postgres`、缓存服务名为 `redis`，并且 PostgreSQL 属于此 Compose 项目；不符合时先替换服务名/改用实际数据库备份方式。

```bash
PG_CID=$("${DC[@]}" ps -q postgres)
REDIS_CID=$("${DC[@]}" ps -q redis)
test -n "$PG_CID" && test -n "$REDIS_CID"
PG_IMAGE_ID=$(docker inspect --format '{{.Image}}' "$PG_CID")
REDIS_IMAGE_ID=$(docker inspect --format '{{.Image}}' "$REDIS_CID")
"${DC[@]}" exec -T postgres sh -ec 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "SHOW server_version;"'
```

核对应用的实际 `DATABASE_HOST`、`DATABASE_DBNAME`、`DATABASE_USER`（可能来自 `.env`，也可能来自挂载的 `config.yaml`）。下文采用仓库标准部署：应用连接此 `postgres`，数据库名/用户分别等于容器内 `POSTGRES_DB` / `POSTGRES_USER`。如果不同，所有 dump、恢复和验收命令必须改用实际应用数据库及相应角色；不能备份一个空的默认库就继续升级。使用自定义 `DATA_DIR` 或 `CONFIG_FILE` 的部署，也要把文中的 `/app/data` 换成实际持久化位置并备份独立配置挂载。

如线上是比本次上游基线 `0.2.5 / 881f32026` 更新的版本，先核对差异，不要把这里的操作当作降级指南。旧于 0.2.5 的数据库还会执行相应上游迁移，因此必须做第 4 步的副本演练。

## 2. 在开发机打包当前二开代码

**本次已经提供构建好的 Linux amd64 镜像**：开发机 `C:\Users\Administrator\Desktop\sub2api-release-r3\sub2api-custom-r3.tar`，大小 45,568,000 字节。若第 1 步确认服务器为 `linux/amd64`，且未继续修改代码，可直接使用此文件，从本节「上传」命令开始，不必再构建。SHA256：`c55dc2f734bdfdd893b257f6c7ce5a8d841f768f1a7b7a444404b9b39636a637`。服务器 ARM 平台需要按下面的方法另行构建并验证。

直接上传前在 PowerShell 设置：`$ReleaseDir = Join-Path $env:USERPROFILE 'Desktop\sub2api-release-r3'`。发布目录同时附带完整源码快照、校验清单和本轮报告，源码包含当前未提交的修复和新增测试。

本轮修复保存在当前工作区；旧的 `3464465c7` 提交、远端 `main` 和官方 `latest` 都不能代表包含本轮修复的代码。以下直接构建当前目录，不要求先推送 GitHub。

确认 Docker Desktop 已启动并使用 Linux 容器。在本机 PowerShell 中：

```powershell
Set-Location 'C:\Users\Administrator\Desktop\Sub2api'
$ReleaseDir = Join-Path $env:USERPROFILE 'Desktop\sub2api-release-r3'
New-Item -ItemType Directory -Force -Path $ReleaseDir | Out-Null
$Image = 'sub2api-custom:0.2.5-daily-reset-r3-20260917'

# 与第 1 步的平台一致。ARM 服务器改为 linux/arm64；本轮 amd64 验证不能替代 ARM 验证。
docker build --platform linux/amd64 `
  --build-arg VERSION=0.2.5-daily-reset.r3.20260917 `
  --build-arg COMMIT=3464465c7-review3-worktree `
  --build-arg POSTGRES_IMAGE=postgres:18.1-alpine3.23 `
  -t $Image .
if ($LASTEXITCODE -ne 0) { throw '镜像构建失败，停止发布' }
docker run --rm $Image -version
if ($LASTEXITCODE -ne 0) { throw '镜像无法运行，停止发布' }
docker image inspect $Image --format '{{.Id}} {{.Os}}/{{.Architecture}}'
docker save -o (Join-Path $ReleaseDir 'sub2api-custom-r3.tar') $Image
if ($LASTEXITCODE -ne 0) { throw '镜像导出失败' }
Get-FileHash (Join-Path $ReleaseDir 'sub2api-custom-r3.tar') -Algorithm SHA256
```

构建中的 `POSTGRES_IMAGE` 用于提供镜像内备份客户端，**不会替换线上 PostgreSQL 服务**。如果实际数据库比 PostgreSQL 18 更新，需要匹配更高版本客户端并重新验证。本机内存/磁盘不足时，可在单独构建机使用同一份完整源码和 Dockerfile 构建；不要从远端旧分支重新拉取代替当前源码。

把 `sub2api-custom-r3.tar` 上传到服务器，例如 PowerShell：

```powershell
# 替换 deploy@YOUR_SERVER 为实际 SSH 用户和地址。
scp (Join-Path $ReleaseDir 'sub2api-custom-r3.tar') deploy@YOUR_SERVER:/tmp/sub2api-custom-r3.tar
```

服务器执行，核对 SHA256 与本机一致：

```bash
sha256sum /tmp/sub2api-custom-r3.tar
docker load -i /tmp/sub2api-custom-r3.tar
CUSTOM_IMAGE=sub2api-custom:0.2.5-daily-reset-r3-20260917
docker image inspect "$CUSTOM_IMAGE" --format '{{.Id}} {{.Os}}/{{.Architecture}}'
docker run --rm "$CUSTOM_IMAGE" -version
```

## 3. 保留旧镜像并做演练备份

回到第 1 步的服务器 Bash 会话，保留 `DC` 等变量。先留住旧镜像，避免 `latest` 更新后无法找回原版本：

```bash
STAMP=$(date +%Y%m%d-%H%M%S)
BACKUP="$DEPLOY_DIR/backups/pre-custom-$STAMP"
mkdir -p "$BACKUP/app-data"
chmod 700 "$BACKUP"
OLD_TAG="sub2api-local-backup:pre-custom-$STAMP"
docker tag "$OLD_IMAGE_ID" "$OLD_TAG"
docker save -o "$BACKUP/old-image.tar" "$OLD_TAG"
"${DC[@]}" config > "$BACKUP/compose.resolved.yaml"
docker inspect "$APP_CID" > "$BACKUP/app.inspect.json"
printf '%s\n' "$OLD_TAG" > "$BACKUP/old-image-tag.txt"
printf '%s\n' "$PG_IMAGE_ID" > "$BACKUP/postgres-image-id.txt"
printf '%s\n' "$REDIS_IMAGE_ID" > "$BACKUP/redis-image-id.txt"
printf '%s\n' "${FILES[@]}" > "$BACKUP/compose-files.txt"
```

另行备份上述原始 Compose 文件、`.env`、额外 `--env-file` 以及容器挂载的独立配置文件，保留权限和相对目录。`compose.resolved.yaml` 只是核对证据，不能代替原始文件；里面可能含密码，请限制读取并将整个备份加密复制到另一台机器。

先导出用于演练的数据库快照；这是运行中一致性快照，正式切换前还要重新做最终备份：

```bash
"${DC[@]}" exec -T postgres sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl' > "$BACKUP/rehearsal.dump"
test -s "$BACKUP/rehearsal.dump"
"${DC[@]}" exec -T postgres pg_restore --list < "$BACKUP/rehearsal.dump" > "$BACKUP/rehearsal.contents.txt"
docker cp -a "$APP_CID:/app/data/." "$BACKUP/app-data/"
```

这里使用数据库容器自带的 `pg_dump`，以匹配服务器主版本；不要在运行中的 PostgreSQL 上直接复制 `postgres_data` 目录作为唯一备份。

## 4. 用独立数据库副本演练

在同一服务器的独立目录和独立 Compose 项目中演练，或使用隔离测试服务器。**测试应用必须连接副本数据库、全新 Redis 和复制的 `/app/data`，不能连接线上数据库或 Redis。** 数据副本含真实账号凭据、回调和后台任务配置，因此应用、数据库和 Redis 只连接禁止外部访问的演练网络，避免发送邮件、回调支付或自动刷新真实上游凭据。单独的 Nginx 预览入口连接该网络和普通桥接网络，只发布宿主机回环端口，固定反代到测试应用；不要让测试应用本身加入普通桥接网络。

```bash
STAGE_DIR="$DEPLOY_DIR/rehearsal-$STAMP"
mkdir -p "$STAGE_DIR/data"
cp -a "$BACKUP/app-data/." "$STAGE_DIR/data/"
export STAGE_PG_IMAGE="$PG_IMAGE_ID"
export STAGE_REDIS_IMAGE="$REDIS_IMAGE_ID"
export CUSTOM_IMAGE
STAGE_DB_PASSWORD=$(openssl rand -hex 24)
STAGE_REDIS_PASSWORD=$(openssl rand -hex 24)
export STAGE_DB_PASSWORD STAGE_REDIS_PASSWORD
cat > "$STAGE_DIR/nginx.conf" <<'NGINX'
server {
    listen 80;
    location / {
        proxy_pass http://sub2api:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_read_timeout 600s;
    }
}
NGINX
```

在 `$STAGE_DIR/compose.yaml` 写入：

```yaml
services:
  postgres:
    image: ${STAGE_PG_IMAGE:?}
    environment:
      POSTGRES_USER: sub2api_stage
      POSTGRES_PASSWORD: ${STAGE_DB_PASSWORD:?}
      POSTGRES_DB: sub2api_stage
      PGDATA: /var/lib/postgresql/data
    volumes:
      - ./pg:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U sub2api_stage -d sub2api_stage"]
      interval: 2s
      retries: 30
  redis:
    image: ${STAGE_REDIS_IMAGE:?}
    command: ["redis-server", "--save", "", "--appendonly", "no", "--requirepass", "${STAGE_REDIS_PASSWORD:?}"]
  sub2api:
    image: ${CUSTOM_IMAGE:?}
    pull_policy: never
    volumes:
      - ./data:/app/data
    environment:
      AUTO_SETUP: "true"
      DATABASE_HOST: postgres
      DATABASE_PORT: "5432"
      DATABASE_USER: sub2api_stage
      DATABASE_PASSWORD: ${STAGE_DB_PASSWORD:?}
      DATABASE_DBNAME: sub2api_stage
      DATABASE_SSLMODE: disable
      REDIS_HOST: redis
      REDIS_PORT: "6379"
      REDIS_USERNAME: default
      REDIS_PASSWORD: ${STAGE_REDIS_PASSWORD:?}
      REDIS_DB: "0"
      REDIS_ENABLE_TLS: "false"
      SERVER_HOST: 0.0.0.0
      SERVER_PORT: "8080"
      TZ: ${STAGE_TZ:-Asia/Shanghai}
  preview:
    image: nginx:alpine
    ports: ["127.0.0.1:18080:80"]
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro
    networks:
      - default
      - preview
networks:
  default:
    internal: true
  preview:
    driver: bridge
```

`STAGE_TZ` 应设置成线上原时区。若 JWT/TOTP 等密钥仅通过环境注入，也应通过权限为 `600` 的独立环境文件传入相同值；不要覆盖上面的测试数据库连接。若线上配置不在 `/app/data` 内，复制相应文件到演练目录并挂载副本。副本上的邮件/上游访问失败在隔离网络中属于预期，但数据库迁移和本地账号登录应该成功。

这里给测试 Redis 设置非空的新密码和 `default` 用户，是为了覆盖副本配置中的旧密码/ACL 用户；本项目的 Viper 配置加载默认忽略空环境变量，`REDIS_PASSWORD: ""` 不一定能清掉 `config.yaml` 中的密码。不要把生产 Redis 密码复制成测试密码。

在 Docker 29 的实际烟测中，只连接 `internal: true` 网络的应用即使设置 `ports`，宿主机端口也未映射成功。上面的预览服务用于解决这一点；它只反代固定的 `sub2api:8080`，不是可转发任意外部地址的代理。不要给 PostgreSQL、Redis 或应用额外发布端口。

```bash
STAGE=(docker compose -p "sub2api-review-$STAMP" -f "$STAGE_DIR/compose.yaml")
"${STAGE[@]}" up -d postgres redis
for attempt in {1..60}; do
  if "${STAGE[@]}" exec -T postgres pg_isready -U sub2api_stage -d sub2api_stage; then break; fi
  sleep 2
done
"${STAGE[@]}" exec -T postgres pg_isready -U sub2api_stage -d sub2api_stage
"${STAGE[@]}" exec -T postgres pg_restore --exit-on-error --no-owner --no-acl -U sub2api_stage -d sub2api_stage < "$BACKUP/rehearsal.dump"
"${STAGE[@]}" up -d --no-deps sub2api
"${STAGE[@]}" up -d --no-deps preview
"${STAGE[@]}" logs --tail 150 sub2api
curl --retry 30 --retry-connrefused --retry-delay 2 --max-time 5 --fail --show-error http://127.0.0.1:18080/health
```

用 SSH 隧道从本机浏览器访问：`ssh -L 18080:127.0.0.1:18080 deploy@YOUR_SERVER`，然后打开 `http://127.0.0.1:18080`。本地密码登录可用于演练，第三方 OAuth 的正式回调域名不一定适用。不要为测试修改生产 OAuth 配置。

演练验收：原用户/管理员能登录，API Key、余额、订阅到期和三种已用额度与快照一致，原分组功能和模型白名单正常；授权一个测试订阅分组，确认已有月卡能看到重置功能、用户自动开关仍默认关闭；给测试订阅设置少量日消费，手动重置应只扣 24 小时、只清日额度，周/月用量保留；刷新页面与重复点击不能重复扣天；关闭分组许可后不能再付费重置。测试都发生在副本上。

可查询迁移状态：

```bash
"${STAGE[@]}" exec -T postgres psql -U sub2api_stage -d sub2api_stage -c "SELECT filename,applied_at FROM schema_migrations WHERE filename IN ('235_group_model_allowlist.sql','235_subscription_daily_reset.sql') ORDER BY filename;"
```

这两个 `235_` 文件应同时存在；迁移器按完整文件名识别，不能删掉一个、改名或手工伪造 checksum 来绕过错误。若演练迁移失败，保留日志并修正原因，**不要进行正式切换**。

演练完成后停止此项目，避免继续运行扫描器：

```bash
"${STAGE[@]}" stop
```

## 5. 进入维护窗口，做最终一致性备份

先在反向代理/负载均衡设置维护入口，暂停新 API 请求、后台管理操作和支付入口；等待正在进行的流式请求、计费及支付回调处理结束。多副本部署要停止所有旧应用实例和相关写入任务；不要让新旧版本同时为同一数据库提供服务。单纯 `stop -t 120` 不能保证长请求结算完成，应用本身的 HTTP 关闭超时只有 5 秒，因此要先排空业务流量。

```bash
"${DC[@]}" stop -t 120 sub2api
"${DC[@]}" exec -T postgres sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl' > "$BACKUP/final.dump"
test -s "$BACKUP/final.dump"
"${DC[@]}" exec -T postgres pg_restore --list < "$BACKUP/final.dump" > "$BACKUP/final.contents.txt"
mkdir -p "$BACKUP/final-app-data"
docker cp -a "$APP_CID:/app/data/." "$BACKUP/final-app-data/"
sha256sum "$BACKUP/final.dump" "$BACKUP/old-image.tar" > "$BACKUP/SHA256SUMS"
```

检查 `pg_dump` 与 `pg_restore --list` 均退出 0，备份非空，并完成异机备份。`--list` 只验证备份目录可读，真正可恢复性依靠第 4 步的恢复演练；要求严格的部署还应将最终快照恢复到另一个临时数据库验收。

如应用另有对象存储、自定义插件或外部文件挂载，按原部署补充备份。Redis 通常包含缓存，也可能包含任务与会话，需先确认异步任务已排空；不要随意执行 `FLUSHALL` 或删除 Redis 卷。

## 6. 仅更换应用镜像

在原部署目录创建覆盖文件，保留原配置：

```bash
OVERRIDE="$DEPLOY_DIR/compose.custom-r3.yaml"
cat > "$OVERRIDE" <<'YAML'
services:
  sub2api:
    image: sub2api-custom:0.2.5-daily-reset-r3-20260917
    pull_policy: never
YAML
NEW_DC=("${DC[@]}" -f "$OVERRIDE")
"${NEW_DC[@]}" config --quiet
"${NEW_DC[@]}" config > "$BACKUP/compose.custom.resolved.yaml"
diff -u "$BACKUP/compose.resolved.yaml" "$BACKUP/compose.custom.resolved.yaml" || true
```

本机检查 diff：应仅改变应用 `image`、`pull_policy` 及 Compose 自身格式；数据库/Redis 镜像、卷、网络、端口和原环境必须保持一致。此 diff 可能含密码，勿贴到公开渠道。

```bash
"${NEW_DC[@]}" up -d --no-deps --no-build --pull never sub2api
"${NEW_DC[@]}" logs --tail 200 sub2api
NEW_CID=$("${NEW_DC[@]}" ps -q sub2api)
docker inspect "$NEW_CID" --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}'
docker inspect "$NEW_CID" --format '{{.Image}}'
docker image inspect "$CUSTOM_IMAGE" --format '{{.Id}}'
"${NEW_DC[@]}" exec -T sub2api /app/sub2api -version
"${NEW_DC[@]}" exec -T sub2api sh -ec 'wget -q -O - "http://127.0.0.1:${SERVER_PORT:-8080}/health"'
```

两个镜像 ID 必须一致，日志不得有 checksum mismatch、迁移失败、字段缺失或持续重启。首次迁移可能耗时，数据库大表迁移不要因为等待而强制反复重启。现有域名和代理继续指向原应用端口。

## 7. 先验证原业务，再开放新功能

1. 保持维护入口，用管理员和普通测试账号分别登录；检查用户数、API Key、余额、分组配置、上游账号及订阅到期/用量。
2. 使用原 API Key 完成一笔小额真实请求；确认返回、流式结束、使用记录和扣费正确。按实际使用协议分别验证 `/v1/messages`、`/v1/chat/completions`、`/v1/responses`、Gemini 等，不必调用未使用的平台。
3. 检查登录/支付/上游账号刷新等你实际启用的原业务，确认没有二开回归，再逐步恢复流量。
4. 需要开放月卡重置时，在管理员「分组」编辑**订阅分组**，设置有限正数日额度并打开「允许订阅日额度重置」。已有同组有效订阅适用，无需重新购买。
5. 用户在「我的订阅」选择手动重置或自行打开自动重置；管理员许可不会自动替用户开启。每次成功扣整整 24 小时，扣后必须仍有效；每张订阅每系统自然日最多 100 次。周/月额度耗尽时不能通过付费日重置绕过。
6. 开放后的首轮检查错误率、延迟、账单以及 `subscription_daily_reset_events` 的次数/扣天记录。保持原 `TZ`；默认上海时区是北京时间 00:00 自然刷新，实际配置覆盖后按该时区执行。

## 8. 回滚：区分代码切回与数据恢复

**启动失败、尚未执行新迁移且尚未产生新业务写入**时，可以切回保存的旧镜像。若新迁移已经执行，不能假定旧二进制兼容；先在升级后数据库副本上验证旧镜像，再决定是否仅回退代码。

```bash
"${NEW_DC[@]}" stop -t 120 sub2api
ROLLBACK_OVERRIDE="$DEPLOY_DIR/compose.rollback-r3.yaml"
printf 'services:\n  sub2api:\n    image: %s\n    pull_policy: never\n' "$OLD_TAG" > "$ROLLBACK_OVERRIDE"
ROLLBACK_DC=("${DC[@]}" -f "$ROLLBACK_OVERRIDE")
# 仅在满足上面的数据库兼容条件后执行：
"${ROLLBACK_DC[@]}" up -d --no-deps --no-build --pull never sub2api
"${ROLLBACK_DC[@]}" logs --tail 150 sub2api
```

**已迁移且无法确认兼容，或已发生付费重置/其他新写入**时，可靠恢复基线是「旧镜像 + final.dump + final-app-data + 原密钥配置」。只换镜像不会返还已扣的有效期。恢复快照会丢失备份之后的新订单、充值、请求用量和重置记录；先保存故障库，核对这段时间的账务，决定是否能回到该时间点。

以下数据恢复步骤与上面的「仅回退代码」是两个分支。选择数据恢复时，先完整完成下面的准备和验收，再启动旧应用，不要先运行上面的启动命令。

### 8.1 保留故障状态并恢复到新数据库

恢复到同一个 PostgreSQL 服务中的**新数据库名**，保留故障数据库。继续使用第 1 步确认的数据库主版本及原角色，不改变 PostgreSQL 数据卷。确保磁盘可同时容纳原库、恢复库和备份。

```bash
# 保持维护状态，所有应用和写入任务已停止。
"${NEW_DC[@]}" stop -t 120 sub2api
sha256sum -c "$BACKUP/SHA256SUMS"
test -s "$BACKUP/final.dump"
"${DC[@]}" exec -T postgres sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl' > "$BACKUP/failed-state.dump"
test -s "$BACKUP/failed-state.dump"
"${DC[@]}" exec -T postgres pg_restore --list < "$BACKUP/failed-state.dump" > "$BACKUP/failed-state.contents.txt"
FAILED_CID=$("${NEW_DC[@]}" ps -a -q sub2api)
test -n "$FAILED_CID"
mkdir -p "$BACKUP/failed-app-data"
docker cp -a "$FAILED_CID:/app/data/." "$BACKUP/failed-app-data/"

RESTORE_DB="sub2api_restore_$(date +%Y%m%d_%H%M%S)"
"${DC[@]}" exec -T -e RESTORE_DB="$RESTORE_DB" postgres sh -ec 'createdb -T template0 -U "$POSTGRES_USER" -O "$POSTGRES_USER" "$RESTORE_DB"'
"${DC[@]}" exec -T -e RESTORE_DB="$RESTORE_DB" postgres sh -ec 'pg_restore --exit-on-error --no-owner --no-acl -U "$POSTGRES_USER" -d "$RESTORE_DB"' < "$BACKUP/final.dump"
"${DC[@]}" exec -T -e RESTORE_DB="$RESTORE_DB" postgres sh -ec 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$RESTORE_DB" -c "SELECT current_database(); SELECT COUNT(*) AS users FROM users; SELECT COUNT(*) AS subscriptions FROM user_subscriptions; SELECT filename, applied_at FROM schema_migrations ORDER BY filename DESC LIMIT 10;"'
```

`createdb` 在同名库已存在时会失败，这是保护措施；不要加 `dropdb` 或 `pg_restore --clean` 强行覆盖。恢复中途失败时，保留该库排查，改用新的 `RESTORE_DB` 从头恢复。若原应用使用的数据库角色与 `POSTGRES_USER` 不同，需将新库所有者设为原应用角色，并通过 `pg_restore --role=<原应用角色>` 恢复对象所有权；先完成权限验证再启动应用。

### 8.2 恢复应用目录并发现原网络

下面创建新目录挂载 `/app/data`，原应用数据卷保留；`cp -a` 保留备份中的 UID/GID 和权限。若 Docker 命令需要 `sudo`，复制文件也使用具有保留所有权权限的同一运维账号执行。

```bash
RESTORE_STAMP=$(date +%Y%m%d-%H%M%S)
RESTORE_DIR="$DEPLOY_DIR/restore-$RESTORE_STAMP"
test ! -e "$RESTORE_DIR"
mkdir -p "$RESTORE_DIR/data"
cp -a "$BACKUP/final-app-data/." "$RESTORE_DIR/data/"
RESTORE_DATA_DIR="$RESTORE_DIR/data"

# 取当前应用容器与 PostgreSQL 容器共同连接的实际网络名。
# 仓库标准部署通常得到 <PROJECT>_sub2api-network，不要手写猜测项目名前缀。
mapfile -t RESTORE_NETWORKS < <(comm -12 \
  <(docker inspect "$FAILED_CID" --format '{{range $name, $_ := .NetworkSettings.Networks}}{{println $name}}{{end}}' | sed '/^$/d' | sort) \
  <(docker inspect "$PG_CID" --format '{{range $name, $_ := .NetworkSettings.Networks}}{{println $name}}{{end}}' | sed '/^$/d' | sort))
printf '共同网络：%s\n' "${RESTORE_NETWORKS[@]}"
test "${#RESTORE_NETWORKS[@]}" -eq 1
RESTORE_NETWORK=${RESTORE_NETWORKS[0]}
docker network inspect "$RESTORE_NETWORK" --format '{{.Name}} {{.Driver}}'
```

若共同网络不是恰好一个，上面的检查会停止：从容器网络配置中选择应用用于数据库通信的网络，手动设置 `RESTORE_NETWORK` 后再继续；不要创建同名的新网络。独立挂载的 `config.yaml`、自定义 `CONFIG_FILE`/`DATA_DIR`、对象存储和插件文件仍需恢复相应副本。尤其注意原来若有 `/app/data/config.yaml` 等子路径挂载，Compose 会保留它，**只替换 `/app/data` 并不会移除这个子挂载**；必须在覆盖文件中按同一 `target` 指向恢复的配置副本，且保留原密钥。

### 8.3 新建独立 Redis 卷和完整回滚覆盖文件

新 Redis 避免旧余额/订阅缓存污染恢复后的数据库。原 Redis 的卷、会话与任务全部保留；新 Redis 不恢复未核对的旧队列，用户可能需要重新登录，异步任务要按账务核对结果处理。

```bash
RESTORE_REDIS_HOST="sub2api-restore-redis-$RESTORE_STAMP"
RESTORE_REDIS_VOLUME="sub2api-restore-redis-data-$RESTORE_STAMP"
RESTORE_REDIS_PASSWORD=$(openssl rand -hex 24)
RESTORE_REDIS_IMAGE="$REDIS_IMAGE_ID"
# 检查名字未使用；不要重用故障阶段的卷。
if docker volume inspect "$RESTORE_REDIS_VOLUME" >/dev/null 2>&1; then
  printf '恢复卷已存在，请换一个 RESTORE_STAMP 后重新准备。\n' >&2
  exit 1
fi
docker volume create "$RESTORE_REDIS_VOLUME" >/dev/null
export OLD_TAG RESTORE_DB RESTORE_DATA_DIR RESTORE_NETWORK
export RESTORE_REDIS_HOST RESTORE_REDIS_VOLUME RESTORE_REDIS_PASSWORD RESTORE_REDIS_IMAGE

ROLLBACK_OVERRIDE="$RESTORE_DIR/compose.rollback.yaml"
cat > "$ROLLBACK_OVERRIDE" <<'YAML'
services:
  sub2api:
    image: ${OLD_TAG:?}
    pull_policy: never
    environment:
      DATABASE_DBNAME: ${RESTORE_DB:?}
      REDIS_HOST: ${RESTORE_REDIS_HOST:?}
      REDIS_PORT: "6379"
      REDIS_USERNAME: default
      REDIS_PASSWORD: ${RESTORE_REDIS_PASSWORD:?}
      REDIS_DB: "0"
      REDIS_ENABLE_TLS: "false"
    volumes:
      - type: bind
        source: ${RESTORE_DATA_DIR:?}
        target: /app/data
  redis-restore-r3:
    image: ${RESTORE_REDIS_IMAGE:?}
    pull_policy: never
    restart: unless-stopped
    command: ["redis-server", "--appendonly", "yes", "--appendfsync", "everysec", "--requirepass", "${RESTORE_REDIS_PASSWORD:?}"]
    environment:
      REDISCLI_AUTH: ${RESTORE_REDIS_PASSWORD:?}
    volumes:
      - restore_redis_r3_data:/data
    networks:
      rollback-r3-network:
        aliases: ["${RESTORE_REDIS_HOST:?}"]
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 2s
      timeout: 3s
      retries: 30
volumes:
  restore_redis_r3_data:
    external: true
    name: ${RESTORE_REDIS_VOLUME:?}
networks:
  rollback-r3-network:
    external: true
    name: ${RESTORE_NETWORK:?}
YAML
ROLLBACK_DC=("${DC[@]}" -f "$ROLLBACK_OVERRIDE")
"${ROLLBACK_DC[@]}" config --quiet
"${ROLLBACK_DC[@]}" config > "$BACKUP/compose.rollback.resolved.yaml"
diff -u "$BACKUP/compose.resolved.yaml" "$BACKUP/compose.rollback.resolved.yaml" || true
# 安全保存重建当前命令所需的 Bash 变量；此文件含新 Redis 密码。
declare -p DC BACKUP OLD_TAG RESTORE_DB RESTORE_DATA_DIR RESTORE_NETWORK \
  RESTORE_REDIS_HOST RESTORE_REDIS_VOLUME RESTORE_REDIS_PASSWORD RESTORE_REDIS_IMAGE \
  ROLLBACK_OVERRIDE ROLLBACK_DC > "$BACKUP/rollback-session.sh"
chmod 600 "$BACKUP/rollback-session.sh" "$ROLLBACK_OVERRIDE" "$BACKUP/compose.rollback.resolved.yaml"
```

检查合并结果：原 PostgreSQL/Redis 镜像、数据卷、端口和应用原网络保持不变；应用只切换旧镜像、新库名、恢复数据目录及独立 Redis 连接。`/app/data` 应只有一个有效挂载，所有额外配置子挂载应指向正确副本。上例 Redis 不发布宿主机端口，使用刚发现的现有网络；应用沿用原网络即可访问唯一的恢复 Redis 别名。原 `depends_on.redis` 可能仍保留，因此后续按下面的顺序显式启动恢复 Redis，再用 `--no-deps` 启动应用。

### 8.4 先启动恢复 Redis，再验收旧应用

```bash
docker image inspect "$OLD_TAG" >/dev/null || docker load -i "$BACKUP/old-image.tar"
"${ROLLBACK_DC[@]}" up -d --no-deps --no-build --pull never redis-restore-r3
for attempt in {1..60}; do
  if "${ROLLBACK_DC[@]}" exec -T redis-restore-r3 redis-cli ping; then break; fi
  sleep 2
done
"${ROLLBACK_DC[@]}" exec -T redis-restore-r3 redis-cli ping
"${ROLLBACK_DC[@]}" up -d --no-deps --no-build --pull never sub2api
"${ROLLBACK_DC[@]}" logs --tail 150 sub2api
RESTORED_CID=$("${ROLLBACK_DC[@]}" ps -q sub2api)
docker inspect "$RESTORED_CID" --format '{{.Image}} {{range .Mounts}}{{println .Source "->" .Destination}}{{end}}'
docker image inspect "$OLD_TAG" --format '{{.Id}}'
"${ROLLBACK_DC[@]}" exec -T sub2api sh -ec 'printf "数据库=%s Redis=%s:%s DB=%s\n" "$DATABASE_DBNAME" "$REDIS_HOST" "$REDIS_PORT" "$REDIS_DB"; wget -q -O - "http://127.0.0.1:${SERVER_PORT:-8080}/health"'

# 查询真实连接，不只查看环境变量：应用连接应出现在 RESTORE_DB 上。
docker inspect "$RESTORED_CID" --format '{{range $name, $net := .NetworkSettings.Networks}}{{println $name $net.IPAddress}}{{end}}'
"${DC[@]}" exec -T postgres sh -ec 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres -c "SELECT datname, usename, client_addr, count(*) FROM pg_stat_activity WHERE backend_type = '\''client backend'\'' AND client_addr IS NOT NULL GROUP BY datname, usename, client_addr ORDER BY datname, client_addr;"'
```

核对两处镜像 ID 一致、挂载源是恢复目录、应用 IP 对应的数据库连接只指向 `RESTORE_DB`；如果仍连接原库，立即停止应用并检查配置来源，不能放开维护入口。若启动尚未完成，先检查日志并等待迁移/健康检查完成再重试健康请求，不要持续重建容器。旧版本自己的迁移器不应把新二开迁移当作「已应用」；新恢复库应保持 `final.dump` 对应的迁移基线。

按第 7 步验证原业务及快照余额/到期时间，核对故障期间订单和用量后，再恢复流量。回滚后所有维护命令改用 `ROLLBACK_DC`；新会话可读取自己创建且权限受保护的 `rollback-session.sh` 重建变量。原数据库、原应用卷、原 Redis 和故障快照继续保留，不执行 `down -v`、`dropdb` 或删除原目录。

## 9. 之后的更新方式

- 保存整个源码修复、回归测试、此次发布标签、镜像 ID 和审查记录。每次修改使用新标签，不复用本次标签覆盖镜像。
- 以后维护命令继续使用 `NEW_DC` 所含的全部文件。把这组参数记入服务器运维记录/部署脚本；新开 SSH 会话需要重建数组。若继续仅用原 Compose 文件执行 `up`，会重新切回原镜像配置。
- 禁用针对此应用的 Watchtower/面板自动镜像替换。后台「在线更新」当前仍以 `Wei-Shaw/sub2api` 官方发布为来源，不要用它更新二开部署，否则可能覆盖定制实现。
- 后续上游更新先合并源码、审查二开差异并跑回归，再构建自己的新镜像；不要直接追随官方 `latest`。
- 保留备份到业务验收和观察期结束；不要执行 `docker compose down -v`、删除数据库目录或自动清理尚需回滚的旧镜像。

## 参考依据

仓库依据：根目录 `Dockerfile`、`deploy/docker-compose.local.yml` / `docker-compose.yml`、`backend/internal/repository/migrations_runner.go`、`backend/internal/service/update_service.go` 及本轮升级集成测试。

命令语义核对于 2026-09-17：[Docker Compose 配置合并](https://docs.docker.com/compose/how-tos/multiple-compose-files/merge/)、[docker compose up](https://docs.docker.com/reference/cli/docker/compose/up/)、[PostgreSQL SQL dump/restore](https://www.postgresql.org/docs/current/backup-dump.html)。
