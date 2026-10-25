# QQ 官方 WebSocket 长连接接入

日期：2026-10-23（Asia/Shanghai）。本轮依据用户“保留 WebSocket，补上长连接接入，然后更新到测试站，别影响正式站”的授权，在既有 QQ 隔离工作树继续实施。稳定变更名称为 **QQ 官方 WebSocket 接收与隔离测试站更新**。

本轮接收器、后台配置与状态展示已实现，两类独立审查问题已修正关闭，最终 QQ 专项验证及 Linux 构建通过。**WebSocket 版本已更新至独立测试站，真实 QQ READY 和首个心跳 ACK 已确认**；部署后的生产、用户数据与凭据保全核对通过，22:55 仍维持同一连接且心跳持续推进；两个域名公网严格 TLS 和健康检查通过。未发送 QQ 消息或执行付费生图。

## 源码与发布边界

工作树为 `E:\sub2二次开发项目\.worktrees\qq-image-generation-20261023`，分支 `codex/qq-image-generation-20261023`，开发基线 HEAD 为 `c9a9209ea79163dbbf1fb20b5db2dc67c6b15394`。QQ 首版、群识别和本轮接收器均为工作区未提交修改，该 HEAD 不是包含这些功能的提交。本轮没有提交、合入主目录、推送 GitHub 或公开发布镜像。

当前正式发布来源仍为 `76ff7c88a` / `0.2.17-r4`。更新前测试站运行 `0.2.17-qq-acceptance.55406431e342`，私有镜像 `sub2-qq-test:55406431e342`；其源码快照和首次部署证据见[原远端验收记录](2026-10-23-qq-remote-acceptance.md)。生产仍为 r4，本轮仅允许读取指定容器身份、配置元数据及健康状态。

主目录本轮接手时有 11 份 tracked WIP，已保存 SHA256 基线；部署完成后逐一复核仍全部不变，摘要为 `main-wip-protection.json`。此前记录中的“10 份 WIP”描述旧阶段，不覆盖本次接手状态。源码与验证均在 E 盘工作树进行，复用主项目 `.cache`，没有迁移或重置本机 18089 验收数据。

## 接入方式和功能边界

后台“福利中心 → QQ 群生图”新增接收方式：`webhook` 与 `websocket`。历史配置缺少字段时按 `webhook` 读取，新配置默认仍为 `webhook`；未知模式拒绝保存。选择 WebSocket 并保存后由服务器主动向官方网关建连，无需为该模式配置公网 HTTP 回调。原 HTTP 入口继续校验原始请求字节的 QQ 签名，未新增免签 HTTP 接口；两种接收方式按当前配置互斥。

模式与“允许新 QQ 生图申请”独立。生图关闭、没有生图 API Key 时，只要已保存有效 QQ AppID / App Secret，WebSocket 可接收受限的群识别事件。`/识别群` 仍须管理员主动开启 5 分钟一次性窗口，候选必须由管理员确认后才能进入群白名单；该指令不发 QQ 回复、不创建绑定、订单或资金变动。

长连接仅将 `GROUP_AT_MESSAGE_CREATE` 交给已有 QQ 命令逻辑。平台字段构造机器人、群和成员作用域身份；不从消息正文读取网站用户身份。HTTP 与 WebSocket 共用同一规范化内容和消息去重键，事务先提交业务与处理结果，再尝试回复；重放不能重复生成、扣款或重发未知收据。事件仍受原有时效、群白名单、本人绑定、邮箱及额度规则约束。

后台连接状态接口只对管理员开放，返回受控状态、最近连接/心跳/事件时间及有限错误码；不返回 token、session ID、原始网关消息或错误字符串。前端保存模式时保留原有配置与留空保密字段语义；退出登录、切换身份、编辑配置或遇到迟到响应时清除旧状态。状态刷新不发 QQ 消息。

## 传输、单接收者及取消

