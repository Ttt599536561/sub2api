# QQ 群生图首版实现与验证记录

日期：2026-10-23（Asia/Shanghai）。状态：**独立工作树实现，未提交、未合入主目录、未推送、未发布或部署**。

工作树：`E:\sub2二次开发项目\.worktrees\qq-image-generation-20261023`；分支 `codex/qq-image-generation-20261023`；基线 `c9a9209ea79163dbbf1fb20b5db2dc67c6b15394`。用户已批准开发，并分别明确选择 **QQ 官方机器人**与 **OpenAI 兼容 Images**。详细需求合同见[主设计](../superpowers/specs/2026-10-23-qq-image-generation-design.md)，执行拆分见[实施计划](../superpowers/plans/2026-10-23-qq-image-generation.md)。真实凭据、平台权限、价格与生产部署均未在本轮代填或联调。

## 用户和管理员可以完成的流程

- 用户在授权群 `@机器人 /绑定` 获取原站短期链接或12位申请编号；登录原站 `/qq/bind`，验证当前邮箱并在当前会话确认，明确同意消费授权后得到单次核对码；原 QQ 在原群 `/确认绑定 核对码` 激活。链接/编号只定位申请，不授予消费权限。
- 申请候选账号与会话锁定，重复/过期/冲突/旧版本拒绝；正确码由错误身份提交使候选失效，任意错误码只限流提交者。身份按 App、群作用域与成员 opaque ID 隔离，多群须分别授权。昵称与邮箱不用于认领 QQ。
- `/生图 提示词` 在资格、群、参数、限流与配额通过后，对同一个 `users.balance` 原子预留单张固定价。生成有效图片并由原网站账号安全领取时结算；失败释放。排队可取消，已派发不能保证取消。
- 用户 `/qq/orders` 查看订单，`/qq/orders/:id` 查看四类状态与精确金额、鉴权预览/下载；QQ `/任务` 与 `/重发` 只查询本人当前授权、原群和原授权版本的订单。解绑后网站原账号继续拥有旧单，新的 QQ 授权不会继承旧版读取权限。
- 福利中心“QQ 群生图”页签配置两个凭据分区、基础 URL、固定模型/规格/USD价格、授权群、并发/额度/期限，以及可选精确图片 CDN 域名。用户福利与账户导航都有入口；关闭福利不会丢失 QQ 订单入口。
- 管理员查看绑定、订单和审计，撤销授权、对已结算订单全额退款，对 `held + reconciling` 订单填写理由并确认失败释放。原生同步 Images 没有标准任务查询接口，“记录人工核查”只保存真实操作记录，不伪装为查询或再次生成。

## 实现选择与范围

### 身份和邮箱

新增独立业务授权域 `internal/qqimage`，不添加 QQ 登录 provider。旧 `auth_identities.verified_at` 兼容回填不作为新功能邮箱控制权证明。用户对当前邮箱实际验证后写入 `qq_image_email_verifications`，近期授权另绑定会话摘要/当前邮箱，15分钟有效；授权与解绑需要该近期证明。

专用 `AuthService.SendQQImageEmailCode` 读取当前 active 用户邮箱，复用原站验证码、冷却、次数上限和实际同步发送结果，但不套用新注册域名额度。未配置 SMTP 时明确失败，不把入队成功视为邮件送达。原注册白名单/额度规则未修改。

绑定首屏 `/qq/bind` 在 SPA 响应写入前强制 `no-store / no-referrer`，不全局改变其他页面策略。前端只保存短期申请定位信息，授权核对码不写持久存储；账号/会话切换清空敏感状态，并阻止迟到请求写入或发出旧身份操作。

### 钱包、价格、恢复

- 网站可用余额是 `users.balance`，冻结后不重复减 `frozen_balance`。本功能使用已有资金，不限制充值/赠送/兑换来源；不扣月卡或福利钱包，不累计 QQ 福利消费奖励。
- 用户价格与 `users.balance` **固定采用 USD**，8位十进制精度，无汇率或可切换币种。上游成本、上游币种单独存为 `null`，状态 `unknown`，不得当作零成本；标准 Images 没有货币成本返回，本轮不猜成本，响应缺少 usage 不影响有效图片交付。
- 订单、独占 hold、账本及缓存失效 outbox 原子持久化；capture/release 在同一订单终态互斥，refund 关联原 capture 并持久化退款意图。现有 API 后付和透支政策保持；API 欠款不挪用 QQ 独占冻结，QQ 自身不足额不预留。
- 同一领域采用跨实例事务 advisory lock 统一锁序，网络请求在事务外。首版面向低并发授权群，后台可配配额；单实例 worker 最多5个并行执行槽，配置的更高全局值不保证单实例达到该吞吐。
- 订单固定价格/配置/凭据版本、付款 user_id、原授权 revision 和业务日。钱包行锁等待之后再次读取数据库时间/资格；截止不能被锁等待、重试或重启延长。
- 生图执行意图在 POST 前持久化。同步超时、断连、429/5xx及无法确认交付的响应进入 reconciling，不再次 POST、不换上游。预留时确定总期限，默认2小时、最长2小时；30分钟产生一次 `order.reconcile_attention` 管理审计提示。没有配置外部提醒接收人，也未发送任何外部通知。
- 明确终态失败释放；到期即只允许释放恢复，即使数据库短暂失败仍 held 也禁止迟到 capture。图片先私有持久保存再捕获，崩溃可恢复；已释放后的安全迟到图可免费由原网站账号领取，不追扣。
- 站内图片存 PostgreSQL `bytea`，默认7天保留，最长可配30天；到期后台实际清空图片字节并标过期。原订单/提示词/审计/配置版本暂保留，**尚无长期自动删除策略**，后续需按运营数据保留政策处理，不能宣称已实现180天清理。

