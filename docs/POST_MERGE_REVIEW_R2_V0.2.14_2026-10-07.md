# Sub2API v0.2.14 第二轮全面复审

## 结论

从 `7cedfd96cbf1aae5d4ed3bc29e90d64f19faf627` 重新审查，没有把上一轮通过结论当作本轮结论。本轮交换三个子代理的领域，结合实际调用链、确定性失败回归及交叉复核，确认并修复 **7 类问题（3 个 P1、4 个 P2）**。

修复源码：`9e6da49e32aec8c91da1e611c3b6a6f5f20477be`；分支：`codex/merge-upstream-v0.2.14`。这些问题来自继承的上游/二开代码及其交互，不应全部归因于 v0.2.14 的 Git 合并。上一轮修复继续保留，本轮未确认它们带来新的回归。

目前没有遗留已确认的阻断发现。代码审查和测试覆盖是有边界的，不能据此保证不存在所有潜在问题。

## 发现及修复

| 级别 | 问题与复现 | 修复及保护 |
| --- | --- | --- |
| P1 | **Grok 实时语音异常结束漏计费。** 实际 handler 与本地 TLS WebSocket 上游先交换音频，再异常结束；旧实现没有产生账单，正常关闭对照会产生费用。 | 异常仍按协议关闭连接，但保留已发生音频的结算。无音频、零时长仍不计费；关闭错误不再导致已观测用量被丢弃。 |
| P2 | **Grok HTTP/Realtime 未获取用户并发槽。** 用户槽位拒绝且等待队列已满时，仍实际调用 TTS 上游或建立 WS 上游连接。路由没有外层用户槽代为限制。 | 两个入口沿既有 helper 获取和释放用户槽。WS 握手前使用非流式等待，不写 SSE。账号槽、取消、预占及释放语义保留。 |
| P1 | **异步计费读取复用的 Gin Context。** 阻塞计费 worker、提交实际 SystemOne 用量，再把同一个 Gin Context 换成下一请求后放行；原付费模型被替换为下一请求的免费模型，真实 UsageBillingCommand 的费用由测试配置的 1000 变成 0。 | 12 处模型/渠道字段在每次入队前形成值快照，覆盖 Messages、兼容 Chat/Responses、SystemOne、Gemini、Grok 音频及 OpenAI 入口。AlphaSearch PricingAt 的同类晚读也在静态扫描中补齐；未将它另算作已经复现的峰谷错价。WS 原有每轮快照保留。 |
| P1 | **标准 WebSocket 后续轮次不复核已耗尽余额。** passthrough/dedicated 两种实际 WS 模式，在首轮用量已经落账、可读余额为零后，再发独立第二轮，旧实现仍转发并完成。正余额对照通过；不是同一长流或异步尚未落账的窗口。 | 首轮账号等待后、后续轮槽位取得后复查费用准入。复用原余额、平台额度、Key 金额窗口和订阅规则，不重复消耗 RPM；复制订阅结构，避免修改旧轮异步任务持有的快照。simple 默认和显式 Key 限额模式保持原行为。 |
| P2 | **退款扣除前取消使订单卡在 REFUNDING。** 两种原状态、两种取消时序均证明余额未变、支付网关零调用，但恢复状态因已取消上下文失败，无法重新退款。 | restoreStatus 使用独立有界 5 秒清理上下文，仅更新仍为 REFUNDING 的记录，并记录恢复失败或状态已经变化。增加三种后续退款状态不得被迟到恢复覆盖的保护回归。 |
| P2 | **合规确认被旧 GET/423 反向覆盖。** 启动并发状态查询或旧管理请求的 423 晚于确认成功返回，导致已确认弹窗再次强制打开。 | 状态版本隔离旧读；同版本 423 重新查询权威状态，新版本通知优先。真实服务端撤销、后续主动刷新及会话切换仍有效，不永久屏蔽服务端要求。 |
| P2 | **旧 OAuth 的失败/成功仍污染新会话。** 旧 setToken 获取资料遇 0/429/503 会清掉新登录；旧 OAuth 完成响应还可能在 setToken 之前覆盖凭据。真实 Axios adapter 验证了这些时序。 | setToken 的成功继续动作和错误清理均核对原会话。新增统一 postOAuthCompletion，调用时绑定发送身份、回包后校验且保留 AxiosResponse；API 的 exchange/create 和七个页面十五处完成调用统一使用它。URL、正文、验证码、邀请/affiliate/adoption 字段保持不变。 |

主要改动位于 handler/grok_audio.go、各异步用量提交入口、handler/openai_gateway_handler.go、service/billing_cache_service.go、service/payment_refund.go，以及前端 api/auth.ts、stores/auth.ts、stores/adminCompliance.ts 和 OAuth 完成页面。

## 审查范围和交叉复核

- 网关与计费：请求入口、账号等待/故障切换、用户/账号槽、订阅终检、流式和 WS 每轮计费、预占引用生命周期、媒体/复合分组/模型映射/分时价格、原子结算与福利事实。
- 支付与权益：EasyPay 安全补丁、回调金额和商户身份、同步/异步退款及补偿、退款缓存失效、福利奖励回退、日重置和续期锁序、bootstrap 新安装及已有用户策略。
- 前端：登录/OAuth/Passkey、请求发送与响应归属、支付恢复与报价、订阅和福利未决恢复、公告/合规、路由权限、弹窗、GroupsView 和 UseKeyModal。
- 主代理：官方差异、迁移及依赖边界、测试文件指纹、构建与容器清理。AST 检查直接提交的 **16 个用量任务闭包**，未留下对 Gin 变量 c 的读取；该静态检查有明确语法范围，不是所有异步代码无问题的证明。

