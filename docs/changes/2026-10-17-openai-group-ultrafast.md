# OpenAI 分组强制 Ultrafast：实施与验证记录

- **日期**：2026-10-17；中断续审完成于 2026-10-18（Asia/Shanghai）
- **功能分支**：`codex/openai-group-ultrafast-20261017`
- **设计基线**：`b9c36c6ee`
- **当前状态**：已完成源码实现与本地验证，未发布镜像、未部署生产

## 已实现行为

- 管理员 OpenAI 分组新增 `force_openai_ultrafast` 开关；Composite 和其他平台归一为关闭，前端平台切换时也立即清除残留状态。
- 强制 Ultrafast 与强制 Fast 互斥，后端以 Ultrafast 优先；免费 Fast 保持独立，不作用于 Ultrafast。
- 只对 `gpt-6.1-sol` 与 `gpt-6-astra`（沿用项目已有别名规范化）强制出站 `service_tier=ultrafast`。
- 不区分 API Key、OAuth 或上游能力清单；命中后统一发送 Ultrafast，上游不支持时返回现有上游错误，不自动降级。
- 对命中分组规则的目标模型，全局 Fast/Flex 策略只允许 `block` 拒绝；`filter` 与 `force_priority` 不会把强制 Ultrafast 降级。
- 只有启用该开关的 OpenAI 分组、目标模型且最终 tier 仍为 Ultrafast 时，才对本次有效 Standard token 定价应用 8×覆盖；未启用分组沿用既有 Ultrafast 计费。
- 覆盖作用于克隆后的请求级定价对象，不修改全局模型目录、共享缓存或其他分组价格。
- 新增数据库迁移 `246_group_force_openai_ultrafast.sql`；认证缓存版本从 24 升至 25。

## 主要提交

- `718f9b457`：分组字段、迁移、Ent 生成代码、管理 API、复制、认证缓存与互斥归一化。
- `c261a20d8`：管理员 UI、类型、国际化、OpenAI-only 显示和 Fast/Ultrafast 互斥交互。
- `26dab2c3b`：HTTP、WS、Responses patch 请求策略及全局策略优先级。
- `c928288eb`：分组感知的 Ultrafast 8× token 计费与回归测试。
- `ea6156c4d`：平台切换时清除不可见 Ultrafast 状态，并补充前端归一化回归测试。

## 原功能实施阶段的实际验证（早于本轮复审）

以下命令均在 E 盘功能工作树执行，并在同一次 PowerShell 调用中显式加载 `tools/project-env.ps1`：

- `go generate ./ent`：通过，生成 Ent 字段和迁移 schema 代码。
- `go test -tags=unit ./...`：通过；Windows 测试进程显式把 Git `usr/bin` 加入 PATH，以满足既有 `pg_dump` 测试夹具调用 `sh` 的环境前提。
- OpenAI Fast/Ultrafast、WS、计费和用量记录聚焦测试：通过。
- 前端全量 `vitest`：424 个测试文件、3607 项测试通过；包含本功能的分组和 i18n 测试。
- 前端 `vue-tsc -b`：通过。
- 前端 production build：通过；项目脚本通过临时 pnpm 9.15.9 shim 执行，直接构建器也通过；仅有既存 chunk size / dynamic import / Browserslist 提示，无构建错误。
- 前端 `pnpm install --offline --frozen-lockfile`：通过，复用 E 盘共享 pnpm store，未下载新包。
- 未执行数据库集成测试、Docker 构建或生产服务器操作；因此不能据此宣称迁移已在线执行或已发布。

前端验证复用了主项目 E 盘现有依赖目录的临时 junction，验证后已删除 junction；没有复制、下载或删除主项目依赖。

## 部署与迁移注意事项

- 发布前需在目标数据库执行应用现有迁移流程，使 `groups.force_openai_ultrafast` 存在；历史迁移未修改。
- 发布新版本前确认 Redis/API Key 认证缓存刷新，代码已通过快照版本 25 使旧快照失效重建。
- 本次没有构建或推送镜像，没有连接生产服务器；已发布的 `0.2.16-r1` 镜像不包含本功能。

## 对 fcaf5f054 的完整功能链复审与修复（2026-10-17）

- **固定基线**：`fcaf5f054f3d5696a3ab9f9234ab299c3f14cd53`；审查 `b9c36c6ee..fcaf5f054` 的整个新增功能链，非只读最后一个文档提交。
- **修复来源**：本节、回归与代码同一提交；稳定主题 `fix: align ultrafast pricing, retries, and group updates`。实际 SHA 由 Git 历史追溯，不猜测本次提交号。
- **状态**：本地源码修复与验证，未推送、未发布新镜像、未部署生产。

