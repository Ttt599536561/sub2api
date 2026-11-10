# 2026-11-10 NapCat 邀请月卡接入

## 交付状态

本轮按用户明确选择实现 **NapCat 专用福利账号 + Sub2 统一后台 + 同服务器独立 Docker 服务**。原官方机器人继续负责生图/绑定，原 ai-check/Cloudflare 菜单与智商查询保持。

当前为本地开发成果；没有推送/发布新的 Sub2 镜像、没有部署服务器、没有登录真实 QQ 或执行真实群/私信测试。用户询问“是否替换镜像”后，已说明首次部署需要新 Sub2 镜像和新增 NapCat 服务两部分。生产启用必须等新镜像发布及管理员扫码确认。

开发分支：`codex/napcat-integration-20261110`；基线 `c4105ca17`；独立工作树 `E:/sub2二次开发项目/.worktrees/napcat-integration-20261110`。主目录原有未提交修改保留。

## 使用入口与行为

管理后台 → 福利中心 → **QQ邀请奖励**。

- 连接管理：服务地址/指定福利 QQ 号、加密保存凭据、真实在线状态、获取和刷新二维码、显式重启登录。二维码在浏览器本地生成，仅管理员可见；登录会话变化或离开页面清空。
- 活动：名称、允许群、起止时间、奖励订阅分组、五种通知文案，建立已有成员基线后开启。同时间同群不允许活动重叠。
- 库存：批量粘贴本站已有的未使用 30 天订阅码，预检再入池；或直接生成 1–500 张入池。未来有效期可接受，过期码不接受/不分配；列表只显示掩码。
- 资格与发奖记录：邀请归属待核验、等级未知、等级不达标、缺库存、已获奖、消息失败/未知/已发送等状态；支持带理由核实邀请者、重新核验、明确重发原奖励。
- 活动内按被邀请人真实 QQ 号跨群去重；一个邀请者可邀请多个不同合格新人。机器人自身和活动启动基线成员不产生奖励。
- 真实 QQ 账号等级必须 >10。使用 `get_stranger_info.qqLevel`；不可用则保持待核验，不以群活跃等级替代。
- 只对明确 `group_increase/sub_type=invite` 且合法的邀请操作人自动归因。普通 approve、operator 缺失/为0、来源不明确进入待核验，不把审批管理员当邀请者。`get_group_system_msg.invitor_uin` 在某些事件中实际是申请人，本实现不拿它发奖。
- 管理员核验只补充邀请归属，之后仍须通过等级校验，不绕过 >10 条件。
- 后台模板变量：`{invitee}`、`{inviter}`、`{level}`、`{plan_name}`；私信另允许 `{code}`、`{url}`。所有群模板禁止码变量；私信必须有码和地址。每模板最多 2000 个 UTF-16 单位。`{url}` 指向本站 `/redeem`。
- 默认月卡 30 天，按现有订阅 group_id 判断同类。发出码时不自动开通订阅，用户后续兑换才开始/延长；同组累加期限，不同组分别持有。

## 持久化与重试

新增迁移 `253_qq_invite_rewards.sql`：连接版本、活动与版本、成员基线、事件/资格、码池、唯一奖励、消息 outbox、审计。迁移可重复执行，已有数据和码保护保持。

奖励分配和私信/群通知任务在同一 PostgreSQL 事务。新人资格、码池和奖励有唯一约束，多节点争最后一张码不会重发权益。未分配的池码不能通过普通网站兑换；池码权益字段被冻结，删除受外键保护。分配后仍保持原兑换码 unused，供网站原兑换流程消费。

私信与群通知分开。私信确认成功后群内 @ 祝贺；私信失败或结果未知则用独立“待领取”文案 @ 邀请者，引导添加好友后私信“领取奖励”。

- 明确失败：有限退避，最多 8 次，之后 blocked。
- 请求超时/响应不明确：unknown，不自动重发。
- 工作进程退出留下过期 sending：恢复为 unknown，不直接重送。
- 管理员明确重发或本人私信领取：始终复用原码；未确认的群祝贺可一并重试，已经成功的群祝贺不重发。
- 发码资格与消息送达分开统计；“已发送”仅是平台确认，不表示对方已读。
- 已分配码在投递前过期会记录失败，不自动改发第二张。当前没有自助换码入口，需管理员核对原码和权益后处理。
- 存在活动记录后暂不允许直接更换福利 QQ 账号，防止旧奖励由错误身份投递。需要迁移时另行保留账本和账号作用域。

秒级 OneBot 时间戳无法区分同一秒的精确先后。活动规则按事件秒时间匹配版本，入库后快照不变；跨启停边界不应以毫秒级承诺解释。离线期间未收到的事件不能保证完整找回。

## 订阅兑换加固

真实 PostgreSQL 复现了“同用户两张首次同组月卡并发兑换，一笔唯一冲突”，并在强制两笔使用码后同步的测试中复现了 FK KEY SHARE 升级 FOR UPDATE 的死锁。

本次仅修改订阅码兑换事务：
- 使用 Read Committed，确保等待锁后读取新提交的订阅。
- 标记码使用后，在持久用户行获取 **FOR NO KEY UPDATE**，串行同用户订阅创建/延长，同时兼容兑换码 used_by 外键的 KEY SHARE。
- 事务内推迟缓存失效，提交后同步清订阅缓存、计费缓存并广播、清鉴权缓存；请求被取消也不跳过已提交结果的必要失效。
- 订阅备注记录码 ID，避免把完整码放入普通备注。
- 余额/并发额度兑换和付款履约事务保持原语义；原有 suspended 订阅兑换恢复 active 的行为保持。

