# 专属推广子域名与可靠注册归因

任务开始于 2026-10-20，最终验证完成于 2026-10-21（Asia/Shanghai）。源码：`14aec28502a0e26cf97895d8d265f7d978f3ac6f`（`feat: add trusted affiliate subdomain registration`），已快进整合到本地主目录 main。

后续状态：本功能已随 [0.2.17-r3](../../deploy/CUSTOM_IMAGE_0.2.17-r3.md) 发布，生产已运行 r3 并正式启用专属推广入口。下文保留实现阶段的原始验证与部署状态，后续发布结果见文末，不把不同阶段的计数混用。

## 改动与业务口径

为既有推广用户配置一个主站下的专属子域名。浏览器进入推广域名后由后端解析用户归属，通过一次性不透明票据进入主站，再跳转干净注册页；链接、注册页和公开状态接口不展示推广邀请码。管理员可独立配置、修改、清除域名；邀请页优先展示专属链接。

已有有效的服务端专属来源不被后来的邀请码/推广入口覆盖，也不刷新原 30 天期限。OAuth 开始时固定来源，并在 pending 中保存有效来源、明确无来源（JSON null）或无效锁定状态；请求体、后来的 cookie 和 URL 参数不能覆盖流程快照。历史 pending 缺字段保留兼容回退。

普通邮箱及全部第三方新建分支将用户、准入邀请码、邀请关系、第三方身份与 pending 消费（适用时）放在同一事务。失败回滚不留账号或邀请计数；提交后签发登录结果，发 token 失败不删除已正确绑定的账户。已有账号登录/绑定不新绑邀请人，注册准入邀请码继续按原规则验证。充值返利比例、期限、冻结和上限沿用原有功能。

## 实现与复核

- 新增迁移 `247_affiliate_promotion_hosts.sql`：版本化域名映射、启用用户唯一约束、软/硬删除用户时停用及版本递增。停用 hostname 作为墓碑保留，旧归因不因域名复用而重新生效。
- Redis 访问通过 service 存储接口与 repository 适配器隔离，HTTP handler 不直接依赖 Redis；GETDEL、SetNX 和计数首次 TTL 由仓储测试覆盖。Redis 交接票据两分钟，主站不透明归因最多 30 天；host-only `__Host-` Cookie 使用 Secure、HttpOnly、SameSite=Lax、Path=/。主站严格 HTTPS origin、不接受显式端口。
- 精确 Request.Host 归属，不采用任意转发 Host；标准化合法尾点写法，已知主机异常端口拒绝，原始非 ASCII Host 在去空白前拒绝。入口在 SPA fallback 之前处理，推广域名不成为普通应用/API 站点。
- 读取已有来源的 Redis/数据库暂时故障不会被当成“没有来源”，不能换绑。失效来源只在新的有效入口下显式恢复；请求限流、一次性消费、无缓存及日志防泄漏一并处理。
- 主站地址查询使用短期缓存，当前实例的设置更新立即失效；防止给每次主站请求增加设置数据库查询。跨实例主站地址缓存最长 30 秒；映射与归因绑定仍查询并锁定权威记录。
- 注册事务相关 Setting/User/Redeem 读取复用当前 Ent 事务，避免连接池满时等待第二连接；OAuth 用户名补全在成功提交后执行，保留已采纳的昵称。
- 前端域名编辑独立于邀请码/比例，异步搜索/保存受会话、权限和弹窗生命周期保护。归因状态独立读取，不进入共享 public settings 缓存；状态失败可重试，普通登录不等待归因查询。

独立审查确认并修复了存储故障换绑、Host 写法/失效配置绕过、明确空来源被后来 cookie 覆盖、token 失败删除补偿、事务内另租连接及用户名提前独立提交问题。每类均补充回归，复现记录与最终输出分别保存。

## 最终验证

以下均为本轮当前源码验证，不沿用旧发布数字：

