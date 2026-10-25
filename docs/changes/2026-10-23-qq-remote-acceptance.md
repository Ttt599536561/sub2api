# QQ 公网隔离验收部署

日期：2026-10-23（Asia/Shanghai）。用户授权在现有服务器新增独立 HTTPS 验收站。入口为 **https://qq-test.tangguo.xin**；正式站仍为 https://api.tangguo.xin。本文记录验收部署，不是正式镜像发布或生产升级。2026-10-23北京时间21:44～21:45复核两个域名严格TLS与health均通过；新站已开放应用入口，生产资源和配置保持原样。

## 同日 WebSocket 更新完成（后续阶段，未正式发布）

用户随后要求保留 WebSocket 并补齐长连接，只更新本独立测试站。已部署版本 **`0.2.17-qq-ws.742bbe8031ba`**，镜像 `sub2-qq-test:ws-742bbe8031ba`，运行二进制 SHA256 `c8a6b292d8396c83af1377d7c7dddfa8247dd301eb34196ae6b68f0168069149` 与冻结构建产物一致。后端 226 + 25 个测试 pass 事件均无失败/跳过，前端 101 项、四包 vet、前后端构建及独立审查修正复核通过，无新增迁移；全部身份及验证边界见[WebSocket 实现与部署记录](2026-10-23-qq-websocket.md)。本文件下方原版本、无 QQ 配置和首次合规结果保留为首次部署历史。

北京时间 22:26 更新前只读核对到用户已有 2 版 QQ 配置、机器人凭据已配置、生图关闭且无生图 Key；仍为 1 用户，余额、订单、冻结及授权均 0。原备份 `/srv/sub2-qq-test/backups/websocket-20261023-142615` 保留；临停前新增 `/srv/sub2-qq-test/backups/ws-switch-20261023-145124`，dump 619,183 字节及原 Compose 已保存、`pg_restore --list` 通过，未恢复或下载。只重建测试应用；专用 stack stop/start 短暂停启测试 PG/Redis，保留原容器和数据，旧应用镜像保留回退。

私有管理员登录及管理读取均 HTTP 200，原 423 门禁已由用户自行通过，本轮未代确认。模式通过已有管理员 API 携带当前 ID 修改：Webhook → WebSocket，配置 ID 2→3，除 ID/接收方式外返回字段全部不变；凭据保留，生图仍关闭、生图 Key 未配置。

真实连接北京时间 **22:51:42.701949712** 为 READY/connected，**22:52:23.740572815** 收到首个心跳 ACK，应用 health 200。没有发出 QQ 消息、邮件或付费生图，未进行群身份/媒体/支付链路联调；测试域名隔离不等于 QQ 平台沙箱。22:55:15 复核仍维持该连接，心跳已推进至 22:55:08.742063601，网关会话锁持有者为 1，迁移仍 304。

部署后最终核对已完成：生产三个容器的指定身份/运行字段、三份配置 stat、Nginx 哈希及两域证书指纹不变；测试 PG/Redis 容器 ID、app config/.installed 哈希、settings/orders/grants 摘要及用户数/余额/业务计数均保留。QQ 旧配置 1/2 保留，配置 2/3 的两类凭据 ciphertext 和除接收模式外 JSON 相等。用户整行摘要因登录活动改变，已对照临停备份逐列确认仅 updated_at、last_login_at、last_active_at 变化，没有声称用户整行哈希不变。

22:57:20 独立公网链路核对两站严格 TLS 1.3、证书链/主机名和 health 200 均通过；12 份 QQ/福利静态资源返回 200，SHA256 与构建目录对应文件一致。匿名连接状态 401、缺少媒体 token 404；HTTP 回调 503 当前是因为已选择 WebSocket，不再是初始无 QQ 配置。本轮浏览器库存为空，未执行新版视觉 smoke、控制台和移动端验证，旧版浏览器记录不作为新版证据。最终安全摘要在 `output/qq-websocket-20261023`，关键结论归档于上述 WebSocket 记录。

## 入口、账号与下一步

