# Sub2API 二开 0.2.15-r1 发布与升级记录

归档日期：2026-10-11（Asia/Shanghai）。本记录根据本机保存的发布结果整理，本次没有重新查询 GitHub、运行 Docker 或部署生产。生产服务器尚未连接或更新。

## 已发布内容

官方 v0.2.15 已整合，全面复审确认的 10 类问题已修复；审查后的应用源码已在 GitHub main 发布为双架构 GHCR 镜像。支持 `linux/amd64` 和 `linux/arm64`。

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.15-r1
```

需要固定部署内容时使用完整 index digest 地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:9de84a0c93c19cae3f753aa8a2c798c491dd3267eb8d238ffdf177fd7e30c15f
```

| 身份 | 值 |
| --- | --- |
| 镜像版本 | `0.2.15-r1` |
| 镜像源码 / revision / 已发布 main | `570e2e00d733ebb20d6692c394c21f05101d0381` |
| 审查后的应用源码 | `15d7c43413313bce676e66c7a1254d9591d05322` |
| 本地官方合并提交 | `58d58e185111153e76bfad4e0e3d524143f2d540` |
| 合入的官方 main | `3a6fd1c9db07203ca308aaba69e502bc1f35b307` |
| 最终多架构索引 digest | `sha256:9de84a0c93c19cae3f753aa8a2c798c491dd3267eb8d238ffdf177fd7e30c15f` |

复审之后的发布准备仅改 CI、发布与安全审计工具；`backend`、`frontend`、`Dockerfile` 与复审提交一致。既有 17 份未提交文件保持原样，未包含在发布镜像中。

维护入口：[变更索引](../CHANGELOG.md)、[当前交接](../docs/HANDOFF.md)、[完整整合与复审归档](../docs/changes/2026-10-11-v0.2.15-r1.md)。

## 本版变化与验证依据

官方整合相较前一轮 v0.2.14 基线纳入 146 个提交、278 个增量路径，包含 provider profile/平台清单、CommandCode/Cline、多协议兼容、模型目录、流式/计费及前端异步修复。295 份既有 SQL 与 292 份官方 SQL 未改写，最终 296 份 SQL；新增官方迁移为 `242_drop_platform_check_constraints.sql`，平台约束转由应用层校验。合并保留会话隔离、支付恢复、订阅终检、计费和福利等二开能力。

随后修复的 10 类问题为：CommandCode/Cline Codex 元数据过滤、Cline 默认模型目录、资料/OAuth 身份绑定跨会话响应、支付结果恢复记录与卸载后轮询、管理员确认发送归属、用户配额串表单、模型同步串供应商、退款跨期补偿误续期、用户删除与余额结算锁序及角色复核、佣金转余额等私有操作请求归属。修复涉及 38 个文件，其中生产文件 17 个；新增 193 项前端回归与真实 PG 等后端回归。

历史本机复审记录：后端 unit 22,865 通过/18 条件跳过、integration 14,130 通过/16 条件跳过、前端 406 文件/3,412 通过，均 0 失败；lint、typecheck、生产构建、内嵌前端 105 项及部署检查通过。这些运行包含既有未提交工作，不能视为干净镜像逐项运行了全部本机数字。Go 通过数包含父测试与子测试，unit/integration 可能重叠；真实外部 OAuth、模型和支付网关使用测试桩。整合阶段及详细边界见完整归档。

发布证据使用相同源码 SHA `570e2e00d733ebb20d6692c394c21f05101d0381` 的 GitHub 运行。保存报告记录以下运行均 completed/success；本次文档整理未重新请求其状态：

