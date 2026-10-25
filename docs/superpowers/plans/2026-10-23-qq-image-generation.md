# QQ 群生图 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 在原站交付默认关闭、可配置、可本地验收的 QQ 官方群机器人单张生图首版。

**Architecture:** 新增 internal/qqimage 模块保存作用域授权、配置版本、订单、独占资金hold与持久恢复。Gin handler连接既有认证/管理员路由，后台worker和平台适配器调用该模块；前端复用原站登录、福利入口及会话保护。既有API透支政策保持，QQ只用原子足额预付；不接入福利奖励。

**Tech Stack:** Go、PostgreSQL事务、Gin、Vue3/TypeScript、Vitest、标准库HTTP/Ed25519/AES-GCM；使用现有依赖，pnpm。

---

工作树：`E:\sub2二次开发项目\.worktrees\qq-image-generation-20261023`，分支`codex/qq-image-generation-20261023`，基线c9a9209ea。主目录用户修改保持不动，仅精确带入已接受设计。每次PowerShell调用必须先执行 `. 'E:\sub2二次开发项目\tools\project-env.ps1'`；日志在主.cache/qq-image-generation-20261023。不提交、不推送、不合主目录、不生产迁移或真实外部调用。

## 固定接口合同

所有HTTP响应沿用现有 `{code,message,data}` 包装，金额使用十进制字符串，时间使用RFC3339。用户由JWT中间件决定，QQ身份仅来自验证事件。

- `GET /api/v1/user/qq-image/status` → `{enabled,email_verified,email,grants:[]}`。
- `POST /api/v1/user/qq-image/email/send-code` 和 `/email/verify`（`{code}`）只验证当前邮箱；具体邮箱绑定/变更走原站个人资料。
- `GET /api/v1/user/qq-image/requests/:id` → `{id,status,group_id,subject_hint,expires_at}`，短编号或票据定位；不返回他人候选账号。
- `POST /api/v1/user/qq-image/requests/:id/authorize` → `{confirmation_code,expires_at}`，请求`{consent:true}`，候选和当前会话锁定。
- `DELETE /api/v1/user/qq-image/grants/:id` 撤销本人的作用域授权。
- `GET /api/v1/user/qq-image/orders`、`GET /orders/:id` → 订单数组/详情；字段`id,prompt,price,model,size,status,funds_status,delivery_status,created_at,deadline,artifact_expires_at,error`。
- `POST /api/v1/user/qq-image/orders/:id/cancel`；`GET /orders/:id/image` 返回鉴权图片二进制，不使用匿名公共图URL。
- `GET/PUT /api/v1/admin/qq-image/config`；字段`enabled,app_id,bot_secret,api_url,api_key,model,size,price,allowed_groups,public_base_url,user_daily_limit,user_daily_amount,group_concurrency,global_concurrency,queue_limit,queue_timeout_seconds,deadline_seconds,retention_days,prompt_max_chars`。secret空值保留，读取返回`bot_secret_configured,api_key_configured,encryption_available`，从不回传原secret。
- `GET /api/v1/admin/qq-image/orders`、`GET /grants`、`GET /audit`；`POST /orders/:id/refund`（`{reason}`）、`POST /orders/:id/reconcile`、`DELETE /grants/:id`；管理下载仍鉴权并审计。
- `POST /api/v1/qq-image/events`：QQ官方webhook验签、挑战响应、群@事件解析、持久事件去重。禁止未经签名的假身份入口。

前端路由 `/qq/bind`（含手动编号与原站注册返回）、`/qq/orders`、`/qq/orders/:id`。福利设置增加QQ页签，独立于福利开关；账户导航保留绑定/订单入口。

## Task 1：领域、数据库、账务（core agent）

Files: `backend/internal/qqimage/types.go`, `store*.go`, `binding*.go`, `orders*.go`, `config*.go`, `*_test.go`; 新SQL迁移使用当前最大编号后一个。此agent不编辑HTTP路由、worker、平台适配、前端。

- [x] 写RED测试：十进制金额转换、完整身份命名空间、正确/错误候选码、旧revision、订单状态/截止互斥；使用`go test ./internal/qqimage -run 'TestAmount|TestBinding|TestOrder'`观察缺失行为。
- [x] 实现独立业务授权，不把兼容verified_at视为邮箱证明；`qq_image_email_verifications`绑定user_id+当前邮箱，根handler在验证码成功后写入。
- [x] 实现SQL事务预留`UPDATE users SET balance=balance-$1,frozen_balance=frozen_balance+$1 WHERE balance >= $1`，同事务订单/hold/事件唯一键；capture/release按本订单状态互斥，不访问其他hold。
- [x] 加真实PostgreSQL并发测试：两个相等余额订单只一个成功、capture/release竞争、重复refund、到期held禁止capture、解绑不转移owner、配置版本和候选码防冒领；测试记录命令/结果，不用mock代替资金验收。
- [x] AES-GCM加密凭据且按不可变配置版本引用；环境密钥未配置时禁止保存secret/启用，不阻止站点其他功能启动。
- [x] 持久租约、额度、核查/截止、投递意图和审计；方法合同先发给其他agent，SQL事务锁序固定。

