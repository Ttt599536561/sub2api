# 二开 v0.2.8-r1 升级至官方 Sub2API v0.2.14

> 最新状态：第二轮全面复审已修复 7 类问题，源码提交 9e6da49e3；后端 unit 22,399 项、integration 13,904 项、前端 2,957 项通过。详见 [第二轮完整报告](POST_MERGE_REVIEW_R2_V0.2.14_2026-10-07.md)。

> 上一轮追加审查：三个子代理确认并修复 5 类问题，源码提交 aae0f1829，最终后端 unit 22,373 项、integration 13,895 项、前端 2,915 项通过。详见 [合并后追加独立审查](POST_MERGE_REVIEW_V0.2.14_2026-10-07.md)。本文其余内容保留初次合并及迁移后验证的历史快照。

> 2026-10-07 补充：Docker 数据盘迁到 E 盘后，完整真实数据库集成测试 13,885 项通过，三条历史升级路径全部通过。首轮 Docker 环境缺口已补齐，详见 [迁移后验证记录](POST_MIGRATION_VALIDATION_V0.2.14_2026-10-07.md)。

## 结论与工作位置

不能从线上二开基线无检查地直接合并：从 v0.2.11 起存在 Wire 文本冲突，余额预占、新请求入口、支付与退款还有语义兼容要求。本次复用已经提交的 v0.2.13 合并及三轮修复，继续合入官方 v0.2.14；新增兼容调整只涉及配置生成测试，生产安全逻辑与依赖沿用官方。

- 官方远程：`upstream-official` → `https://github.com/Wei-Shaw/sub2api.git`。
- 官方分支：`upstream-official/main`；标签使用独立命名空间 `upstream-official/v0.2.*`，不覆盖原有标签。
- 本地整合分支：`codex/merge-upstream-v0.2.14`。
- 工作树：`E:/sub2二次开发项目/.worktrees/sub2api-upstream-v0.2.14`。
- 主目录原分支及未提交修改原样保留，未混入本次基于线上部署版本的整合；未推送、发布镜像或部署。

## 固定来源与项目结构

| 用途 | 固定提交 |
| --- | --- |
| 已部署 v0.2.8-r1 的源码 | `d3eb9815b8de58a83fdecbfe5f4b245cd97cd497` |
| 部署基线加发布记录 | `ebaa8c0222b797930f7469bd68154d637d3b5a1a` |
| 官方共同祖先 | `a3eb7ef302961cba716dc78b39b93b60c467db0e` |
| 已有 v0.2.13 合并和最终复审起点 | `f162fda580ee85d89ee4a1d992bffd366e628ec8` |
| 官方 v0.2.14 标签 | `0363b8cdba8cec3e2ba4b2dbd49c4481143fa55d` |
| 本次固定合入的官方 main | `3f1a2ea0a760730e3bc528105c00b4ee4f23e469` |

官方 main 比 v0.2.14 标签仅多一个 VERSION 同步提交，源码内版本为 0.2.14；没有混入未发布功能。线上源码至部署文档基线只差文档。继承链为 `ebaa8c022 → 7352a8606 → 84e9dc95e → d5a2febf5 → f162fda58`，7352 的另一父提交是官方 v0.2.13 主线。

后端是 Go/Gin，通过 Wire 装配 handler/service/repository，Ent 与专用 SQL 操作 PostgreSQL，Redis 提供缓存、调度和并发状态。网关依次完成鉴权、费用准入、用户/账号槽位、上游转发和原子结算。前端是 Vue 3/TypeScript、Pinia、Vue Router，pnpm 管理依赖。

核心二开为月卡付费日额度重置、福利签到/抽奖/兑换、真实余额消费累计抽奖、CNY 套餐单笔赠抽奖及退款回退、缓存可靠失效和会话隔离。支付、生图、模型广场及自然额度刷新等原本就在官方项目中。

## 逐版及分支对比

以下提交数含合并提交，文件数取相邻正式标签；二开交集按共同祖先到部署版本的定制文件集统计。试合并每次固定从原部署基线 ebaa8c022 出发，不把先前已修复的状态混入冲突判断。

| 官方版本 | 区间提交 | 变更文件 | 二开交集 | 文本冲突文件 | 重点 |
| --- | ---: | ---: | ---: | ---: | --- |
| 0.2.9 | 70 | 117 | 8 | 0 | 流式协议、计费、断连、额度恢复、分组规则 |
| 0.2.10 | 33 | 118 | 11 | 0 | Claude 额度、复合路由、WS、用量归一化 |
| 0.2.11 | 23 | 90 | 11 | 1 | API Key 创建限制、余额在途预占、远程模型目录 |
| 0.2.12 | 29 | 151 | 20 | 1 | TypeSafe、充值赠金/折扣、验证码/密码令牌、两份迁移 |
| 0.2.13 | 10 | 7 | 2 | 1 | 删除 Key 后继续正确结算、TypeSafe 计费探测 |
| 0.2.14 | 8 | 25 | 2 | 1 | EasyPay 回调防伪、首次安装管理员强化、远程模型发现、Vue 更新 |

