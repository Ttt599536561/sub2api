# 专属推广子域名与可信注册归因

日期：2026-10-20（Asia/Shanghai）。用户已选择专属子域名方案。

## 目标与已确认配置

为指定推广用户提供不包含邀请码参数的子域名入口。入口识别推广人后进入主站注册，注册成功时持久绑定邀请人，充值返利沿用现有规则。

实际主站、推广子域名和完整推广邮箱已在聊天中确认；仅在本机交付说明中保存具体配置，不写入通用源码或受跟踪文档，不在没有真实身份查验时推测用户 ID。文档测试示例统一使用 example.test。

用户选择已确定方案方向；具体数据库、HTTP 和事务实现遵循下述规则。生产域名解析、证书、线上账号配置和实际部署分别验证，不把本机开发当作上线完成。

## 用户与管理员体验

- 管理员在邀请返利专属用户区域为既有用户设置一个专属子域名，支持修改和清除。域名与邀请码、返利比例独立保存，避免部分更新混淆。
- 主站地址复用设置中的 `frontend_url`（数据库优先、配置文件兜底）。配置推广域名时要求主站是无路径前缀、查询、片段和用户凭据的 HTTPS origin。
- 推广域名必须是主站 hostname 的严格子域名，规范化为小写 ASCII hostname；不支持通配符、IP、端口、路径或 URL。域名全局唯一，一个用户最多一个启用域名。
- 推广用户邀请页优先显示服务端返回的专属 HTTPS 链接。未配置专属域名的用户保留现有邀请链接。
- 带服务端归因的注册页隐藏返佣邀请码字段；不能通过 URL、表单或 API 请求覆盖邀请人。注册准入邀请码与返佣邀请码不是同一概念，准入校验继续执行。

## 入口与服务端归因

1. 在嵌入前端 middleware 前处理推广 hostname。使用规范化的 Request.Host 精确匹配，忽略 X-Forwarded-Host 和 Forwarded 的主机声明。
2. 已知推广主机仅提供 GET/HEAD 的 `/` 和 `/register` 入口；其他页面、API 和写请求拒绝，不能成为第二个完整站点。停用、失效或用户被删除的已知域名显示无效入口，不退化为普通注册。
3. 入口产生 32 字节随机一次性交接票据，在 Redis 保存来源域名、邀请人、域名 revision、捕获/过期时间；票据 TTL 两分钟。
4. 跳转到固定主站 `/api/v1/auth/affiliate/claim?ticket=...`；主站先核验 Host，再原子消费票据。已领取或过期票据拒绝，存储故障拒绝。
5. 主站设置随机不透明归因 cookie，归因实体在 Redis 有效 30 天。cookie 名称使用 __Host- 前缀，使用 host-only、Secure、HttpOnly、SameSite=Lax 和 Path=/；主站响应 303 跳转干净 `/register`。
6. 交接、领取及状态响应使用 Cache-Control: no-store、Referrer-Policy: no-referrer。反向代理配置不得记录交接 URL 的 query。
7. 浏览器已经持有有效受保护归因时，后续入口不覆盖它，也不延长原始截止时间。第一次且无 cookie 的并发访问可能按浏览器最后接收的有效领取响应建立归因，不承诺跨并发请求的严格先后。过期来源可由新的完整入口替换。无效/被撤销的 cookie 对新用户创建明确报错，不静默回退为无归因注册。普通登录、已有账号绑定和找回密码不能因归因失效而不可用。
8. 归因随各 OAuth 流程的受信本地状态保存，最终完成注册优先使用该流程的归因，不能被请求字段或新的推广访问覆盖。Pending LocalFlowState 可以复用，不能把本地可信信息伪装成第三方 claims。

## 持久化与事务

新增 `affiliate_promotion_hosts` 表：hostname 主键、可空 inviter_id 外键（删除用户后保留失效主机记录）、enabled、revision、created_at、updated_at。启用记录对 inviter_id 建唯一索引。修改归属、停用或更换域名时递增 revision，所有历史 cookie/票据/pending 归因必须重新验证，不转移给新邀请人。

