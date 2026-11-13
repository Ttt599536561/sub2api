# AI上下文卡：sub2api / qq-image

本仓文档核对日期：2026-10-27（外部服务状态未在本次在线核验）。先读 [跨项目路由](AI_START_HERE.md) / [机器索引](PROJECT_INDEX.json)。本卡是定位资料，不是在线状态检查。

## Identity

- `repository_id=sub2api`；canonical root=`E:/sub2二次开发项目`；Go/Gin/Ent + Vue/pnpm；数据库PostgreSQL + Redis。
- `sub2-web`：账户、渠道、API代理、余额、福利与管理后台。`qq-image`：同一应用内的QQ群付费生图模块，随同一Docker镜像发布。
- 每次PowerShell先 `. 'E:\sub2二次开发项目\tools\project-env.ps1'`。工作树也用此入口；检查dirty后再决定隔离，不覆盖用户修改。
- QQ功能已合入main。`.worktrees/qq-image-generation-20261023` 是保留的同仓工作树，新任务默认主目录；明确指定工作树或已有任务时先核对其状态再继续。
- `C:/Users/Administrator/Desktop/Sub2api` 当前为指向E盘主目录的旧联接；侧栏“Sub2api”不代表独立仓库。不要在C盘路径构建或清理联接。

## Environments / publication

