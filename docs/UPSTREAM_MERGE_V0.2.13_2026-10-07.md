# 二开 v0.2.8 升级至 Sub2API v0.2.13

本文和 627 行比对清单记录合并提交 `7352a8606` 当时的源码与验证。后续再次审查发现的问题、修复及最新验证见 [第二轮审查报告](POST_MERGE_REVIEW_V0.2.13_2026-10-07.md)；原清单作为历史快照保留。

## 来源与范围

- 用户确认线上基线为二开 **0.2.8**。采用 `ebaa8c0222b797930f7469bd68154d637d3b5a1a`，其应用源码为 `d3eb9815b8de58a83fdecbfe5f4b245cd97cd497`，最后一个提交仅增加发布记录。
- 此前 0.2.5、0.2.7、0.2.8 的合并、月订阅日重置、福利中心、订阅购买抽奖、退款精度和会话隔离修复均已包含在该基线。
- 本次从官方仓库抓取正式发布标签与 main，固定合并 `b8dece9000c68815a5b867ca5a1e6f236e173905`。它与 [v0.2.13](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.13) 发布提交 `3040209f205472038c1ba745a1bedd2edd9053b1` 仅有 `backend/cmd/server/VERSION` 同步差异。
- 共同祖先：`a3eb7ef302961cba716dc78b39b93b60c467db0e`（官方 0.2.8 版本同步点）；二开独有 28 个提交，上游独有 165 个提交。
- 工作分支：`codex/merge-upstream-v0.2.13`。当前聊天原先位于只含早期设计的 `main`/`b0ba68f93`，已明确避免以它作为功能基线。

## 项目结构与定制边界

后端为 Go/Gin，入口 `backend/cmd/server` 通过 Wire 装配路由、handler、service、repository；Ent 描述数据库实体，PostgreSQL 保存订阅、支付、用量和福利事实，Redis 承担缓存、并发控制及调度状态。请求先经过鉴权和费用准入，再获取用户/账号槽、选择上游及转发，最后通过原子结算更新余额或订阅用量。

二开主要叠加在订阅日重置服务/仓库、福利钱包及消费/支付奖励仓库、支付退款收尾、网关订阅终检和相关缓存失效。日重置按成功操作扣减 24 小时订阅有效期，仅清空日额度；福利奖励和退款回退保留自己的幂等事实与事务。前端为 Vue 3/TypeScript，`api`、Pinia stores/composables、router 和 views 连接上述能力；二开对身份切换后的异步响应、弹窗焦点以及版本展示另有保护。这些定制边界均参与此次比较，而非仅处理 Git 标记的冲突行。

## 逐版审查

下表提交数和文件数来自相邻正式标签的 Git 比较；不同区间的文件可能重复。正式合并覆盖全部累计提交，不需要重复逐个 cherry-pick 发布版本。

| 版本 | 正式源码 | 区间提交数 | 区间改变文件数 | 重点检查 |
| --- | --- | ---: | ---: | --- |
| [0.2.9](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.9) | `4c00df2e0` | 70 | 117 | 白名单 glob、响应/工具转换、额度暂停、流式计费、客户端断连、Redis 启动形式。 |
| [0.2.10](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.10) | `2f3fed2fd` | 33 | 118 | Sonnet 5.5、Claude 原生额度查询、风控豁免、复合分组路由、工具和流式用量修复。 |
| [0.2.11](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.11) | `96f4c115c` | 23 | 90 | GPT-6.1、Claude 原生额度兑换、远程模型目录、API Key 创建限制、余额在途预占与二开原子计费/福利记账。 |
| [0.2.12](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.12) | `510606571` | 29 | 151 | TypeSafe 全链路、充值赠金/折扣、验证码/密码令牌原子处理、公开订单查询限制、两个数据库迁移。 |
| [0.2.13](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.13) | `3040209f2` | 10 | 7 | TypeSafe 账号计费探测默认值、API Key 在请求完成前被删除时继续正确结算。 |

## 合并判断和修复

**不能无检查地直接合并。** Git 三方试合并只有一处文本冲突，但新增入口和既有测试存在额外兼容问题。