- [main CI，run 38066293997](https://github.com/Ttt599536561/sub2api/actions/runs/38066293997)
- [main Security Scan，run 38066294012](https://github.com/Ttt599536561/sub2api/actions/runs/38066294012)
- [双架构构建与发布，run 38067137741](https://github.com/Ttt599536561/sub2api/actions/runs/38067137741)

两种架构均在原生 GitHub runner 上通过版本、源码、运行资源、启动、安装状态与内嵌前端检查，最终 manifest/config 与 GitHub 验证产物逐一匹配。历史核对时间为北京时间 2026-10-11 00:29；空认证完整拉取核对约为 00:27，两种架构 pull exit 均为 0，revision/version 一致。因此，该固定内容镜像在核对时可由服务器匿名拉取。

| 平台 | CI 验证架构索引 digest | 最终运行镜像 digest |
| --- | --- | --- |
| linux/amd64 | `sha256:5a97cd92a948caa56dac710e2db0eb21d540a77ee7d02873decfaf3345f186dc` | `sha256:e1d9a9387247ccdec91024620a13884994f2fe3ad28ccb4a929e921bd3b8e82f` |
| linux/arm64 | `sha256:23db462224a69c238bd6ee4b8cc79057883b0ddfe31657bf65d272c674831717` | `sha256:d67ef8484e752db1004f33fa6864fbb8e5c72f5073d68c706a3a6c4522c6ad10` |

该架构表是审计信息；日常部署直接使用上面的多架构标签或固定 index 地址，让 Docker 选择对应运行架构。安全扫描成功是已保存的流水线结论。

## 升级现有部署

先备份数据库、应用持久数据目录、原 Compose 文件与 `.env`，保存旧镜像地址和 digest，并确认备份能恢复。升级会在启动时执行待应用的数据库迁移，须按实际生产数据和维护窗口安排。

在现有配置中只替换最终生效的应用 image。保持当前数据库、Redis、配置、环境变量与挂载，并保留原部署的 Compose 文件、环境文件、项目名及其它参数。示例服务名为 `sub2api`，实际服务名不同则替换：

```yaml
services:
  sub2api:
    image: ghcr.io/ttt599536561/sub2api-custom:0.2.15-r1
```

若需锁定内容，将 `image` 改为完整固定地址：

```yaml
image: ghcr.io/ttt599536561/sub2api-custom@sha256:9de84a0c93c19cae3f753aa8a2c798c491dd3267eb8d238ffdf177fd7e30c15f
```

在服务器原部署目录执行以下步骤。每行 `docker compose` 均须沿用原来的完整调用参数，包括已有的 `-f`、`--env-file`、`-p` 及 profile 选择；不要因示例省略参数而启动另一个 Compose 项目。

```sh
docker compose config --images
docker compose pull sub2api
docker compose up -d --no-deps --no-build --force-recreate --pull never sub2api
docker compose ps sub2api
docker compose logs --tail=100 sub2api
docker compose exec -T sub2api /app/sub2api -version
```

`config --images` 应显示本版标签或固定地址。明确拉取成功后再重建应用容器；`--pull never` 使重建使用刚才取得的镜像，`--no-deps` 保留既有数据库和 Redis 服务。版本输出应包含 `0.2.15-r1` 与源码提交 `570e2e00d733ebb20d6692c394c21f05101d0381`。

检查容器状态、迁移和启动日志，并按现有域名/端口确认前端、登录、主要模型请求与订阅/支付/配额等实际使用功能。遇到异常先保留日志与当前数据库状态，再按下节决定恢复方式。不要执行删除持久卷的命令。

## 回退边界

保留旧镜像只是回退准备之一。本版新增官方迁移，数据库已迁移后不能仅通过切回旧镜像假定安全，也不能把旧镜像当成数据库备份。

若升级失败，先停止应用写入并保存故障现场，核对旧版本与当前 schema/数据是否兼容。只有确认兼容后才考虑沿用原 Compose 参数切回旧镜像；不兼容时，应按预先验证的方案恢复升级前数据库和必要的应用数据，并配套运行其对应旧版本。恢复前评估升级后新增交易、订单和用户数据，避免未经处理直接覆盖。保留数据库/Redis 卷，不通过清卷重建处理业务数据问题。

历史三条数据库升级路径通过真实 PostgreSQL/Redis 验证，但不替代生产备份及生产业务验收。实际线上升级与验收仍待部署时记录，本归档不宣称已完成。

## 来源与证据留存

详细事实与修复清单保存在[完整归档](../docs/changes/2026-10-11-v0.2.15-r1.md)，本文件已包含部署所需的版本、SHA、digest、验证边界及恢复原则。

本机历史发布报告 `output/publish-v0.2.15-r1/PUBLISHED_IMAGE.md`、`publication-verification.json`、`anonymous-full-pull-verification.json` 保持原样；原始 GitHub 运行、digest 产物和 OCI JSON 位于 `.cache/publish-v0.2.15-r1`。整合/复审报告分别位于 `output/upstream-v0.2.15-merge` 与 `output/postmerge-review-v0.2.15`。这些路径以 `E:\sub2二次开发项目` 为基准，仅作为历史证据附注，不是阅读本记录的前置条件。

匿名拉取报告的历史 `config_digest` 字段值实际对应最终运行镜像 digest；完整归档另列发布报告中的 OCI `configDigest`，避免字段混用。本轮文档整理未修改既有报告。