### 确认并修复的问题

#### UF-R1 / P1：8× 结算没有同步到余额预占

开启分组开关的目标模型出站、结算已使用 Ultrafast ×8，但在途估算仍不携带 tier 与请求级倍率。实际回归中，有效输入/输出单价 0.001/0.002、分组倍率 0.5、估算 100 输入 + 100 输出 token 时，预占 0.15，实际扣减 1.20；余额 1.50 可以错误接纳两笔本应各预占 1.20 的请求。

修复 `billing_inflight_reservation.go` 与 `billing_token_cost_request.go`：将 tier 与 Ultrafast 覆盖透传到共享 token 计费入口；按上游模型而非仅计费模型判断强制状态。即使采用 requested 定价，渠道或可选账号映射到目标模型也按同一口径预留；明确映射到非目标模型则不因请求别名是目标模型而误乘倍。账号候选只读调度快照、请求内复用，不新增数据库查询或按模型名无限缓存。非 token 项不乘倍，未开启分组保持原路径，Free Fast 不减免 Ultrafast。

估算仍受项目既有 token 上限、预占上限与候选保守报价规则约束，不等于对未知最终 token 消耗的精确保证。首批永久回归包含两目标模型、开关对照、真实预占并发拒绝、渠道/账号映射、非目标模型和无 Resolver，共 14 个叶子场景；随后补入 10 个完整账号候选/路由场景及 13 个 handler 元数据场景。续审的二级 compact 映射与当前验证见后文。

#### UF-R2 / P2：无分组与无 Resolver 的旧计费路径发生回归

原新增功能把原来直接计费的请求统一改走 Unified，导致无分组账号关闭长上下文加价时仍应用目录阶梯。`gpt-5.4` 输入 300000、输出 2000、默认倍率 1.1 的回归中，原应扣 0.858，却扣 1.6995；无 Resolver 的另外两个场景还改变了原推理倍率语义。

修复 `openai_gateway_usage.go`：只要没有本功能的请求级覆盖，且无分组或无 Resolver，就恢复原计费函数及原参数；真正命中 Ultrafast 覆盖的请求继续使用 Unified，即使 Resolver 为 nil 仍按 Standard ×8。永久回归 8 个场景，覆盖长上下文开/关、旧推理倍率、无 Resolver 和两个目标模型的 8×、缓存费用及 Free Fast 独立性；RED 中 3 项失败，GREEN 全部通过。

#### UF-R3 / P2：compact 模型回退未重新计算 tier 与 block

compact 回退会更换实际模型，但此前只在首次转发前执行策略，造成非目标→目标时遗漏 Ultrafast、目标→非目标时残留自动注入的 Ultrafast，并且未检查回退模型的全局 block。

修复 `openai_compact_fallback.go` 及原生、透传两条 HTTP retry loop：启用开关的可信 OpenAI 分组在回退后的 attempt 先恢复 canonical 入站 `service_tier`，再按实际模型应用既有策略，避免把上一模型自动注入的值当成客户端意图。显式客户端 tier 保留；全局 block 在第二次上游发送前返回原策略错误；其他分组不改变重试语义。真实 `Forward` 的 20 场景在原代码上 14 项失败，修复后全部通过。

#### UF-R4 / P2：部分更新无法从 Ultrafast 切回 Fast

已有 Ultra=true / Fast=false 的分组，仅提交 `{"force_openai_fast":true}` 时，服务先合并旧状态，再把历史 Ultra=true 当成同时提交的冲突输入，返回 200 却仍保留 Ultra。

修复 `admin_group.go`：仅在显式开启 Fast、未提交 Ultrafast 字段时清除旧 Ultra；同次显式双 true 仍以 Ultra 优先，单独关闭或省略字段不影响另一模式，Free Fast 和非 OpenAI 平台归一规则不变。新增真实 handler→service→内存仓储的 32 场景，验证响应、持久化值及认证缓存失效；失败用例在修改业务代码前已复现，修复后全部通过。

#### UF-T1：集成测试夹具漏持久化新字段