- 使用已有 WebSocket 依赖，固定官方 token 与 `/gateway/bot` 引导入口；QQ 返回的地址仍须经过 WSS、官方精确主机、443、路径和 JSON 参数校验。禁止重定向、环境代理和私网 DNS 目标，不提供管理员任意网关 URL。当前支持单分片；平台要求多个分片时会停止并报告受控错误，未实现多分片扩展。
- 实现 HELLO、Identify、READY、Heartbeat/ACK、Reconnect、Resume 和 InvalidSession，配置有限的认证/读写期限、帧大小、串行事件队列、重连退避及会话启动额度。心跳报告最后收到的序列；Resume 则只使用已经提交或明确拒绝的恢复序列，数据库临时故障不能被视为已处理。队列满时关闭网络读写并在父上下文仍有效时，最多 30 秒处理已接收的有界前缀，再从已提交位置恢复；父级取消立即中止。
- 单个应用使用专用 PostgreSQL 会话持有双键 advisory lock：namespace `716709`、ID `231250`。该锁与 QQ 业务事务锁 `716709231248` 分离，不长期占用业务事务；每个活动接收者占用一个数据库连接。
- supervisor 定期检查当前 QQ 配置版本和数据库会话。模式、配置 ID、机器人身份、凭据状态改变或会话失效时，先取消并等待旧接收器退出，再释放旧所有权、考虑新连接。普通业务在同一业务事务中复核当前配置以及 `pg_locks` 中当前数据库、持锁 PID 和锁键。
- 识别群也在其写事务中复核接收者 Guard，避免初次校验之后等待业务锁期间失去所有权，旧接收器仍写入候选。
- 运行时统一负责 Start/Stop；simple 模式不启动长连接接收器。网络回复仍不在数据库事务内执行。

本轮 **无新增数据库迁移、无新增依赖或必需环境变量**。既有迁移 248、249 及 `QQ_IMAGE_ENCRYPTION_KEY` 继续有效；`receive_mode` 保存在原有 JSONB 配置快照中。回退应用前先停止测试接收器，保留数据库、凭据和既有账务记录，不以回退为由清库。旧镜像不支持 WebSocket；保留 QQ 平台当前设置时，回退只能恢复旧网站，QQ 事件接收会暂停，不会自动改走 HTTP 回调。

## 独立审查与实际验证

审查发现及处置分别记录，尚未完成的项不作为通过结论：

| 项目 | 已核实结果 |
| --- | --- |
| 识别群所有权竞态 | 独立审查发现 `/识别群` 直接写入而跳过事务内 Guard。真实 PostgreSQL 回归先复现旧代码返回 nil 并写候选；修正为绑定当前事务的 Guard 后返回冲突、候选保持 waiting。该 P2 已复核关闭，全部 Discovery 专项同时通过。 |
| 队列背压与重放 | 独立审查复现：队列满立即取消同一连接业务上下文，快速重放超过 64 条且首业务较慢时反复重连但 checkpoint 不前进（RED 为 9 次连接、0 完成）。已分离网络关闭与受父级取消的业务上下文，关闭连接后限时处理已接收前缀，再恢复；父级取消不继续处理。专项 GREEN 与独立重放复测通过：128 条、每条 50ms 的积压以 2 次连接完成 128 条，各序号恰好一次，checkpoint=130；该 P2 已关闭。 |
| 认证计时与连接状态 | 同轮回归另复现 RESUMED 排在慢业务后触发 15 秒认证超时，以及已关闭连接在处理积压时被显示为 connected。修正为合法线上恢复事件到达时停止认证计时，恢复序列与业务状态仍按顺序提交，积压处理状态不再误报 connected；对应 RED/GREEN 通过。独立复测 16 秒慢 replay 完成 1 条且未触发 ready_timeout；连同序号分离、父取消、积压状态等 6 项复核全部成功（22.857 秒）。 |
| 接入与 supervisor | 模式缺省/非法模式、旧配置拒绝、事务内再次校验、跨 HTTP/WS 去重、关闭生图时识别零业务副作用、模式切换/停机/租约丢失及真实 PG 会话锁互斥/释放专项通过。 |
| handler / routes / config 最终专项 | QQ 运行时启动/取消、管理员连接状态权限、路由及配置专项共 25 个测试 pass 事件，0 fail、0 skip，3 包均通过。 |
| QQ 后端最终整包 | 包含本地模拟 HTTP/WebSocket 与真实 PostgreSQL 的 QQ 整包共 226 个测试 pass 事件，0 fail、0 skip；覆盖认证、心跳、恢复、READY 后立即断开、错误分类、官方 URL、预算、数据与租约竞态。pass 事件含子测试，不冒称独立叶用例数。 |
| 静态检查与依赖注入 | qqimage、handler、routes、config 四包 go vet 通过；Wire 使用已有缓存 v0.7.0 重新生成成功。 |
| 前端专项 | 5 文件 / 101 项通过，包含 QQ API、管理面板、用户视图、工具和双语键完整性；类型检查、针对变更文件的 ESLint 通过。前端生产构建通过：依次执行原 build 的 check:i18n、vue-tsc -b、vite build 三步，Vite 21.18 秒完成。使用已缓存 pnpm 10.11.0 的明确入口，避开嵌套 pnpm 自动解析其他版本的工具链验证错误；未修改依赖或 lockfile。既有 Browserslist 与大 chunk 提示保留。 |
| 当前版本浏览器验证 | 本轮 Cua 没有可用浏览器或应用，无法进行新版页面视觉 smoke、控制台、管理 UI 或移动端验证；旧版浏览器截图/记录不能作为本版证据。实际后端 API、前端专项及公网静态资源哈希检查已另行完成，不等同于浏览器渲染验证。 |
| race 检查 | 尝试未运行：当前 Windows 环境 CGO 关闭，Go 返回 `-race requires cgo`；不能宣称 race 通过。 |
| 最终构建/线上 | Go 1.27.3、linux/amd64、CGO=0、embed 前端构建通过，已部署独立测试站并核对运行二进制 SHA256；真实 READY、持续心跳、生产/数据/凭据保全及两站公网严格 TLS/健康检查通过。 |

