# Ultrafast 全文本模型实施计划

> 执行方式：本轮使用 subagent-driven-development 分工实现、独立复核。

**Goal:** 开启现有 OpenAI 分组 Ultrafast 后，所有文本/推理模型统一发送 ultrafast、按 Standard 有效价格 × 8 计费。

**Architecture:** 移除模型白名单，由可信分组开关控制转发、预占和结算。保留分组/用户倍率、价格来源、缓存与上下文定价、全局 block 和 Fast 互斥。上游降档不降低此分组固定计费，观测档位仍保留。用户已明确媒体按张/按次计费不变；文本模型配置按次价格也应用8倍。无数据库变更。

**Tech Stack:** Go、Vue/TypeScript、Vitest。

## 实施与验收

- [x] 转发：修改 openai_gateway_request_body.go 的分组判断，所有模型返回强制tier；compact/WS复用。更新旧白名单测试，验证自定义模型、映射、回退、filter/force_priority与block。
- [x] 预占：billing_inflight_reservation.go 直接使用分组开关；删除仅为白名单探测而存在的快照扫描，保留价格候选解析。所有文本报价乘8，媒体报价保持。
- [x] 结算：openai_gateway_usage.go 移除最终模型限制；固定tier与8倍覆盖，响应降档保留观测但不降低费用；媒体分支保持。验证输入/输出/缓存费用、余额扣款、无resolver、文本按次、旧分组兼容。
- [x] 前端：中英文 ultrafastHint 说明所有文本/推理模型、8倍、媒体不变；更新并运行提示/互斥测试。
- [x] 验证：先RED再GREEN；运行 Go service/handler 相关专项及整包unit，前端专项，git diff --check；独立复核后同步 CHANGELOG/HANDOFF/变更记录。

## 命令与隔离

工作树 `E:/sub2二次开发项目/.worktrees/ultrafast-all-models-20261103`，基线 `1b156c8f3`。每次 PowerShell 先加载主仓 tools/project-env.ps1；证据在主仓 `.cache/ultrafast-all-models-20261103`。主目录现有WIP保留。未请求发布镜像或服务器部署。

后端核心命令（backend目录）：`go test -tags=unit ./internal/service ./internal/handler -count=1`。
前端命令（frontend目录）：`pnpm exec vitest run src/views/admin/__tests__/groupsOpenAIFast.spec.ts`。

验证结果：service/handler整包20,555条通过事件、0失败、12条件跳过；前端5文件18项、ESLint、gofmt/diff检查及独立复核通过。最终实际命令和例外见 docs/changes/2026-11-03-ultrafast-all-models.md。
