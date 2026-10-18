# Sub2API 二开 0.2.17-r1 发布与升级记录

日期：2026-10-18（Asia/Shanghai）。用户指定源码已推送 GitHub，完成同 SHA 的 CI、安全扫描、双架构构建与运行检查，并已发布到 GHCR；匿名完整运行层校验通过。生产未部署。上一版见 [0.2.16-r1](CUSTOM_IMAGE_0.2.16-r1.md)。

## 发布身份与状态

| 项目 | 记录 |
| --- | --- |
| 目标镜像 | `ghcr.io/ttt599536561/sub2api-custom:0.2.17-r1`（已发布） |
| 镜像源码 / OCI revision | `1cfafd30e83e52d5980e2be4c7b78a80e797a797`，用户指定提交 |
| 工作流 | [`publish-custom-v0217.yml`](../.github/workflows/publish-custom-v0217.yml)，固定应用源码，独立于工作流控制提交 |
| 工作流控制提交 | `a2ed695af025638dd637233b24cca5807a4f991c`；只增加工作流及记录，不改变镜像源码 |
| 源码 CI | [CI](https://github.com/Ttt599536561/sub2api/actions/runs/39119399862)，已成功 |
| 源码安全扫描 | [Security Scan](https://github.com/Ttt599536561/sub2api/actions/runs/39119399948)，已成功 |
| GitHub 构建发布 | [运行 39120160188](https://github.com/Ttt599536561/sub2api/actions/runs/39120160188)，两个架构及最终发布全部成功 |
| 多架构 index digest | `sha256:4f704571235509de55e266b28023855027f4d965dac3957c33e05e5f21eacd32` |
| 目标架构 | `linux/amd64`、`linux/arm64` |
| 生产部署 | 未执行 |

完整源码已快进推送 main。发布工作流要求精确源码 SHA 的 main push CI 与 Security Scan 成功；两个架构分别原生构建并验证版本、源码标签、PostgreSQL 客户端、定价资源、安装状态及前端资源后才合并最终索引。已有最终标签不可覆盖。工作流控制提交与之后的发布记录不改变应用源码；不通过一般 `v*` tag 工作流发布其他镜像或资产。

本次发布准备不在本机重新构建应用，复用 GitHub 构建缓存。现有本地源码验证见[修复记录](../docs/changes/2026-10-18-f099e0739-fixes.md)和[官方合并复审](../docs/changes/2026-10-18-upstream-v0.2.17-merge-review.md)，不能替代本次 GitHub 运行及最终镜像验证。本次 CI 后端 unit 58 个包、integration 52 个包通过；前端类型/lint 与 30 文件、581 项关键测试通过；Go lint、部署脚本和 10 项发布工具测试通过。前端不是全量套件，Go 包数不是独立用例数，非详细日志未提供逐项跳过数量。安全扫描按仓库既有依赖例外策略通过：govulncheck 未报告代码可达漏洞，仍提示 7 个未被代码调用的模块级漏洞；pnpm audit 例外校验通过，不能描述为依赖绝无漏洞。

## 变化与升级边界

本版包含官方 v0.2.17、OpenAI 分组 Ultrafast 及后续计费/回退修复，以及 `f099e0739` 审查中的退款恢复、按渠道与实际 HTTP/WS 模式的模型准入、目录别名和筛选器无障碍名称修复。月卡、福利、支付退款恢复和会话保护继续保留。详细变化见[更新日志](../CHANGELOG.md)。

相对已发布 0.2.16-r1 源码 `084673c739ddfa3b5429cf5b27f792e82f640fbc`，原有 300 份 SQL 内容不变，仅新增 `246_group_force_openai_ultrafast.sql`，共 301 份。该迁移新增 `groups.force_openai_ultrafast BOOLEAN NOT NULL DEFAULT FALSE`，默认关闭，不会自动给全部分组启用 8× 计费。无新增必填环境变量或依赖变更，不要求升级 PostgreSQL / Redis 主版本。

认证快照版本从 24 升为 25，旧版本自动失效并回源重建，无需清空 Redis。多实例应统一升级后再启用 Ultrafast，以免旧实例忽略新策略。

升级前备份数据库、应用持久数据及原 Compose / 环境配置；在现有部署中替换应用 image，保留原数据库、Redis、端口、网络、卷和其他参数。应用启动会执行未应用迁移；需要检查启动/迁移日志、实际版本及登录、模型请求、订阅额度、支付退款和福利等实际业务。本页的无数据库安装页 smoke 不代表生产迁移或业务验收。

退款审计新增 `gatewayFailure` 和 `REFUND_ROLLBACK_SUCCEEDED` 等恢复语义：已知网关失败只做本地补偿，不再重复向网关退款。出现相关 pending 或历史 rollback-failed 记录后，应先核对并完成恢复，再评估旧版兼容性，不能仅切换旧镜像。旧稀疏审计无法证明原退款金额/订阅归属时返回 `REFUND_HELD_METADATA_INVALID`，需要人工对账，不自动修复所有历史退款。

## 验证证据

准备检查：新工作流 YAML、5 段 Bash 语法、完整源码常量、相对上一版仅预期字段变更，以及文档链接检查均通过；独立只读复核未发现阻断问题。应用构建输入保持指定提交不变。

两个 GitHub 原生架构均验证 `Sub2API 0.2.17-r1` 与完整源码 `1cfafd30e83e52d5980e2be4c7b78a80e797a797`，PostgreSQL 18.6 客户端、定价资源、安装页状态与前端静态资源均通过。安装页检查未连接生产数据库，未调用真实模型或支付服务。

固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:4f704571235509de55e266b28023855027f4d965dac3957c33e05e5f21eacd32
```

| 架构 | Runtime manifest digest | OCI config digest |
| --- | --- | --- |
| `linux/amd64` | `sha256:8f3ca25707bbc309eebb25eb5e30bb820c8fccc9912e65bc23f41179cb36cf83` | `sha256:74e4b1d8ac3e89d86cd181bc1f25d1fdd5e65c688f005a2b349844f4f8600de7` |
| `linux/arm64` | `sha256:8fd7c8bc0876f431ab10da721b83e8a1f19ab1c84c82b520e3df4d8bb888bdff` | `sha256:4902fefe3d9973c29895788fb05e975373ac69c7aa985811dd595571eacd6dea` |

来自发布运行 39120160188 的已通过运行检查的架构产物 index，与最终运行 manifest/config 逐一绑定：

- `amd64`：`sha256:40a55f80fd0a82a5d709da7490b8be7f6cdd20d276cb7266c93c2d7f4727fb76`。
- `arm64`：`sha256:39b5fac323c57b2add9da43bb714cf5eca134459741ce4aa44aa6afe4be6f92e`。

匿名校验时间：2026-10-18 18:47:46（Asia/Shanghai）。最终 tag / index、runtime manifest/config 的原始 SHA256、descriptor 大小、平台及 source/revision/version 标签均通过；全部 24 个去重运行层共 89,408,884 字节已流式下载并逐层验证大小和 SHA256。未将运行层写入本机 Docker 缓存。仅跳过明确标注的 attestation descriptor，未下载或验证证明材料内容，不将其计入完整运行层结论。

本机证据保存于 E 盘主项目 `output/publish-v0.2.17-r1`：三个 Actions 运行及日志、job/artifact 来源、实际 digest 文件、匿名校验脚本与 JSON，以及发布准备摘要。关键身份与结论已在本页受 Git 跟踪保存，不依赖本机文件交接。工作流控制提交与之后的发布记录仅为 CI/文档变化；应用源码始终固定为 `1cfafd30e83e52d5980e2be4c7b78a80e797a797`。

维护入口：[当前交接](../docs/HANDOFF.md)、[更新日志](../CHANGELOG.md)。历史“未发布”“未推送”描述当时阶段，以本次发布记录为准；镜像发布不代表生产已升级。
