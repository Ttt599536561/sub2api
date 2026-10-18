# Sub2API 二开 0.2.17-r1 发布与升级记录

日期：2026-10-18（Asia/Shanghai）。本页用于用户指定源码的 GitHub 构建发布；当前为准备状态，尚未确认镜像发布或生产部署。上一版见 [0.2.16-r1](CUSTOM_IMAGE_0.2.16-r1.md)。

## 发布身份与状态

| 项目 | 记录 |
| --- | --- |
| 目标镜像 | `ghcr.io/ttt599536561/sub2api-custom:0.2.17-r1`（待发布） |
| 镜像源码 / 预期 OCI revision | `1cfafd30e83e52d5980e2be4c7b78a80e797a797`，用户指定提交 |
| 工作流 | [`publish-custom-v0217.yml`](../.github/workflows/publish-custom-v0217.yml)，固定应用源码，独立于工作流控制提交 |
| 发布准备提交主题 | `ci: publish exact 1cfafd30e as custom 0.2.17-r1`；提交后补充已有 SHA |
| 源码 CI | [CI](https://github.com/Ttt599536561/sub2api/actions/runs/39119399862)，等待完成 |
| 源码安全扫描 | [Security Scan](https://github.com/Ttt599536561/sub2api/actions/runs/39119399948)，已成功 |
| GitHub 构建 / 最终 digest | 待运行及验证 |
| 目标架构 | `linux/amd64`、`linux/arm64` |
| 生产部署 | 未执行 |

完整源码已快进推送 main。发布工作流要求精确源码 SHA 的 main push CI 与 Security Scan 成功；两个架构分别原生构建并验证版本、源码标签、PostgreSQL 客户端、定价资源、安装状态及前端资源后才合并最终索引。已有最终标签不可覆盖。工作流控制提交与之后的发布记录不改变应用源码；不通过一般 `v*` tag 工作流发布其他镜像或资产。

本次发布准备不在本机重新构建应用，复用 GitHub 构建缓存。现有本地源码验证见[修复记录](../docs/changes/2026-10-18-f099e0739-fixes.md)和[官方合并复审](../docs/changes/2026-10-18-upstream-v0.2.17-merge-review.md)，不能替代本次 GitHub 运行及最终镜像验证。CI 前端检查为类型、lint 和关键用例，不是全量前端测试；安全扫描按仓库现有依赖例外策略执行。

## 变化与升级边界

本版包含官方 v0.2.17、OpenAI 分组 Ultrafast 及后续计费/回退修复，以及 `f099e0739` 审查中的退款恢复、按渠道与实际 HTTP/WS 模式的模型准入、目录别名和筛选器无障碍名称修复。月卡、福利、支付退款恢复和会话保护继续保留。详细变化见[更新日志](../CHANGELOG.md)。

相对已发布 0.2.16-r1 源码 `084673c739ddfa3b5429cf5b27f792e82f640fbc`，原有 300 份 SQL 内容不变，仅新增 `246_group_force_openai_ultrafast.sql`，共 301 份。该迁移新增 `groups.force_openai_ultrafast BOOLEAN NOT NULL DEFAULT FALSE`，默认关闭，不会自动给全部分组启用 8× 计费。无新增必填环境变量或依赖变更，不要求升级 PostgreSQL / Redis 主版本。

认证快照版本从 24 升为 25，旧版本自动失效并回源重建，无需清空 Redis。多实例应统一升级后再启用 Ultrafast，以免旧实例忽略新策略。

升级前备份数据库、应用持久数据及原 Compose / 环境配置；在现有部署中替换应用 image，保留原数据库、Redis、端口、网络、卷和其他参数。应用启动会执行未应用迁移；需要检查启动/迁移日志、实际版本及登录、模型请求、订阅额度、支付退款和福利等实际业务。本页的无数据库安装页 smoke 不代表生产迁移或业务验收。

退款审计新增 `gatewayFailure` 和 `REFUND_ROLLBACK_SUCCEEDED` 等恢复语义：已知网关失败只做本地补偿，不再重复向网关退款。出现相关 pending 或历史 rollback-failed 记录后，应先核对并完成恢复，再评估旧版兼容性，不能仅切换旧镜像。旧稀疏审计无法证明原退款金额/订阅归属时返回 `REFUND_HELD_METADATA_INVALID`，需要人工对账，不自动修复所有历史退款。

## 验证证据

准备检查：新工作流 YAML、5 段 Bash 语法、完整源码常量、相对上一版仅预期字段变更，以及文档链接检查均通过；独立只读复核未发现阻断问题。应用构建输入保持指定提交不变。

发布完成后在此补充工作流控制提交、实际运行链接、最终索引与双架构 manifest/config digest，以及匿名完整运行层校验结果。必要本机证据保存在 E 盘主项目 `output/publish-v0.2.17-r1`；临时数据在 `.cache/publish-v0.2.17-r1`，不纳入 Git。