### QQ 和上游适配

- QQ 使用 Ed25519 对时间戳原字符串加原始 HTTP body 验签，验证 App/消息身份；业务事件永不免签。业务变更与事件指纹/结果在 `ProcessEvent` 同一事务提交，未知数据库错误整体回滚并允许可信重投。提交后 QQ 回复结果未知时不重复执行业务或盲目发第二次回复。
- op=13 挑战与业务严格分开；未签挑战仅接受严格字段及1–256位 `[A-Za-z0-9_-]` token，防止把业务 JSON 当 plain_token 获取合法签名。此限制若与实际 QQ 挑战格式不符，必须先核对文档/真实联调再调整，不能放宽为可签任意文本。
- token 和业务 API 使用 `https://api.bot.qq.com`。群消息以原 `d.id` 作为被动回复 `msg_id`，持久事件/投递意图避免重复序号；受理回复序号1、完成图片序号2，未知回执标 unknown。过期或无权限可从网站领取，新的 `/重发` 建立新上下文。
- 群图片走官方 URL 上传（`srv_send_msg=false`）再发送 media.file_info，不猜未经明确约定的 file_data 编码。本站为该次上传签发高熵短期媒体抓取 token，只存摘要，120秒、最多3次，绑定原订单/授权版本，解绑、权限移除、退款、旧 QQ 凭据撤销后拒绝。图片超过 QQ 5MiB 限制时 `site_only`，仍保留网站交付。
- 媒体拉取是专用平台传输的短时 bearer 例外，不是普通取图权限：普通 `/user/qq-image/orders/:id/image` 始终需要原 owner 的 JWT，管理下载也有角色与审计。应用日志只记媒体请求 Path；部署代理也必须关闭 query 日志，不能记录 token。
- Images 基础地址示例 `https://images.example.com/v1`，末尾追加 `/images/generations`；不重复插入 `/v1`，完整 endpoint 被拒绝。请求只发固定 `model/size/n=1/prompt`，不强塞 GPT Image 不支持的 response_format 或 DALL-E 专属 style/quality。模型与尺寸实际支持须管理员确认。
- 支持有效单张 b64_json 或图片 URL。默认仅抓取 API 同主机，跨 CDN 须配置精确 `image_hosts`。始终 HTTPS443、禁用户凭据/重定向、每次拨号验证公网 DNS 并固定地址，图片请求不带上游 Key；限制20MiB、2000万像素并实际解码。HTTP200空图/坏图不结算。
- Key 用 `QQ_IMAGE_ENCRYPTION_KEY` 的 AES-GCM 加密；空输入保留。明确清除某类凭据会关闭功能并撤销所有旧配置版本对应引用；正常轮换保留旧版在途能力。这里的清除不等于替管理员到服务商撤销远端 Key。
- 简易模式拒绝新平台事件、新网站绑定和启用配置，但保留原用户历史读取、邮箱确认、解绑以及旧授权任务排空/资金恢复；不出现已结算后因本功能模式判断而拒绝取图的情况。

## 迁移和后续配置

新增[迁移248](../../backend/migrations/248_qq_image_generation.sql)，16张 `qq_image_*` 表，使用现有 users 钱包，不改既有 API 结算字段语义。完整历史303份 SQL 已在独立 PostgreSQL 16.15 数据库执行并重复幂等通过；另有每用例独立 schema 的真实并发回归。没有在生产执行迁移。

将来部署前需要：