- 登录页：https://qq-test.tangguo.xin/login。新站登录专用文件为 `E:\sub2二次开发项目\output\qq-test-deploy-20261023\private\login.json`，只用于此验收站；账号密码不写入仓库或聊天。数据库、Redis、JWT、QQ 加密主密钥等另在服务器私有目录保存，没有随登录文件交付。
- 初始注册关闭，全部注册来源的默认余额为 0。新站仅有 1 个独立测试管理员；没有复制正式站用户、余额、上游账户、SMTP、QQ 配置或任何生产密钥。
- 登录后保留既有“部署与运营合规确认”。初始化检查返回 HTTP 423 / `ADMIN_COMPLIANCE_ACK_REQUIRED`，确认记录为 0；需用户本人阅读并决定，部署没有代确认或绕过。
- 用户完成确认后可进入“福利中心 → QQ 群生图”（`/admin/welfare`），配置测试机器人的 AppID / App Secret，先保持生图关闭。回调 URL 为 `https://qq-test.tangguo.xin/api/v1/qq-image/events`；官方媒体拉取入口为 `https://qq-test.tangguo.xin/api/v1/qq-image/media`。首次部署时未配置真实 QQ 凭据，因此当时公网可达不代表 QQ 平台验签、挑战、权限或消息发送已经联调通过。
- 群识别可在生图关闭、无生图 Key 时使用：管理员开启 5 分钟识别，在测试群实际 @ 机器人并发送 `/识别群` 加本次口令，再回后台核对候选并明确确认。用的是官方 `group_openid`，不是普通数字群号；识别不发送 QQ 回复、不创建订单或资金变动。详细步骤见[本地阶段说明](2026-10-23-qq-local-acceptance.md#https入口接好后识别测试群)。

原本机 `http://127.0.0.1:18089`、PG 55449、Redis 16389 和 `.cache/qq-image-acceptance-20261023/private.json` 均保留。本轮没有读取、迁移、重置其私有配置或用户操作；它与公网新站是两个独立数据库和账号体系。原[本地验收记录](2026-10-23-qq-local-acceptance.md)保留其初始化时点。

## 源码与部署身份

工作树为 `E:\sub2二次开发项目\.worktrees\qq-image-generation-20261023`，分支 `codex/qq-image-generation-20261023`，HEAD / 开发基线为 `c9a9209ea79163dbbf1fb20b5db2dc67c6b15394`。QQ 功能及迁移 248、249 仍是工作区未提交修改；该 HEAD 不等于包含新功能的提交。

| 项目 | 实际身份 |
| --- | --- |
| 验收版本 | `0.2.17-qq-acceptance.55406431e342`，BuildType `qq-acceptance` |
| 源码快照 SHA256 | `55406431e342027c225dd73e090096832968a86de98b22ba82a32bba113c47f9`，4,783 文件 |
| 前端 dist SHA256 | `2aa36855140f14fd95274a39863989ab4e7c7aeb8b840e0f5080502a41950585`，652 文件 |
| Linux 二进制 SHA256 | `d9e88379bd45e80d12012981a29c656242c2c892b8c3ce729c334a670dfbbf22`，145,690,784 字节 |
| 上传包 SHA256 | `ef448061dd5abc6ae0ecfbb61ca1e99799376d382bbe1787d82d371f48d647a0`，43,450,855 字节；仅含二进制和 manifest |
| 构建 | Go 1.27.3，linux/amd64，CGO=0，embed 前端，静态 ELF 无动态解释器 |
| 远端私有镜像 | `sub2-qq-test:55406431e342`，仅存此服务器 |
| 运行目录 | `/srv/sub2-qq-test`；构建文件在 `releases/55406431e342` |

构建前后源码和 dist 快照一致；二进制内逐字节核对迁移 248 / 249、当前 index 和群识别前端块。复用服务器已有 `0.2.17-r4` 镜像的运行资源，覆盖为上述已核验当前二进制；没有重新使用 r4 的旧应用程序。资源目录与 r4 相容，运行时 `--version` 和二进制 SHA 均已核对。部署后再次核对4,551个backend/frontend源码文件，与构建快照全部一致。源码快照先于本文等部署记录更新，后续文档修改不改变已上传产物身份。

没有 Git 提交、合入主目录、推送、公开镜像发布或生产应用升级。正式发布来源仍为 `76ff7c88a` / `0.2.17-r4`，该发布镜像不包含 QQ 功能。不得将验收版本或未提交快照当作正式发布 SHA。

## 隔离与资源边界

服务器为 `49.51.35.163`。新 Compose project 为 `sub2-qq-test`，使用独立网络 `sub2-qq-test-net`（`172.31.249.0/28`）、独立数据目录和新生成凭据。正式站已有容器及其目录没有纳入测试 Compose。

| 资源 | 配置与限额 |
| --- | --- |
| 应用 | `sub2-qq-test-app`；只发布 `127.0.0.1:21089 → 8080`；CPU 0.5、内存 768 MiB、PID 128；非 root、只读根文件系统、64 MiB `/tmp` |
| PostgreSQL | `sub2-qq-test-postgres`，缓存 postgres:18-alpine / 实际 18.3；无宿主端口；CPU 0.35、内存 384 MiB、PID 96；独立 qq_test 数据库/用户 |
| Redis | `sub2-qq-test-redis`，缓存 redis:8-alpine / 实际 8.6.2；无宿主端口；CPU 0.15、内存 128 MiB、PID 32；64 MiB maxmemory / noeviction、独立认证与 AOF |
| 合计 | CPU 1 核、内存 1,280 MiB、PID 256；容器日志各 10 MiB × 3，共至多约 90 MiB |
| 持久数据 | `/srv/sub2-qq-test/data.ext4`，2 GiB 固定大小 ext4 文件，挂载到 `/srv/sub2-qq-test/data`；应用、PG、Redis、Nginx 请求体及代理临时文件都在此文件系统内 |
| 启动顺序 | `srv-sub2\x2dqq\x2dtest-data.mount` → `sub2-qq-test.service`；服务启动前检查挂载点和固定 UUID；三个容器均为 `restart=no` |

2 GiB 是数据卷硬上限，不是服务器全部新增文件的总上限；镜像、上传包和受限 Docker 日志另在宿主磁盘上。初始服务器约有 46 GiB 可用磁盘和 4.1 GiB 可用内存，未扩大生产资源或更改宿主全局 sysctl。Redis 的宿主 overcommit 提示保留，未为测试修改全局内存策略。

挂载 UUID 为 `e1ecc2a0-5140-47d4-9b16-64ad7b3db968`；`/srv/sub2-qq-test/operations/check-mount.sh` 在挂载缺失或 UUID 不符时拒绝启动。独立应用服务依赖 Docker 和此 mount unit；Docker 自身不自动启动测试容器，防止先写入空的底层目录。没有修改全局 Docker unit、重启 Docker daemon 或重启宿主。首次启动因默认 Redis 最小空闲连接 128 大于测试连接池 8 而退出；只把测试应用环境设为 `REDIS_MIN_IDLE_CONNS=2`、保留 pool=8 后恢复，已初始化的数据和管理员均沿用，没有更改产品业务源码。

Nginx 仅新增测试域名站点，代理到 loopback 21089；固定 Host 和 HTTPS 转发信息，覆盖客户端传入的 X-Forwarded-For。应用只信任测试 Docker 网关 `172.31.249.1/32`，API Key ACL 不信任转发 IP。入口支持长请求与 WebSocket/SSE、32 MiB 请求体、禁用响应缓存；没有会阻断 QQ 回调的 Basic Auth。测试站 access log 关闭、error log 指向 `/dev/null`；请求体/代理临时目录归入上述配额，避免带 token 的 query、Referer 或请求体被写入公共日志。

## 证书与续期

独立证书仅含 SAN `qq-test.tangguo.xin`，Issuer 为 Let's Encrypt YE2，过期时间为 **2027-01-21 12:16:03 UTC / 北京时间 20:16:03**。证书链、主机名及 TLS 1.3 已严格验证，没有关闭证书校验。HTTP 返回 308 跳转 HTTPS。正式站证书未更换。

复用现有 ACME 账号，没有代接受新服务条款。独立 webroot 为 `/var/www/qq-test-acme`；仅测试域名的续期 dry-run 成功，使用既有 staging 账号，既有生产续期配置保持不变。`certbot.timer` 已启用且运行；`/usr/local/sbin/qq-test-nginx-deploy-hook` 仅匹配此证书 lineage / 域名，正向 reload 与其他 lineage 不执行检查均通过。维护模式仍保留 HTTP challenge 路由，因此暂停应用不需要删除证书或关闭续期。

## 实际验证与剩余边界

已完成的私有初始化核对：

- `/health` 为 200；新管理员通过 loopback 登录成功；运行版本和上传二进制 SHA 一致。
- 304 项迁移含 `249_qq_image_group_discovery.sql` 已应用；1 用户 / 1 管理员、非零余额 0、上游账户 0、QQ 订单 0、授权 0、QQ 配置 0、SMTP 配置 0。
- 注册在开放代理前已关闭，`frontend_url=https://qq-test.tangguo.xin`，默认余额与各认证来源默认余额为 0；用户首次合规确认记录为 0，管理读取返回 423。
- 新实例生成的密钥、密码和登录 token 在测试容器 stdout / stderr 逐值扫描中均未命中；秘密配置与登录文件权限收紧，仅登录专用信息交付到本机私有目录。
- 私有初始化完成、注册关闭后才撤除 HTTPS 503 维护页。北京时间21:44～21:45从独立公网链路严格验证两个域名TLS和health均为200；Google / Cloudflare DoH的A记录均为49.51.35.163，没有AAAA/CNAME，未依据本机代理的198.18.*假DNS地址判断公网状态。
- 公网 `/api/v1/settings/public` 返回注册关闭；`/setup/status` 为 `needs_setup=false` / completed。匿名管理与用户接口均401；无QQ配置时空回调503、缺失媒体token时404，响应均带no-store / no-referrer。远端只读核实QQ加密主密钥为有效64位hex / 32字节，启动无invalid-key告警，QQ配置表仍为0行；503来自 `Events → HandleWebhook → BotRuntimeConfig → configFrom → sql.ErrNoRows → ErrNotFound`，不是运行时未接线或simple模式。代码路径见[事件入口](../../backend/internal/handler/qq_image_handler.go#L438)、[签名前读取配置](../../backend/internal/qqimage/qq_commands.go#L40)、[机器人运行配置](../../backend/internal/qqimage/discovery.go#L48)、[错误归一化](../../backend/internal/qqimage/store.go#L57)。有配置后签名失败的实现返回403；本轮没有为探测填写假凭据，也没有执行真实平台验签。
- 公开检查后，用户仍1、非零余额0、订单0、授权0、QQ配置0。观测内存为应用43.78MiB、PG64.87MiB、Redis11.74MiB；这是当时用量，不是资源限额。配额文件系统使用76,615,680字节；宿主剩余49,182,154,752字节。
- 生产应用、PG、Redis三个容器的ID、image、StartedAt和RestartCount与部署前完全一致；生产应用仍为r4固定镜像。三份已有生产配置的大小/mtime/inode不变（未读取其秘密内容）；原Nginx站点SHA256仍为 `b5a9ce0ba24ae04d1cce9b7c58b03665e45cc530bf153f0ac070038b315cfd72`，生产证书SHA256仍为 `2f254ef59911c9f6ddedd0f8a7125ad19e283037ef2024b7684f4b581b2d0e80`；正式站health始终200。共享Nginx仅执行配置检查和reload，新站没有导致生产容器重建或重启。
- 独立隔离复核无剩余阻断项：三个测试容器实际资源限额/端口/挂载/网络符合配置，mount UUID guard通过，systemd已加载精确Requires/After/ExecStartPre且active/running、NRestarts=0。没有进行测试服务重启、卸载数据卷或宿主重启恢复演练，因此不宣称重启恢复已实测。Redis无Docker healthcheck，本轮检查为running，不能把它写成Docker healthy。
- 最终代理root/health及群识别Welfare、qqImage和中英文locale资产均可加载；识别API和官方group_openid文案存在。仅保留一个no-referrer响应头；开放应用后，用虚构query/Referer标记检查Nginx与测试应用stdout/stderr，均未命中。
- 北京时间21:49匿名浏览器核对 `/login`、`/qq/bind` 均显示独立站名；`/register` 显示注册关闭且没有注册表单；实际管理页面 `/admin/welfare` 跳转到 `/login?redirect=/admin/welfare`。1280×720下三个页面scrollWidth与clientWidth均为1280，无横向溢出，所查console warn/error为空。没有读取或输入凭据、登录管理员、确认合规或发出外联业务请求；临时tab已关闭。本轮手机版未测，当前工具未提供已文档化的resize方法，不能沿用本地阶段移动端截图作为远端验证。

本轮没有重跑全项目回归。功能、前端与真实数据库回归的执行时点和范围分别见[首版记录](2026-10-23-qq-image-generation.md)、[本地验收记录](2026-10-23-qq-local-acceptance.md)。公网部署验证不替代真实 QQ 挑战/签名、机器人群/媒体权限、SMTP 收件、绑定双端确认、上游模型/价格、付费生成及资金结算联调；这些尚未执行。没有发送真实 QQ 或邮件、没有触发付费生图或真实账户扣费。

## 运行检查与暂停验收站

以下命令供已连接该服务器的管理员复核；仅针对新建验收资源，不读取凭据。Windows 端执行项目维护命令前仍须在同一次 PowerShell 调用先载入 `E:\sub2二次开发项目\tools\project-env.ps1`，并使用既有严格主机密钥校验的连接方式。

```sh
systemctl is-active sub2-qq-test.service
systemctl is-enabled sub2-qq-test.service
findmnt --mountpoint /srv/sub2-qq-test/data
/srv/sub2-qq-test/operations/check-mount.sh
docker stats --no-stream sub2-qq-test-app sub2-qq-test-postgres sub2-qq-test-redis
curl --fail --silent --show-error https://qq-test.tangguo.xin/health
```

已保存维护配置 `/var/backups/qq-test-nginx-20261023/qq-test-https-maintenance-before-proxy.conf`，SHA256为 `563bf3d8b502664c149dcd8883da1f816701e6bbb060e0f98eea92f65300c106`；当前代理备份为同目录 `qq-test-proxy-active.conf`，SHA256为 `b5f68e1bb86ab3f28a0fa21251c5ac57bcaa9ec35a57e08f2c8de60119d33d43`。后续配置变化时以当次新备份为准，不覆盖用户后续调整。

需要暂停公网验收时，先把测试域名恢复为已保存的维护配置，验证成功后再停止独立应用服务。先记录当前配置备份路径；若 `nginx -t` 失败，恢复该备份后重新检查，不重载失败配置。下列操作保留数据卷、密钥、证书和续期：

```sh
set -eu
site=/etc/nginx/sites-available/qq-test.tangguo.xin
maintenance=/var/backups/qq-test-nginx-20261023/qq-test-https-maintenance-before-proxy.conf
saved=/var/backups/qq-test-nginx-20261023/qq-test-before-maintenance-$(date -u +%Y%m%dT%H%M%SZ).conf
test -f "$site"
test -f "$maintenance"
test "$(sha256sum "$maintenance" | cut -d ' ' -f 1)" = 563bf3d8b502664c149dcd8883da1f816701e6bbb060e0f98eea92f65300c106
cp -p "$site" "$saved"
install -m 0644 "$maintenance" "$site"
if ! nginx -t; then
    install -m 0644 "$saved" "$site"
    nginx -t
    exit 1
fi
systemctl reload nginx
test "$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' https://qq-test.tangguo.xin/health)" = 503
systemctl stop sub2-qq-test.service
```

恢复时先检查 mount UUID，启动 `sub2-qq-test.service` 并确认 `curl --fail --header 'Host: qq-test.tangguo.xin' http://127.0.0.1:21089/health` 为 200，再将上面记录的 `saved` 文件恢复到测试站点，经 `nginx -t` 成功后 reload，最后检查公网 health。停止服务不删除其开机启用状态；即使服务器重启后应用重新启动，维护站点也继续返回 503，须明确恢复代理才会开放。

不要删除数据库、清空 Redis、重置管理员或撤销 248 / 249 迁移来关闭验收；不要把生产目录或容器带入上述命令。后续一旦出现订单、冻结、退款或用户配置，应保留账务数据并按[首版回退说明](2026-10-23-qq-image-generation.md)处理，不能用“初始空库”验证要求清库。

本机无密摘要位于 `output/qq-test-deploy-20261023`，原始证据在 `.cache/qq-test-deploy-20261023`；二者不随 Git 交接。远端证据位于 `/srv/sub2-qq-test/evidence`，测试 Nginx 备份位于 `/var/backups/qq-test-nginx-20261023`。本记录保存必要身份、状态、限额与运维边界；私有登录文件、远端秘密配置和任何私钥不得加入 Git。
