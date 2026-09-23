# v0.2.8 上游合并与镜像发布记录

## 固定来源与范围

- 二开起点：`713d2852e8ea8addce365f595d95823d26bb5aa2`，分支 `feature/monthly-subscription-daily-reset`。
- 官方最新正式发布：[v0.2.8](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.8)，发布于 2026-09-23，源码 `fd80b08c90b55edcad5b00171b53f08721d30da1`。
- 本次同步点：`a3eb7ef302961cba716dc78b39b93b60c467db0e`。它在发布之后仅同步 `backend/cmd/server/VERSION` 为 `0.2.8`，应用逻辑与发布标签相同。
- 共同祖先：`aea725f2ea644d5592d0bbb1d63b607efa7e200a`。二开独有 26 个提交，上游独有 238 个提交。
- 用户明确排除原工作区尚未提交的 Codex/网关修改；这些修改未进入本次源码或镜像。
- 所有实现、验证与审查在 `codex/merge-upstream-v0.2.8` 隔离工作树完成。

## 官方更新范围

新版本增加 GPT-6 Sol/Luna、Claude Opus 5.5、Grok 4.7、OpenCode Go 用量窗口、按推理力度计费、Claude Code 版本同步、可选简易模式消费窗口、月度备份、线下提现登记、Codex 积分/邀请与审核引擎配置。还修复流式响应结束、工具 Schema、模型路由、兑换剩余时长及多处前端过期请求问题。详细说明见上方官方发布链接。

## 逐文件比较与冲突决策

相对共同祖先，上游改变 473 个文件，二开改变 256 个文件，交集 32 个文件，共 697 个不同路径。完整路径、来源分类与最终 Git blob 对比见 [文件清单](upstream-v0.2.8-file-comparison.csv)。441 个仅在上游改变的路径全部与上游一致；同时核对了自动合并的 DTO、路由、服务装配、网关及前端字段。

| 直接冲突文件 | 决策 |
| --- | --- |
| `.gitignore` | 保留官方新增白名单及仍适用的二开文档白名单。 |
| `backend/cmd/server/VERSION` | 完全使用官方 `0.2.8`。 |
| `backend/cmd/server/wire_gen.go` | 保留官方 Claude Code/OpenCode Go/推荐邀请/插件服务装配和清理，加入二开福利 outbox 生命周期。 |
| `backend/internal/service/billing_cache_service.go` | 保留官方数据库权威的简易模式消费窗口检查，二开订阅复核继续作为独立方法。 |
| `backend/internal/service/gateway_usage_billing.go` | 官方简易模式仅累计 API Key 窗口；二开原子计费保护限定到标准计费路径，保留日重置调用。 |
| `backend/internal/service/redeem_service.go` | 整个文件使用上游版本，采用上游行锁、真实锁错误及保留不足一天余量的处理。 |
| `frontend/src/components/common/BaseDialog.vue` | 原样保留上游滚动锁登记逻辑，将二开焦点/inert 管理放在独立弹窗栈。 |
| `frontend/src/stores/announcements.ts` | 使用上游 fetchGeneration 请求顺序保护，并保留二开身份切换保护。 |
| `frontend/src/views/admin/__tests__/GroupsView.duplicate.spec.ts` | 保留上游推理倍率测试及二开日重置权限测试。 |

旧二开计费保护在缺少原子仓库时会覆盖上游简易模式的错误。新增交叉回归测试先确认该问题，再限定二开保护适用范围；上游业务逻辑和上游测试保持不变。

第一轮后端审查还发现既有二开实时订阅检查提前返回错误，会掩盖官方分组删除/停用错误与拒绝统计。修复仅调整二开鉴权接入部分：先用权威分组状态执行官方检查，再处理订阅错误；分组数据库故障仍返回 503，旧缓存里的停用状态不能盖过数据库中的正常状态。Google 入口复用原官方错误消息与监控标记。新增两种协议共 24 个场景，审查代理对修复进行了复核。

第二轮后端审查实际复现了二开后台日重置扫描器在简易模式中扣减订阅 24 小时的问题。修复只限制二开 `ProvideSubscriptionService` 的仓库装配：简易模式不启用付费日重置，标准模式保持原功能；官方订阅构造函数不变。虚拟时钟测试覆盖启动、定时扫描及直接调用入口，标准模式和空配置作为正向对照；原审查代理使用未改动的复现测试独立确认修复。

## 数据库升级约束