相对共同祖先，上游改变 394 个文件，二开改变 261 个文件，两侧交集 33 个文件，合计 622 个不同路径。其中 361 个为仅上游改动，228 个为仅二开改动。最终 [逐路径清单](upstream-v0.2.13-file-comparison.csv) 共 627 行：622 个源差异路径加 5 个此次额外修改/新增测试路径，不包含本次审计文档自身。361 个仅上游路径中，360 个与官方原始 blob 相同，唯一适配为 SystemOne 订阅终检。228 个仅二开路径中，225 个与已部署版本相同，另外三个仅调整升级/福利集成测试。33 个重叠路径均完成领域审查，未以批量覆盖方式解决。

| 文件/领域 | 处理 |
| --- | --- |
| `backend/internal/service/wire.go` | 合并唯一文本冲突：完整保留官方 Claude 原生额度兑换 provider，并添加原二开福利服务及 outbox worker；两类重置服务分别面向账号和用户订阅。 |
| `gateway_systemone.go` | 新 TypeSafe 入口需在每次账号获取/排队/failover 后执行二开权威订阅复核；拒绝时释放账号槽，保留上游响应和计费流程。 |
| `usage_billing_repo_unit_test.go`、`welfare_spend_repo_integration_test.go` | 上游新增“Key 已删除仍扣费”测试须同时期望二开福利消费事实写入；旧福利集成测试也不能再把不存在的 Key 当成回滚错误。保留真实事务失败回滚检查，并增加软删除 Key 的扣款、福利累计与去重数据库回归。生产结算继续采用官方实现。 |
| 升级测试 fixture | 将既有官方 0.2.8、二开 0.2.7、线上二开 0.2.8 的 SQL 集合以文件边界、数量和 SHA-256 固定；新迁移只属于目标数据库，不能漂移进入旧 fixture。 |
| `settings.authSourceDefaults.spec.ts` | 上游 TypeSafe 已令默认平台从五个变为六个，但原测试未更新；更新测试并补 TypeSafe 额度读写/清洗检查，保留上游业务实现。 |
| Axios 1.20.0 | 增加真实 transport 回归，验证福利操作在派发前换账号/重新登录时取消，同会话刷新 token 保持正常；无需修改原二开身份隔离实现。 |

前端六个重叠文件中，白名单任意位置通配符与二开日重置许可并存；新增充值优惠沿官方支付路径处理，福利兑换仍走独立余额流程。后端保留官方余额预占、删除 Key 继续结算、简易模式以及二开订阅终检、24 小时扣天、福利幂等/退款回退与缓存失效逻辑。

## 数据库升级

历史 SQL 不改名、不改内容。两个官方新增迁移分别是：

- `241_add_payment_order_bonus_amount.sql`
- `241_add_typesafe_platform.sql`

迁移器以完整文件名识别，两个 `241` 前缀可共存。原二开 `235_subscription_daily_reset.sql`、`239_welfare_center.sql`、`240_welfare_subscription_rewards.sql` 保持已部署版本的原始内容。固定历史校验采用文件名排序后拼接 `filename + NUL + trim(content) + NUL` 的 SHA-256。

| 历史源 | SQL 数量 | SHA-256 |
| --- | ---: | --- |
| 官方 0.2.8 `a3eb7ef302` | 289 | `6075250885f45555d7671005dc80db75c8848e9066a0cc3d291cd36a80c30947` |
| 二开 0.2.7 `713d2852e` | 289 | `47bd915b3a9b9baf8b145c432617b26082f138dcc34170ccfeabdd4079484648` |
| 线上二开 0.2.8 `ebaa8c022` | 292 | `48aad43a88107ef6f79d07952c8c1e19347f7f498b8f21295bd71816c2afcb64` |

新增升级场景使用固定历史迁移和构造数据，覆盖已有用户、订阅、日重置事件、余额/订阅订单、部分退款审计、福利钱包/账本/奖励回退、outbox、旧平台配额/路由及原迁移记录；验证新增赠金默认零、TypeSafe 可用、无效平台仍拒绝，以及再次启动的数据和迁移历史幂等性。没有读取或修改线上数据库。最终 294 个 SQL 中，全部 292 个已部署 SQL 和全部 291 个官方 SQL 均与对应来源的 Git blob 一致。

## 升级时需要了解的官方行为变化

- 0.2.11 默认限制每用户 200 个有效 API Key、每小时创建 60 次；可通过 `api_key_create.max_active_per_user` 和 `api_key_create.max_per_user_per_hour` 调整，零代表不限制。
- 0.2.11 默认开启余额在途预占，低余额并发请求可能较早被拒绝；配置位于 `billing.inflight_reservation`。本次保留官方默认行为。
- 0.2.12 改用哈希存储密码重置令牌，升级前尚未使用的旧密码重置链接需要重新申请。

