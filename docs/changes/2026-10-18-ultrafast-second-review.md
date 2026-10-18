# Ultrafast 功能第二轮完整复审

- 日期：2026-10-18（Asia/Shanghai）
- 应用代码基线：`e84623e8c4ad9e2f727db2a3da58d44264773bf3`
- 功能比较范围：`b9c36c6ee..e84623e8c`，包含原功能和上一轮修复。
- 工作树：`E:\sub2二次开发项目\.worktrees\openai-group-ultrafast-20261017`
- 本轮性质：只读代码审查与复现；不修改业务代码或永久测试。复现代码通过 Go overlay 从主项目 E 盘 `.cache` 载入。
- 审查时结论：新增确认 3 项 P2，当时均未修复，没有确认 P0/P1。随后用户授权修复，三项及终态补齐遗漏现已关闭，见[后续修复与验证](2026-10-18-ultrafast-review-fixes.md)。本文保留发现时证据。

## UF-R5 / P2：Messages 转 Chat Completions 遗漏强制模式和全局 block

位置：`backend/internal/service/openai_gateway_messages_chat_fallback.go:108–111`；生产入口为 `openai_gateway_messages.go:66–69`。

触发条件：OpenAI 分组开启强制 Ultrafast，调用 `/v1/messages`，最终模型为 `gpt-6.1-sol` 或 `gpt-6-astra`，选中仅支持 Chat Completions 的 OpenAI API Key 账号（显式 force_chat_completions 或探针判定不支持 Responses）。

该路径以“Anthropic 请求没有 service_tier”为由跳过 `applyOpenAIFastPolicyToBody`。这个假设不适用于由分组主动强制的 Ultrafast。转换后的出站请求没有 `service_tier`，返回结果的 tier 也为空；全局 Ultrafast block 即使命中该账号/模型，仍会请求上游并返回成功。

真实 `ForwardAsAnthropic` 复现：两个目标模型 × 正常/block 共 4 个场景，断言全部失败。正常场景期望出站 `ultrafast`、实际为空；block 场景期望发送前拒绝，实际 `err=nil`、HTTP 200，并已调用 `/v1/chat/completions`。

修复方向：转换及模型映射完成后调用共同的 tier 策略入口；用最终出站 body 提取返回和计费 tier；正确转换 block 错误为 Messages 响应。

## UF-R6 / P2：WebSocket 转发策略和计费的 Ultrafast 开关快照不同步

位置：`backend/internal/handler/openai_gateway_handler.go:2208–2210` 刷新整个计费分组；`openai_ws_forwarder_ingress.go:441` 和 `openai_ws_v2_passthrough_adapter.go:1098` 仍用建连上下文执行强制策略；`openai_gateway_usage.go:578–591` 按刷新后的分组决定 8×。

触发条件：已建立目标模型 WebSocket，在两个 turn 之间管理员修改同一分组的 Ultrafast 开关。现有设计承诺已建立连接沿用策略快照，分组价格/倍率可以按 turn 更新，但这两类状态现在通过完整 Group 替换混在一起。

真实双轮 handler→WS→用量日志复现覆盖 passthrough 和 ctx_pool 两种模式，以及关闭→开启、开启→关闭两方向，共 4 个场景，全部因计费不一致失败：

- 开启→关闭：客户端两轮均没有 tier，转发仍按连接快照强制 Ultra，但计费用量由 8× 降为旧默认 6×。
- 关闭→开启：客户端两轮均显式请求 Ultra，出站、模型、token 和倍率均不变，计费用量却从旧 6× 改为 8×。
- 测试中 Astra 两轮都是 2 输入/1 输出 token，费用在 `$0.00042` 与 `$0.00056` 之间改变。该真实 WS 夹具使用 simple mode，因此本项动态证据是用量费用计算，不声称测试实际执行余额扣减。

修复方向：保留按 turn 更新价格/倍率的原功能，同时让“是否属于分组强制 Ultrafast 的 8×范围”与该 turn 实际执行的策略快照一致；可单独保留策略字段或将本次有效策略归属随转发结果传给计费，不能把所有价格回退为建连旧价。

## UF-R7 / P2：上游 cyber 拒绝的非零用量丢失 Ultrafast 计费档位

位置：`backend/internal/service/openai_gateway_usage.go:85–93`；调用方 `backend/internal/handler/openai_gateway_handler.go:4467–4486`；新增 8× 判断在 `openai_gateway_usage.go:578–591`。

触发条件：启用分组的目标请求已经以 Ultra 发往上游，上游返回 `response.failed` / `cyber_policy`，包含非零 token 用量，终态仍为 `service_tier=ultrafast`。这是上游处理后的错误结算，与本地全局 block 在发送前拒绝、无用量的情况不同。

