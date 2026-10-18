# Ultrafast 第二轮审查问题修复

- 日期：2026-10-18（Asia/Shanghai）
- 应用基线：`e84623e8c`；修复前审查记录提交：`6341a52fd`。
- 分支：`codex/openai-group-ultrafast-20261017`；工作树沿用 E 盘原功能工作树。
- 稳定提交主题：`fix: preserve Ultrafast policy across fallbacks and error billing`。
- 本阶段状态：源码修复与验证完成；实现、永久回归和记录同一提交。未推送、发布或部署。

用户在[第二轮审查](2026-10-18-ultrafast-second-review.md)后明确授权修复 UF-R5～UF-R7。原失败证据保留，实施步骤见[修复计划](../superpowers/plans/2026-10-18-ultrafast-review-fixes.md)。

## 行为修复

### UF-R5：Messages→Chat Completions

OpenAI 账号在协议转换和最终模型映射完成后调用共同 tier 策略，命中目标模型时强制 Ultra；全局 block 在任何上游发送前返回 Anthropic 格式 403，并标记本地策略拒绝。结果中的 tier 来自最终出站 body，供现有 API/OAuth 统一结算逻辑处理。非 OpenAI 原有行为保留；原 Fast 与 missing-tier force_priority 在该 OpenAI 漏接入口同步收敛到共同策略。

永久测试覆盖两目标模型、stream/buffered、block/filter/force_priority、账号范围、模型映射、旧 Fast、未启用组、非目标及非 OpenAI 对照。原 4 项复现先 RED；20 个新增场景 GREEN，相关旧回归 70 个叶场景通过。

### UF-R6：WS 策略归属和价格分别保留

`refreshOpenAIWSTurnBillingAPIKey` 复制最新 Group，仅把 `ForceOpenAIUltrafast` 保留为建连快照值，其余价格、倍率、FreeFast 与利润配置继续按 turn 刷新。没有修改连接对象或认证缓存引用，不将整组价格冻结在建连时刻。

真实 passthrough/ctx_pool 双轮回归覆盖双向开关切换、同时倍率 3→0.3、重连使用新配置、非目标及副本隔离。新增 8 场景先出现 6 项预期失败、2 项对照通过，修复后通过；之后补两个真实 cyber 终态场景，确认 8×归属、新倍率和仅两条用量日志，最终该文件相关 26 个叶场景全部通过。cyber 终态在正常 `RecordUsage(CyberBlocked)` 分支结算；只回滚错误辅助调用参数的 overlay 未命中该分支，不计作 RED。

### UF-R7：错误结算沿用实际请求与终态事实

新增按值计费快照，分开保存明确的 `BillingModel`、实际出站 `UpstreamModel`、出站 tier/reasoning、上游响应 model/conflict/tier 和完整 token 用量（含缓存/图片 token）。HTTP 普通、透传、CC 共用出站处和各 WS 发送路径均在实际发送时采集；显式计费模型按正常结果生成的位置记录，compact 改上游模型不会改原计费模型；WS 本地 turn 与 HTTP→WS 各沿用其正常定价规则。

cyber 标记从完整原始上游 payload 取终态事实，先于日志截断和客户端模型名恢复。handler 在异步任务前复制快照；错误结算重建完整结果，复用现有 `ApplyOpenAIServiceTierBillingResolution`，API 响应可降级、OAuth default 非权威的规则不变。HTTP 请求和 WS 错误 turn 同步传递原定价时刻；WS 错误与正常结算采用同一个 turn API key。无实际用量仍为零，不重复写正常用量。

专项还确认：账号映出目标后按正确的正常计费模型结算；requested/channel_mapped 显式来源继续优先；compact 回退保留原定价来源；无响应 tier 不猜测、明确降级不收 8×、未启用/非目标不套用分组 8×，仍按实际映射、tier 和缓存用量结算；长终态超过日志截断长度仍保留元数据。

### 最终复核补充：先 error、后失败终态

独立复核在真实 HTTP Responses 流复现：上游先发无用量 bare cyber error，后发带真实用量的 `response.failed`；旧 first-wins 阻止终态补齐计费事实。当前保留首次 code/message/body 等风控审计信息和原出站身份，但在第一个权威 response 终态到达时按值替换计费事实与用量；不会原地修改此前快照，重复终态不再覆盖。新增永久真实转发和标记生命周期测试，保留最初失败证据。

## 验证方式与边界

