# 108c63d01 三项复审问题修复

基线：应用代码 108c63d01；工作树 HEAD 43eec7181（仅追加文档）。用户明确授权主代理完成三项修复、回归、重新验证及独立复审；不使用子代理。保留上一轮审查文档和主目录既有 WIP。

## 设计与执行计划

- [x] R11：在 useAdminDialogSession 中区分表单边界和登录失效。登录变化使当前打开周期不可提交并触发两个弹窗的 close；即使父组件暂未更新 show，也不能发送 mutation。重新打开/切换目标后才开启新周期，沿现有 watcher 重读权威列表。选择关闭而非自动重载，避免把清空状态误当权威空列表，也避免增加自动重试状态机。
- [x] R12：缓存创建返回后一次性捕获 storageStartedAt，先计算 expireTime = Clamp(upstreamExpiry, storageStartedAt + ttl)，再按 expireTime - storageStartedAt 计算非负存储量。准入、报价、记录和结算使用同一组值，继续保留输入费和原报价；已有失败清理、续期逻辑不变。覆盖较短/超长/缺失到期时间及只能支付实际时长的余额。
- [x] R13：RPM 的清空/保存按钮与方法入口均检查 clearing/saving/loading，避免 DELETE 与 PUT 同时执行。覆盖两个顺序、失败解锁重试。
- [x] RED：新增 frontend/src/components/admin/group/__tests__/GroupDialog.reviewFixes.spec.ts 和 backend/internal/handler/gemini_cached_contents_expiry_review_test.go，先运行并保存预期失败。
- [x] GREEN：只修改关联 helper、两个 Vue 组件及缓存创建 handler；运行专项及已有相关回归，确认首次故障转为通过。
- [x] 完整验证：前端全量 Vitest、类型、lint、生产构建；后端 unit 然后 integration 串行执行（不并行生成 Ent）；Linux/amd64 编译；历史 SQL blob、git diff --check、主目录状态检查。所有命令使用主项目环境入口，日志放主项目 .cache/fix-review-108c63d01-20261015。
- [x] 主代理独立复审最终 diff 与调用链：重新核对 owner/权限/重开边界，迟到请求不可关新窗或解锁新操作，费用快照与实际时长/失败清理保持一致；发现问题先复现再修复。
- [x] 归档实际验证与限制，更新 CHANGELOG.md、docs/HANDOFF.md 和本记录；保留上一阶段历史结论并明确三个待办的完成状态。源码、测试和记录同一提交，不推送、不发布、不部署。

## 验收命令

每条命令所在的 PowerShell 调用先执行 `. 'E:\sub2二次开发项目\tools\project-env.ps1'`。

- frontend：`corepack pnpm@9.15.9 exec vitest run src/components/admin/group/__tests__/GroupDialog.reviewFixes.spec.ts --maxWorkers=2 --minWorkers=1`，修复前应因会话关闭/提交隔离或 RPM 并发约束失败，修复后通过。
- backend：`go test -p 2 -count=1 -tags=unit ./internal/handler -run TestGeminiCachedContentsCreate_ActualExpiry -v`，修复前较短时长计费和余额准入失败，修复后通过。
- 后端全量：`go test -p 2 -count=1 -timeout=20m -tags=unit -json ./...`，完成后再用 integration 标签执行。
- 前端全量：`corepack pnpm@9.15.9 exec vitest run --maxWorkers=6 --minWorkers=1`；`vue-tsc --noEmit`、`pnpm run lint:check`、`vite build --outDir E:/sub2二次开发项目/.cache/fix-review-108c63d01-20261015/frontend-dist-final`。

## 已执行的复现与修复

- RED 前端初始 12 项中 10 项按预期失败：未发出关闭事件、父组件未关闭前仍可搜索提交、双向互斥按钮未禁用；两项既有迟到加载隔离通过。后端 5 个子场景中较短到期、仅能支付实际时长、已过期存储量三个场景失败，较长/缺失到期兼容场景通过。
- GREEN 前端既有相关 13 文件 179 项通过；补充原配置加新用户、渲染前快速双击顺序后，新文件 16 项通过；独立复审又补了两个加载失败用例，先得到 2 个预期失败，修复后相关 13 文件 185 项通过，新文件共 18 项。缓存 handler/service 专项通过，较短时长案例由 $109.25 改为约 $55.25（允许测试执行期间自然消耗的极小时间差）；余额 $60 可以完成创建。
- R11 的失效标记在关闭通知之前设置，父组件晚更新 show 或权限再次恢复都不能接纳空表单；重开/目标变化建立新周期，现有加载 watcher 重新读权威列表。旧响应继续受代次、owner、target、权限和卸载边界检查。
- R12 在返回上游后仅捕获一次时钟，expireTime、storageTokenHours 和报价在额度准入前统一；后续保存延迟不能再延长收费时长。超过请求上限或缺失有效期仍按请求 TTL，非正剩余时长不产生存储费；输入费、失败清理及既有价格快照保持原契约。
- R13 方法入口与按钮双层约束 clearing/saving/loading，连续点击也只能派发一个 mutation；失败时清除当前操作忙状态，允许重试，旧周期 finally 仍不能解锁新操作。

完整测试、最终独立复审与提交前检查见下方。原始证据在主项目 `.cache/fix-review-108c63d01-20261015`；本记录随源码保存。