新增的域名仓储能力使用独立小接口，由现有 affiliateRepository 实现；不扩大原 AffiliateRepository 的所有测试替身。服务能力放 `affiliate_domain_service.go`，领域类型和 context 放 `affiliate_attribution.go`。

可信归因类型：

```go
type SignupAffiliateAttribution struct {
    InviterID int64 `json:"inviter_id"`
    SourceHost string `json:"source_host"`
    HostRevision int64 `json:"host_revision"`
    CapturedAt time.Time `json:"captured_at"`
    ExpiresAt time.Time `json:"expires_at"`
}
```

普通邮箱、OAuth 自动创建和 OAuth 补全创建必须覆盖。存在可信归因时，用户创建、注册准入邀请码占用、第三方身份绑定（适用时）、邀请绑定、pending 消费（适用时）在同一 Ent 事务内完成，提交后签发登录结果。复用已有 TxFromContext，不能嵌套事务。绑定失败回滚本轮账号创建和邀请计数，不能仅记录日志后返回成功。已有账户登录、账号绑定不新增或改写邀请人。相同用户/邀请人绑定幂等，冲突邀请人拒绝覆盖。

绑定时校验推广开关、源域名的启用状态/revision/归属、邀请人有效性、自邀请及截止时间，并在事务中锁定权威记录，避免与管理员停用/改绑竞态。注册成功清除归因 cookie；OAuth 已有账号登录/绑定成功也清理本次浏览器归因，避免后续换账号误用。

## API 契约

- `PUT /api/v1/admin/affiliates/users/:user_id/domain` 请求 `{ "domain": "partner.example.test" }`；空字符串清除。返回 `{ "affiliate_domain": "...", "referral_url": "https://.../" }`。
- 专属用户列表和用户邀请详情新增 `affiliate_domain`、`referral_url`。只有域名配置的用户也必须出现在专属列表中。
- `GET /api/v1/auth/affiliate-attribution` 返回 `{ "locked": true|false }`，不公开邀请码或邀请人 ID，不放进共享 public settings 缓存。
- `GET /api/v1/auth/affiliate/claim` 只处理一次性交接，不接收任意跳转 URL。

## 失败与边界

未进入专属子域名、换设备或清除所有浏览器记录后从普通主站注册，无法还原邀请来源；不使用 IP 或浏览器指纹猜测。子域名在用户持续持有父域名并维持 DNS/证书配置时可长期使用，不能承诺永久域名所有权。

域名被撤销、邀请人无效、Redis/数据库故障、票据过期和绑定失败必须有明确可重试错误。退出/常规登录不因旧归因异常被阻断。推广总开关关闭时不接纳新的专属归因；已捕获但尚未注册的受保护流程明确拒绝，不静默漏绑。

## 验证与交付

- 域名规范化/重复/归属/仅域名列表、管理员修改/清除/停用旧票据。
- 精确 Host、伪造转发 Host、固定主站跳转、ticket 过期/重放/Redis 失败、cookie 属性和首次来源优先。
- 普通注册、邮箱验证码、GitHub/Google 自动与补全、LinuxDo/OIDC/微信/钉钉各新建入口；覆盖不同入口共用的核心分支及 provider 传递。
- 可信来源覆盖恶意 aff_code，已有账号不改绑，事务回滚不留账号/邀请计数，pending 消费失败回滚。
- 前端弹窗新增/清除/错误/迟到请求保护，专属链接展示与旧链接回退，锁定归因后不显示邀请码。
- 运行相关后端单元和真实 PostgreSQL 集成测试、前端专项/类型/lint、生产构建；记录实际结果。Docker 测试前另核实磁盘位置，优先本机 E 盘既有 PostgreSQL。
- 同步 CHANGELOG、HANDOFF 和部署说明。所有本机证据放主项目 .cache，源码/文档提交到独立分支；DNS、证书、线上配置与部署状态单独列明。
