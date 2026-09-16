# 把现有 Docker Compose 部署切到自己的 GitHub 二开版

适用于面板/服务器当前通过 Docker Compose 运行官方 Sub2API 镜像的部署。本次代码在自己的仓库 [Ttt599536561/sub2api](https://github.com/Ttt599536561/sub2api/tree/feature/monthly-subscription-daily-reset)，分支为 `feature/monthly-subscription-daily-reset`，固定源码提交为 **`112875bd73c73fbc464f39f04f302772d292aab1`**。分支之后可能增加文档提交，构建仍使用这个已经确定的源码提交。

GitHub 地址是**源码仓库地址**，不能直接填进 Compose 的 `image:`。这里的流程是：服务器获取自己的源码 → 固定提交 → 用根目录 Dockerfile 构建本地镜像 → 给原 Compose 增加一个只修改应用镜像的覆盖文件。无需先发布 GHCR 镜像，也无需合并到 `main`。

**保留线上原来的全部 Compose 文件及顺序、项目名、环境变量、密钥、数据卷、数据库、Redis 和代理配置。不要用刚克隆仓库里的 `deploy/docker-compose*.yml` 覆盖线上文件。** 备份、副本演练和最终备份按 [完整升级与回滚指南](DOCKER_COMPOSE_CUSTOM_UPGRADE_2026-09-17.md) 的第 1、3、4、5 步执行；本页的源码构建替代该指南第 2 步的 tar 打包、上传和导入。

## 1. 先确认线上实际部署

以下全部是**服务器上的 Linux Bash** 命令。SSH 登录后，非 root 用户先执行 `sudo -i`；已是 root 则直接继续。逐段执行并查看结果，遇到错误停止；后续在同一 Bash 会话保留变量。

```bash
set -euo pipefail
umask 077
docker compose version
git --version
docker ps --format 'table {{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Ports}}'
read -r -p '输入线上 Sub2API 应用容器的 ID 或名称：' APP_CONTAINER
APP_CID=$(docker inspect --format '{{.Id}}' "$APP_CONTAINER")
APP_SERVICE=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$APP_CID")
PROJECT=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$APP_CID")
DEPLOY_DIR=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$APP_CID")
CONFIG_FILES=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$APP_CID")
printf '服务：%s\n项目：%s\n原部署目录：%s\n全部配置文件：%s\n' \
  "$APP_SERVICE" "$PROJECT" "$DEPLOY_DIR" "$CONFIG_FILES"
for value in "$APP_SERVICE" "$PROJECT" "$DEPLOY_DIR" "$CONFIG_FILES"; do
  test -n "$value" && test "$value" != '<no value>'
done
test -d "$DEPLOY_DIR"
cd "$DEPLOY_DIR"
IFS=',' read -r -a FILES <<< "$CONFIG_FILES"
DC=(docker compose --project-directory "$DEPLOY_DIR" -p "$PROJECT")
for f in "${FILES[@]}"; do test -f "$f"; DC+=(-f "$f"); done
# 若原部署使用额外 --env-file，按原顺序加入实际文件，例如：
# DC+=(--env-file /实际路径/prod.env)
```

容器标签用于发现原目录和 `-f` 文件，**不能保证还原面板使用的全部 `--env-file`、shell 导出变量或 profile 参数**。先从原面板/部署记录核对并补齐 `DC`；保留 `.env`、独立配置文件和密钥，不重新生成。标签为空、文件不存在或无法重建原配置时，先找回原部署参数，不能猜路径继续。

```bash
"${DC[@]}" config --quiet
"${DC[@]}" ps
test "$("${DC[@]}" ps -q "$APP_SERVICE")" = "$APP_CID"
docker ps --filter "label=com.docker.compose.project=$PROJECT" \
  --format 'table {{.ID}}\t{{.Names}}\t{{.Label "com.docker.compose.service"}}\t{{.Image}}'
OLD_IMAGE_ID=$(docker inspect --format '{{.Image}}' "$APP_CID")
docker image inspect "$OLD_IMAGE_ID" --format '旧镜像={{.Id}} 平台={{.Os}}/{{.Architecture}}'
docker inspect "$APP_CID" --format '{{range .Mounts}}{{println .Type .Source "->" .Destination}}{{end}}'
docker exec "$APP_CID" /app/sub2api -version
docker exec "$APP_CID" date
```

现在完成旧指南**第 1 步的剩余检查**：从上表确认数据库和 Redis 的实际服务名，取得 `PG_CID`、`REDIS_CID`、`PG_IMAGE_ID`、`REDIS_IMAGE_ID`，确认 PostgreSQL 主版本、应用实际连接的数据库及 `/app/data` 等持久化位置。不是本项目数据库的部署，应使用实际数据库对应的备份方式。线上版本若比本次上游基线 `0.2.5 / 881f32026` 更新，先核对兼容性，不能直接当作降级执行。

旧指南在**生产命令**中默认应用、数据库、Redis 服务名为 `sub2api`、`postgres`、`redis`。若本次发现的名称不同，后续引用旧指南时把这些生产服务名替换为实际名称；**旧指南第 4 步新建的演练项目仍使用其自定义的 `sub2api`、`postgres`、`redis` 名称，不随生产服务名更改**。本页的切换命令使用发现的 `$APP_SERVICE`。

## 2. 从自己的 GitHub 分支取固定源码

源码放在独立的 `/opt/sub2api-custom-src`；线上原部署仍在 `$DEPLOY_DIR`。克隆时保留下面的 `--branch`，不要省略后落到默认 `main`；不要加 `--depth 1`，需要保留分支历史来取得和核验固定源码提交。需要服务器可访问 GitHub、镜像仓库和构建依赖源，且有足够磁盘与内存完成构建。私有仓库使用已有的 Git 凭据，不把令牌写进命令或文档。

```bash
REPO_URL='https://github.com/Ttt599536561/sub2api.git'
BRANCH='feature/monthly-subscription-daily-reset'
RELEASE_COMMIT='112875bd73c73fbc464f39f04f302772d292aab1'
SOURCE_DIR='/opt/sub2api-custom-src'
test "$SOURCE_DIR" != "$DEPLOY_DIR"
test ! -L "$SOURCE_DIR"
if [ -e "$SOURCE_DIR" ]; then
  test -d "$SOURCE_DIR/.git"
  test "$(git -C "$SOURCE_DIR" rev-parse --show-toplevel)" = "$SOURCE_DIR"
  git -C "$SOURCE_DIR" remote get-url origin
  git -C "$SOURCE_DIR" status --short
  test "$(git -C "$SOURCE_DIR" remote get-url origin)" = "$REPO_URL"
  test -z "$(git -C "$SOURCE_DIR" status --porcelain=v1 --untracked-files=all)"
else
  git clone --single-branch --branch "$BRANCH" "$REPO_URL" "$SOURCE_DIR"
fi
git -C "$SOURCE_DIR" fetch --no-tags origin \
  "refs/heads/$BRANCH:refs/remotes/origin/$BRANCH"
[[ "$RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ ]]
git -C "$SOURCE_DIR" cat-file -e "${RELEASE_COMMIT}^{commit}"
git -C "$SOURCE_DIR" merge-base --is-ancestor "$RELEASE_COMMIT" "refs/remotes/origin/$BRANCH"
git -C "$SOURCE_DIR" checkout --detach "$RELEASE_COMMIT"
test "$(git -C "$SOURCE_DIR" rev-parse HEAD)" = "$RELEASE_COMMIT"
test -z "$(git -C "$SOURCE_DIR" status --porcelain=v1 --untracked-files=all)"
git -C "$SOURCE_DIR" show --no-patch --format=fuller HEAD
```

已有目录若来源不同、不是 Git 仓库或存在修改，上面的检查会停止。先保留并检查该目录；不要用 `reset --hard`、清理文件或覆盖目录来绕过。已有正确仓库若使用 SSH origin，先人工确认它确实是自己的同一仓库，再让 `REPO_URL` 与实际 origin 一致。此处核验提交存在且属于远端分支历史；不能只看分支名就构建随时变化的分支最新状态。[Git 提交祖先检查](https://git-scm.com/docs/git-merge-base)、[分离 HEAD](https://git-scm.com/docs/git-checkout)。

## 3. 构建服务器本机架构的镜像

构建的是应用镜像；正式切换尚未开始。每次使用新标签，并把提交写入版本信息和镜像标签。本例标签包含短 SHA 和 UTC 构建时间，避免下一次构建覆盖本次产物。

```bash
PLATFORM=$(docker version --format '{{.Server.Os}}/{{.Server.Arch}}')
case "$PLATFORM" in
  linux/amd64|linux/arm64) ;;
  *) printf '需单独验证的 Docker 平台：%s\n' "$PLATFORM" >&2; exit 1 ;;
esac
SHORT_COMMIT=${RELEASE_COMMIT:0:9}
BUILD_STAMP=$(date -u +%Y%m%d%H%M%S)
VERSION="0.2.5-custom.$SHORT_COMMIT"
CUSTOM_IMAGE="sub2api-custom:$SHORT_COMMIT-$BUILD_STAMP"
if docker image inspect "$CUSTOM_IMAGE" >/dev/null 2>&1; then
  printf '镜像标签已存在，请使用新的 BUILD_STAMP。\n' >&2
  exit 1
fi
docker build --platform "$PLATFORM" \
  --build-arg VERSION="$VERSION" \
  --build-arg COMMIT="$RELEASE_COMMIT" \
  --build-arg POSTGRES_IMAGE=postgres:18.1-alpine3.23 \
  --label org.opencontainers.image.source="$REPO_URL" \
  --label org.opencontainers.image.revision="$RELEASE_COMMIT" \
  -t "$CUSTOM_IMAGE" -f "$SOURCE_DIR/Dockerfile" "$SOURCE_DIR"
docker image inspect "$CUSTOM_IMAGE" --format '{{.Id}} {{.Os}}/{{.Architecture}}'
docker run --rm --pull never "$CUSTOM_IMAGE" -version
docker run --rm --pull never "$CUSTOM_IMAGE" pg_dump --version
export CUSTOM_IMAGE
printf '待发布镜像：%s\n源码提交：%s\n' "$CUSTOM_IMAGE" "$RELEASE_COMMIT"
```

`VERSION` 和 `COMMIT` 记录本次应用版本；镜像平台应与上面 Docker 服务端的平台一致。`POSTGRES_IMAGE=postgres:18.1-alpine3.23` **只为应用镜像提供 `pg_dump`、`psql` 和客户端库，不会升级线上 PostgreSQL 服务或数据卷**。线上数据库若高于 PostgreSQL 18，需匹配更高客户端并重新验证。本次已有 amd64 验证不能替代 ARM 服务器上的构建、运行及副本演练。[Docker 构建参数与平台](https://docs.docker.com/reference/cli/docker/buildx/build/)。

## 4. 完成备份、副本演练和最终备份

回到原目录，继续同一 Bash 会话。按旧指南依次完成下表后，才能执行下一节：

| 旧指南步骤 | 本次必须完成的工作 |
| --- | --- |
| 第 3 步 | 保留旧镜像 ID、唯一 `OLD_TAG` 和 `old-image.tar`；备份全部原配置、密钥及应用目录；导出 `rehearsal.dump`。保留 `STAMP`、`BACKUP` 等变量。 |
| 第 4 步 | 恢复到独立数据库、独立 Redis 和复制的应用数据；使用隔离网络演练迁移、原账号登录及日重置；完成后停止演练项目。 |
| 第 5 步 | 维护窗口先排空请求和后台写入，再停止全部应用副本；导出并核对 `final.dump`、`final-app-data`，完成异机备份。 |

```bash
cd "$DEPLOY_DIR"
export CUSTOM_IMAGE
```

旧指南演练 Compose 使用 `image: ${CUSTOM_IMAGE:?}`，必须保持为**本页刚构建的标签**；不要重新执行旧指南第 2 步把变量改回旧的 `r3` 镜像，也无需上传/导入 tar。原时区、JWT/TOTP 等密钥继续按旧指南传入演练副本；副本不能连接生产数据库、生产 Redis 或真实上游。原数据库和原挂载目录不能直接拿来演练。

在旧指南第 3 步创建 `$BACKUP` 后，把当前发布记录及 Bash 变量保存到该备份目录；新 SSH 会话需要先核对并读取自己保存的文件，才能继续使用数组。此文件不代替原面板/环境配置的备份。

```bash
printf 'repository=%s\nbranch=%s\ncommit=%s\nimage=%s\nplatform=%s\n' \
  "$REPO_URL" "$BRANCH" "$RELEASE_COMMIT" "$CUSTOM_IMAGE" "$PLATFORM" > "$BACKUP/release.txt"
docker image inspect "$CUSTOM_IMAGE" --format '{{.Id}}' > "$BACKUP/custom-image-id.txt"
declare -p DC FILES APP_CID APP_SERVICE PROJECT DEPLOY_DIR CONFIG_FILES \
  OLD_IMAGE_ID OLD_TAG STAMP BACKUP PG_CID REDIS_CID PG_IMAGE_ID REDIS_IMAGE_ID \
  REPO_URL BRANCH RELEASE_COMMIT SOURCE_DIR CUSTOM_IMAGE PLATFORM > "$BACKUP/github-release-session.sh"
chmod 600 "$BACKUP/github-release-session.sh"
```

演练失败、迁移校验失败或最终备份不完整时停止发布，不能靠重命名迁移、修改 checksum 或跳过备份继续。

## 5. 在原 Compose 上只覆盖应用镜像

**本节开始前，上一节要求的最终备份已完成，业务仍在维护状态。** 新覆盖文件放在原部署目录，不修改原文件；`NEW_DC` 包含原 `DC` 的全部参数和最后追加的覆盖文件。[Compose 按文件顺序合并配置](https://docs.docker.com/compose/how-tos/multiple-compose-files/merge/)。

```bash
cd "$DEPLOY_DIR"
test -s "$BACKUP/final.dump"
test -s "$BACKUP/old-image.tar"
OVERRIDE="$DEPLOY_DIR/compose.custom-$STAMP.yaml"
test ! -e "$OVERRIDE"
printf 'services:\n  %s:\n    image: %s\n    pull_policy: never\n' \
  "$APP_SERVICE" "$CUSTOM_IMAGE" > "$OVERRIDE"
NEW_DC=("${DC[@]}" -f "$OVERRIDE")
"${NEW_DC[@]}" config --quiet
"${NEW_DC[@]}" config > "$BACKUP/compose.custom.resolved.yaml"
diff -u "$BACKUP/compose.resolved.yaml" "$BACKUP/compose.custom.resolved.yaml" || true
```

人工查看差异，应只有应用 `image`、`pull_policy` 以及 Compose 格式变化。数据库/Redis 镜像、环境、卷、网络和端口都应保留。输出可能含密码，不要贴到公开渠道。确认后执行：

```bash
"${NEW_DC[@]}" up -d --no-deps --no-build --pull never "$APP_SERVICE"
"${NEW_DC[@]}" logs --tail 200 "$APP_SERVICE"
NEW_CID=$("${NEW_DC[@]}" ps -q "$APP_SERVICE")
test -n "$NEW_CID"
test "$(docker inspect --format '{{.Image}}' "$NEW_CID")" = \
  "$(docker image inspect --format '{{.Id}}' "$CUSTOM_IMAGE")"
docker inspect "$NEW_CID" --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}'
"${NEW_DC[@]}" exec -T "$APP_SERVICE" /app/sub2api -version
"${NEW_DC[@]}" exec -T "$APP_SERVICE" sh -ec \
  'wget -q -O - "http://127.0.0.1:${SERVER_PORT:-8080}/health"'
declare -p NEW_DC OVERRIDE NEW_CID >> "$BACKUP/github-release-session.sh"
```

`--no-deps` 限定应用服务，`--no-build --pull never` 使用本地已验证镜像；不执行全项目更新。日志不能出现迁移失败、checksum mismatch、缺字段或持续重启。首次迁移耗时较长时查看日志和数据库状态，不反复重建容器。健康检查和镜像 ID 校验成功后，再做业务验收。[docker compose up 参数](https://docs.docker.com/reference/cli/docker/compose/up/)。

## 6. 验证原业务，再开启日额度重置

1. 保持维护入口，用原管理员和普通账号登录原域名，核对原用户、API Key、余额、分组、上游账号及订阅到期时间和用量。
2. 用**原 API Key** 按实际使用的协议发一笔小额真实请求，检查响应、流式结束、使用记录和扣费；随后验证实际启用的支付、OAuth 等原有业务，再逐步恢复流量。
3. 管理员在「分组」编辑需要启用的**订阅分组**：设置有限正数日额度，打开「允许订阅日额度重置」。已有有效月卡同样适用，不必重买。
4. 用户在「我的订阅」手动重置或自行开启自动重置。管理员许可不会自动打开用户开关；每次成功扣 24 小时，扣后订阅仍须有效，只清日用量，不清周/月用量，也不能绕过周/月额度限制。保留线上原 `TZ`。

观察错误率、计费、订阅到期及重置事件记录；详细验收和限制见旧指南第 7 步。

## 7. 回滚及今后的更新

需要回滚时保持维护状态，使用旧指南**第 8 步完整流程**。本页保留了它依赖的 `DC`、`NEW_DC`、`APP_CID`、`BACKUP`、`OLD_TAG`、`STAMP`、`DEPLOY_DIR`、`PG_CID` 等变量；生产服务名不同的地方仍按第 1 节替换。

尚未迁移、未产生新写入时才可按条件只切回旧镜像。已迁移或已发生重置/新账务时，不能假定旧程序兼容：按旧指南保留故障库，评估恢复 `final.dump`、`final-app-data` 和旧镜像；快照恢复会丢失备份后的写入，需要核对账务。**不执行 `down -v`，不删除原数据库、Redis 卷和原应用目录。**

以后运维命令继续使用 `NEW_DC` 对应的全部配置文件和参数。把这组参数同步到面板的实际部署配置/维护记录，关闭针对该应用的自动换镜像；否则面板再次按旧 Compose 部署可能切回官方镜像。新 Bash 会话中可读取自己保存的 `github-release-session.sh`，并恢复原有额外环境来源。

下次更新先在自己的开发分支合并、审查和测试，再推送。将本页 `RELEASE_COMMIT` 换成新的完整 SHA，重新核验分支历史，构建**新的唯一标签**，重新做备份与副本演练，再追加新的镜像覆盖文件。前一次的覆盖文件作为原部署配置保留在 `DC` 中，新的文件放在最后，便于追溯和回滚。

**不要使用管理后台的官方「在线更新」或直接追随官方 `latest` 来更新二开部署**；当前更新器仍以 `Wei-Shaw/sub2api` 官方发布为来源，可能覆盖定制实现。后续更新继续走自己的源码提交与镜像发布流程。
