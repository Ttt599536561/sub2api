# Sub2API 二开 0.2.16-r1 发布与升级记录

发布日期：2026-10-16（Asia/Shanghai）。`0.2.16-r1` 已通过同一源码提交的 CI、安全扫描及 GitHub 双架构构建与运行检查，已发布到 GHCR，并完成匿名完整运行层下载校验。本次未连接或更新生产服务器，服务器升级由用户自行执行。上一版记录见 [0.2.15-r1](CUSTOM_IMAGE_0.2.15-r1.md)。

## 镜像与发布结果

可部署标签如下，Docker 会按服务器架构选择对应镜像：

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.16-r1
```

| 身份或检查 | 当前记录 |
| --- | --- |
| 目标版本 | `0.2.16-r1`（已发布） |
| 最终应用修复源码 | `7c8161b777f8f6314df31e6b62f8c2f3ff9ee9cf` |
| 本地官方合并提交 | `5fc3298e74a9c6423ce0463ec2eb5c9b59f6c584` |
| 合入的官方 main | `698d142468ab15374bc22bd5247759e5b7352006`，包含官方 `v0.2.16` 及其后更新 |
| 上一版已发布镜像源码 | `570e2e00d733ebb20d6692c394c21f05101d0381` / `0.2.15-r1` |
| 已推送发布准备 / 检查触发提交 | `4be3da680a85efa6e7c36a86dc53189f53fedb44` / `c0fc687a54c38a7420c9df2e7ae8bb6c9ad12b6f`；后者为空提交，树内容相同 |
| 镜像实际源码 / OCI revision | `084673c739ddfa3b5429cf5b27f792e82f640fbc` |
| GitHub CI / Security Scan | [CI](https://github.com/Ttt599536561/sub2api/actions/runs/38899063095) / [Security Scan](https://github.com/Ttt599536561/sub2api/actions/runs/38899063075) 均成功，SHA 与镜像一致 |
| GitHub 镜像构建与发布 | [发布运行](https://github.com/Ttt599536561/sub2api/actions/runs/38900492932) 成功；两个原生架构检查及最终发布全部通过 |
| 目标架构 | `linux/amd64`、`linux/arm64` |
| 多架构 index digest | `sha256:b4102fe7f60206d0ea46179b647d825e64b9274d836f5dea89d676c05e4208f9` |
| 各架构运行镜像 digest / OCI config digest | 见下表；已从最终索引递归核对，与架构构建产物一致 |
| 匿名完整拉取、版本与启动检查 | 匿名 Registry API 已下载并核验全部 24 个运行层（89,288,796 字节）；版本与启动由两个 GitHub 原生 runner 执行通过 |
| 生产部署和业务验收 | 未执行；待用户在服务器升级后确认 |

固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:b4102fe7f60206d0ea46179b647d825e64b9274d836f5dea89d676c05e4208f9
```

| 架构 | Runtime manifest digest | OCI config digest |
| --- | --- | --- |
| `linux/amd64` | `sha256:f79a25d7bdcc07c05156f9d182f33399b47a681a88e67e89ced041ab8a8b650d` | `sha256:40fb0cd2d3edcb91684854a10c8d9c54913304f1413bb267ad917082924567e8` |
| `linux/arm64` | `sha256:5c3be11fa96d96a935fe84ed26337cc71116059fb4f378fa911efb5af915ed02` | `sha256:d88ec35b07f5fd5647e992aa395e3be83e32e3dc2a06bec50a858d6b8ef5466e` |

架构构建产物（包含证明材料的 index）与最终运行 manifest 已逐一绑定：

- `amd64`：`sha256:8bafec441331ca3086014d0c3a2e1e21ce5f20cee05a35e504f17e386a56c1f2`。
- `arm64`：`sha256:7448b61c429a018663bdc187caa2c38cfe4b4af54a5385a670e90ecf99a15c7a`。

验证时间：2026-10-16 20:33:53（Asia/Shanghai）。校验包括索引与 manifest 原始 SHA256、descriptor 大小、平台 os/architecture、config 标签，以及每个运行层的大小与 SHA256。本机未执行 Docker 拉取/启动；完整层以匿名 Registry HTTP 流式读取，未落盘为镜像缓存，容器运行检查由 GitHub 原生 runner 完成。证明材料不计入运行层下载结论。