关键 RED/GREEN 证据为主项目 `.cache/qq-websocket-20261023/discovery-lease-red.log`、`discovery-lease-green.log`；传输修正 RED 为 `gateway-backpressure-sequence-red.log`、`gateway-wire-auth-red.log`、`gateway-drain-status-red.log`，统一 GREEN 为 `gateway-review-fixes-green.log`；后者未配置 PG 测试 DSN，3 项真实 PG 测试跳过，不能替代前述真实 PG 单独成功记录。最终后端整包另以真实 PG 配置运行，226 + 25 个测试 pass 事件均无 skip；摘要 `backend-verification-summary.json`，独立复测 `review-gateway/independent-recheck-green.log`。其余专项日志同目录。上述结论已在本仓库文档归档，缓存日志只作原始证据，不作为唯一交接入口。没有重跑或借用前一阶段全项目回归数字。

## 当前构建与部署产物

本轮冻结源码构建的是未提交工作区快照，不是正式发布提交；源码 HEAD 仅用于追溯开发基线。

| 项目 | 已核实身份 |
| --- | --- |
| 版本 / BuildType | `0.2.17-qq-ws.742bbe8031ba` / `qq-acceptance-websocket` |
| 源码 SHA256 / 文件数 | `742bbe8031ba5c1fba9a66c99493ee661c68c1f34e98fb46f59702cb79e7c8ca` / 4,796 |
| 前端 dist SHA256 / 文件数 | `80f30aeedee5a47ba4dc69a863e1a49ad3b06e3638f2efc6fa9fdd3f74fef6e7` / 768 |
| Linux amd64 二进制 SHA256 / 大小 | `c8a6b292d8396c83af1377d7c7dddfa8247dd301eb34196ae6b68f0168069149` / 150,790,304 字节 |
| 上传包 SHA256 / 大小 | `05c01e823cd20f832aadc7126d01de442d91e1fce61e49eec312f7b9980f3ffc` / 44,276,721 字节 |
| 构建时间 | `2026-10-23T14:44:17Z`（北京时间 22:44:17） |

构建命令退出码 0，编译器 Go 1.27.3，`GOOS=linux`、`GOARCH=amd64`、`CGO_ENABLED=0`。构建产物与摘要保存在主项目 `.cache/qq-websocket-20261023`、`output/qq-websocket-20261023/artifact-summary.json`。部署已核对运行二进制 SHA256 与本表一致；源码快照先于后续部署结果文档补记，文档更新不改变已部署产物。当前镜像为 `sub2-qq-test:ws-742bbe8031ba`，旧镜像 `sub2-qq-test:55406431e342` 保留回退。

## 更新前数据、备份与生产保护

北京时间 2026-10-23 22:26 的只读核对确认：测试站已有 2 版 QQ 配置，AppID / App Secret 已配置，生图 API Key 未配置，`enabled=false`，旧配置接收方式按 Webhook 解释。测试库仍为 1 用户 / 1 管理员，总余额 0、非零余额用户 0、订单 0、冻结订单/金额 0、授权 0。此前“未配置 QQ 凭据”的描述仅属于首次部署时点；本轮保留用户已保存的配置，不初始化、不覆盖密钥，也未输出配置原文。