兼容入口返回错误后，handler 用 `CyberPolicyUsageInput` 重建结果，但没有保存实际 tier 和最终上游模型。于是 `openAIGroupUltrafastMultiplier` 因 tier 为空返回 nil，错误结算按 Standard 收费，日志 tier 也为空。

动态复现使用真实 `ForwardAsChatCompletions`，先断言出站 Ultra、上游终态 Ultra、风控标记含 1200 输入/300 输出 token，再使用与生产 handler 相同的输入调用 `RecordCyberPolicyUsageLog`。余额仓储桩记录实际扣减 `$0.027`，应为 `$0.216`，断言失败。静态追踪确认 HTTP 三种文本入口和 WS 错误 turn 均可调用该公共错误记录入口；动态复现只覆盖 Chat HTTP。

修复方向：在丢弃错误转发结果之前保留实际发送/观察到的 tier、最终模型及必要计费元数据，传入公共错误结算链；不得仅凭开关把所有上游错误盲目按 8× 计费，仍须尊重实际 tier 和降级结果。

## 本轮执行与证据

所有命令在 E 盘工作树，同一次 PowerShell 先加载主项目 `tools/project-env.ps1`；Go 使用已有 1.27.2 工具链和共享缓存，前端使用缓存 pnpm 9.15.9。

| 检查 | 结果 |
| --- | --- |
| Messages raw CC 新增复现 | 4 场景失败，均为缺失 tier 或 block 未拒绝的行为断言；不是编译/环境失败。 |
| WS 分组快照新增复现 | 4 场景失败，真实双轮连接的计费用量不一致。 |
| cyber 新增复现 | 1 场景失败，真实转发及错误用量提取成功，最终金额少计。 |
| 既有计费专项 | 3 个顶层测试通过，覆盖原 cyber 用量、启停 8×及上游降级；这些测试没有覆盖本次错误结算缺口。 |
| 管理与数据链路 | 静态复核未确认新增 P1/P2。四个 Go 包精准筛选共 20 个顶层测试、61 个通过事件（57 个叶场景），0 失败/跳过；前端既有 4 文件 / 80 项及既有真实 GroupsView 挂载夹具 1 文件 / 5 项通过，合计 85 项。没有重跑真实数据库迁移或 Redis 分布式失效。 |

复现命令模板（在功能工作树 `backend` 目录）：

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
$env:GOTOOLCHAIN = 'local'
$env:GOTELEMETRYDIR = 'E:\sub2二次开发项目\.cache\go-telemetry'
$reviewGo = 'E:\sub2二次开发项目\.cache\go-mod\golang.org\toolchain@v0.0.1-go1.27.2.windows-amd64\bin\go.exe'
# 以下命令在审查应用基线 e84623e8c 上预期退出码为 1；后续修复源码采用永久回归验证。
& $reviewGo test -overlay 'E:\sub2二次开发项目\.cache\ultrafast-review2-20261018\protocol\overlay.json' -tags=unit ./internal/service -run '^TestReviewUltrafastMessagesRawCC$' -count=1 -v
& $reviewGo test -overlay 'E:\sub2二次开发项目\.cache\ultrafast-review2-20261018\ws-snapshot\overlay.json' -tags=unit ./internal/handler -run '^TestReview2UltrafastWSSnapshotBilling$' -count=1 -v
& $reviewGo test -overlay 'E:\sub2二次开发项目\.cache\ultrafast-review2-20261018\billing\overlay.json' -tags=unit ./internal/service -run '^TestReview2UltrafastCyberBillingPreservesSentTier$' -count=1 -v
```

管理端验证命令、测试范围和配置记录在本机证据目录的 `admin/SUMMARY.txt`，复制的旧表单夹具及临时配置已清理，原始夹具和验证日志保留。

原始证据保留在主项目 `.cache/ultrafast-review2-20261018`：`protocol/messages-raw-cc.log`、`ws-snapshot/repro-final.log`、`billing/cyber_billing_result.txt` 及各自 overlay/测试。工作树中未加入这些临时测试，报告中的触发条件和实际/预期独立保留关键结论。

已排除的线索：WS 跨账号重试使用强制 tier 之前的原始账号身份快照，未把先前自动注入的 Ultra 当成客户端意图；rejected-field 重试的删除白名单不含 service_tier。账号统计已有客户倍率前总价 × 账号倍率的默认语义，而默认上游价重算未携带本功能 8×覆盖，未确认新的成本污染问题。

没有执行全量测试、数据库集成、Docker、真实外部上游或生产验证。本轮不修改业务代码、不新增迁移、不推送或发布镜像；应用源码仍为 `e84623e8c`。以上描述审查阶段状态；上述 3 项已按后续修复记录关闭。
