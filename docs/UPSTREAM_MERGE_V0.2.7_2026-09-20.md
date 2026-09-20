# v0.2.7 上游合并与冲突修复记录

日期：2026-09-20。

## 对比基准

- **二开基准是本次操作开始时本项目最新提交**：`11ac4ab7e56c114d8030c87fa05493245817797c`，`fix(welfare): preserve retry identity and isolate transient failures`，2026-09-20 01:43:18 +08:00。
- 原分支：`feature/monthly-subscription-daily-reset`。没有使用远端较早的 `f89835007`、之前的合并分支或工作区内旧 `merge-tree-output.txt` 作为基准。
- 官方最新正式发布：[v0.2.7](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.7)，提交 `aea725f2e`。
- 共同祖先：`881f3202694c6bc932446931a30c27d9675178b9`。相对共同祖先，二开独有 14 个提交，上游独有 70 个提交；上游差异涉及 130 个文件。
- 在 `codex/merge-upstream-v0.2.7` 隔离工作树完成三方合并、冲突修复和验证。

## 直接冲突及处理

| 文件 | 处理 |
| --- | --- |
| `.gitignore` | 合并双方文档白名单，保留二开交付文档及官方 Antigravity 修复说明。 |
| `frontend/src/components/common/BaseDialog.vue` | 保留二开焦点栈、嵌套弹窗、inert 恢复；二开原本已具备上游同样的模块级唯一 ID 计数器，因此该文件最终与二开基准一致。 |
| `frontend/src/stores/announcements.ts` | 使用上游 `Promise.allSettled` 保存部分成功并传播失败，同时保留二开的会话代次保护；成功后按 ID 更新当前列表，兼容请求期间列表刷新。 |
| `frontend/src/views/admin/__tests__/ChannelMonitorView.grok.spec.ts` | 保留二开的 10 平台检查及 MiniMax、OpenCode Go 断言；最终与二开基准一致。 |

公告合并新增 3 个测试实例，覆盖刷新替换记录后的部分成功/重试，以及新旧账号具有相同公告 ID 时旧批次成功或失败的隔离。先在原二开实现确认相关用例失败，再修复到公告专项 16 项全部通过。

自动合并的 `subscriptions.clear()` 出现重复 `loading.value = false`，已去除冗余赋值，保留上游修复效果和二开会话清理逻辑。

官方发布标签内 `backend/cmd/server/VERSION` 仍是 `0.2.5`。按官方随后唯一的版本同步提交 `1a9d49e16f7a22c432b428fce4af8d731f1fa364` 更新为 `0.2.7`，避免从合并分支源码构建时显示旧版本。业务代码以 v0.2.7 发布标签为准。

## 二开兼容复核

- 月卡提前重置、福利中心、余额结算、会话隔离的最新修复均保留。
- 上游本轮没有 Ent 或数据库迁移改动。二开的 `235_subscription_daily_reset.sql`、`239_welfare_center.sql` 及历史迁移校验保留原样。
- 上游兑换记录分页与本地订阅退款扣天行锁改动不重叠，二者均保留。
- Seedance 入口复用媒体计费与账号槽位获取；本地槽位后的订阅复核继续生效，余额结算继续经过福利消费累计。
- 插件 KV store、账号目录 setter、福利 outbox 与月卡扫描器的启动/关闭装配均已核对。
- 前后端分别经过独立代理只读复审，未发现本次合并新增的阻塞问题。

## 验证结果

| 检查 | 结果 |
| --- | --- |
| 原二开前端相关基线 | 17 个文件、144 项测试通过。 |
| 原二开后端专项基线 | service、repository、middleware 的福利、日重置、准入与熔断测试通过。 |
| 合并后前端完整 Vitest | 315 个文件、2425 项测试通过，0 失败、0 跳过。 |
| 最终公告/订阅专项 | 4 个文件、43 项测试通过。 |
| 前端 ESLint / vue-tsc / Vite 构建 | 全部退出 0；保留已有 Browserslist 数据与大分块警告。 |
| 后端完整 unit | 57 个测试包、20,205 项测试/子用例通过，0 失败；16 项依各自环境条件跳过。 |
| 后端数据库专项 | 本机 Docker 隔离容器，46 个顶层测试/套件、87 个含子测试结果通过，0 失败、0 跳过。 |
| 后端 golangci-lint 2.13.0 | 退出 0，`0 issues.`。 |
| 后端源码构建 | `CGO_ENABLED=0 go build -trimpath ./cmd/server` 通过，`-version` 输出 `Sub2API 0.2.7`。 |

数据库验证设置 `CI=true`，缺少 Docker 不会被静默当作测试成功；覆盖历史数据升级、迁移 checksum、重置/计费/删除锁序、并发续期、福利幂等及余额缓存。测试没有连接生产数据库。

主要命令：

