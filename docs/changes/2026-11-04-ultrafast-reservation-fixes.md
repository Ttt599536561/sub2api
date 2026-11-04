# 2026-11-04 Ultrafast 图片及强制平台预占修复

2026-11-04 发布更新：本功能及预占修复已纳入 0.2.19-r2 候选源码 `1e3ca5541`。最新门禁、镜像及部署状态见[本版发布记录](../../deploy/CUSTOM_IMAGE_0.2.19-r2.md)。下文保留开发时的验证与交付状态。

## 问题与基线

基于本地功能提交 `e1c2e00a2`（其父基线为已实时核实的GitHub main `1b156c8f3`）。后续独立复审发现两处预占范围错误；用户在11月4日确认只修按张或按次的图片预占。通过Responses调用纯生图模型时，如果渠道配置为按Token收费，就使用该渠道既有的Token计费规则。主目录既有WIP、分支和发布状态保持。

1. `/v1/responses` 明确使用 `gpt-image-2`，配置按次0.3、分组倍率0.5：实际产图结算0.15，但预占1.20。余额0.50本可支付两次调用，第二次并发被误拒绝。
2. OpenAI分组Key经强制Antigravity路由调用：Claude实际结算0.20，预占1.60，同样错误拒绝足够余额的并发请求。

## 修复规则

- HTTP Responses 使用图片模型、`input` 或兼容的 `prompt`，且 `tool_choice` 缺省或明确选择 `image_generation` 时，进入图片预占判断。Codex 客户端使用图片模型时，缺省选择会在桥接时强制生图。
- 预占根据同一份可调度账号快照核对实际模型和协议。确认是按张或按次计费的图片路径后，使用图片倍率。账号或工具指定多个可能的图片模型时，按实际计费来源取已确认候选的价格上界。渠道按请求模型或渠道映射模型固定计价时，使用该固定价。
- 图片报价考虑分组模型价、各尺寸的分组图片价和渠道模型价。可选图片工具、图片输入、重复字段、可能转为文本或无法确认的路由采用保守预占。Token 计费沿用已有规则。
- Ultrafast 文本预占以 `QuotaPlatform` 判定实际平台。强制走 Antigravity 的请求使用对应平台的预占口径。

## 验证

- 图片真实Forward→RecordUsage→Estimate链路先RED（0.15对1.20），修复后0.15对0.15，两笔在途均准入。
- 强制/已解析平台及优先级表测、真实Antigravity结算与余额0.50两笔0.20准入，先RED再GREEN；OpenAI默认/强制OpenAI仍8倍。
- handler与service回归覆盖严格识别、真实预占入口、重叠预占和映射；补充Spark/账号strip、图片映射文本及none/auto选择可能返回文本的反例，避免预占降低而实际仍按文本8倍结算。
- 真实转发和结算覆盖账号映射、固定计费模型和工具模型。分组倍率0.5时，原请求价格0.30、映射后价格0.90。按原请求计价预占0.15，按映射后模型计价预占0.45，分别与结算一致。
- 最终版本使用 `go test -p 1 -tags=unit ./internal/service ./internal/handler -count=1 -json` 串行复测。并行编译曾触发 Windows 提交内存不足。设置 GOMAXPROCS=4、GOMEMLIMIT=3GiB 后串行命令退出 0。service 与 handler 整包均通过，20,744 条通过记录，0 失败，12 项条件跳过。完整摘要在主仓 `.cache/fix-ultrafast-reservation-20261103/20261104-serial.summary.json`。独立复核、gofmt 和 git diff --check 均通过。
- 日志留在主仓 `.cache/fix-ultrafast-reservation-20261103`、`.cache/ultrafast-platform-reservation-fix-20261103`；复用E盘缓存，无真实上游收费、数据库或服务器操作。

## 交付边界

本地功能分支 `codex/ultrafast-all-models-20261103`，工作树 `E:/sub2二次开发项目/.worktrees/ultrafast-all-models-20261103`。代码、回归与记录一并保存为本地提交 `fix: align Ultrafast reservations with image prices and routed platforms`。未合并主目录、推送GitHub、发布镜像或部署。无新增数据库迁移、依赖或外部配置。
