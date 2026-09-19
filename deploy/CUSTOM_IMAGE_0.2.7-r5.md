# Sub2API 0.2.7-r5 更新说明

- 目标镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.7-r5`
- 固定源码：`1f428c0242d3ff9ab6f958f23135c00bbc6ad05c`，包含最新福利中心功能及上游 v0.2.7 合并。
- [GitHub Actions 发布记录（成功）](https://github.com/Ttt599536561/sub2api/actions/runs/35460991852)
- 发布状态：**2026-09-20 已发布并验证**，支持 `linux/amd64`、`linux/arm64`；匿名读取镜像清单返回 HTTP 200，无需 GitHub 登录即可公开拉取。
- 多架构镜像 digest：`sha256:28c95db7541d6794f5eb67d7b46cbdf0170459b5016f87d0b7bc176a58c73070`。

两个原生架构 runner 均完成源码构建，随后按 digest 拉取并验证程序版本、源码 revision、PostgreSQL 客户端、初始化状态接口和内嵌前端。发布后的统一标签已在本机再次拉取，amd64 程序输出 `0.2.7-r5` 及预期完整源码 SHA。[源码完整 CI](https://github.com/Ttt599536561/sub2api/actions/runs/35460648316) 与[安全扫描](https://github.com/Ttt599536561/sub2api/actions/runs/35460648315)均成功。

需要固定不可漂移引用时，可将下文镜像替换为：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:28c95db7541d6794f5eb67d7b46cbdf0170459b5016f87d0b7bc176a58c73070
```

下面命令用于服务器上的 **Linux Bash**；服务器本机源码构建方式见最后一节。本次没有连接或修改生产服务器。

## 更新前

沿用线上原部署目录、Compose 项目名、全部 `-f` 文件及顺序、`--env-file` 和其他原部署参数。保留 `.env`、JWT/TOTP 密钥、配置挂载、数据卷、PostgreSQL、Redis、端口及代理设置；不要用仓库中的 Compose 文件覆盖线上文件。

相对之前 r4，新增 `239_welfare_center.sql`，程序启动会自动迁移数据库，福利功能默认关闭。更新前排空请求及后台写入，备份实际应用数据库、`/app/data`、原配置和密钥，并保留旧镜像。生产数据库建议先做副本恢复及迁移演练；详细步骤见[升级与回滚指南](../docs/DOCKER_COMPOSE_CUSTOM_UPGRADE_2026-09-17.md)，其中旧镜像标签和源码提交不适用于本次发布。已迁移或产生新业务写入后，不能假定只换回旧镜像就完成回滚。

## 使用已发布镜像更新

在原部署配置中，将**最终生效的应用服务** `image:` 改为：

```yaml
image: ghcr.io/ttt599536561/sub2api-custom:0.2.7-r5
```

如果使用面板或多个 Compose 文件，修改面板保存的配置或最后生效的覆盖文件，确保今后重建也使用新镜像。以下以单文件 `docker-compose.yml`、应用服务名 `sub2api` 为例；`DC` 必须替换为原部署的完整命令参数，服务名也以实际为准。

```bash
set -euo pipefail
cd /原来的部署目录
DC=(docker compose -f docker-compose.yml)
APP_SERVICE=sub2api
IMAGE=ghcr.io/ttt599536561/sub2api-custom:0.2.7-r5

"${DC[@]}" config --quiet
"${DC[@]}" pull --policy always "$APP_SERVICE"
"${DC[@]}" up -d --no-deps --no-build --force-recreate --pull never "$APP_SERVICE"
"${DC[@]}" logs --tail 100 "$APP_SERVICE"

APP_CID=$("${DC[@]}" ps -q "$APP_SERVICE")
test -n "$APP_CID"
test "$(docker inspect "$APP_CID" --format '{{.Image}}')" = \
  "$(docker image inspect "$IMAGE" --format '{{.Id}}')"
"${DC[@]}" exec -T "$APP_SERVICE" /app/sub2api -version
"${DC[@]}" exec -T "$APP_SERVICE" sh -ec \
  'wget -q -O - "http://127.0.0.1:${SERVER_PORT:-8080}/health"'
```