## Task 2：QQ与生图适配、持久执行（transport agent）

Files: `backend/internal/qqimage/qq_*.go`, `provider_*.go`, `worker*.go`, 对应tests。不编辑store/types/core与HTTP路由。

- [x] 先用签名fixtures和httptest写失败测试：Ed25519签名/时间/挑战/重复事件上下文；仅@群事件可信身份；错误app/scope拒绝。
- [x] 核对官方现行事件鉴权/发送/媒体文档，记录来源；实现官方适配，域名/回复窗口/发送回执受控。真实账号权限留待联调。
- [x] 用模拟OpenAI Images写RED：200空图、有效base64、危险URL/重定向/过大图片、POST超时不重试；实现单模型单图、HTTPS与目标限制、实际解码验证。
- [x] worker通过core持久claim出站；生成成功私有落存/捕获，同一订单重启不重POST；同步协议没有查询ID时进核查，deadline释放。配置/Key版本保持；失败不盲目切上游。
- [x] QQ失败只补发已有产物；最终投递claim校验原grant/revision，回执未知标unknown；无媒体能力则站内领取，并准确显示未投递。
- [x] 执行`go test ./internal/qqimage -run 'TestQQ|TestProvider|TestWorker'`并报告真实结果。

## Task 3：用户与管理员前端（frontend agent）

Files: `frontend/src/api/qqImage.ts`, `views/user/QQ*View.vue`, `components/admin/QQImage*.vue`, admin福利页/用户福利页/router/账户导航、i18n及专项tests。不编辑后端。

- [x] 用Vitest写RED，测试未登录返回、候选授权不可自动提交、邮箱补验、单次码、过期/冲突、会话变化清空私有状态；实现简洁移动页面。
- [x] 用户订单区分任务/资金/投递，blob鉴权图、取消/截止提示，解绑说明，旧请求不能污染新会话。
- [x] 后台配置两个凭据分区、脱敏回读、群/固定价/限额；订单核查、refund与审计，关闭不隐藏旧订单。
- [x] 贯穿登录/注册/邮箱验证的固定站内返回路径，禁止任意redirect；文案中英完整。
- [x] pnpm专项测试、typecheck、lint:check、build；通过computer-use技能子代理/自身浏览器验证移动/桌面关键页面，使用独立本地fixture不连接生产。

## Task 4：HTTP、依赖注入与启动/停止（root）

Files: `backend/internal/handler/qq_image_handler*.go`, `handler/wire.go`, `handler/handler.go`, server routes、`cmd/server/wire.go`/生成wire文件；邮箱资格集成与配置说明。

- [x] 写HTTP失败测试：JWT归属、管理员权限、CSRF/Origin保护、请求体上限、不可由body指定user_id、私有图的跨用户拒绝。
- [x] 实际接通user/admin/webhook路由；使用现有邮箱验证码服务，验证成功写可信当前邮箱记录，发送失败不假成功。
- [x] worker注入、Start/Stop及有界关闭；缓存失效回调连接BillingCache与APIKey auth cache，持久重试防缓存失败丢失。
- [x] 运行wire生成并检查路由不缺线；安装/构建只在E盘树，复用主.cache。

## Task 5：集成验收、独立审查与交接（root + review agent）

- [x] 对设计A-01到A-32逐项标注已自动测试/本地浏览器/真实联调待办，不把未执行项写成功。
- [x] 真实PostgreSQL专项并发、关键handler/service/route回归、前端专项/i18n/类型/lint/构建、Go静态检查及后端编译；Docker仅在已核实E盘VHDX后运行。
- [x] 独立审查身份/资金/恢复与接线，修复确认问题并重测相关范围。
- [x] 更新CHANGELOG、HANDOFF与`docs/changes/2026-10-23-qq-image-generation.md`，记录新增迁移、加密密钥、存储、默认关闭、联调条件和回退限制；精确gitignore放行。
- [x] 检查diff和既有WIP保全；保留工作树/必要日志，不提交推送或合并。
- [ ] 清理本次未使用的Linux二进制并正常关闭专用PG：自动审批拒绝，原位保留，未重试。

## 执行收口

开发、本地验证、独立审查和交接已完成，真实权限与付费联调仍为上线条件。用户已确认QQ官方与OpenAI兼容Images。变更及最终证据见[实施记录](../../changes/2026-10-23-qq-image-generation.md)。未提交、推送或合入主目录。最后一步的源码/日志保留与WIP检查已完成；二进制删除及PG关闭被自动审批拒绝，作为唯一清理未完成项记录，未绕过。