更新前私有管理员只读预检随后确认：原测试登录资料仍可用，登录 HTTP 200、管理配置 GET HTTP 200，当前配置 ID 为 2；机器人凭据已配置、生图 Key 未配置、生图关闭。首次部署的 HTTP 423 合规门禁只是历史时点，用户后续已自行通过，本轮未代确认或绕过门禁。更新时可经已有管理员配置 API 携带当前 ID 乐观校验，仅切换 receive_mode，保留其余配置和留空凭据语义。

已创建 `/srv/sub2-qq-test/backups/websocket-20261023-142615`，目录权限 `0700`；`qq_test.dump`、测试 `compose.yaml`、应用 `config.yaml` 与 `.installed` 副本均为 `0600`。PostgreSQL 自定义格式 dump 大小 **618,912 字节**，SHA256 为 `3c4b8a87b3dcf8dedfc11b89377cb3a30af3ccf9eabf16cc16a7c9ec02f07f18`；`pg_restore --list` 检查 1,407 项成功，**未执行实际恢复**。备份留在远端私有目录，未下载。

备份前后再次核对：生产 `sub2api`、`sub2api-postgres`、`sub2api-redis` 的 ID、镜像、StartedAt、RestartCount 与运行状态保持不变；`/root/sub2api-deploy/docker-compose.yml`、`.env`、`data/config.yaml` 仅取 stat，元数据不变；原 Nginx default SHA256 仍为 `b5a9ce0ba24ae04d1cce9b7c58b03665e45cc530bf153f0ac070038b315cfd72`。测试三个容器与配置文件哈希也不变；测试 QQ 配置整行（含加密凭据）、settings、users、orders、grants 均只返回聚合摘要，前后相同。两站严格 TLS `/health` 均 200。这是**备份阶段**保全证据，部署后仍须重新比较。

原始无密证据：`.cache/qq-websocket-20261023/remote-before.json`、`remote-backup-postcheck.json`、`backup-postcheck-summary.json`；交付摘要为 `output/qq-websocket-20261023/backup-summary.json`。可复用 `Invoke-QQSnapshot.ps1` 获取后续只读状态，不加 `-Backup` 不会再建备份；所有本机命令先显式加载主项目环境入口。

## 测试站已更新，真实 QQ 连接验证

北京时间 2026-10-23 22:51～22:52，已更新独立测试站 **https://qq-test.tangguo.xin**，版本 `0.2.17-qq-ws.742bbe8031ba`，镜像 `sub2-qq-test:ws-742bbe8031ba`。运行二进制 SHA256 与上述构建产物一致，应用 `/health` 为 200。

部署仅替换测试 Compose 的 app image；脚本限定 `sub2-qq-test` 项目、三个精确测试容器名、测试目录挂载和独立网络，停机前再次确认无活动订单且生图关闭。专用测试 stack stop/start 会短暂停启测试 PG/Redis，原容器与数据保留，只重建应用容器；测试 PG/Redis 的启动时刻不作为不变项，生产容器不在该操作范围。应用 readiness 或运行二进制核验失败时可恢复原 Compose/image；管理员 API 阶段失败单独记录是否请求/接受模式变化，不盲回退用户配置或误报镜像已回退。本次没有执行回退。

临停前另建最新备份 `/srv/sub2-qq-test/backups/ws-switch-20261023-145124`，其中 `qq-test-before.dump` 为 **619,183 字节**，并保留原 Compose；dump 经 `pg_restore --list` 成功检查。此备份与前述 22:26 备份分别保留，未实际恢复。

模式切换通过已有管理员配置 API 完成，GET 后再次确认生图关闭，并携带当前配置 ID 提交：`receive_mode` 从 `webhook` 改为 `websocket`，配置 ID **2 → 3**；返回对象除 ID 与接收模式外全部字段不变。机器人凭据保留，生图仍关闭，生图 Key 仍未配置；没有重置管理员、数据库或密钥。

真实连接状态由 Webhook → connecting → connected。北京时间 **22:51:42.701949712** 收到 READY 并记录 connected，**22:52:23.740572815** 收到首个心跳 ACK；状态未含 session ID、token 或原始网关错误。部署摘要为 `.cache/qq-websocket-20261023/update-result.json`。既有测试登录仍有效，所用登录密码和本次 access token 未在测试应用 stdout/stderr 中出现；该检查不扩大表述为扫描了全部凭据。

