# Ultrafast 第二轮问题修复 Implementation Plan

> **For agentic workers:** 使用 subagent-driven-development / systematic-debugging / test-driven-development 执行；主代理统一集成、验证和提交，子代理不提交。

**Goal:** 修复第二轮审查确认的 UF-R5、UF-R6、UF-R7，保留现有平台、模型、计费和部署范围。

**Architecture:** Messages 转 raw CC 后复用统一档位策略。WS 计费仅保留建连的 Ultrafast 策略归属，其余定价字段继续按 turn 刷新；采用分组副本避免修改认证缓存对象。cyber 错误用量保存实际出站模型/档位与上游观察值，传入现有 RecordUsage 的统一档位结算，不从客户端请求或开关猜测最终 tier。

**Tech Stack:** Go 1.27.2、Gin、现有 HTTP/WS 转发与 Go 回归测试；沿用 E 盘功能工作树和主项目共享缓存。

应用基线 `e84623e8c`，审查文档基线 `6341a52fd`。用户明确要求“修复它”，即批准按[审查记录](../../changes/2026-10-18-ultrafast-second-review.md)中的修复方向完成三项修复；不扩展至发布或生产操作。

## UF-R5：Messages raw CC

- [x] 将已有 overlay 4 个失败场景写为永久测试 `backend/internal/service/openai_group_ultrafast_messages_fallback_test.go`，先运行确认业务断言失败。
- [x] 修改 `openai_gateway_messages_chat_fallback.go`：最终模型/转换 body 确定后，OpenAI 账号调用 `applyOpenAIFastPolicyToBody`；遇到 block 以 Anthropic 格式返回 403 并且不发送；返回 tier 从最终 body 提取。
- [x] 验证两目标模型、流式/非流式、block/filter/force_priority、未启用组、非目标、非 OpenAI，以及旧 ForceFast 的合理共同入口行为。

## UF-R6：WS 策略与计费快照

- [x] 在 `backend/internal/handler/openai_gateway_ws_group_pricing_test.go` 固化两 WS 模式 × 开关双向变化的 4 个真实双轮失败场景；补同时调价、新连接及对象隔离对照。
- [x] 修改 `refreshOpenAIWSTurnBillingAPIKey`，采用最新 Group 的值拷贝，单独把 `ForceOpenAIUltrafast` 保留为建连值；不改变倍率、价格、FreeFast 更新，不修改 conn 或 auth snapshot。
- [x] 跑 WS 计费、原分组热调价、换组/刷新失败和前后轮快照相关回归，确认旧功能继续工作。

## UF-R7：cyber 错误用量

- [x] 将已确认的真实 Chat 转发→cyber 用量复现固化，并覆盖 Responses/Messages 和 HTTP passthrough 的共同派发点，先确认错误扣费/丢 tier 失败。
- [x] 在实际 HTTP/WS 出站处保存小型请求计费元数据，cyber mark 按当前 attempt/turn 固定副本；保存上游实际响应 tier/模型，不能继承旧 turn 或失败账号的值。
- [x] `CyberPolicyUsageInput` 与 `RecordCyberPolicyUsageLog` 携带上述数据，继续由 `ApplyOpenAIServiceTierBillingResolution` 按账号协议判定降级；无真实用量仍零费用，未命中组/模型保持原规则。
- [x] handler 在异步任务前复制元数据；WS 错误 turn 使用与正常结算一致的 turn API key 和定价时刻，避免修好档位但仍用错快照。
- [x] 验证实际 Ultra、API 响应降级、OAuth 既有 default 语义、无 tier/无 usage、映射到目标与非目标，以及相邻 attempt/turn 不泄漏。

## 集成与交付

- [x] 主代理先按需求核对各补丁，再做代码质量复核；最终安排独立只读复审，修复可证实遗漏。
- [x] 运行新增和相关 Go 精准回归，完整后端 unit，以及 Linux/amd64 编译；前端无改动，数据库/迁移无改动，不重跑无关 Docker/集成或前端构建。
- [x] 同步 CHANGELOG、HANDOFF、审查记录的修复状态和实际验证；保留原失败证据、区分历史与当前结果。
- [x] 检查 gofmt、git diff --check、文档链接/跟踪状态；源码、测试和记录一起本地提交，不推送/发布/部署。

所有 PowerShell 调用先执行 `. 'E:\sub2二次开发项目\tools\project-env.ps1'`；日志放主项目 `.cache/fix-ultrafast-r2-20261018`，复用依赖。测试使用 `go test -p=2 -tags=unit -count=1`，全量增加 `-timeout=20m -json ./...`，构建使用 `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -p=2 -o <本轮缓存>/sub2api-linux-amd64 ./cmd/server`。

最终复核补充：bare cyber error 后到达真实失败终态时，首次审计证据保留，但终态按值补齐计费元数据与用量；重复终态不覆盖，旧快照不变。对应永久回归已先 RED 再 GREEN。

验收结论：新增 68 叶回归通过；全量其他 57 包通过，服务包适配旧终态断言与隔离进程堆检查后完整复测通过；Linux 构建、独立复核和仓库检查通过。详细计数与条件跳过见修复记录。
