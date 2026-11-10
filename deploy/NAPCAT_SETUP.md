# NapCat 与 Sub2 同机独立容器接入

核对日期：2026-11-11。此目录提供部署文件与初始化脚本；本轮未连接服务器、登录 QQ 或发送真实消息。执行部署需要已有服务器连接权限，以及包含本次邀请功能的 Sub2 应用镜像。现有生产镜像、数据库和 Redis 的发布/升级流程仍由原部署管理。

本功能已随 Sub2 `0.2.20-r2` 发布；首次部署可先读[简明步骤](NAPCAT_QUICKSTART.md)。Sub2镜像地址为 `ghcr.io/ttt599536561/sub2api-custom:0.2.20-r2`。

## 适用的 Compose

叠加 `docker-compose.napcat.yml` 到服务器**当前使用的同一份**主 Compose、同一个项目名。仓库的 `docker-compose.yml` 与 `docker-compose.local.yml` 均有 `sub2api` 服务和 `sub2api-network`，可用作基底；不要为接 NapCat 从本地目录模式切换成命名卷模式，也不要重建一套生产数据库。

新增 `napcat-network` 只连接 Sub2 和 NapCat；Sub2 保留原数据库网络。NapCat 需要访问 QQ 互联网服务，故该桥接网络不设置 `internal: true`；3000/6099 只在容器网络中开放，宿主机没有端口映射。后台通过 Sub2 已有管理员认证调用 NapCat，浏览器不直连 NapCat。

## 已验证的协议与镜像