```text
# frontend
node node_modules/vitest/vitest.mjs run --maxWorkers=2 --minWorkers=1
node node_modules/eslint/bin/eslint.js . --ext .vue,.js,.jsx,.cjs,.mjs,.ts,.tsx,.cts,.mts
node node_modules/vue-tsc/bin/vue-tsc.js -b
node node_modules/vite/bin/vite.js build

# backend
go test -p 1 -tags=unit ./... -count=1 -json
go test -p 1 -tags=integration ./internal/repository -run 'Test(Review3|SubscriptionDailyReset|PostmergeBulkRenewal|Postmerge|SubscriptionBulkReset|Welfare|UsageBillingRepository|BillingCache)' -count=1 -timeout=10m -json
golangci-lint run --timeout=10m ./...
```

前端依赖清单和 lockfile 未变，隔离工作树复用原工作区已有依赖。Go 首轮检查因为默认直连官方依赖源超时失败；仅给验证进程使用本机现有代理，下载成功后重跑，没有降级依赖或改写持久 Go 配置。Windows Go 测试进程 PATH 包含 Git 的 `sh`。

日志目录：`%TEMP%/sub2api-merge-v027-20260920/`。前端汇总：`frontend-vitest.json`；后端最终单元：`backend-unit-retry.jsonl`；数据库：`backend-integration.jsonl`；lint：`backend-lint-retry.log`。首轮网络失败日志另行保留。

## 范围与后续注意

- 本次为本地源码合并与验证，没有推送远端或部署线上；原来的三个无关未跟踪文件保留。
- 合并前提交保留在 `codex/pre-upstream-v0.2.7`，合并结果保存于 `codex/merge-upstream-v0.2.7` 并快进到原 `feature/monthly-subscription-daily-reset` 分支。
- 未对真实上游账号发起付费 API 请求；浏览器原生 Tab/inert 与过渡动画的组合行为仅有组件测试覆盖。
- 官方 v0.2.7 的插件账号目录 setter 位于 `wire_gen.go`，Wire provider 本身尚未表达该 setter。当前合并完整保留调用；以后重新生成 Wire 时应复核此处，本轮没有重生成或扩大修改该上游既存问题。

## 2026-09-20 合并后再次审查与 r6 发布

本次重新 fetch 远端后，以功能分支最新提交 `6bd6654ee616a942410850be8902dc35111ec7bb` 为审查基准，复查合并 `1f428c024` 及之后的三个提交。合并后的提交仅增加 r5 发布工作流、注册表错误处理和部署说明，应用源码、Dockerfile、入口脚本没有再变化。

重新检查 Git 索引、冲突标记和两个父分支的重叠修改，未发现未解决冲突或可证实的合并行为回归。前后端及发布流程分别经过独立只读复审：

- 前端保留公告批量已读的部分成功及会话隔离、订阅清理、弹窗唯一 ID 和焦点栈；兑换记录分页与后端响应兼容。
- 后端重叠文件保留双方逻辑；Seedance 账号槽位后的订阅复核、余额计费中的福利累计、插件 KV 和账号目录装配、福利 outbox 与日重置扫描器均保留。
- 未新增业务代码修复或数据库迁移。本次只记录复查证据，并将专用发布工作流更新为 `0.2.7-r6`，固定构建本次审查的完整源码 SHA；保留双架构验证和最终版本禁止覆盖保护。

本次重新执行的本地验证（不复用上次测试结果）：

| 检查 | 结果 |
| --- | --- |
| 前端完整 Vitest | 315 个文件、2425 项通过，0 失败、0 跳过。 |
| 前端 ESLint / vue-tsc / Vite 构建 | 全部退出 0；仍有已有的 Browserslist 数据及大分块警告。 |
| 后端完整 unit | 57 个测试包、20,205 项测试/子用例通过，0 失败；16 项按测试环境条件跳过。 |
| 数据库专项 | `CI=true`、本机 Docker 隔离数据库；46 个顶层测试/套件、87 个含子测试结果通过，0 失败、0 跳过。 |
| 内嵌前端的后端编译 | `CGO_ENABLED=0 go build -tags embed -trimpath` 通过，版本输出 `0.2.7-r6` 及固定源码 `6bd6654ee616a942410850be8902dc35111ec7bb`。 |
| 发布配置 | YAML 解析及 `git diff --check` 通过；独立复审确认版本、触发分支、并发组及源码 SHA 一致。 |

固定源码的 [GitHub CI](https://github.com/Ttt599536561/sub2api/actions/runs/35461419874) 和[安全扫描](https://github.com/Ttt599536561/sub2api/actions/runs/35461419868) 已重新查询为成功。发布通过推送 `codex/publish-v0.2.7-r6` 触发，两架构镜像通过版本、revision、客户端资源及 setup 接口验证后才合成最终标签。

本次日志保存于 `%TEMP%/sub2api-postmerge-r6-20260920/`。本轮未连接生产数据库或真实付费上游，未执行生产服务器部署；浏览器原生焦点及过渡组合行为仍以组件测试覆盖。
