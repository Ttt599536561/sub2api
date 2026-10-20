# 专属推广子域名实施计划

> **For agentic workers:** Use subagent-driven-development for bounded modules; execute test-first, then independent specification and code review. Shared interfaces below are fixed before integration.

**Goal:** 专属子域入口无明文邀请码，主站所有新注册可靠绑定指定推广人。

**Architecture:** PostgreSQL 维护版本化域名映射；Redis 一次性交接与 opaque 归因 cookie；主站 auth context 和 OAuth 本地 pending 快照传递可信来源；账号创建与归因共用 Ent 事务。

**Tech Stack:** Go/Gin/Ent/PostgreSQL/Redis；Vue3/TypeScript/pnpm/Vitest。无新依赖。

## 环境与基线

工作树：`E:/sub2二次开发项目/.worktrees/affiliate-subdomain-20261020`，分支 `codex/affiliate-subdomain-20261020`，基线 `3df78abd9`。

每次 PowerShell 调用先执行：

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
```

所有日志与临时文件复用主项目 `.cache/affiliate-subdomain-20261020`。保留主目录 WIP。不得使用 C 盘缓存/工作树，不运行未经磁盘位置核验的 Docker。

- [x] 读取交接、现有邀请/注册/收益路径，用户选择专属子域名。
- [x] 服务基线 `go test -tags=unit -count=1 ./internal/service -run TestAffiliate` 通过。
- [x] 前端邀请/注册基线。

## Task 1：域名数据与管理员接口

Files: `backend/migrations/247_affiliate_promotion_hosts.sql`；`backend/internal/service/affiliate_domain_service.go`；`backend/internal/repository/affiliate_domain_repo.go`；现有 `affiliate_service.go`、`affiliate_repo.go`、`handler/admin/affiliate_handler.go`、`server/routes/admin.go` 及对应测试。

- [x] 先测试规范化、严格子域、重复归属、停用墓碑、revision 失效、仅域名用户列表和清除。
- [x] 新增独立域名仓储扩展接口，由 affiliateRepository 实现；不强制修改原全部 mock。
- [x] PostgreSQL 表保留墓碑，删除推广人后 inviter_id 置 NULL；启用映射按邀请人唯一。更新/停用同事务递增 revision，幂等重复保存不使有效归因失效。
- [x] 新增以下公共契约，服务验证主站为固定 HTTPS origin：

```go
type AffiliatePromotionDomain struct {
    Hostname string
    InviterID int64
    Enabled bool
    Revision int64
}
// AffiliateService methods:
// ResolveAffiliateDomain(ctx, hostname) (*AffiliatePromotionDomain, error)
// SetUserAffiliateDomain(ctx, userID, domain) (*AffiliatePromotionDomain, error)
// ValidateSignupAffiliateAttribution(ctx, attr) error
// BindSignupAffiliateAttribution(ctx, userID, attr) error
```

- [x] 绑定必须有外部 Ent 事务并在该事务中权威校验/锁定总开关、域名版本与推广用户；相同邀请人幂等、不同邀请人拒绝。
- [x] `PUT /admin/affiliates/users/:user_id/domain` 独立原子保存，返回域名和专属 URL；列表/detail 返回 `affiliate_domain`、`referral_url`。
- [x] 运行 `go test -tags=unit -count=1 ./internal/service ./internal/repository ./internal/handler/admin -run 'Affiliate.*Domain|Affiliate.*Attribution|Affiliate.*Promotion'`，并运行对应真实 PostgreSQL integration。

## Task 2：HTTP 入口与不透明归因

Files: `backend/internal/service/affiliate_attribution_store.go`、`backend/internal/repository/affiliate_attribution_store.go` 与仓储测试；`backend/internal/handler/affiliate_promotion.go`、`affiliate_promotion_test.go`；`handler/auth_handler.go`、`server/router.go`、`server/routes/auth.go`。

- [x] 使用 miniredis 测试未知/伪造 Host、固定主站、cookie 属性、一次性消费、过期、复放、Redis 故障、已有归因不覆盖。
- [x] 在前端 middleware 之前识别精确 Request.Host；推广域名只允许 GET/HEAD 的 `/` 和 `/register`，其他路径拒绝。主站自身快速跳过域名查询。
- [x] 生成 32 字节随机票据（2 分钟），主站 claim 先校验 hostname 再 GetDel；从独立随机 token 创建 30 天归因记录，使用 `__Host-` cookie，303 跳干净注册页。
- [x] 入口/claim/status 设置 no-store/no-referrer；错误与日志不包含 token。入口和领取按 IP 限流，故障不降级归因。
- [x] 主站 auth middleware 加入可信 context；持有无效 cookie 时以非 nil 的无效归因标记保留锁定，新注册拒绝但已有账号登录继续。
- [x] 独立状态接口仅返回 `{locked:boolean}`；普通成功注册、已有账户成功登录/绑定后清理 cookie。
- [x] 运行 `go test -tags=unit -count=1 ./internal/handler ./internal/server -run 'AffiliatePromotion|AffiliateAttribution'`。

## Task 3：注册与 OAuth 原子归属

Files: `service/affiliate_attribution.go`、`service/auth_service.go`、`service/auth_oauth_email_flow.go`、`service/auth_email_oauth_auto.go`；`handler/auth_oauth_pending_flow.go`、`handler/auth_email_oauth.go`；四个 provider 的 CompleteRegistration 函数；对应测试。

- [x] 定义设计文档中的归因类型和以下 context helpers：

```go
func WithSignupAffiliateAttribution(ctx context.Context, attr *SignupAffiliateAttribution) context.Context
func SignupAffiliateAttributionFromContext(ctx context.Context) (*SignupAffiliateAttribution, bool)
func (s *AuthService) AffiliateService() *AffiliateService
```

- [x] 先验证可信来源覆盖伪造 aff_code、已有用户不绑定、绑定/身份/pending 失败全部回滚、计数不虚增。
- [x] 普通新注册创建+准入邀请码占用+邀请绑定同事务；复用已有 TxFromContext。
- [x] 可信 OAuth 自动和补全流程：用户、身份、邀请与 pending 消费共用外部事务，提交后签发 token；保留默认权益/验证码/风控语义。
- [x] pending LocalFlowState 保存并恢复可信快照；无效非 nil 标记不得当作无归因。优先级为 pending 快照、可信 cookie、旧 aff_code。
- [x] 覆盖 GitHub/Google、LinuxDo/OIDC/微信/钉钉和 legacy OAuth 的实际新建分支；邮箱冲突复用 existing 不执行新绑定。
- [x] 运行相关 auth/affiliate unit、handler/provider 测试及真实数据库事务回归。

## Task 4：OAuth state 绑定快照

Files: `handler/auth_oauth_affiliate.go` 与测试，各 provider start/callback 的 state 处理位置。

- [x] 按 provider + 已生成 OAuth state 保存可信快照，区分明确无来源与来源失效，TTL 以现有 OAuth 流程期限为准。
- [x] callback 在现有 state 校验成功之后恢复快照，再允许自动创建或 pending 创建；不同 state/provider 不能互用。
- [x] 测试流程开始后更换 cookie、快照缺失/过期/Redis 故障、重复 callback 和已有账号登录。
- [x] 微信支付 OAuth 不接入推广归因。

## Task 5：前端配置和注册链接

Files: `frontend/src/views/admin/affiliates/AffiliateDomainDialog.vue` 与测试、`SettingsView.vue`、`api/admin/affiliates.ts`、`types/index.ts`、`views/user/AffiliateView.vue`、`views/auth/RegisterView.vue`、独立归因 API 和中英 locale。

- [x] 管理专属域名独立弹窗，支持 lookup 任意用户、配置/修改/清除域名；列表显示和复制链接。会话/权限/弹窗生命周期变化后迟到响应无效。
- [x] 用户邀请页优先服务端 referral_url，无专属域名回退原链接。
- [x] 注册页读取独立 locked 状态，隐藏返佣码，跳过 URL/localStorage 来源传递；准入邀请码继续显示。
- [x] 先运行行为回归，再实现；完成后跑专项、`pnpm typecheck`、`pnpm lint:check`、`pnpm build`，记录实际命令版本和输出。

## Task 6：复核、交付和启用说明

- [x] 独立审查需求覆盖和代码质量，修复已确认问题并补有意义回归。
- [x] 汇总运行后端相关包、真实数据库事务/迁移回归、前端专项/类型/lint/构建。输出精简验证摘要。
- [x] 更新 CHANGELOG、HANDOFF、`docs/changes/2026-10-20-affiliate-subdomain.md` 和通用部署说明；本机含真实域名/邮箱的启用清单放 output。
- [x] 配置说明覆盖 DNS 指向现有入口、专属 hostname HTTPS 证书、反代保留 Host/票据 query 脱敏、主站 frontend_url、完整邮箱查验用户 ID、后台绑定、实际注册与返佣验收。
- [x] `git diff --check`、文档链接/跟踪检查后提交独立功能分支。生产 DNS/证书/账号设置/部署未经实际执行不得宣称生效。

## 收尾记录

全后端 unit 58 包 / 22,547 叶通过，18 个既有条件跳过；前端扩大回归 217 项、PostgreSQL 159 叶、类型/全量 lint、Linux 嵌入式编译通过。静态分层复核将 Redis 访问抽至 repository，handler 仅依赖 service 接口；源码/行为回归随后重新执行。Windows 会话补充现有 Git sh，未更改备份源码或全局环境。实际记录见 `docs/changes/2026-10-20-affiliate-subdomain.md`。

PG 已停止；其临时数据、4 个辅助文件及目录联接删除被自动审批拒绝（blocked by policy，未给具体规则），原位保留，没有绕过。生产 DNS/证书/反代由用户自行操作，本轮不宣称已上线。

应用源码 `14aec2850` 已提交并快进整合到主目录 main，67 份本轮源码与验证快照一致，原 WIP 完整保留；未推送或发布。