1. 先备份数据库和稳定64位十六进制 `QQ_IMAGE_ENCRYPTION_KEY`（32字节）。四份 Compose 样例和 `.env.example` 已接入此可选变量；未配置或错误时禁凭据配置/启用，不让整个站点启动失败。更换加密主密钥会使现有凭据不可读，不能把它当普通 API Key 轮换。
2. 管理员在默认关闭状态填写 QQ AppID/Secret、授权群、原站 HTTPS origin、生图 URL/Key/模型/尺寸、USD单价和每日金额上限；需要 CDN URL 图片时填写准确域名。保存只做校验，不暗中请求生图，也未实现收费“测试生成”按钮。
3. 配置官方回调到 `/api/v1/qq-image/events`，允许 QQ 拉取 `/api/v1/qq-image/media`；反向代理需去掉媒体 query 日志，绑定页 query 同样不落日志，不缓存绑定/媒体/私有订单响应。平台 URL许可、群权限、媒体权限、挑战实际格式、域名白名单及 QQ 内登录回跳需实测。
4. 配置并实际验证 SMTP 收件；为现有账号完成本功能当前邮箱补验。真实账号登录、QQ跨群身份和付费上游联调仍需另行授权。
5. 确认上游支持所填模型与尺寸、允许的图URL主机、固定售价与成本风险，以及默认额度/期限/留存；标准同步协议没有原任务查询能力，运营必须接受未知结果到期释放的成本承担规则。

回退时先关闭新单并排空/释放在途 hold、处理待退款和余额缓存 outbox。旧版本不知道 QQ 独占 hold，不能在仍有 held/pending refund 时直接移除表或清零聚合冻结。保留16表、原账本和加密密钥以恢复历史，镜像回退与数据库回退分别评估。本轮没有生成发布镜像、提交或部署。

## 独立审查与修正

一次独立完整审查及一次定向复审确认没有剩余 P1/P2 阻塞项。实际修复并增加回归的发现：

- 挑战 plain_token 可签任意业务消息：限制 token 字符及严格信封，完整攻击链 RED→GREEN。
- 消息先去重再执行导致崩溃吞命令：事件与业务同事务、嵌套事务复用、事务内读取可见，未知数据库错误不消费事件。
- 等钱包行锁跨过排队/资金截止：锁后重读数据库时间；到期不派发、不捕获。
- 派发后改邮箱导致有效图片不收费：旧单结算依据原 owner 是否可领取，邮箱要求仅用于新授权/新单/派发前。
- 既有非白名单邮箱补验被注册额度拦截：专用当前用户发码路径，保留注册原规则。
- 简易模式生成收费但禁止领取：统一禁新与保留历史领取/排空策略。
- 账务结算回包丢失、退款失败、媒体撤销、超限与旧凭据等边界均有真实 PostgreSQL 或模拟协议回归。

## 验收覆盖与实际限制

| 设计验收 | 本轮验证方式及边界 |
| --- | --- |
| A-01～A-08 | 前端登录/注册返回、邮箱与授权状态、手工编号及服务端绑定/候选/会话/作用域回归；签名事件完整HTTP→数据库链路。QQ实际开通能力/链接许可未联调。 |
| A-09～A-16 | 真实PG用户状态/邮箱/余额、API欠款并发、日限额、多群、重复/冲突事件、旧配置及取消/派发/结算竞争；handler归属/显式授权/管理权限。 |
| A-17～A-21 | 模拟POST超时/断连/429/5xx/空图与危URL、持久dispatch恢复、先落存后capture、到期/迟到/人工释放/退款意图；没有真实上游任务查询能力。 |
| A-22～A-27 | 模拟QQ回执未知、官方媒体/发送请求合同、网站fallback、原图重发、解绑/换绑/移群/模式切换、媒体token撤销、权限及SSRF、退款与恢复竞争。 |
| A-28～A-29 | QQ独立账本不调用福利累计/权益扣减；按user_id合并额度、Asia/Shanghai业务日与固定订单价。既有API透支策略不变。 |
| A-30 | 有真实到期字节清理和私有领取拒绝；存储灾难恢复/生产备份恢复不在本轮实测范围，人工退款能力已实现。 |
| A-31 | 配置保存只本地校验，无隐藏生图请求；无收费试生成按钮。 |
| A-32 | 账号在交付前停用不捕获；已交付后不自动反转；派发后邮箱变化保留原单收费语义。 |

测试与浏览器都使用本机模拟身份/内存SMTP/模拟协议及专用PG。没有真实 QQ、外部邮件、用户账户扣费或付费生图调用。前端桌面与390px移动、中英及鉴权blob图片已由子代理浏览器验证；最终人工释放复用已验理由确认样式，通过组件回归，未重复浏览器联调。8张截图存主项目 `output/qq-image-generation-20261023`，不是生产截图。

本轮 `-race` 未执行：本机CGO关闭且无C编译器；没有将普通并发数据库测试冒称race检测。长时真实压力、实际QQ权限、服务商成本及正式部署均保留上线条件。

## 验证证据收口