本轮真实 PostgreSQL 验证发现原认证投影测试失败：`mustCreateGroup` 虽接收 Ultra=true，却没有调用 Ent setter，数据库实际保存默认 false。生产 repo 的字段写入与认证投影均正确，不属于线上存储缺陷。补齐 `fixtures_integration_test.go` 的 setter，并在生产 GroupRepository 上新增开关创建/更新/重读往返测试；同一数据库专项由失败转为通过。

### 2026-10-17 验证记录（中断前阶段）

所有项目命令均在 E 盘工作树、同次 PowerShell 先载入主目录 `tools/project-env.ps1`。源码基线为上述固定提交加本轮修复。

| 检查 | 实际结果与范围 |
| --- | --- |
| 后端全量单元 | Go 1.27.2，`go test -json -tags=unit -count=1 ./...`：58 个有测试包通过，0 失败，18 项条件跳过。Windows 显式加入 Git `usr/bin`，满足原有 `sh` 测试夹具。 |
| 首批新增永久回归 | compact 20、预占 14、旧计费 8、管理 API 32，共 74 个叶子场景；此后候选、handler 和续审补充后的最终范围见下文。 |
| 数据库专项 | `go test -tags=integration ./internal/repository -run '^(TestGetByKeyForAuthCarriesGroupForceOpenAIFast\|TestGroupForceOpenAIUltrafastRoundTrip\|TestGroupRepoSuite\|TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate\|TestMigrationsRunner_AuthIdentityAndPaymentSchemaStayAligned)$' -count=1 -v`：通过；实际命令中的正则为分支 `|`，此表为避免分列转义显示。覆盖新库全部迁移与幂等、schema、认证投影、分组 CRUD 及新字段往返。 |
| 容器环境 | 使用本机已缓存 PostgreSQL `18.1-alpine3.23` / Redis `8.4-alpine`。先核对 WSL 注册 BasePath、实际 VHDX 及父目录均在 `E:\CodexData\DockerBackup\DockerDesktopWSL`，无 C 盘目录联接；测试后没有遗留运行中的测试容器。 |
| 前端全量测试 | 缓存 pnpm 9.15.9 执行 Vitest，424 个文件 / 3608 项通过，0 失败；临时配置只将 root/cacheDir 指向本工作树和 E 盘 `.cache`，禁用测试结果缓存，不改变测试范围或业务配置。 |
| 前端类型与构建 | `pnpm exec vue-tsc --noEmit`、`pnpm exec vite build`（原配置加 E 盘缓存/产物路径覆盖）均通过。只有既有 chunk size、dynamic import、Browserslist 提示。 |
| 后端 Linux 构建 | Go 1.27.2，`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/server`：通过，实际 ELF 产物写入主项目 `.cache`，不作为已发布版本。 |

复核还涵盖管理表单实际交互、分组字段全链路、复制、认证缓存版本失效、请求模型别名、HTTP/WS 统一策略、有效定价与共享价格隔离。最终独立补丁复核的结果在提交前确认；不把测试通过解释为已覆盖所有外部上游或生产环境。

### 交付与未执行边界

- 本轮没有新增 SQL/Ent schema 或修改历史迁移；原功能迁移 246 仍是部署前提。
- 没有执行所有集成包、历史生产数据库升级、多实例 Redis 部署或真实外部 OpenAI 端到端验证；没有发布 Docker 镜像、推送源码或操作生产。
- 前端依赖 manifest/lock 与主目录哈希一致，复用 E 盘依赖和共享 pnpm store，没有重新安装、下载或删除主目录依赖。
- 完成前尝试清理本轮创建的临时依赖 junction；经绝对路径和 junction 目标校验后，原生 PowerShell 删除仍被执行策略以 `blocked by policy` 拒绝，未改用其他方式绕过。临时 junction、构建验证产物及以下 `.cache` 目录因此保留；必要结论已在本受跟踪记录中归档。
- 本机原始证据：`.cache/audit-fcaf5f054-20261017`、`.cache/review-ultrafast-billing-fcaf5f054-20261017-3110a1c9`、`.cache/readonly-ultrafast-admin-review-20261017-8c2e`。主目录原有 WIP 完整保留；已发布 `0.2.16-r1` / `084673c73` 不含本功能或本轮修复。

## 2026-10-18 中断续审

### 接续范围与最后修正

接续聊天“审查并修复 fcaf5f054 代码缺陷”中断时的未提交补丁，继续使用同一功能工作树，保留既有四类修复及主目录 WIP。补齐此前未结束的独立复核、当前源码验证、计划和文档记录。没有重新拉取上游或扩大模型/平台范围。