以上对应官方 [0.2.11 发布说明](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.11) 和 [0.2.12 升级说明](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.12)。生产升级前应备份数据库；本次只处理源码合并与验证，未部署生产或发布镜像。

## 验证结果与审查

证据目录：`%TEMP%/sub2api-merge-v0213-20261007/`。以下均为本次实际执行结果；测试数包含测试和子测试，不同构建标签之间有重叠，不能相加为唯一用例数。

| 检查 | 结果 | 主要证据文件 |
| --- | --- | --- |
| 前端冻结安装 | pnpm 9.15.9 成功，官方 lockfile 未改 | `frontend-install.log` |
| 前端完整 Vitest | 355 文件，2,800 项通过，0 失败、0 跳过 | `frontend-vitest-final.json` |
| 前端 ESLint、TypeScript、i18n、生产构建 | 全部通过；保留现有 Browserslist 和大分块提示 | `frontend-lint.log`、`frontend-typecheck.log`、`frontend-i18n.log`、`frontend-build.log` |
| Linux 后端完整 unit | 58 个测试包，22,221 项通过，0 失败，17 项既有环境条件跳过 | `backend-linux-unit.jsonl` |
| 完整 integration | 52 个测试包，13,856 项通过，0 失败，19 项既有环境条件跳过；`CI=true`，真实 PostgreSQL 18.1/Redis 8.4 | `backend-integration-final.jsonl` |
| 三条数据库升级路径 | 官方 0.2.8、二开 0.2.7、线上二开 0.2.8 → 当前版本均通过，0 跳过 | `migration-upgrade-tests-retry.log` |
| 最后测试 helper 标签调整 | 固定历史基线 unit 和全部三个真实升级场景再次通过 | `migration-helper-unit.log`、`migration-helper-integration.log` |
| 后端完整 golangci-lint | 官方 2.13.0 / Go 1.27，退出 0，`0 issues.` | `backend-golangci-lint-final.log`、`backend-golangci-lint-final-result.json` |
| Ent/Wire 生成一致性 | 在隔离 Linux 副本重新生成，所有 Ent 文件及 Wire 输出与合并结果完全一致，无需额外生成改动 | `linux-generation.log`、`generated-ent/`、`generated-wire.go` |
| 内嵌前端的后端构建 | Windows `CGO_ENABLED=0 go build -tags embed -trimpath` 通过，程序版本输出 `0.2.13` | `backend-build.log`、`sub2api-v0213.exe` |
| 部署/发布辅助检查 | 五项 Linux shell 检查、10 项 release-helper 测试及脚本语法检查通过 | `deploy-tests.log` |
| 源码完整性 | 历史 SQL 与上游 SQL 全部不变；无未解决冲突；Git diff whitespace 检查通过 | 逐文件清单与 Git 索引 |

前后端、迁移分别由领域代理审查，再由独立代理复核实际修改、历史 Git 摘要和 RED/GREEN 日志。发现的 SystemOne 缺少终检、删除 Key 结算测试假设、平台枚举 fixture、历史迁移 fixture 均修复；最终未发现未处理的 P0/P1/P2。Lint 发现历史快照查询 helper 只供 integration 使用，已将它原样放入相同标签的测试文件并复测，未放宽 lint 规则。

本机默认 pnpm 11 与项目 CI 的 pnpm 9 不一致，因此使用 `corepack pnpm@9.15.9`；生产构建的各脚本也使用该版本执行。Go 全局模块缓存中部分目录缺失源码，改用本任务隔离模块缓存重新获取原锁定版本。Windows 原地 Ent 生成遇到文件映射锁，改为隔离 Linux 副本生成并逐文件核对。Windows 首轮测试中三项 PgDumper 用例因 PATH 缺 `sh` 失败，临时添加 Git Bash 后七项专项通过，最终完整集成测试也在相同修正环境下通过；Linux 完整 unit 无此问题。没有为绕过环境问题修改生产逻辑或依赖版本。

本次没有触发 GitHub CI/安全扫描、发布镜像或部署生产。macOS 专用 Apple container 功能测试无法在本机验证，仅检查 shell 语法；真实外部模型/支付凭据相关用例遵循现有环境条件跳过。最终构建使用默认 `commit: unknown`，未冒用上游提交作为二开构建来源。
