# Sub2API v0.2.14 合并后追加独立审查

## 结论和来源

本轮按用户要求启用三个领域子代理，对合并后的实际代码继续审查；确认并修复 5 类 P2 问题。审查起点 `6bf48f3c98be61fc27eb09a47b4fafb06e08ffa6`，修复源码提交 `aae0f1829beb14a45f3dd3c5d9f7097e59fe39b5`，分支仍为 `codex/merge-upstream-v0.2.14`。

部署基线为 v0.2.8-r1/ebaa8c022，官方目标仍为 3f1a2ea0a（v0.2.14 加 VERSION 同步）。问题来源包括官方累积功能和原有二开恢复逻辑的交互遗漏，不全是 Git 文本冲突导致。官方安全修复、定价/结算路径及二开业务能力继续保留。

三个领域分别是计费/网关，订阅/福利/退款，前端/认证/支付；主代理检查实际差异、依赖装配、迁移边界和最终验证。后端代理交叉审查退款修复，另一代理交叉审查前端并发现初次认证修复遗漏的发送前 401 窗口；该窗口已补齐并再做真实 transport 回归及完整前端验证。本轮没有遗留已确认的阻断发现。

## 确认的问题与修复

| 级别/问题 | 触发和实际影响 | 修复与保护 |
| --- | --- | --- |
| P2：显式免费媒体被识别成无价格 | 开启 fail_closed_on_unpriced，Grok 音频/独立搜索/图片/视频明确配置为零价，预占仍可能返回“余额不足”。这是 0.2.11 新增预占能力累积到当前版本的定价覆盖遗漏。 | 仅识别对应明确零价；保留 nil 价格默认值、未知 token/音频模式的拒绝、显式 Group/Channel token 规则优先。图片仍按既有保守估算检查全部尺寸。实际两种 RecordUsage 路径确认目录 token 价不会覆盖分组媒体零价。 |
| P2：退款后的正余额缓存继续准入 | 同步退款或 pending 确认已把数据库余额扣为零，缓存仍为旧正数，下一请求及其结算前并发请求仍可通过费用准入。原官方与部署代码都继承该遗漏。不能断言顺序请求必然花完全部旧余额，后续成功计费也会纠正缓存。 | 在余额预扣已提交、pending 事务提交、失败补偿返还成功后，清余额和 API Key 认证缓存。两类失效各用独立 2 秒且不继承请求取消；不在事务内清理，不把缓存错误转成可重复执行的退款失败。 |
| P2：旧支付建单使用新账号凭据 | API 调用后、Axios 真正 dispatch 前，另一标签换号或同账号重新登录；真实 adapter 收到新 token，可能给新账号创建旧页面发起的订单，响应又被旧 owner 丢弃。 | 在 API 调用时捕获会话，在每次实际发送前校验。正常同会话 token 轮换继续使用新 token，公开 signed resume 契约不变。 |
| P2：未决日重置丢失原操作 ID | 原 POST 连接失败但事务可能仍在提交，查事件暂时 404；重试再被 429/401/403 前置拒绝，前端曾清原 operation ID 并停止核验可能已生效的扣天。后端版本保护仍在，本轮没有宣称已证明重复扣天。 | 对已经未决的操作，前置拒绝保持 checking 和原持久化 ID，后续仍用同 ID 恢复；首次明确拒绝以及权威 RESET 领域拒绝保持原结束行为。 |
| P2：旧认证请求覆盖或清除新会话 | 登录、2FA、注册、Passkey 的旧成功响应覆盖新 token/用户；旧 503/429/网络失败清新会话。额外真实 Axios 回归确认：发送前换号时，旧 401 也会先被拦截器当成新会话错误清 token。 | API 写存储之前和 store 提交/失败清理前校验原会话；发送时绑定调用身份。Passkey 从 begin、浏览器认证到 finish 全程使用入口 owner，每个异步返回点都检查。正常登录、同账号重登录隔离、同会话刷新和当前登录持久化失败清理均有控制用例。 |

代码位置：

- `backend/internal/service/billing_inflight_reservation.go:515`：零价媒体识别及预占接线。
- `backend/internal/service/payment_refund.go:1039`：提交后缓存清理；payment_service.go/service/wire.go 提供依赖，cmd/server/wire_gen.go 由 Wire 生成。
- `frontend/src/api/client.ts:47`、`frontend/src/api/payment.ts:49`：调用时所有权与发送前校验。
- `frontend/src/stores/subscriptions.ts:29`：未决请求的确定性拒绝分类。
- `frontend/src/api/auth.ts:132`、`frontend/src/api/passkey.ts:108`、`frontend/src/stores/auth.ts:250`：认证持久化和流程身份校验。

其余抽查了订阅账号等待/WS 终检、降级重新预占、引用计数释放及异步计费交接、福利同事务记账、退款奖励回退、自然日重置与续期锁序、官方充值优惠/OAuth 续付、币种渠道范围、公告合规和 BaseDialog；未确认其他可复现的新缺陷。该结论不等于所有代码逐行或所有运行情境均已穷尽。

