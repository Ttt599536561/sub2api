# 2026-11-03 分组 Ultrafast 扩展至所有文本/推理模型

2026-11-04，本功能及预占修复已合入 main，并随 Linux 镜像 `0.2.19-r2` 发布。服务器由用户自行升级。下文为开发阶段记录。[本版发布记录](../../deploy/CUSTOM_IMAGE_0.2.19-r2.md)

## 基线与范围

GitHub `Ttt599536561/sub2api` 的 `main` 已在本轮通过 GitHub 连接器 `GET /repos/Ttt599536561/sub2api/branches/main` 实时核实为 `1b156c8f39493c2554e567812363d1b1f2fce49e`（北京时间 2026-11-01 22:20:50 提交）。工作树由该提交建立，包含 0.2.19-r1 发布源码。Git CLI 直连失败不作为远端证据；未把本地缓存的 origin/main 当成核实结果。

本次工作树：`E:/sub2二次开发项目/.worktrees/ultrafast-all-models-20261103`，分支 `codex/ultrafast-all-models-20261103`。主目录原有未提交修改保留。

用户明确选择：所有文本/推理模型应用8倍；图片、音频、视频的按张/按次计费保持现状。

## 行为

- 沿用 OpenAI 分组现有 `force_openai_ultrafast` 开关，移除 GPT-6.1 Sol / GPT-6 Astra 模型名单。所有文本/推理请求（包括自定义别名、映射、HTTP/WS、Chat/Messages兼容及compact回退）统一发送 `service_tier=ultrafast`。
- 分组/渠道有效 Standard 输入、输出、缓存读写价 × 8，保留已有用户/分组、区间、分时、推理等级等倍率。文本模型配置为按次价格时同样应用8倍；不把最终总费用整体乘8。
- 强制分组固定8倍，上游自报default/flex/priority不降低售价；观测档位继续保留。关闭开关的自然Ultrafast定价和原降档规则不变。
- 并发预占与结算同口径，不再为了判断模型是否在名单中读取账号候选快照；价格无法解析时仍使用原账号映射回退。
- 图片/音频/视频及单独网页搜索费用、混合请求的搜索附加费保持原口径。Embeddings不注入Ultrafast且不套用8倍。
- 保留Fast互斥、免费Fast不适用于Ultrafast、全局block规则；filter/force_priority不会覆盖分组强制Ultrafast。不新增上游支持探测，上游不支持时返回原错误。
- 补齐Chat/Messages流式部分用量错误返回的计费元数据，避免有用量的失败请求丢tier而漏收8倍；成功状态和会话绑定仍只在成功后更新。

## 验证

- 基线专项先通过；新增普通/自定义模型、固定8倍、流式部分用量、专用端点例外均先复现失败再通过。
- 首轮整包发现旧 WS 与 Cyber 白名单/上游降档断言仍使用旧规则；实际出站tier/费用符合新需求。仅更新这些旧预期并加强8倍精确断言，生产代码未因此变更；原关闭开关、WS价格/倍率刷新、模型身份及缓存断言保留。
- 前端 `node node_modules/vitest/vitest.mjs run` 指定分组Fast、Fast policy、service tier文案、locale完整性/编译等5文件，18项通过；修改文件ESLint通过。依赖清单/锁与主仓哈希一致，使用E盘主仓node_modules junction复用；本机pnpm12的旧overrides配置检查拒绝exec，使用已安装Vitest直接执行，不安装或改锁。
- 独立审查及后续测试断言复核通过。覆盖文本按次8倍、混合搜索附加费保持、媒体/Embeddings保价；没有重复乘8或弱化原断言。
- 最终 `go test -tags=unit ./internal/service ./internal/handler -count=1 -json` 退出0：service 226.228s、handler 51.683s，20,555条测试通过事件（包含父级/子测试）、0失败、12项条件跳过。跳过涉及需外部PG/真实API/插件环境及原有禁用用例；不把它们列为通过。完整跳过列表及摘要位于 `.cache/ultrafast-all-models-20261103/final-service-handler-unit.summary.json`。
- gofmt检查、`git diff --check`通过。未执行生产网络/真实收费、数据库集成或容器发布；本次不改数据库结构或仓储。
- 必要证据留在主仓 `.cache/ultrafast-all-models-20261103` 与 `.cache/ultrafast-*.log`，复用共享编译/依赖缓存。没有创建C盘副本或清理其他任务文件。

## 发布与迁移

无新增数据库迁移、配置字段或依赖。已有开启开关的分组在升级应用后扩大适用模型并采用固定8倍；发布时应协调替换应用节点，避免新旧计费语义并存。回退旧应用会恢复原模型名单与上游降档口径。

本轮代码与记录保存为本地功能分支提交（稳定主题 `feat: apply group Ultrafast to all text models`），未合并主目录、推送GitHub、发布镜像或部署服务器。最新已公开镜像仍以既有0.2.19-r1发布记录为准。