v0.2.8 标签到 v0.2.14 标签累计 173 个提交。统一共同祖先口径下，上游改变 408 个路径，部署二开改变 261 个路径，两者交集 33 个。唯一文本冲突是 `backend/internal/service/wire.go` 末尾并行追加 Provider；既有整合保留官方 Claude 原生额度重置 Provider 和二开福利服务/outbox Provider。

官方共 42 条远程分支：目标 main 1 条，36 条分支头已被 main 包含；另外 5 条为 `cla-signatures`、`dev`、`preview`、`preview-dev`、`fix/5394-moderation-fail-closed-scope`，是签名数据或旧分叉，未合入。逐条关系与提交距离见 [官方分支对比](upstream-v0.2.14-branch-comparison.csv)。

[逐文件对比](upstream-v0.2.14-file-comparison.csv) 覆盖 669 个路径：375 上游独有、228 部署二开独有、33 双方交集、33 后续复审独有。最终 303 路径不同于官方、446 路径不同于部署。清单快照包含本轮 UseKeyModal 测试修复，排除本轮新报告、计划、CSV 自身和构建产物；这些数值不是人工逐行审查的覆盖率。

## 兼容策略与修复

1. **保持官方请求和结算路径。** TypeSafe 新入口保留官方响应、计费和 failover，在账号等待后补二开权威订阅终检；拒绝时释放账号槽位。Key 删除后仍按官方规则结算，二开福利消费流水继续同事务记账与去重。
2. **沿官方定价能力适配预占。** 继承复合路由、跨组降级重新预占、渠道/峰谷/长上下文定价、显式免费模型以及福利兑换后缓存重核修复，复用 `CalculateTokenCostForRequest`，不恢复旧的独立估价路径。
3. **保留月卡和福利不变量。** 成功日重置精确扣 24 小时、只清日用量；幂等、版本和锁顺序保留。余额消费奖励、套餐抽奖、退款资格回退与财务事实同事务，可靠 outbox 和缓存版本避免旧值回填。
4. **保留退款/支付复审修复。** pending 退款重试只恢复原操作；保存管理员扣减意图；按实付币种金额查询；失败撤销恢复订阅；订阅调整回读在提交前完成；事务提交后失效缓存。支付报价采用既有十进制修复，待支付快照绑定用户和登录会话。
5. **完整采用官方 0.2.14 安全修复。** 首次管理员凭据强化、EasyPay 拒绝非标准通知字段、建单 return_url 清除客户端查询参数均原样保留。服务端随后重新注入 order_id/out_trade_no/resume_token，正常支付恢复与二开身份保护兼容。
6. **本轮新发现与修复。** 完整前端测试最初只有 UseKeyModal 三条旧紧邻文本断言失败；官方新增 `api_key_model_discovery` 改变配置格式。仅修改测试使完整块匹配新格式，并对普通/WS、复合/其他平台、Unix/Windows 的 remote/file 开关做正反保护。生产组件保留官方行为和此前 CMD JSON 转义补丁，没有降级 Vue 或放宽/删除原有断言。

本轮 25 个官方增量路径全部进入结果。22 个与官方 blob 完全相同，另 3 个仅保留 README 中英文二开说明和 UseKeyModal 已有 CMD 转义差异。本轮额外变更是上述测试及审计文档。三名领域代理分析来源、版本与功能，独立审查代理复核安全支付增量及测试适配，没有留下已确认的阻断项。

## 数据库与生成文件

历史 292 份 SQL 与部署基线逐 blob 一致；官方 291 份 SQL 与 main 逐 blob 一致，最终共 294 份 SQL。新增仅为：

- `241_add_payment_order_bonus_amount.sql`
- `241_add_typesafe_platform.sql`

迁移以完整文件名排序、记账和校验，两个 241 前缀可共存。二开 `235_subscription_daily_reset.sql`、`239_welfare_center.sql`、`240_welfare_subscription_rewards.sql` 未改名、未改内容。339 个 Ent 文件、go.mod/go.sum 与复审起点一致；前端 package.json/pnpm-lock.yaml 与官方目标一致。

## 运行验证说明