本轮没有发送 QQ 消息、邮件、开启生图、调用付费模型、执行绑定或真实资金测试。测试站宿主隔离不等于 QQ 平台沙箱，使用的是用户已有的官方机器人；READY/心跳正常不代表群消息、媒体权限、邮箱、付费生成或资金链路已验收。下一步可由用户在后台开启识别窗口，在目标测试群发出本次识别口令，再回后台核对候选并确认。

## 部署后保全、持续连接与公网核对

北京时间 **22:55:15** 只读复核仍为 connected，`last_connected_at` 保持 **22:51:42.701949712**，心跳时间推进至 **22:55:08.742063601**，说明同一连接在观测窗口内持续收到 ACK。数据库仅有 **1 个**网关会话锁持有者，迁移数仍 **304**，没有新迁移。旧配置 1、2 继续存在；直接比较配置 2 与 3，机器人及生图凭据 ciphertext 不变，JSON 除 `receive_mode` 外全部相等。

更新前后指定生产状态逐项一致：`sub2api`、`sub2api-postgres`、`sub2api-redis` 的 ID、镜像/镜像 ID、StartedAt、RestartCount、运行状态不变；三份生产配置的 stat 元数据和原 Nginx default SHA256 不变。测试 PG/Redis 容器 ID 保留，应用 `config.yaml` 与 `.installed` 哈希不变。测试 settings、orders、grants 的聚合摘要及用户/管理员数、非零余额数、余额总额、订单、冻结和授权数量均不变，仍为 1 用户、余额与上述业务计数 0。

**用户整行摘要发生变化**，没有将其误报为不变。已与临停前数据库备份逐列比较唯一测试用户，仅 `updated_at`、`last_login_at`、`last_active_at` 三个登录/活动时间字段改变，其余列保留；比较没有输出秘密值。QQ 配置整行摘要也会因新增 ID=3 的接收方式版本而改变，以前述旧记录保留、ciphertext 与其他 JSON 字段相等验证用户配置保全。

匿名连接状态接口返回 401；HTTP 回调返回 503 的**当前原因是已选择 WebSocket 接收模式**，不再是首次部署时 QQ 配置为空；缺少媒体 token 返回 404。现有管理员 API 验证为 200。两域名证书指纹与更新前相同：测试站 `0208e24f6c0ece17cfb0f284ee24a3302c82c2d5cedbf58a1529e529f54dcd09`，正式站 `2f254ef59911c9f6ddedd0f8a7125ad19e283037ef2024b7684f4b581b2d0e80`。

北京时间 **22:57:20**，通过本机物理网卡绑定的公网链路独立核对两站：TLS 1.3、可信证书链与主机名校验通过，`/health` 均为 200，没有关闭 TLS 校验。抽查构建目录对应的 **13 份** QQ/福利静态资源，公网均返回 200，SHA256 与本次构建目录中的相应文件一致；这证明资源可达且字节匹配，不代表浏览器已加载或渲染这些资源。

本轮浏览器工具库存为空，创建 IAB 失败，未能执行新版 `/login`、`/qq/bind`、`/admin/welfare` 的视觉 smoke、控制台检查或移动端验证。原首次部署浏览器记录属于旧版，不能沿用。当前前端验证边界为 101 项主专项、84 项独立专项、类型/lint/构建、真实管理员 API 与上述公网资源哈希核对。

交付摘要位于主项目 `output/qq-websocket-20261023`：`runtime-verification.json`、`preservation-verification.json`、`users-preservation-verification.json`、`public-tls-verification.json`、`public-assets-verification.json`、`browser-verification.json`。原始比较基线为 `.cache/qq-websocket-20261023/remote-before.json`、`remote-after.json`。源码构建已冻结，最后的文档补记没有修改 backend/frontend，也不改变已部署快照身份。

补充当前入口核验：公网首页引用 `/assets/index-DP8vj366.js`，其实际引用的福利页面为 `/assets/WelfareSettingsView-B5cuNOgY.js`，两者字节 SHA256 均与构建一致，后者含 WebSocket 模式与状态代码。证据为 `public-active-entry-verification.json`，并非从旧残留资源推断当前页面。更新验证后已删除本次远端上传临时压缩包44,276,721字节；本机部署包、远端已验证release及新旧镜像、数据库备份和必要证据保留，未触碰旧拒绝清理目标。

设计与执行清单：[WebSocket设计](../superpowers/specs/2026-10-23-qq-websocket-design.md)、[已完成实施计划](../superpowers/plans/2026-10-23-qq-websocket.md)。新文档已按精确路径放行Git忽略规则，仍未暂存或提交。