- NapCatQQ `v4.18.42` 对应源码提交 `9335843246e3b7a17944861f3ac8731a259732c2`。[官方发行](https://github.com/NapNeko/NapCatQQ/releases/tag/v4.18.42)
- 官方部署文档指向 `mlikiowa/napcat-docker`。[官方 Docker 仓库](https://github.com/NapNeko/NapCat-Docker)、[官方 Docker 启动说明](https://napneko.github.io/guide/boot/Shell#docker)
- 2026-11-10 读取 [Docker Hub 标签 API](https://hub.docker.com/v2/repositories/mlikiowa/napcat-docker/tags/v4.18.42) 证实该标签存在，更新时间 `2026-10-30T07:09:30.964605Z`。多架构 index：`sha256:696e9574a0f6fb2f86cbecba8a32048f2a5823117dc9b13c6c02637ad890fa8b`；Linux amd64 manifest：`sha256:6c1b1a2736588060f484ea273a190309bdeca32c15d6a9c61325d3499ee091fa`；Linux arm64 manifest：`sha256:149fae81e2ccec10e5d1be2f9f86a1a2d6e1d5c70691f892dce00b7328f2ca72`。Compose 固定标签和 index digest，不追随 `latest`。
- 持久目录沿用官方容器约定：`/app/.config/QQ`、`/app/napcat/config`、`/app/napcat/plugins`。[官方 entrypoint](https://github.com/NapNeko/NapCat-Docker/blob/main/entrypoint.sh)
- OneBot 在账号登录后优先加载 `onebot11_<QQ>.json`，仅在该文件不存在时才由 `onebot11.json` 派生。初始化脚本同时写入正确的两份文件。[官方配置加载器](https://github.com/NapNeko/NapCatQQ/blob/9335843246e3b7a17944861f3ac8731a259732c2/packages/napcat-core/helper/config-base.ts)

## 首次初始化

在服务器部署目录放置本 overlay、`napcat/init.py` 和 `napcat/.gitignore`。主 Compose 与本 overlay 放在同一目录，`napcat/` 也在该目录内。需要 Python 3 和支持多个 `-f` 的 Docker Compose v2。命令中的 QQ 号必须是即将扫码登录的机器人账号，域名必须是实际 Sub2 入口。

```bash
python3 napcat/init.py --qq 123456789 --public-url https://api.example.com
```

该命令本地生成四个彼此独立的 32 字节随机值：WebUI token、OneBot API token、HTTP 事件签名 secret，以及 `QQ_INVITE_ENCRYPTION_KEY`。后者只用于邀请配置的数据库加密，不更改 `QQ_IMAGE_ENCRYPTION_KEY`。标准输出只有文件路径，不输出任何密钥；Linux 文件权限为 0600、数据目录为 0700。不要使用 `set -x`，不要把私有配置或完整 `docker inspect` / `docker compose config` 输出贴入聊天或日志。

生成的 `napcat/sub2api.env` 仅注入 Sub2 后端，配置键如下：

| 配置 | 初始化值/用途 |
| --- | --- |
| `QQ_INVITE_ENCRYPTION_KEY` | 新的稳定加密密钥；必须与数据库一起备份 |
| `QQ_INVITE_ALLOWED_HOSTS` | `napcat` |
| `QQ_INVITE_ONEBOT_URL` | `http://napcat:3000` |
| `QQ_INVITE_WEBUI_URL` | `http://napcat:6099` |
| `QQ_INVITE_ONEBOT_TOKEN` | OneBot HTTP API 独立鉴权 |
| `QQ_INVITE_WEBUI_TOKEN` | WebUI 登录所需原始 token，仅后端使用 |
| `QQ_INVITE_EVENT_SECRET` | HTTP 事件原始请求体 HMAC-SHA1 签名 |
| `QQ_INVITE_BOT_QQ` | 指定机器人账号；扫码账号必须匹配 |
| `QQ_INVITE_PUBLIC_BASE_URL` | 实际 Sub2 HTTPS origin |

后端仅在邀请配置尚未初始化时把这些值写入加密配置，且默认不启用邀请奖励；已有配置不会被环境变量覆盖。重复运行初始化脚本遇到配置、会话或凭据时会直接拒绝，防止重生成密钥导致历史密文不可读。若因磁盘/权限异常只生成了部分新文件，先检查该目录，不能直接删除已在使用的会话或密钥后重跑。

## 启动与扫码

下面以当前主文件为 `docker-compose.local.yml` 举例。如果生产原来使用 `-p` 或其他主文件，所有命令保持相同参数。先确认主文件的 Sub2 `image` 是包含本次功能的已发布版本；本 overlay 不设置或更新应用镜像。

```bash
# 只检查配置，不把解析后的密钥写到终端。
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml config --quiet
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml pull napcat
# 复用正在运行的数据库/Redis，不重建依赖。
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml up -d --no-deps napcat sub2api
docker compose -f docker-compose.local.yml -f docker-compose.napcat.yml ps napcat sub2api
```

登录 Sub2 管理后台，在 QQ 邀请设置中获取二维码，以指定 QQ 账号扫码，确认状态显示该账号在线。二维码在浏览器本地编码，Sub2 不获取外部二维码图片。更换/刷新二维码和重启登录均通过管理员接口；不公开 WebUI、QR 接口或 token。QQ 新设备验证可能需要手机额外确认；此时只展示真实状态，不能把获取二维码成功视为 QQ 已在线。

在启用奖励前配置允许的群、阈值与奖励，并按后台流程初始化群成员基线。机器人须在目标群内，并具有观察入群信息所需权限；需要审批的群入群事件不一定能识别真实邀请人。只把 `group_increase` / `sub_type=invite` 且明确邀请者的事件用于自动归因，未知或仅有管理员批准的信息进入审核；`get_group_system_msg.join_requests[].invitor_uin` 实际可能是申请者，不能作为自动发奖证据。

初始化后的 OneBot 使用 HTTP server 3000，独立 API Bearer token；HTTP client 将事件发送到 `http://sub2api:8080/api/v1/qq-invite/events`，`token` 用于 `X-Signature: sha1=<HMAC>`，不是回调 Bearer token。Sub2 验签原始请求体并校验账号/群；不会因普通消息回调自动回复。不要把个人 OneBot 与既有官方 QQ 生图机器人配置混用。

## 日志、备份和维护

官方 WebUI 启动代码会打印 token 及带 token 的 URL。初始化为登录前的 `napcat.json` 和账号专属 `napcat_<QQ>.json` 同时关闭 console/file 日志，Compose 也关闭 Docker 日志驱动，避免持久化登录凭据、二维码与消息内容。默认诊断使用 Sub2 的脱敏状态/错误码和容器 health 状态；不要为了找二维码打开原始日志。无 Docker socket、无 privileged、无对主机的进程管理接口；后台显式重启调用 NapCat 自身 API。

备份 `napcat/sub2api.env`、`napcat/config/`、`napcat/qq/`、`napcat/plugins/` 和 Sub2 数据库，按凭据文件限制访问。容器首次启动后官方 entrypoint 会将挂载目录所有者改为 UID/GID 1000；Compose 操作用户仍需有读取后端 env 文件的权限。不要删除会话目录来解决一般离线，不要用初始化脚本旋转现有密钥。启用 WebUI 2FA 会使后台自动认证返回需要人工配置的错误，本集成不存储或自动生成 2FA 代码。

更换账号时先停止奖励和 NapCat，备份原配置，再按新账号建立专属配置并同步后台的 bot QQ；不能只改扫码账号。升级镜像需要重新核对协议、真实入群归因与故障行为，并固定新的 digest。发送超时或回包不完整属于投递结果未知，不能自动再次发奖消息。

## 本地验证边界

已用假的 HTTP 服务验证协议、认证缓存、重定向/大小限制、敏感错误脱敏和发送不确定性；初始化单元测试覆盖各文件凭据一致、账号配置路径、已有数据保护与日志无秘密。未在此任务中拉取运行 QQ 镜像或执行真实扫码、入群、私聊、群消息。部署后的真实账号验证应由有权限的管理员按后台流程完成。