原始日志在主项目 `.cache/qq-image-generation-20261023`；前端说明为 `frontend-validation.md`，核心说明为 `core-verification-summary.txt`。首次整合测试的PG连接失败源于专用夹具进程退出，恢复同一E盘数据与端口55439后重跑，不把连接失败记为业务断言失败或省略该轮记录。

所有命令均在独立工作树执行，同次PowerShell首句载入主项目tools/project-env.ps1。前端使用E盘pnpm10.11.0局部shim；package.json、锁文件及全局配置未改变。

| 实际检查 | 结果与证据 |
| --- | --- |
| `go -C backend test -tags=unit ./internal/qqimage ./internal/handler ./internal/server/routes ./internal/server/middleware ./internal/config -count=1 -json` | 数据库恢复后重跑qqimage/handler整包；5个相关包最终全部通过，各包最新运行合并2920个pass事件、0fail、1skip（含顶层和子用例，不是独立叶数）。日志backend-related-final.jsonl及backend-db-final.jsonl。 |
| 最后成本元数据、人工释放、HTTP真实PG端到端专项 | 7个顶层场景通过，0失败/跳过；final-contracts.jsonl覆盖最终成本字段及Simple切换后排空/领取。 |
| 当前用户专用邮箱发送及原注册回归 | 11个顶层和7个子用例通过；使用loopback内存SMTP，无外部投递；qq-email-green.log。 |
| `go -C backend test ./cmd/server -count=1` | 通过。首次vet发现旧cleanup夹具缺新增参数，补齐后通过；server-wiring-final.log。 |
| `go -C backend test -tags=embed ./internal/web -count=1` | 通过；embedded-web-final.log。 |
| `go -C backend vet ./internal/qqimage ./internal/handler ./internal/service ./internal/server/... ./internal/config ./cmd/server` | 最终exit0；go-vet-final.log。 |
| 固定Wire v0.7.0生成 | 成功，含QQ handler/runtime及清理依赖；工具在共享.cache/tools，不改变go.mod/go.sum。 |
| 前端回归 | 14文件191项通过；后续相关38项及成本提示28项通过（重叠不累加）。最终typecheck、相关ESLint通过；frontend-validation.md记录逐轮范围。 |
| `pnpm --dir frontend run build` | root在最终文案/类型后完整执行，i18n、vue-tsc与Vite成功；frontend-root-final-build.log。旧Browserslist、Node弃用、混合导入及大chunk提示为警告。 |
| Linux/amd64、CGO=0、embed编译 | 最终前端资源一并编译成功；未运行Linux产物，不代表镜像/生产验证。183405458字节，SHA256为66630d1696172fc42f3e3011755ed802a8f34b7bb340a979955431d83c30bcb2；linux-build-final.log。 |
| 全历史迁移 | 专用PostgreSQL16.15，303份SQL两次成功，16个QQ表；248 SHA256为f8d27f480cfbc03f13e4371c1874c696abd949360afd85bc9d4875f827ed8e41；full-migration-final.log。 |

唯一跳过是既有TestDingTalkOAuthStart_Disabled哨兵用例自身Skip，不是QQ测试。并发资金采用真实PG，不宣称执行了CGO race检测。未执行Docker、镜像发布、真实外部接口或生产验证。

主目录11份既有tracked WIP SHA256与开发前一致，包括设计阶段追加的.gitignore/HANDOFF；只在本功能树写源码/交接。新增源码和关键文档可被Git跟踪，目前未暂存/提交。

**本地资源保留与清理限制**：Vite/SMTP模拟服务及临时浏览器标签已关闭，前端构建资源、依赖、必要日志和PG测试数据在E盘保留。收尾的“删除本次Linux编译二进制并停止专用PG”工具命令在创建进程前被自动审批拒绝，仅返回blocked by policy，没有具体原因；该命令没有执行，未重试或绕过。183MB编译文件保留原位，专用PG最近监听127.0.0.1:55439，尚未确认正常关闭；宿主工具会话结束可能终止进程，下次使用先检查状态并按既有数据恢复，不能据此声称它已安全停止。未清理任何其他任务目录。

协议资料（2026-10-23核对）：[QQ签名](https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/sign.html)、[QQ挑战](https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/event-emit/webhook.html)、[QQ token](https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/access-token.html)、[QQ群消息](https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_messages.post.html)、[QQ群媒体](https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_files.post.html)、[OpenAI Images](https://developers.openai.com/api/reference/resources/images/methods/generate)。资料与模拟回归不等于真实机器人权限或服务商联调通过。
## 后续本地验收准备

同日新增真实本地应用、群标识发现与迁移249，见[本地验收记录](2026-10-23-qq-local-acceptance.md)。上文首版验证与清理限制保留为历史；新验收资源与旧被拒清理资源分别管理，未接公网或正式站。