## 集成与部署

新模块 `backend/internal/napcat` 只承接传输；`backend/internal/qqinvite` 负责领域和 SQL；`qq_invite_handler/runtime` 接入现有管理员认证、同源检查、审计和应用停机。

OneBot HTTP 回调 `POST /api/v1/qq-invite/events` 校验原始请求体的 HMAC-SHA1、账号和活动群。数据库写入成功后才 ACK。业务管理前缀为 `/api/v1/admin/qq-invite`，库存/记录嵌套在 `campaigns/:id` 下。敏感接口 no-store；配置和入库请求体不落普通审计正文。

连接目标仅允许部署配置中的精确主机，禁用代理继承和重定向；WebUI 凭据留在后端。首次启动配置引导有 5 秒上限，NapCat 初始化故障不能无限阻塞主站启动。

固定 NapCat `v4.18.42` / 源码 `9335843246e3b7a17944861f3ac8731a259732c2`；镜像多架构 digest 已由 Docker Hub API 核对。部署文件：

- [同机 Compose overlay](../../deploy/docker-compose.napcat.yml)
- [部署与首次扫码说明](../../deploy/NAPCAT_SETUP.md)
- [一次性私有配置初始化](../../deploy/napcat/init.py)

首次部署：更新为包含本功能的 Sub2 镜像 → 在原 Compose 同项目叠加 NapCat 服务 → 初始化私有配置 → 启动 → 管理后台扫码 → 填活动/文案/库存/基线 → 启用。不要另建生产数据库/Redis，不更换原 QQ_IMAGE_ENCRYPTION_KEY，不公开 NapCat WebUI/OneBot 端口。

回退先停奖励接收和投递，保留账本/会话/加密 key。数据库触发器继续保护未分配池码，旧应用无法管理新模块；不得通过删除新表或历史奖励来回退。

## 验证与限制

已完成并独立复核：
- NapCat HTTP mock 协议、认证缓存、签名、等级字段、边界与未知投递测试；覆盖率约 82.5%。
- 领域层 20 项真实 PostgreSQL 测试，含跨群并发、最后一码、事务回滚、导入与网站兑换竞态、重启/租约、活动暂停迟到、有效期、模板和二次迁移。
- handler 管理员/会话边界、同源、原 body 签名、账号不匹配、secret 脱敏、500 数量上限、启动引导超时测试。
- 前端相关组件、API/session、真实路由契约、模板/i18n 与既有福利/QQ回归测试；类型检查及生产构建。
- 订阅真实 PG：数据库默认 RC/RR 两种环境，两张首次月卡并发均成功，唯一一条订阅累计 60 天，两码 used，无锁升级死锁。
- 初始化脚本 4 项测试；Compose 只读合并验证原网络、私有新网络、环境项及无端口发布。

最终收尾验证（2026-11-10）：
- `go test -tags=unit ./internal/napcat ./internal/qqinvite ./internal/handler ./internal/server/routes ./internal/server/middleware ./internal/service ./migrations -count=1` 全部通过；service整包227.341秒。
- qqinvite在默认RC、默认RR各自整包通过。写事务及worker任务领取显式使用RC，避免默认RR并发序列化失败；不改数据库全局隔离或只读预检。
- `go test -tags=integration ./internal/service -run TestConcurrentFirstSubscriptionRedemptionsStackSixtyDays -count=1 -v` 两种默认隔离均通过。
- 前端最终5个文件59项专项/i18n测试、`vue-tsc -b`、Vite生产构建通过；前期既有福利/QQ页面35项回归也通过。新增状态显示等待扫码/等待手机确认/初始化/重连，并给出具体凭据/服务错误提示。
- `golangci-lint run --build-tags=unit --new-from-rev=c4105ca17 ...` 本次变更0 issues；qqinvite独立全包lint也0 issues。全范围unit-tag lint仍有未改动旧handler测试的depguard/errcheck/staticcheck问题，本轮未改规则或扩大修复。
- `CGO_ENABLED=0 GOOS=linux GOARCH=amd64/arm64 go build -tags embed ... ./cmd/server` 两架构成功；这是编译验证，不是镜像运行验证。
- `git diff --cached --check` 通过。前端使用固定pnpm9.15.9逐步执行build所含检查；直接run build的嵌套pnpm会调用本机全局新版导致环境错误，未修改仓库依赖/lockfile规避。

原始日志在主项目 `.cache/napcat-integration-20261110`，部署包在 `output/napcat-integration-20261110/napcat-deployment.zip`。仅打包Compose、部署说明和初始化代码，不含生成密钥或QQ会话。

未执行：Docker QQ 镜像实际启动、真实扫码、真实入群/私信/群通知、线上发布部署。自动审批拒绝了本地浏览器预览服务的启动，返回“blocked by policy”，未重试；因此浏览器交互验收未完成。代码依靠组件测试、类型/生产构建及真实数据库/协议 mock 验证，不能称为已在真实 QQ 上线。

### 2026-11-11验证记录更正

本机早期Docker daemon不可用时，repository的integration TestMain会在m.Run前以0退出，原“integration专项通过”记录应视为跳过；当时真实执行的证据为PG16 unit模式迁移专项，以及service包直接连接真实PG的并发月卡测试。之后重新核对Docker Desktop设置、WSL注册与实际VHDX，确认数据在E盘且daemon已可用；修正旧负数订阅夹具后，已实际运行Docker PG18两项测试并逐项观察RUN/PASS，全仓integration正在补验。最终CI和运行验证以发布记录为准，不以0退出码代替测试执行。