镜像源码为 `084673c739ddfa3b5429cf5b27f792e82f640fbc`；发布准备与恢复仅增加 CI/文档及检查触发提交，`backend`、`frontend`、Dockerfile、忽略规则和入口脚本相对应用修复 `7c8161b77` 无差异。后续发布记录提交不改变镜像源码。

维护入口：[变更记录](../CHANGELOG.md)、[当前交接](../docs/HANDOFF.md)。历史报告中的“未发布”描述当时阶段，不能用旧运行结果替代本次发布验证。

## GitHub Actions 恢复与发布过程

2026-10-16 实际检查：仓库权限 API 显示 `enabled: true`、`allowed_actions: all`，CI / Security Scan / 本版发布工作流均为 active；仓库未归档或禁用。正常快进推送成功，但没有 Actions 运行。为重触发检查增加一个空提交后仍无运行，手动发布请求明确返回 `HTTP 422: Actions has been disabled for this repository`。

随后调用仓库 Actions 启用接口和三个工作流的启用接口均成功，但再次手动触发仍返回同一禁用错误；因此没有降低或绕过发布门禁。GitHub [官方说明](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository)指出，平台独立禁用状态无法通过普通仓库设置恢复，应联系 GitHub Support 复核。具体禁用原因尚未确认，不能据此断言欠费或其他原因。

用户处理后，2026-10-16 恢复运行。推送同树空提交 `084673c739ddfa3b5429cf5b27f792e82f640fbc`，该 SHA 的 CI 与 Security Scan 均成功；随后执行 `publish-custom-v0216.yml`，双架构构建、运行检查、最终索引发布及匿名完整层验证全部通过。此前禁用仅作为历史过程保留，当前版本已经发布。

## 本版主要变化

官方来源相较上一版官方基线 `3a6fd1c9d` 增加 103 个提交、变更 283 个文件，主要包括：

- Gemini 显式上下文缓存，覆盖 AI Studio、Agent Platform（Vertex）和声明支持的上游网关；增加缓存绑定、存储计费、续期及修改恢复。Vertex 支持 API Key 和服务账号模型同步，并修正共享容量 429 与错误策略处理。
- 分组名称和描述多语言、运维请求明细中的账号信息、临时不可调度规则作用范围，以及支付倒计时、监控刷新、分组编辑和模型同步交互修复。
- Gemini 4 Argon、Claude Haiku 5.5、模型目录和定价更新；调整 GPT-6.1 Sol 上下文默认值及订阅长上下文收费处理。补充工具结果图片、原生 Gemini 签名和协议转换兼容。
- Responses/chat 回退约束、Anthropic 协议 base URL、Codex 导入工作区隔离，以及拒绝含 OpenAI 网关 OAuth 的歧义复合路由；同步前端依赖安全修补。

合并与后续复审同时保留月卡日额度提前重置、福利中心、支付退款恢复、管理员福利统计和会话隔离。日重置仍每次扣除 24 小时有效期、保留周/月用量、每订阅每日最多成功 100 次，用户自动重置默认关闭；订阅额度消费不累计账户余额消费抽奖。

本地修复重点如下，详情和逐轮验证见后文链接：

- 固定缓存费用使用权威余额和分组额度，排队后执行订阅终检；预留与结算共用价格快照，按实际上游有效期收费。创建与续期保留绝对截止时间，处理数据库微秒精度，避免无操作 PATCH 误收费。
- 客户端断开后保持缓存创建、清理、结算和并发槽生命周期；本地保存失败时清理上游并仅结算已发生的输入费。结果未知时停止自动重复创建，明确未发送的建连失败仍可安全换号。
- 保留 Gemini/Claude 签名片段、文本顺序、主 candidate 身份、结束状态和分帧用量；补图片输出 token 计费字段，并在收到 `[DONE]` 后及时关闭上游，避免成功响应被尾部错误覆盖。
- 运维缓存、分组 API、倍率/RPM 弹窗和账号模型同步绑定会话及编辑周期；读取失败不允许空表单覆盖配置。新建账号默认映射采用独立副本，迟到默认值不覆盖当前编辑。

上述修复不自动回写历史账单、已保存账号映射或名称未知的上游缓存资源。

## 数据库迁移与升级影响