其他代理独立核对了 Grok 关闭/计费、槽位释放、快照字段，以及 WS 新复核方法的 RPM/simple/订阅快照边界；再核对 OAuth helper 与七个页面的参数保留、错误路径，以及合规状态更新顺序。发现的临时诊断输出在源码冻结前已清除。

## 官方 v0.2.14 支付安全修复

EasyPay 回调白名单、客户端 return_url 查询参数清理及其安全测试，与官方 `3f1a2ea0a` 保持一致。本轮没有修改这些防护。最终 unit 中再次确认以下六个关键回归均 pass：

- TestEasyPayNotifyRejectsForgedSignReuseCallback
- TestEasyPayNotifyRejectsOrderURLReplay
- TestEasyPayNotifyRejectsUnknownParam
- TestEasyPayNotifyAcceptsGenuineCallback
- TestCanonicalizeReturnURLStripsSmuggledTradeStatus
- TestBuildPaymentReturnURL

对应证据为 official-payment-security-result.json 和 source-boundary.json。未用真实商户或线上订单进行支付实测。

## 本轮验证

证据目录：`E:/sub2二次开发项目/.cache/review2-v0.2.14-20261007`。Go 1.27.0、pnpm 9.15.9、golangci-lint 2.13.0。完整测试使用 -count=1/真实新运行；Go 数量含子测试，各标签和专项不能相加作为唯一用例数。

源码在整体验证前冻结，36 个改动文件的 SHA-256 记录于 tested-source-manifest.json；测试结束、提交前再次比较全部一致。修复提交因此对应实际验证的文件内容。

| 检查 | 最终结果 | 证据 |
| --- | --- | --- |
| 完整后端 unit | 58 个测试包，22,399 项通过，0 失败，18 项条件跳过 | backend-unit-final.jsonl / backend-summary.json |
| 完整后端 integration | 52 个测试包，13,904 项通过，0 失败，7 项既有跳过；CI=true | backend-integration-final.jsonl / backend-summary.json |
| 专用安全审计 DB/Redis | 独立包 214 项通过，0 失败/跳过；之后也接入完整 integration | securityaudit-real-services.jsonl / integration-environment.json |
| 三条历史数据库升级 | 官方 0.2.8、二开 0.2.7、部署 0.2.8-r1 及固定历史指纹全部通过 | backend-integration-final.jsonl |
| 完整前端 | 365 文件、2,957 项通过，0 失败/跳过 | frontend-vitest-final.json |
| 前后端 lint、前端类型检查和构建 | 全部退出 0；后端 0 issues；i18n 前置 3 项通过 | backend-lint-final.log / frontend-*-final-exit.json |
| 内嵌最新前端 web 测试 | 98 项通过，0 失败/跳过 | backend-embed-final.jsonl |
| Windows/amd64、Linux/amd64 内嵌构建 | 均退出 0；Windows 和隔离 Linux 容器 -version 均输出 0.2.14 与源码 9e6da49e3 | backend-build-*-result.json / backend-version-*.log |
| 源码边界 | 历史 SQL、当前迁移、Ent、Go/pnpm 依赖及锁文件均未改；gofmt/diff 检查通过 | source-boundary.json |

RED/GREEN 证据包括 grok-audio-red.log、grok-realtime-user-slot-red.log、async-usage-model-red.log、openai-ws-balance-red.log、gateway-three-fixes-green.log、openai-ws-balance-green.log，退款专项摘要 refund-review2-verification-summary.json，以及 admin-compliance-*、auth-oauth-* 日志。所有修复都以明确失败回归为依据。

前端保留 82 条 Vue 测试组件警告，与上一轮同为 82 条；构建仍有 Browserslist、Node 弃用、混合导入及大分块提示。没有为消除提示改依赖、放宽断言或删除测试。

## 限制及清理

完整 integration 的 7 项剩余跳过是：外部 TLS 抓包、xAI 抓包平台限定、TypeSafe 实测、OpenAI API 对比、测试插件包，以及既有钉钉 sentinel 和并发缓存 TODO。unit 中专用安全审计环境导致的跳过已通过本轮独立包与完整 integration 实际补验；不把其余外部条件用例标为通过。

新费用检查不承诺消除所有预占估算误差或异步尚未落账的并发窗口；退款遇共享缓存异常仍遵循上一轮已有 TTL/后续计费补偿限制。未做真实第三方 OAuth/硬件 Passkey、多实例故障注入或真实支付/模型端到端。Linux 容器 smoke 只运行 -version，不是生产部署验收。

Docker 数据位于 E:/CodexData/DockerBackup/DockerDesktopWSL。所有新增数据库/Redis/版本 smoke 容器均独立，监听仅本机或禁止网络；最终容器与卷清单和开始前一致，原有用户容器保持退出状态。临时 pnpm shim 已清理，前端 dist、日志及可复用工具/依赖缓存保留。验证二进制在记录哈希后清理。

主目录原有未提交修改保持原样。本轮没有推送、发布镜像或部署生产。之前报告和 CSV 保留各自历史快照；当前修复可用 `git diff 7cedfd96c 9e6da49e3` 查看。