版本输出应包含 `0.2.7-r5` 和完整源码提交 `1f428c0242d3ff9ab6f958f23135c00bbc6ad05c`。首次迁移可能需要时间；若健康请求尚未成功，先检查日志，等待启动完成后重试验证，不反复重建。确认无迁移失败或持续重启，并验收原账号、API Key、余额、订阅及一次真实请求后恢复流量。

这些命令只重建应用服务。不要执行 `down -v`、删除数据库目录、清空 Redis 或顺带升级数据库主版本；不要使用后台的官方在线更新替换二开镜像。

## 可选：服务器从固定源码重新构建

此方式需要服务器可访问 GitHub、基础镜像及构建依赖源，并有足够内存和磁盘。源码克隆到新建的独立目录，不在现有线上目录切换分支或覆盖文件。构建只生成镜像，之后仍按上面的原 Compose 配置切换应用。

```bash
set -euo pipefail
SOURCE_COMMIT=1f428c0242d3ff9ab6f958f23135c00bbc6ad05c
SOURCE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/sub2api-r5.XXXXXX")
git clone --no-checkout https://github.com/Ttt599536561/sub2api.git "$SOURCE_DIR"
git -C "$SOURCE_DIR" fetch origin "$SOURCE_COMMIT"
git -C "$SOURCE_DIR" checkout --detach "$SOURCE_COMMIT"
test "$(git -C "$SOURCE_DIR" rev-parse HEAD)" = "$SOURCE_COMMIT"

PLATFORM=$(docker version --format '{{.Server.Os}}/{{.Server.Arch}}')
case "$PLATFORM" in
  linux/amd64|linux/arm64) ;;
  *) printf '需要单独验证的平台：%s\n' "$PLATFORM" >&2; exit 1 ;;
esac
LOCAL_IMAGE="sub2api-custom:0.2.7-r5-local-$(date -u +%Y%m%d%H%M%S)"
docker build --platform "$PLATFORM" \
  --build-arg VERSION=0.2.7-r5 \
  --build-arg COMMIT="$SOURCE_COMMIT" \
  --label org.opencontainers.image.source=https://github.com/Ttt599536561/sub2api \
  --label org.opencontainers.image.revision="$SOURCE_COMMIT" \
  --label org.opencontainers.image.version=0.2.7-r5 \
  -t "$LOCAL_IMAGE" -f "$SOURCE_DIR/Dockerfile" "$SOURCE_DIR"
docker image inspect "$LOCAL_IMAGE" --format '{{.Id}} {{.Os}}/{{.Architecture}}'
docker run --rm --pull never "$LOCAL_IMAGE" -version
docker run --rm --pull never "$LOCAL_IMAGE" sh -ec \
  'pg_dump --version; psql --version; test -d /app/resources'
printf '将原 Compose 应用 image 改为：%s\n' "$LOCAL_IMAGE"
```

构建及验证成功后，将原 Compose 最终生效的应用 `image:` 改为输出的本地标签。完成前述备份后，在原部署目录、沿用原 `DC` 参数执行；本地镜像无需 `pull`：

```bash
cd /原来的部署目录
# 与上文相同：按线上实际情况补齐全部 Compose 参数及服务名。
DC=(docker compose -f docker-compose.yml)
APP_SERVICE=sub2api
"${DC[@]}" config --quiet
"${DC[@]}" up -d --no-deps --no-build --force-recreate --pull never "$APP_SERVICE"
IMAGE="$LOCAL_IMAGE"
```

随后重复上一节从查看日志到镜像 ID、版本、健康及业务的验证。本机构建只验证当前服务器架构，不等同于 GHCR 最终多架构镜像；镜像内 PostgreSQL 18 客户端不改变线上数据库服务。