历史二开 SQL 文件 `235_subscription_daily_reset.sql`、`239_welfare_center.sql`、`240_welfare_subscription_rewards.sql` 均保留原始文件名及内容。新增官方 `238b`、`239`、`240` 三个 SQL 文件也与上游一致；迁移系统使用完整文件名识别，重复数字前缀不等于重复迁移。

保留并更新官方基线升级测试，另增加部署中二开 v0.2.7 升级测试，检查订阅、日重置历史、福利钱包/账本/抽奖奖励、支付订单、outbox、旧迁移记录不变，以及官方定价回填和再次启动幂等性。

| 审计基线 | SQL 文件数 | SHA-256：按文件名排序，拼接文件名、NUL、trim 后内容、NUL |
| --- | --- | --- |
| 官方 `a3eb7ef302` | 289 | `6075250885f45555d7671005dc80db75c8848e9066a0cc3d291cd36a80c30947` |
| 已部署二开 `713d2852e` | 289 | `47bd915b3a9b9baf8b145c432617b26082f138dcc34170ccfeabdd4079484648` |

## 审查、验证及发布

已经完成三轮审查，每轮分别由后端、前端、迁移/发布三个独立子代理执行。发现问题后先修复，并由原审查代理复核，再进入下一轮。完整记录见 [三轮审查记录](UPSTREAM_MERGE_V0.2.8_REVIEWS_2026-09-24.md)。

| 轮次 | 审查代码树 | 发现与处理 |
| --- | --- | --- |
| 第一轮 | `cbe6249c9175a72e05576627b85d7161fd48a501` | 分组错误与统计被二开准入检查掩盖；修复后复核通过。前端及迁移/发布无阻塞问题。 |
| 第二轮 | `178dc44ffe087cc8bcd837c76ea5ca95e1b5643c` | 简易模式仍启动付费日重置；修复后原复现测试独立通过。前端及迁移/发布无阻塞问题。 |
| 第三轮 | `045353a21ddaeecbd2898ceb113abeba9e079997` | 三个领域均未发现可操作的 P0/P1/P2 问题，可以进入固定提交 CI 阶段。 |

| 本地检查 | 实际结果 |
| --- | --- |
| 合并前关键基线 | 前端 9 文件、97 项通过；后端 237 项测试/子测试通过。 |
| 前端完整 Vitest | 350 文件、2,687 项通过，0 失败、0 跳过。 |
| 前端 ESLint、vue-tsc、生产构建 | 全部退出 0；保留已有 Browserslist 数据及大分块提示。 |
| 后端完整 unit | 58 个测试包、21,427 项测试/子测试通过；17 项按既有环境条件跳过。 |
| 最后修复后的受影响包复测 | service、cmd/server、middleware 三包共 15,524 项通过，0 失败，4 项既有条件跳过。 |
| 数据库集成测试编译 | integration 标签的 repository 测试二进制编译成功；实际执行由 CI 负责。 |
| 最终内嵌前端的后端构建 | 退出 0，版本输出 `0.2.8-r1`；本地验证标记为 `premerge-review-final`，镜像将使用真实提交 SHA。 |
| 源码完整性 | 441 个仅上游改变的路径与官方完全一致；官方及历史二开 SQL 均保留原始 Git blob；无未解决索引冲突。 |

此提交准备先推送 `codex/merge-upstream-v0.2.8` 做 GitHub 验证；在同一完整 SHA 的 CI 和 Security Scan 全部成功之前，不推进远端开发分支或发布镜像。原工作区未提交内容保持原样，更新后的源码位于隔离工作树。

本地 Docker 引擎无响应且已有数据库进程运行，因此没有重启 Docker。数据库集成测试和依赖 Linux 文件权限的官方 release-helper 测试以 GitHub CI 的实际执行结果作为发布门槛，CI 不允许静默跳过不可用的 Docker。

专用发布工作流为 `.github/workflows/publish-custom-v028.yml`：仅推送 `codex/publish-v0.2.8-r1` 才自动发布；源码固定为本次事件的不可变完整 SHA，原生构建和验证 `linux/amd64`、`linux/arm64` 后合成最终标签，拒绝覆盖已有最终版本。不会触发普通 `v*` 标签发布。

本地原始日志目录：`%TEMP%/sub2api-merge-v028-20260924/`。交付时应记录固定源码 SHA、GitHub CI/构建链接、GHCR 标签及 manifest digest；本任务不部署生产服务器。