## 主代理独立复审记录

实现后从最终 diff 重新追踪两个弹窗 → 会话 helper → 分组 API → 后端整组同步，以及缓存 handler → QuoteUsageCost → ReserveFixedCharge → 异步 RecordUsage。没有启用任何子代理，也没有调用外部代码审查代理。

第一遍复审补出了同类遗漏：重新打开后列表读取失败，旧实现依然允许从本地空列表新增后替换服务器配置。两个新回归先失败；现在 entriesLoaded 只在当前周期的完整列表成功返回后置 true，失效、加载开始和失败均保持 false，搜索/添加/保存入口与按钮受此状态限制。失败提示和关闭重试沿用现有交互。最后的前端全量、类型/lint/构建重新针对这版代码运行；此前未结束的前端全量已中止，不计作完成证据。

第二遍静态复审逐项确认：

- 登录改变与表单目标改变分开处理。登录失效先锁住周期，再发 close，避免父组件更新前的空表单提交；权限重新升回管理员也不会解锁旧周期。
- 重开只在新列表成功后允许修改，旧加载/搜索/保存/清空的成功、失败和 finally 仍受 owner、代次、目标、权限和卸载边界保护。
- RPM 两种派发顺序均在方法入口同步排斥；失败可重试。读取失败不能通过新增行或清空绕过完整列表条件。
- 缓存创建的到期时间与收费时长在同一时点确定，预留和结算共用原报价；上游更短有效期及余额边界正确，超长/缺失时仍保留原上限语义，输入费用不被削减。保存失败/额度拒绝仍清理上游并按原价格收输入费。
- 本轮未改业务 API 的路径/body、历史 SQL、Ent、依赖或月卡/福利规则。最终验证开始时保存了 6 个源码与回归文件的 SHA-256，结束后重新核对，防止将非最终代码的结果混用。

本轮相关变更的第二遍代码复审未发现新的待修复问题。完整测试结果、条件跳过与源码一致性检查另列于最终验证节；此结论不替代真实外部服务和生产验收。
## 最终验证与限制

- 后端 unit：`go test -p 2 -count=1 -timeout=20m -tags=unit -json ./...` 退出 0，58 个有测试包、23,688 个通过事件、18 个条件跳过、0 失败。
- 后端 integration：同命令使用 integration 标签，退出 0，52 个有测试包、14,501 个通过事件、16 个条件跳过、0 失败，含真实 PG/Redis 及历史升级回归。Docker 磁盘再次核对为 E 盘 VHD。
- 后端 Linux/amd64 编译：CGO_ENABLED=0 的 `go build -p 2 -trimpath ... ./cmd/server` 退出 0；本次未使用 embed 标签、未执行发布镜像或生产启动。
- 最终前端 `lint:check`、`vue-tsc --noEmit` 均退出 0。最终 Vite 生产构建退出 0（独立输出目录），仅有既有 Browserslist 时效和大 chunk 提示；最终完整 Vitest（maxWorkers=6、minWorkers=1）退出 0：422 文件、3,593 项通过，0 失败；新增 18 项回归包含在该全量结果内。

Go 数字包含父/子测试事件，unit 与 integration 不可相加。unit 的 18 项、integration 的 16 项条件跳过包含缺少显式 PG/Redis 测试地址的 9 项安全审计，以及既有 OAuth 占位、外部抓包、真实 TypeSafe/OpenAI/插件运行时等；integration 另有既有禁用并发缓存子用例。跳过列表保存于 JSON 摘要，未把跳过当通过。没有访问生产支付、OAuth 或模型服务。

本轮没有新增迁移：当前基线 300 份 SQL、Ent、依赖声明和锁文件无变化。部署不需要额外数据迁移或新增环境变量。历史多收费用的核账/补偿不由此源码修复自动执行，本轮未改生产或历史账单。

清理限制：自动审批审查拒绝了删除本轮 `.cache/fix-review-108c63d01-20261015` 内两份前端构建目录、临时 Linux 二进制及一次性编辑脚本的命令；仅返回 `blocked by policy`，没有具体原因。命令未执行，未尝试绕过，产物保留在 E 盘本轮缓存目录。日志/摘要/源码哈希继续保留，共享依赖缓存和其它任务目录未清理。集成测试结束后的 docker ps 无运行中容器。
最终结论：R11、R12、R13 均已修复；额外复核发现的同类加载失败缺口也完成 RED→GREEN 回归。独立复审范围内没有新的待修复项。后端新增 5 个到期时间子场景、前端新增 18 项回归，未削弱或删除旧测试。最终代码与验证开始时的 6 个源码/测试文件哈希一致，`git diff --check` 和本轮四份文档的本地链接检查通过。主目录 Git 状态仍为接手时的既有 WIP，本轮未修改。

本轮工具环境：backend 目录自动选用 Go 1.27.2（主目录不在 Go 模块内的全局 go 为 1.26.1）；Node v24.13.0、pnpm 9.15.9。编译/依赖缓存为主项目 E 盘 .cache。工作在 `codex/merge-upstream-20261015`，源码、测试与此记录以稳定主题 `fix: preserve group dialog state and bill actual cache lifetime` 同批提交；不预写本提交 SHA，实际来源以 Git 历史为准。未推送、未发布、未部署；生产镜像仍以原交接记录为准。