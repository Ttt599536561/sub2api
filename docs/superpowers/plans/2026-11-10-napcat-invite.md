# NapCat 与邀请月卡实现计划

> 用户于 2026-11-10 明确选择本次完整实现：NapCat 独立 Docker 服务与 Sub2 同服务器；保留现有官方机器人；后台扫码重登；自动邀请月卡。
> 执行采用 subagent-driven-development、TDD、独立审查。所有缓存/工作树位于 E 盘。

**Goal:** 实现可配置的 NapCat 接入、后台扫码管理、可信邀请和 QQ 等级校验、活动跨群唯一奖励、专用月卡库存与可靠通知。
**Architecture:** 新 napcat 包仅承担 WebUI/OneBot 协议；qqinvite 包负责 SQL 持久化活动、库存、事件和 outbox；独立 handler/routes/runtime 接入现有认证和生命周期。Vue 福利页增加 NapCat 福利入口。现有官方群生图和智商服务保持原处理。
**Tech Stack:** Go/Gin/database/sql/PostgreSQL；Vue3/pnpm/Vitest；NapCat v4.18.42 兼容适配，Docker sidecar。

## 已锁定的行为
- 真实 QQ 账号等级 >10；未知/失败不发卡。group level 不可替代。
- 同一活动全群按真实QQ号去重；一个邀请者可邀请多个新人。
- 只采信明确邀请者字段和可靠申请关联，不把 approve 操作人当邀请人。未确认归属记待核验。
- 活动启动前同步当前成员基线。实际入群时间绑定活动版本；暂停前有效事件可补证。
- 发码只分配兑换码，用户自行兑换；同订阅group_id续期、不同组并存。
- 月卡码库引用现有redeem_codes；数据库约束保护未分配码不被兑换、码元数据不被普通入口改写。
- 所有通知任务持久化、租约、重试、原码补发。私信成功后祝贺，失败按真实状态提示。
- QR、token和码不进入日志/普通响应；管理员会话切换清除私有数据。
- 同服务器Docker，WebUI和OneBot内部互访，外网只有Sub2鉴权接口/签名事件入口；不挂Docker socket。
- 生产部署和扫码需要目标运行环境，源码完成与线上启用分别验证。

## Task 1: 协议适配（napcat包）
Files: backend/internal/napcat/{client.go,webui.go,onebot.go,events.go,*_test.go}
- [x] 用httptest写RED：WebUI SHA256(token+".napcat")、Bearer Credential、HTTP200业务错误、2FA、QR状态。
- [x] 实现同实例凭据缓存、401失效后至多一次刷新、安全超时/响应大小/不跟随重定向、目标host白名单。
- [x] 测试并实现OneBot登录/状态、成员列表、qqLevel、邀请系统消息、@群与私信；发送结果unknown不伪成功。
- [x] 测试事件签名、数字QQ标识、invite/approve身份区别、错误/缺字段拒绝。
Run: go test -tags=unit ./internal/napcat

## Task 2: 活动、库存和发奖（qqinvite包）
Files: backend/internal/qqinvite/{types.go,store.go,config.go,campaigns.go,inventory.go,events.go,worker.go,*_test.go}; backend/migrations/253_qq_invite_rewards.sql
- [x] 定义导出的类型和API，给前端/root发送JSON契约。
- [x] RED后实现加密连接配置、version冲突、活动规则版本、成员基线、全活动唯一新人记录。
- [x] RED后实现批量导入现有码/生成月卡码、预检、库存和记录分页，错误不泄漏码。
- [x] 用数据库触发器/约束阻止未分配码使用和通用后台更改池码；测试导入/兑换竞态。
- [x] RED后实现带鉴权接收的事件持久化、真实邀请证据核验、等级阈值和过期/暂停边界。
- [x] RED后实现同事务分配码/outbox；worker租约、限流退避、缺货补发、群和私信任务隔离、手动原码重发。
- [x] PostgreSQL测试并发跨群/重复事件、最后一码、失败回滚、服务重启、未知发送、暂停与补证。
Run: go test -tags=unit ./internal/qqinvite ; real PG uses QQ_INVITE_TEST_DATABASE_URL loopback only.

## Task 3: 管理页面（前端）
Files: frontend/src/api/qqInvite.ts; frontend/src/components/admin/QQInvitePanel.vue及子面板/测试; WelfareSettingsView.vue; 中英locale
- [x] RED后实现API调用与session ownership，token输入不回显，二维码本地渲染。
- [x] 连接配置、状态/QR/刷新；活动创建/编辑/基线/启停、文案预览。
- [x] 码库批量预检/导入/生成与库存；邀请发放记录/筛选/重新核验/原码补发。
- [x] 核验“未配置/服务不可达/未登录/等扫码/等确认/在线”显示，不能把容器运行当QQ在线。
Run: pnpm exec vitest run 新增测试及WelfareSettingsView相关 ; pnpm typecheck ; pnpm build

## Task 4: 系统集成（root）
Files: backend/internal/handler/qq_invite_{handler,runtime,wire}.go; routes/qq_invite.go; handler/wire.go/handler.go; cmd/server/wire.go/wire_gen.go
- [x] 先写handler权限、no-store、原始body HMAC、QQ账号范围和输入上限测试。
- [x] 独立管理员API，不将配置token或码发送到普通用户；接入跨域保护和审计脱敏。
- [x] Webhook耐久写入后才ACK；配置/登录问题不影响原QQ模块；worker正确Start/Stop。
- [x] 订阅首次并发兑换采取按用户/组事务串行或整事务重试；缓存提交后失效，回归同组/不同组/过期/暂停。
- [x] 生成Wire并验证handler/server相关测试。

## Task 5: 部署、文档和验收
Files: deploy/docker-compose.napcat.yml; deploy/napcat配置模板及文档; CHANGELOG.md; docs/HANDOFF.md; docs/changes/2026-11-10-napcat-invite.md; tasks/prd...
- [x] sidecar独立持久化，固定版本，内部端口，主应用密钥/host白名单变量；提供扫码与恢复指引。
- [x] 更新PRD为用户批准的NapCat方案；业务边界和实际验证记录写交接。
- [x] 独立规格审查，再质量/安全审查；修复后针对性回归。
- [x] 集成测试、组件/类型/构建及代码变更检查完成，主目录原WIP保留。
- [ ] 浏览器交互验收：本地预览启动被自动审批拒绝，未重试。
- [ ] 目标服务器/扫码/测试群验收：0.2.20-r2已发布并验证，服务器部署/真实QQ扫码仍待用户执行。