相对 0.2.15-r1，原有 **296 份 SQL 内容保持不变**，最终共 300 份；新增四份官方迁移：

| 新迁移 | 作用 |
| --- | --- |
| `242_group_i18n.sql` | 为分组增加默认 `{}` 的 `i18n` JSONB 字段，缺少译文时回退原名称/描述 |
| `243_gemini_cached_contents.sql` | 建立显式缓存资源绑定表及索引，保存用户、Key、分组、账号、上游名称和到期时间 |
| `244_gemini_cached_contents_channel_usage.sql` | 保存创建时渠道计费口径，供续期沿用 |
| `245_gemini_cached_contents_mutation_lease.sql` | 增加缓存修改意图、租约和恢复查询索引 |

迁移器按**完整文件名**记录与校验，不按数字前缀去重。既有 `242_drop_platform_check_constraints.sql` 与新增 `242_group_i18n.sql` 都应保留，不要重命名、删除或改写历史 SQL，也不要手工把迁移记录改成已执行。

应用启动会执行尚未应用的迁移。升级前备份数据库、应用持久数据、原 Compose 和 `.env`，保存旧镜像地址及 digest，并确认可恢复。历史升级回归保留整行数据比较、历史文件 checksum 和重复启动检查，新增 `i18n: {}` 由快照显式验证；这些本机结果不代替生产备份与实际升级检查。本版不要求同步升级生产 PostgreSQL 或 Redis 主版本。

### 嵌套网关的未知创建结果

使用 Gemini 显式缓存并串接多层网关时，**各层应部署包含本修复的版本（本次统一使用 0.2.16-r1），且反向代理/中间网关必须保留响应头**：

```text
X-Sub2api-Cached-Content-Outcome: unknown
```

下游仅对声明 `explicit_cache_upstream` 的账号、缓存创建 POST 和非 2xx 响应识别该标记，避免内层已创建资源但响应丢失后，外层把 502 当成普通拒绝再次创建。普通明确 HTTP 拒绝、确定未发送的失败及其它方法仍沿用各自处理。

仅升级一层、混用不包含此修复的旧网关，或移除该响应头，都不能提供完整的跨层保护。发布后需逐层确认版本、账号支持声明和响应头传递；客户端也不应把这种未知结果当作已确认未创建而自动重放 POST。首个结果未知的资源仍可能存在，本版无法仅凭未知响应恢复其资源名或保证清理。

## 发布准备检查

新工作流相对 0.2.15-r1 仅替换版本名称、并发组、镜像版本和源码 VERSION 断言；YAML 结构、全部 Bash 段语法及文档链接检查通过。发布工具的 7 项跨平台 Python 测试在显式 UTF-8 环境通过。初次 Windows 全量调用遇到默认 GBK 编码、默认 bash 指向不可用 WSL 和 POSIX 文件权限差异；3 项 Linux shell/mock/权限用例随后在同一发布 SHA 的 GitHub CI 执行通过，完整发布工具套件共 10 项通过；本机初次失败保留为环境边界记录。

## 验证依据与当前边界

以下按阶段区分历史源码验证与本次发布验证：

| 阶段 | 已执行结果与边界 |
| --- | --- |
| 官方合并及初轮复审 | 前后端完整检查、三条历史数据库升级路径、Wire/Ent 和嵌入前端检查通过，详见[合并报告](../docs/changes/2026-10-15-upstream-merge-review.md) |
| 独立复审及 R11–R13 | 修复价格快照、分组请求归属、弹窗失效/载入失败和实际缓存时长；各阶段另行完整验证，见[独立复审](../docs/changes/2026-10-15-main-agent-audit.md)及[修复记录](../docs/changes/2026-10-15-review-fixes.md) |
| R14–R16 | 前端 423 文件 / 3,600 项通过；后端 unit 各包最终覆盖无未解决失败，integration 14,501 个通过事件 / 16 条件跳过；unit 存在修正旧测试后整包复测的过程，不能描述为一次全量直接通过。见[追加审查](../docs/changes/2026-10-16-review-7ad7757be.md) |
| R17–R20 | 前端 36 文件 / 576 项通过；四个受影响后端包最终覆盖通过，类型、目标 lint 和前后端构建通过；未重跑整个项目与数据库集成。见[再次审查](../docs/changes/2026-10-16-review-c7f623da5.md) |
| 最终应用修复 R21–R25 | 四个受影响后端包本轮 unit 命令退出 0：19,559 个通过事件、5 条件跳过、0 失败；前端未改，相关 2 文件 / 9 项通过；Linux/amd64 编译通过。未重跑完整项目、数据库集成或前端生产构建，未使用 embed 标签或执行发布镜像。见[回归审查](../docs/changes/2026-10-16-review-7fb9f26d7.md) |
| 本次 GitHub 发布与最终镜像 | `084673c73` 的后端 unit 58 个包、integration 52 个包通过；前端类型检查及 30 文件 / 581 项关键测试通过；Go lint、部署脚本、发布工具 10 项测试及安全扫描通过；两个镜像均通过版本/源码、PostgreSQL 18.6 客户端、定价资源、安装状态和前端资源检查 |