本次日志统一保存于 `E:/sub2二次开发项目/.cache/upstream-v0.2.14-audit`，所有命令先载入主项目 `tools/project-env.ps1`。共享缓存位于 E 盘；使用 Go 1.27.0、pnpm 9.15.9、golangci-lint 2.13.0。下表由本次实际执行记录填写，不引用旧报告作为当前通过证据。Go 计数包含子测试，各标签/专项之间重叠，不能相加为唯一用例数。

| 检查 | 本次结果 | 证据 |
| --- | --- | --- |
| 继承分支关键后端基线 | 退出 0 | baseline-backend.log |
| 完整后端 unit | 58 个测试包，22,327 项通过，0 失败，18 项既有条件跳过 | backend-unit.jsonl / backend-unit-result.json |
| 首轮 integration 标签（禁用 Docker） | 12,259 项通过、0 失败、15 项条件跳过；repository 数据库整包额外跳过，不能称为完整集成通过 | backend-integration-no-docker.jsonl |
| 迁移后完整 integration（CI=true） | 52 个测试包，13,885 项通过，0 失败，16 项条件跳过；三条历史升级路径全部通过 | post-migration/backend-integration.jsonl / integration-summary.json |
| 安全/支付独立专项 | 208 项通过、0 失败/跳过 | reviewer-security-payment.jsonl / reviewer-payment-reconcile.jsonl |
| 完整后端 lint | golangci-lint 2.13.0，退出 0，0 issues | backend-lint.log |
| 前端完整 Vitest | 358 文件，2,868 项通过，0 失败/跳过 | frontend-vitest.json |
| 配置生成与 CMD 专项 | 2 文件、32 项通过 | frontend-usekey-green.json |
| 前端 lint、typecheck、i18n、生产构建 | 全部退出 0；i18n 3 项通过 | frontend-summary.json 及各日志 |
| 内嵌前端 web 测试 | 98 项通过、0 失败/跳过 | backend-embed-tests.jsonl |
| 内嵌前端后端构建 | Windows/amd64、Linux/amd64 均退出 0；Windows 版本输出 0.2.14 | backend-build-result.json / backend-version.log |
| 部署配置与语法 | 四个 shell 配置检查通过；simple-mode 等价 Windows Python 检查通过；docker-deploy.sh 语法通过 | deploy-checks.log / deploy-compose-windows.log |
| 迁移/Ent/依赖来源边界 | 无历史 SQL、Ent、Go 依赖漂移，官方前端锁文件完整保留 | source-boundary-verification.json |

验证二进制用 integration-uncommitted 标识本轮暂存合并源码，不冒充正式发布镜像或最终提交构建；Linux 为交叉构建，未运行 Linux 容器。

初始前端完整测试的 3 条失败单独保存在 frontend-vitest-red.*，修复后重新运行完整测试。部署 simple-mode 原脚本受 Windows Store python3 别名影响退出 49；最终用已安装 Python 执行同一嵌入代码，仅把 /dev/null 改为 os.devnull，保留全部断言，docker compose 仅执行 config 不创建容器。前端构建仍有既有 Browserslist、Node 弃用、混合导入与大分块提示。
首轮验证时 Docker VHD 仍位于 C 盘，因此禁用 Docker，repository 的真实数据库 TestMain 整体跳过。用户随后完成迁移；本轮核实实际数据目录为 E:/CodexData/DockerBackup/DockerDesktopWSL，目录链无联接，C 盘旧数据盘已不存在，E 盘文件在测试期间产生写入。使用独立 PostgreSQL 18.1/Redis 8.4 容器、CI=true、-count=1 重新运行完整 integration，三条历史数据库升级及真实事务/缓存测试均执行通过。该运行目录虽含 DockerBackup 字样，已经承载运行数据，不能按普通备份清理。未连接生产数据库；详细条件跳过与资源回收见补充记录。

## 官方升级行为与交付边界

- 0.2.11 默认启用 API Key 数量/创建速率限制以及余额在途预占，保留官方配置项和默认行为。
- 0.2.12 密码重置令牌改为单次哈希消费，升级前旧重置链接需要重新申请。
- 0.2.14 新安装需要符合规则的管理员凭据；已有管理员/用户不会被这次首次安装策略阻断。非标准 EasyPay 回调额外字段会按官方策略拒绝。
- 不自动处理历史损坏订单，也不宣称预占估值等于所有真实最终账单；没有调用真实模型、支付网关或生产数据库。
- 本次不创建发布工作流或 0.2.14-r1 镜像，不改动历史 0.2.8-r1 发布工作流。生产部署应使用后续明确的二开版本和独立数据备份；本轮仅验证本地合成历史数据，未部署生产。

官方参考：[v0.2.14 发布说明](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.14)、[本次固定主线](https://github.com/Wei-Shaw/sub2api/commit/3f1a2ea0a760730e3bc528105c00b4ee4f23e469)。
