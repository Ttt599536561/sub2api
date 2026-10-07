# Docker 迁移后：Sub2API v0.2.14 数据库验证补充

## 结论

Docker 数据盘已确认位于 E 盘。对已合并源码 `1a580919969bb2f088b6e05e41a431e2428f1f93` 补跑完整 integration：52 个测试包、13,885 项通过（含子测试）、0 失败、16 项既有条件跳过，退出码 0。三条历史数据库升级路径及其冻结基线检查全部实际运行通过；本轮没有修改生产代码或测试。

此前 unit 的 22,327 项和前端的 2,868 项通过结果保持独立，不与本轮计数相加。service/payment_welfare_* 和 payment_refund_* 中的 unit 标签回归属于此前 unit，不冒充本轮 integration 覆盖。

## 存储及运行环境

- Docker Desktop 设置 CustomWslDistroDir：`E:/CodexData/DockerBackup/DockerDesktopWSL`。
- 实际数据盘：`E:/CodexData/DockerBackup/DockerDesktopWSL/disk/docker_data.vhdx`，测试期间观察到写入时间更新。
- 数据目录到 E:/CodexData 的父目录链无目录联接，C 盘原 docker_data.vhdx 不存在。
- Docker Engine 29.6.2，desktop-linux，连接 npipe:////./pipe/dockerDesktopLinuxEngine。
- 复用已缓存 PostgreSQL 18.1-alpine3.23、Redis 8.4-alpine、testcontainers/ryuk:0.13.0 镜像。测试创建独立容器，不启动或复用已有用户数据库容器。
- 所有项目命令先加载主项目 tools/project-env.ps1，Go 及临时文件使用主项目 E 盘 .cache；Git Bash 在当前进程 PATH 中以满足已有 shell 调用。
- 执行时间：2026-10-07 16:08:06 至 16:11:32（Asia/Shanghai）。

运行命令：

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
$env:DOCKER_HOST = 'npipe:////./pipe/dockerDesktopLinuxEngine'
$env:CI = 'true'
$env:GOMAXPROCS = '4'
$env:PATH = 'C:\Program Files\Git\bin;' + $env:PATH
Set-Location 'E:\sub2二次开发项目\.worktrees\sub2api-upstream-v0.2.14\backend'
go test -p 2 -tags=integration -count=1 -json ./...
```

CI=true 保证 Docker 不可用时失败，-count=1 禁用成功测试结果缓存。日志无缓存回放标记，也没有 repository 整包跳过。DockerBackup 目录名虽包含 Backup，但其 DockerDesktopWSL 子目录已承载运行数据，不可按普通备份清理。

## 三条历史升级路径

| 历史库 | 实际测试名 | 结果 |
| --- | --- | --- |
| 官方 0.2.8 | TestSubscriptionDailyResetUpgradeReview3_ExistingUpstreamDataAndMigrationHistory | pass，5.46 秒 |
| 二开 0.2.7 | TestSubscriptionDailyResetUpgradeV028_PreservesDeployedCustomStateAndHistory | pass，5.61 秒 |
| 已部署二开 0.2.8-r1 | TestSubscriptionDailyResetUpgradeV0213_PreservesDeployedV028StateAndHistory | pass，5.56 秒 |

测试名中的 V028/V0213 是已有历史命名；实际历史来源由冻结 fixture 指定，目标调用当前全部 294 个迁移。独立审查重新计算并匹配：

| 历史来源 | SQL 数 | SHA-256 |
| --- | ---: | --- |
| a3eb7ef302961cba716dc78b39b93b60c467db0e | 289 | 6075250885f45555d7671005dc80db75c8848e9066a0cc3d291cd36a80c30947 |
| 713d2852e8ea8addce365f595d95823d26bb5aa2 | 289 | 47bd915b3a9b9baf8b145c432617b26082f138dcc34170ccfeabdd4079484648 |
| ebaa8c0222b797930f7469bd68154d637d3b5a1a | 292 | 48aad43a88107ef6f79d07952c8c1e19347f7f498b8f21295bd71816c2afcb64 |

对应 TestMigrationUpgradeBaselines 三个子测试也全部通过。覆盖历史用户、分组、订阅、API Key 和迁移记录保持；二开日重置事件、福利事实/账本/钱包、购买奖励及部分退款、订单审计和缓存 outbox 等存量保持。验证两个同号 241 文件均按完整文件名正确执行，旧订单赠金默认零、TypeSafe 约束和再次启动幂等性。

## 真实事务和缓存覆盖

独立审查从源码提取 58 条相关顶层测试及 3 条 fixture 子测试，61 项均各有一次 run/pass，没有失败或跳过的子测试。包括福利付款与订单/奖励/钱包同事务、退款幂等与并发冲突、软删除用户退款、无效金额回滚、消费抽奖回退与欠额、交叉管理员退款锁序、并发兑换防重复入账、订阅撤销回滚保留重置权益、日重置事件约束回滚扣天、订阅调整回读失败回滚，以及续期参与调用方事务。

## 明确的跳过与限制

16 项跳过保留原条件，未放宽或删除断言：

- 安全审计 9 项：3 项未设置 PROMPT_AUDIT_TEST_REDIS_ADDR，6 项未设置 PROMPT_AUDIT_TEST_POSTGRES_DSN。它们不使用 repository 的临时数据库，不能宣称这 9 项已通过。
- 外部 TLS 抓包、TypeSafe 实测、OpenAI API 对比各 1 项，未配置对应外部条件；插件进程 1 项未提供测试插件包。
- xAI 官方抓包 1 项限定 Linux x86_64，本机为 Windows。
- 钉钉 OAuth 1 项为已有 sentinel；并发缓存 TestConcurrencyCacheSuite/TestGetAccountsLoadBatch 1 项为已有 TODO。

历史升级验证使用冻结迁移和构造的代表性存量数据，未读取生产数据库，也未调用真实支付或模型服务。没有推送、发布或部署。

## 清理及证据

测试完成后，临时 PostgreSQL、Redis 和 Ryuk 容器已自动回收；容器及数据卷清单与运行前一致。原有三个用户容器保持原先退出状态，没有删除用户容器、镜像、数据卷或备份。

证据目录：`E:/sub2二次开发项目/.cache/upstream-v0.2.14-audit/post-migration`。

- environment.json：源码、磁盘路径、Docker 连接、CI 和开始时间。
- backend-integration.jsonl / backend-integration-result.json：完整新日志和真实退出码。
- integration-summary.json / skip-reasons.json：测试计数、升级结果与跳过原因。
- containers-before.txt / containers-after.txt / volumes-before.txt / volumes-after.txt / cleanup-verification.json：资源回收对照。

主代理核对退出码和资源回收，独立代理复核 JSON 无损坏、无缓存、逐项升级和关键数据库测试的 run/pass，以及历史 fixture 指纹。首次记录的 Docker 存储导致的数据库验证缺口已补齐；上述独立外部环境条件仍明确保留。