| 范围 | 实际结果 |
| --- | --- |
| 全后端 unit | `go test -tags=unit -count=1 -p 1 -json ./...`，58 个有测试包通过，22,547 个叶测试通过，18 个既有条件/占位测试跳过，0 失败。 |
| PostgreSQL 16.15 | 新隔离数据库中运行全部 Affiliate、SettingRepo、UserRepo、RedeemCodeRepo 相关 integration；159 叶通过，0 失败/跳过。含真实注册 A/B 归属、撤销、外部事务回滚、并发停用及单连接矩阵；前后 43 份 backend 源文件哈希一致。 |
| Redis 适配器 | 全 unit 中覆盖 miniredis 的并发一次性消费、原子计数、TTL、不存在/故障/nil client；未宣称使用真实 Redis 服务。 |
| 前端 | 25 文件 / 217 项扩大回归全部通过；类型检查、全量 ESLint、生产构建通过。复核 24 份前端源码哈希与类型/构建证据一致。 |
| Go 静态检查 | CI 同版 golangci-lint 2.14.0，`run --timeout=30m ./...`，0 issues。 |
| Linux 构建 | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -p 2 -trimpath -tags embed -ldflags="-s -w" ./cmd/server` 成功；295 份嵌入前端资源逐一比对一致，ELF64 x86-64，129,200,288 字节，SHA256 `e2929210325b5dd3efa44b54ae5c700addaa66dae5d53a4247474f9af4c3ab0e`。仅编译，不表示 Linux/容器运行或线上验收。 |

全 unit 初轮的三个旧 PgDumper 测试因 Windows 会话 PATH 缺 `sh` 失败；仅为第二轮会话加入现有 Git 的 bin 目录后，全仓重跑退出 0，未改备份业务或全局环境。18 项跳过包括外部 TLS/真实 API、可选数据库/Redis/插件集成及既有占位条件。本轮独立 PostgreSQL 专项不受这些跳过替代。

初轮静态检查发现 handler 直依赖 Redis、一个测试 rows.Close 未显式处理及字符判断写法问题；已按项目分层抽出存储接口/仓储适配器并修正，最终全 unit 与静态检查均重新执行。Browserslist 提示本机 caniuse 数据较旧，本轮没有借此变更依赖。

必要证据位于主项目 `.cache/affiliate-subdomain-20261020`：`final-verification-summary.json`、`final-backend-unit-green.jsonl`、`root-frontend-regression.json`、`domain-final-snapshot-summary.json`、`lint-final.log` 与 `linux-build.log`。67 份本轮 Go/SQL/TS/Vue 文件在最终验证后再次核对哈希，未变化。

## 部署与恢复

详细步骤见 [专属推广子域名启用说明](../../deploy/AFFILIATE_SUBDOMAIN.md)，设计与实施任务见 [设计](../superpowers/specs/2026-10-20-affiliate-subdomain-design.md) 和 [计划](../superpowers/plans/2026-10-20-affiliate-subdomain.md)。真实域名/邮箱仅在用户聊天与本机 output 启用清单中保存，不硬编码或纳入仓库文档。

前后端应一起升级并应用迁移 247。DNS、HTTPS、保留主站/推广主机 Host、主站配置和实际账号查验均需在服务器验收。实现阶段结束时尚未推送、发布镜像或部署生产，原已发布 0.2.17-r2 的来源保持不变；这是当时的阶段状态，后续 r3 发布见文末。

域名墓碑及 revision 不得手工删除/重置。软删除推广账号会永久停用其当前域名；恢复账号后须管理员重新配置，旧访问仍失效。数据库恢复须同时处理在途归因缓存/pending，不能让恢复的旧版本号重新接受已撤销归因。回退到不支持此功能的旧应用需先停止入口并处理仍在进行的注册；已完成邀请关系留在原有表中。

本机验证使用 E 盘独立工作树，共用主项目依赖缓存。PostgreSQL 测试实例与数据均在主项目 .cache；仅以 E 盘 ASCII 目录联接解决 Windows 工具对中文路径的兼容，不安装系统服务。Docker 和生产数据库未参与本轮验证。

## 本机清理限制

测试 PostgreSQL 已正常停止，54392 端口无监听。删除本轮 PostgreSQL 数据、4 个临时辅助文件和 ASCII 目录联接的 PowerShell 原生命令在创建进程阶段被自动审批以 `blocked by policy` 拒绝，未提供具体规则；没有换方式或重试删除。保留约 112,246,806 字节的本轮测试数据及上述文件/联接，收据为主项目 `.cache/affiliate-subdomain-20261020/postgres-final-cleanup-blocked.json`。可复用工具 ZIP/runtime 与验证摘要继续保留，未清理其他任务的数据。

## 主目录整合

整合前为主目录 10 项跟踪 WIP 保存逐文件备份及 SHA256，只临时 stash 有交叠的 `.gitignore`；快进整合后已恢复。其余 9 项跟踪文件与 4 份单独未跟踪文件哈希全部一致，`.gitignore` 原一条白名单编辑与原差异完全一致；既有 `backend/internal/web/dist/` 没有改动。本轮 67 份源码在主目录逐字节匹配最终验证快照，没有把用户 WIP 纳入功能提交。WIP 恢复 stash 和备份保留，未删除用户原分支或历史工作树。

本轮临时 Linux 二进制与前端构建副本已清理，哈希和验证摘要保留。任务工作树已从 Git 注销，功能分支完整保留；Git 删除因目录非空留下残余，随后原生 PowerShell 删除该已核验残余目录被自动审批以 `blocked by policy` 拒绝，未给具体规则，未绕过或重试。残余目录仍在主项目 `.worktrees/affiliate-subdomain-20261020`，不再作为可用工作树；收据为 `.cache/affiliate-subdomain-20261020/final-artifact-cleanup.json`。

## 2026-10-21 发布状态更新

本功能已随 `ghcr.io/ttt599536561/sub2api-custom:0.2.17-r3` 发布。功能实现提交仍为 `14aec28502a0e26cf97895d8d265f7d978f3ac6f`；最终发布源码 / OCI revision 为 `c1b2134d091c8f0dabe9715788043c2f05ce8bb8`，发布控制为 `4950ee5b6258aa5137f9b1c9fd10ead6702e1442`。精确源码 CI / Security Scan 以及 [GitHub 发布运行 39470386469](https://github.com/Ttt599536561/sub2api/actions/runs/39470386469) 均成功，原生 Linux amd64 / arm64 镜像通过版本、运行资源与安装状态检查；本轮没有 macOS 构建或运行。

固定 index digest 为 `sha256:52736abda13f53c41a051c2d11a96ca07c8ed5d6c64f3f87731c2a12202e6337`。北京时间 2026-10-21 02:03:43 完成匿名全部 24 个运行层、89,516,749 字节及 index / manifest / config / 架构 / revision / version 校验；attestation 内容未验证。本轮发布快照的 CI 统计、各架构 digest 和完整运行链接见[r3 发布记录](../../deploy/CUSTOM_IMAGE_0.2.17-r3.md)，上方实现阶段测试数字作为历史记录保留。

发布结束时生产仍为 r2、入口维护 503；后续同日用户升级并明确要求直接启用，结果如下。既有备份、证书及历史清理拒绝记录保留。

## 2026-10-21 线上启用

2026-10-21 02:34（北京时间），用户自行升级 r3 后，按授权完成主站 HTTPS origin、目标账号域名映射及宿主代理信任配置，并撤除专属入口维护页。仅重建 Sub2API 应用，其他容器 ID 不变；Nginx 检查、重载及主站健康检查均通过。

单次 SSH/OS-root 维护工具沿用 SettingRepository 与 AffiliateService，在一个 serializable 事务中保存 frontend_url 和标准域名/必要邀请摘要；其他设置逐项保持不变，intent/completion 审计成功。没有创建管理员令牌或 API key，也未修改密码、验证码配置。工具本机 30 叶单元测试、8 个真实 PostgreSQL 场景及 Linux 静态编译通过；它不属于发布镜像。上传的维护二进制已清理，备份和收据保留，本轮未重新构建镜像。

02:35 实际 HTTPS 验证：两次 303 后到达不带查询参数的主站 `/register`，状态 200；交接响应为 no-store/no-referrer，Cookie 为 host-only、Secure、HttpOnly、SameSite=Lax。归因 locked=true，独立空会话 false，票据重放 400；仅检查本次测试 Cookie 对应 Redis 记录，确认目标邀请人、域名 revision 及约 30 天期限，随后删除该测试记录。未在生产创建测试账号或进行真实充值，因此不宣称真实注册或返利到账已验收。

域名映射 revision 为 1；既有邀请返利参数保持原值。可信代理限定当前 Docker 宿主网关 CIDR，保留原转发头兼容设置。主站交接路径继续禁止缓存和票据查询日志；HTTPS 证书与续期配置沿用已验证结果。回退配置备份及操作收据已保存，域名墓碑/revision 仍不得手工重置。

本机私有证据位于 `output/affiliate-subdomain-20261020/activation-live-20261021.json` 与同目录 `ACTIVATION.md`，辅助工具 manifest/源码/日志在 `activation-maintenance/`。真实域名、账号、连接地址和 SSH 信息未纳入公开记录；公开结论由本节归档，不依赖未跟踪附件作为唯一交接。主目录原 14 项文件哈希与原 binary diff 均保持一致。