所有命令在 E 盘工作树，同次 PowerShell 先载入主项目 `tools/project-env.ps1`。Go 1.27.2 和依赖/编译缓存均复用主项目 `.cache`；本轮证据目录 `.cache/fix-ultrafast-r2-20261018`。针对性测试先验证失败来自业务断言，夹具不适配、无测试命中或编译阶段错误不计为 RED。

本次不新增迁移、不改变 Ent schema、依赖或前端代码；原功能迁移 246 / auth snapshot v25 的部署要求不变。不重跑无关数据库集成、Docker 或前端构建，不连接生产。已发布版本仍是 `0.2.16-r1` / `084673c73`，不包含本功能；本地修复不代表已发布或已部署。

## 验证过程中的旧测试适配

全量 unit 首轮：其他 57 个包通过，服务包唯一失败为旧 `TestPassthroughLifecycle_CyberTerminalEventsMarkBeforeAfterTurn/error`。它仍要求 first error 的 5/1 token，而后续真实终态是 9/2。只更新该旧断言为终态计费用量，并继续断言首次 message/body 保留、AfterTurn 仅执行一次，补充 Billing.Usage 与计数一致性断言；业务实现没有为通过旧断言回退。

服务包再次完整复测中，未修改的 `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` 报告进程 HeapAlloc 增量 8,643,824 字节，略超原 8,388,608 字节阈值。该测试在前次全量通过，在相同源码下独立 `-count=3` 又全部通过；它测量整个测试进程，不能排除其他用例的后台分配混入差值。现仅把该测试放入当前测试二进制的独立子进程，原 20,000 次随机请求、无直接 DB、快照读取次数与 8 MiB 阈值都保留。子进程精确执行原测试，25 秒测试超时和 30 秒进程超时保证回收；环境和工作目录继承 E 盘入口，不编译、下载或写入 C 盘。此调整经独立只读复核，随后完整服务包复测通过；结果见下表。

Linux/amd64 生产编译已成功（Go 1.27.2，CGO 关闭）。本轮临时 160,917,362 字节 ELF 文件在校验 SHA256 后删除，构建日志和摘要保留；独立复审的临时 overlay/测试副本也已删除，RED 日志和摘要保留。没有重复尝试删除历史任务曾被策略拒绝的缓存。

## 最终验证结果

| 检查 | 实际结果 |
| --- | --- |
| 新增永久回归 | 16 个顶层测试函数、68 个叶场景通过，0 失败。包含 Messages 20、WS 快照/错误终态 10、cyber 请求/计费 19、采集/终态补齐 17、handler 异步快照/峰值 2。 |
| 后端完整单元范围 | Go 1.27.2，`go test -p=2 -tags=unit -count=1 -timeout=20m -json ./...` 中其他 57 个包通过；服务包先暴露上述旧断言及进程堆测试问题。适配测试后，`go test -p=2 -tags=unit -count=1 -timeout=20m -json ./internal/service` 退出 0，16,976 个通过事件、4 项条件跳过、0 失败。合计当前 58 个有测试包最终覆盖通过，18 项条件跳过（服务包 4 项已包含其中）；不将早先退出 1 的命令写成一次全绿。 |
| Linux 生产编译 | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -p=2 -o <任务缓存>/sub2api-linux-amd64 ./cmd/server` 退出 0。业务源码与最终状态一致，之后仅调整测试隔离。没有构建或发布镜像。 |
| 独立复核 | 分别完成需求符合与代码质量复核；最终复核确认首次审计/终态计费分离、模型来源、API/OAuth 降级、跨 turn 清理、快照副本及单次记录约束，没有其他已确认待修复 P1/P2。测试进程隔离另经只读复核。 |
| 仓库检查 | 修改 Go 文件 gofmt、`git diff --check`、文档本地链接与跟踪范围核对通过；SQL、Ent schema、依赖、前端没有本轮差异。 |

必要原始证据保留于主项目 `.cache/fix-ultrafast-r2-20261018`：各子域 RED/GREEN 与摘要、`cyber-final.jsonl`、`backend-unit-final.jsonl`、`backend-service-final.jsonl`、`backend-service-isolated-final.jsonl`、`memory-guard-isolated.log`、`new-regression-summary.json`、Linux 构建摘要。前两份完整测试失败记录保留用以说明验证过程，最终服务包结果为 `backend-service-isolated-final-summary.json`。

UF-R5、UF-R6、UF-R7 及独立复核的 bare-error/terminal 补充已关闭。后续上线仍需单独执行源码推送、镜像发布和部署流程，不能把本地验证当作已上线。