| 对象 | 身份 | 接手依据 |
| --- | --- | --- |
| 正式应用 | https://api.tangguo.xin | [HANDOFF 当前摘要](HANDOFF.md#当前代码和发布状态)及 [QQ 正式接管](changes/2026-10-27-qq-controls-gallery.md)；操作前重新核实 |
| QQ测试应用 | https://qq-test.tangguo.xin；远端 `/srv/sub2-qq-test` | [HANDOFF 当前摘要](HANDOFF.md#当前代码和发布状态)；独立验收栈保留，正式接管后已记录停止收费接收 |
| 发布镜像 | `ghcr.io/ttt599536561/sub2api-custom` | [HANDOFF 最新发布入口](HANDOFF.md#当前代码和发布状态)；版本与 digest 由对应发行记录维护 |

正式/测试最后记录为同宿主机但独立应用、PG、Redis、配置和数据；“同服务器”不代表共用账户/余额。最新发布镜像与正式/测试当前运行镜像必须分开核实。镜像已发布不代表生产已升级；维护入口不固定复制发行号，避免再次漂移。

## Task → source

| 任务 | 最短源码入口 |
| --- | --- |
| QQ命令、识别、绑定、授权、订单/计费、图片存取与发送 | `backend/internal/qqimage/`，先看 `qq_commands.go` |
| 生图侧QQ WebSocket、心跳、恢复 | `backend/internal/qqimage/gateway_supervisor.go`、`gateway_client.go`、`gateway.go`；HTTP入口 `backend/internal/handler/qq_image_runtime.go` |
| QQ管理/API/媒体 | `backend/internal/server/routes/qq_image.go`；`backend/internal/handler/qq_image*.go` |
| QQ后台配置、文案编辑 | `frontend/src/views/admin/WelfareSettingsView.vue`；`frontend/src/components/admin/QQImagePanel.vue`、`QQReplyTemplatesPanel.vue`；`frontend/src/api/qqImage.ts` |
| 用户绑定/作品图库 | `frontend/src/views/user/QQBindView.vue`、`QQOrdersView.vue`；`frontend/src/components/user/QQGalleryCard.vue`、`QQGalleryDetail.vue`；`frontend/src/composables/useQQGalleryMedia.ts` |
| QQ 独立业务开关 | `frontend/src/views/admin/SettingsView.vue`、`backend/internal/handler/admin/setting_handler.go`、`backend/internal/qqimage/business_controls.go`；实现与规则见 [QQ 开关记录](changes/2026-10-27-qq-controls-gallery.md) |
| 福利签到/抽奖 | `backend/internal/service/welfare_service.go`、`backend/migrations/251_welfare_checkin_rules_v3.sql`、`backend/migrations/252_welfare_lottery_rules_v4.sql`；[福利规则](WELFARE_CENTER.md) |
| 邮箱验证/换绑/SMTP | `backend/internal/service/auth_email_binding.go`；检索同目录 `SendQQImageEmailCode` / email；沿用普通账号认证 |
| 构建/发布/迁移 | `.github/workflows/`、`deploy/`、`backend/migrations/`；先读对应release记录 |

## Cross-project contract

- 当前联合模式下，CF `oneapi-observer-qq` 处理裸@、通用菜单和智商截图；Sub2处理 `/识别群 /绑定 /确认绑定 /生图帮助 /生图 /任务 /重发`。`/生图一个黑洞` 也有专门边界规则，改动时核对两侧。
- 联合模式以 `QQ_IMAGE_EXTERNAL_HELP=true` 将通用帮助交给 CF，独立部署默认 false。2026-10-27 接管记录已确认正式站配置此开关及独立加密密钥，测试收费接收停止；后续升级保留原值与原密钥，不双开收费接收器。实际连接状态须另行只读核查。
- CF对生图命令静默，不把收费请求代理到Go；两套独立QQ连接，一个online不代表另一个online。
- Sub2发布回复模板，CF从固定测试站读取六类菜单/智商文案。编辑内容先查后台发布版本；更改同步契约要审两仓。
- 用户/授权/余额/订单/生图图片属于选中的Sub2实例；当前图片保存在PostgreSQL，未启用检测站R2方案。QQ生成图片与检测站生成HTML不是同一执行器；该模块扣本实例 users.balance，不扣月卡/福利钱包。测试域名也不等于QQ平台sandbox。取消/解绑是网站能力，不杜撰 `/取消` 或 `/解绑` QQ命令。
- CF源码位于 `E:/CodexData/sites/ai-check-20261011/cloudflare-qq`；其部署、管理令牌和DO状态由独立仓交接，不随本镜像更新。

## Read next

读取 [HANDOFF 当前摘要](HANDOFF.md#当前代码和发布状态)、[CHANGELOG](../CHANGELOG.md) 最新条目与 [DEV_GUIDE](../DEV_GUIDE.md) 更新记录规范；只按任务追加 [统一菜单契约](changes/2026-10-23-qq-unified-menu.md)、[模板同步](changes/2026-10-24-qq-reply-templates.md)、[QQ 独立开关/图库/接管](changes/2026-10-27-qq-controls-gallery.md)及当前发布记录。历史规格中的“未合并/未部署”不能覆盖后续完成记录。索引不保存或授权使用实际凭据，不以真实QQ发送或收费生成作为常规测试。

## NapCat邀请月卡（2026-11-11指引更新）

2026-11-13本地功能补充：后台可通过福利专用入口按用户调整本周期总月额度和剩余天数，保留已用量，支持0额度停用和0天到期；并发版本与授予次数保护、独立权限及事务审计不可省略。管理员手动延长后月总额度不按30天刷新。257迁移及当前是否部署以[本轮记录](changes/2026-11-13-welfare-admin-adjustment.md)为准，不能把下方r2线上状态理解为已包含此功能。

2026-11-11最新规则：QQ福利码兑换为独立 `welfare` 订阅，同周期累加额度且固定首张到期；购买/普通码为 `standard` 继续原续期规则，两者期限及用量隔离。历史纯福利已迁移，详情与短版本r2部署见[当前交接](HANDOFF.md#当前代码和发布状态)和[额度记录](changes/2026-11-11-welfare-subscription-quota.md)。版本命名遵守DEV_GUIDE，不再追加功能名或SHA。

`qq-invite` 是本仓内独立福利模块，另接同服务器NapCat Docker服务与普通福利QQ账号；不复用官方机器人OpenID，不替换生图/智商处理。管理员入口是“福利管理 → QQ邀请奖励”（`/admin/welfare`），包含“连接与扫码登录”和“活动与奖励”。源码 `backend/internal/napcat/`、`backend/internal/qqinvite/`、`backend/internal/handler/qq_invite*.go`、`frontend/src/components/admin/NapCatWelfarePanel.vue`。

最新发布与生产部署以 [HANDOFF当前摘要](HANDOFF.md#当前代码和发布状态)及其带日期记录为准；实现背景见[首次开发记录](changes/2026-11-10-napcat-invite.md)，首次接入、已有部署升级与扫码见[部署指引](../deploy/NAPCAT_SETUP.md)。已有部署沿用原Compose项目及生产overlay、独立 `QQ_INVITE_ENCRYPTION_KEY` 和QQ会话；应用更新无需重新初始化。

当前源码已取消QQ账号等级门槛，不查询等级；低等级和未知等级均不影响资格。指定机器人在线及账号匹配校验、活动内跨群去重继续生效；普通approve不能证明邀请者，未知归属不自动发奖。旧 `pending_level` 在邀请奖励接收启用且到达重试时间后自动按新规则继续，旧 `ineligible` 不批量追补，可由管理员逐条重查；新账号/连接故障使用 `received` 等待重试。历史等级保留，新记录为NULL，后台及 `{level}` 变量显示“未采集”；自定义活动文案需检查旧门槛，历史快照不改写。此处描述源码规则，实际部署版本仍查最新交接。

活动启用与邀请奖励接收两个开关独立于签到/抽奖，启用前须准备成员基线和月卡库存。仅当前及保存后均停用、从未启用、无任何库存及邀请/入群历史的草稿可改订阅分组；版本冲突与分组锁定分别处理，规则见[草稿修复记录](changes/2026-11-11-napcat-campaign-group.md)。未知投递仅原码显式重发。源码完成、镜像发布、QQ在线和真实发奖全链路分别验证。