最终复核发现 UF-R1 还有一处链式映射缺口：legacy `/responses/compact` 在全局 compact 模型配置为空时，支持先按普通账号映射得到模型，再按这个模型匹配 compact 专用映射。预占却只计算普通映射及原请求名对应的 compact 规则，漏掉第二层。例如普通映射到 `gpt-5.4`、再 compact 映射到 `gpt-6.1-sol`，预占 `$0.15` 而结算应为 `$1.20`；反向映出目标模型则错误预占 `$1.20`，实际应为 `$0.15`。

在 `TestInflightEstimate_GroupUltrafastUsesCompleteAccountCandidates` 增加两个真实转发→用量结算→并发准入场景，先确认两项金额断言 RED，再让候选解析传入真实 `OpenAICompactPath`，复用转发的完整解析顺序。原有按请求名的 compact 规则和全局配置仍保留原优先级；native compact 保留普通首发和潜在回退候选，passthrough 保留其原有映射语义。

独立只读复核确认上述修改与生产入口可达路径一致；同时检查 tier 恢复、全局 block、旧计费参数及 handler 元数据透传，未发现其他已确认且未解决的 P1/P2。未把仅直接调用 Forward、但被生产 handler 的 Responses capability 检查排除的 raw Chat Completions compact 路径判为线上缺陷。

新增永久回归最终为 99 个叶子场景：预占原 14 + 完整候选/路由 12 + compact 重试 20 + 旧计费 8 + 管理部分更新 32 + handler 元数据 13；另有之前的数据库往返测试。预占依然受既有 token 估算、上限及候选保守报价规则约束。
### 当前验证与交付边界

本次所有命令均在原 E 盘功能工作树执行，同次 PowerShell 先载入主目录 `tools/project-env.ps1`，复用共享缓存和既有 pnpm 9.15.9。当前验证与 10 月 17 日的记录分别保留：

| 检查 | 2026-10-18 实际执行 |
| --- | --- |
| 接续后全量 unit | Go 1.27.2，`go test -p=2 -tags=unit -count=1 -timeout=20m -json ./...` 退出 0：58 个有测试包、23,935 个通过事件、18 项条件跳过，0 失败。此轮编译早于本次二级 compact 映射的一行修正和两个新增场景；最后修正另做以下验证。 |
| 最后补丁专项 | 3 个受影响包中执行本轮永久回归，退出 0：99 个叶子场景（115 个含父测试的通过事件），0 失败、0 跳过。二级 compact 映入/映出两场景在修改实现前均以报价不一致失败，修改后完整转发、结算及准入断言通过。 |
| 最后补丁服务包完整复测 | `go test -p=2 -tags=unit -count=1 -timeout=20m -json ./internal/service` 退出 0：16,914 个通过事件、4 项条件跳过、0 失败。此轮包含最后一行映射修正和两项新回归，补齐全量 unit 运行后发生变动的服务包验证。 |
| 前端专项 | `pnpm@9.15.9 exec vitest run` 执行 `groupsOpenAIFast.spec.ts` 与 `localeKeyCompleteness.spec.ts`，2 文件 / 9 项通过。功能前端本轮没有修改；未重复前端全量、类型和生产构建，10 月 17 日结果仅作为历史记录。 |
| 最终 Linux 编译 | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -p=2 -o <任务缓存>/sub2api-linux-amd64 ./cmd/server` 退出 0；构建日志、产物 SHA256 均保留，不属于发布镜像。 |

本次未重复数据库集成测试：仓储/迁移相关代码没有再变动，10 月 17 日的真实数据库专项保留为对应阶段证据。本次未运行 Docker、未连接生产、未推送或发布镜像；原功能迁移 246 及认证快照版本 25 的部署要求不变。

本次日志与摘要位于主项目 `.cache/resume-ultrafast-review-20261018`。删除本次临时 Linux 构建文件和两份过程摘要的命令被自动审批审查以 `blocked by policy` 拒绝，未给出具体原因；文件仍在 E 盘本任务缓存目录，未尝试绕过删除。上轮被拒绝清理的临时 junction 和证据目录也未动；共享依赖缓存继续复用。

收尾检查：本次修改的 Go 文件通过 gofmt 检查，git diff --check 通过；4 份更新记录中的 67 个本地链接及锚点均可解析，引用基线提交存在，历史 SQL、Ent schema 和依赖文件没有本轮差异。源码、永久回归与记录一起保存到原功能分支。