## 复现和独立验证

五类修复均先运行失败回归，再做最小生产修改。后端新增三个回归文件；前端新增四个文件、47 个用例，另更新原 Passkey 用例以验证第三参数的身份元数据，同时保留原正文/proof/WebAuthn 数据转换断言。

- `inflight-free-media-red.log` / `inflight-free-media-admission-red.log` → `inflight-free-media-green.log`；预占专项 94 项通过；实际 RecordUsage 五个路径见 `inflight-free-media-recordusage-review.log`。
- `payment-refund-cache-red.log` → `payment-refund-cache-green.log`、`payment-refund-all-green.log`；独立复核见 `refund-cache-crossreview-green.log`。覆盖取消、事务、两种缓存失败、前者超时后后者仍尝试，以及正常回滚。
- `frontend-candidate-red.log` → `frontend-two-fixes-green.log`：支付发送与订阅恢复。
- `frontend-auth-candidate-red.log` → `frontend-auth-fix-green.log`：晚到成功/失败；`frontend-auth-dispatch-red.log` → `frontend-auth-dispatch-green.log`：发送前 401 和 Passkey 多步窗口。

前一次前端完整 361 文件/2,905 项通过早于发送前 401 修复，单独保存为中间证据。最终验收使用 frontend-*-final 日志，不能混用这两次结果。直接 go generate 首次因 Wire 工具自身缺少 go.sum 项未完成；改为在 E 盘共享工具缓存安装固定 Wire 0.7.0 后生成成功，未改变项目 Go 依赖或锁文件。

## 最终验证

日志根目录：`E:/sub2二次开发项目/.cache/review-v0.2.14-20261007`。Go 1.27.0、pnpm 9.15.9、golangci-lint 2.13.0。Go 计数包含子测试，各标签和专项重叠，不能相加为唯一用例数。

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 后端完整 unit（-count=1） | 58 个测试包、22,373 项通过，0 失败，18 项既有条件跳过 | backend-unit-final.jsonl / backend-summary.json |
| 后端完整 integration（CI=true，-count=1） | 52 个测试包、13,895 项通过，0 失败，16 项既有条件跳过 | backend-integration-final.jsonl / backend-summary.json |
| 三条历史数据库升级 | 官方 0.2.8、二开 0.2.7、部署 0.2.8-r1 的升级与固定基线均 pass，无跳过 | backend-integration-final.jsonl |
| 后端 lint | 退出 0，0 issues | backend-lint-final.log |
| 前端完整 Vitest | 362 文件、2,915 项通过，0 失败/跳过 | frontend-vitest-final.json |
| 前端 lint、typecheck、i18n、生产构建 | 全部退出 0；i18n 3 项通过 | frontend-*-final-result.json |
| 内嵌前端 web 测试 | 98 项通过、0 失败/跳过 | backend-embed-final.jsonl |
| 内嵌前端 Windows/amd64、Linux/amd64 构建 | 均退出 0；Windows -version 输出 0.2.14 和准确源码 aae0f1829；Linux 为交叉构建 | backend-build-*-result.json / backend-version.log |
| Wire 生成及源码边界 | 正常生成；未修改 Ent、任何历史/当前迁移、Go/pnpm 依赖或锁文件；gofmt 和 diff whitespace 检查通过 | wire-generation-verified.log / source-boundary.json |

## 限制及交付边界

- 退款缓存失效是有界同步清理；共享 Redis 异常时仍依赖既有 TTL/后续成功计费纠正，本次没有引入持久 outbox，不宣称强一致或消除全部并发透支窗口。
- 16 项 integration 条件跳过与前轮一致：9 项安全审计专用 Redis/DSN 未配置；外部 TLS、TypeSafe、OpenAI、插件包条件未提供；xAI 抓包平台限定；钉钉 sentinel 及并发缓存已有 TODO。这些不属于本轮新增测试，未通过删除或放宽断言改变结果。
- 没有运行真实支付/模型请求或真实浏览器/Passkey 硬件端到端；前端竞态使用真实 Axios 拦截器和本地 adapter，数据库升级使用固定历史迁移及合成数据。没有做多实例 Redis 故障注入。
- Docker 实际数据继续位于 E:/CodexData/DockerBackup/DockerDesktopWSL。临时测试容器已自动回收，原有容器和数据卷清单不变，用户备份未触碰。临时 pnpm shim 已清理；本轮验证二进制记录 SHA-256 后清理，保留生产前端 dist、日志、摘要，依赖和工具缓存复用。
- 本次修复位于原 E 盘升级工作树，保留主目录原有未提交修改；没有推送、发布镜像或部署生产。

原合并报告和逐文件 CSV 保留各自快照，本轮代码变动可通过 `git diff 6bf48f3c9 aae0f1829` 独立查看。