历史 Go 数字包含父测试与子测试事件，unit/integration 不相加当作独立用例数，条件跳过不计为通过。本次 CI 的 58/52 为通过的包数，非独立用例总数；非详细模式未输出逐条 skip 数量。安全扫描按仓库现有依赖例外策略通过；govulncheck 未报告代码可达漏洞，仍提示 7 个未被代码调用的模块级漏洞，不能描述成依赖绝无漏洞。没有调用真实模型、支付或 OAuth 服务；本地签名/协议回归不代表真实上游签名验签，也不代表生产业务验收。

## 升级现有 Compose 部署

**镜像发布验证已完成；以下服务器操作尚未执行。**先完成备份和恢复准备，再在现有 Compose 配置中只替换最终生效的应用 `image`；保留原数据库、Redis、配置、环境变量、端口、网络和持久卷。示例应用服务名为 `sub2api`，与现有部署不一致时换成实际服务名：

```yaml
services:
  sub2api:
    image: ghcr.io/ttt599536561/sub2api-custom:0.2.16-r1
```

需要固定内容时，将 `image` 设置为本文已验证的完整 index digest 地址。

在服务器原部署目录操作。下列 Bash 数组仅为参数形状示例；执行前把路径、项目名和全部参数逐项替换为**现有部署原本使用的完整调用**，包含所有 `-f`（保持顺序）、`--env-file`、`-p`、`--profile` 和其它选项，不要用示例新建另一个 Compose 项目。

```bash
compose=(docker compose -f /path/to/existing-compose.yml --env-file /path/to/existing.env -p existing-project)
service=sub2api

"${compose[@]}" config --images
"${compose[@]}" pull "$service"
# 仅在上一步拉取成功、且最终 image 已核对为本版后继续：
"${compose[@]}" up -d --no-deps --no-build --force-recreate --pull never "$service"
"${compose[@]}" ps "$service"
"${compose[@]}" logs --tail=100 "$service"
"${compose[@]}" exec -T "$service" /app/sub2api -version
```

`config --images` 应显示最终采用的本版标签或已验证固定地址。拉取成功后再重建应用；`--pull never` 使用刚取得的镜像，`--no-deps` 保留既有数据库/Redis 服务。版本输出须包含 `0.2.16-r1`，源码提交须与最终发布结果表中的**镜像实际源码 / OCI revision**一致，不能只与应用修复 SHA 比较。

核对容器健康、迁移及启动日志，并通过现有域名/端口验收前端、登录、主要模型请求以及实际使用的订阅、支付、配额、福利和显式缓存功能。串联网关另外核对上一节的逐层版本与响应头。保留原持久卷，不使用 `down -v` 或清卷重建处理升级问题。部署后另记服务器实际镜像、版本、时间和业务验收结果；本记录不会因镜像发布而自动代表这些步骤已经完成。

## 回退边界

保留旧镜像不等于数据库回退准备。新增迁移执行后，不能只切回 0.2.15-r1 就假定数据与 schema 兼容。

异常时先停止应用写入，保存日志和当前数据库状态，再核对旧版本兼容性。只有确认兼容才沿用原 Compose 参数切回旧镜像；不兼容时按预先验证的恢复方案还原升级前数据库及必要应用数据，并使用相匹配的旧镜像。恢复前评估升级后的新增交易、订单和用户数据，避免直接覆盖业务增量；数据库和 Redis 持久卷继续保留。
