# Sub2API 二开 0.2.19-r1 Linux 发布与升级记录

日期：2026-11-01（Asia/Shanghai）。本版处于 GitHub 门禁与镜像发布准备阶段，尚未确认镜像发布成功。用户自行替换服务器镜像，本轮不连接或更改服务器。

## 固定来源与平台

- 应用源码/OCI revision：`b2a9207d8876af773758ed9179022a345791c127`。包含官方 v0.2.19 合并 `15abe960d`、后续修复 `587133d4b`，以及本轮计费/权限加固。
- 专用发布工作流：[publish-custom-v0219-r1.yml](../.github/workflows/publish-custom-v0219-r1.yml)，固定上述完整应用 SHA；要求此 SHA 的最新 main/push CI 和 Security 成功，拒绝覆盖已有最终标签。
- 目标镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.19-r1`，仅 `linux/amd64` 与 `linux/arm64`，各自使用原生 Ubuntu runner。不推送 `v*` Git 标签，不触发通用 Release 或 Apple/macOS 工作流，不更新 latest。
- 发布控制 SHA、CI/发布 run、最终 digest 和匿名核验结果在构建成功后据实补录；此准备记录不作为镜像已可用的证据。

## 本版变化

合入官方 v0.2.19 的受限管理员密钥、图片按张计费、Grok 模型目录与诊断、Gemini 模型/重试等更新，保留 QQ 生图/独立开关/图库、签到 v3、抽奖 v4、月卡重置、Ultrafast、会话隔离及既有二开安全行为。

合并兼容修复包括 auth snapshot v26、Gemini 交付计数调用、管理密钥跨会话与刷新竞态、历史迁移基线保护；另修复支付只读响应凭证暴露、PAY- 通用兑换入口、零图图片 SSE 计费。本轮继续修复 Messages 丢弃图片仍按张收费、余额流水嵌套分组、用户搜索匹配隐藏凭证。详细实现和本地验证见[合并记录](../docs/changes/2026-11-01-upstream-v0219.md)、[此前修复](../docs/changes/2026-11-01-upstream-v0219-review-fixes.md)和[发布前加固](../docs/changes/2026-11-01-v0219-release-hardening.md)。

## 升级与回退

1. 替换前备份数据库、Compose/配置及原镜像。从 `0.2.18-r2` 升级新增 `247_scoped_admin_api_keys.sql` 和 `248_group_image_count_billing.sql`，启动自动执行，迁移历史从 308 增至 310。按完整文件名区分二开相同数字前缀；不改写旧迁移或清空历史数据。
2. 新按张计费默认关闭；auth snapshot 升至 v26，协调替换所有应用节点，避免混合版本策略与缓存结构交错。保留现有福利状态及奖池规则，本次不调整奖励。
3. 保留现有 `QQ_IMAGE_ENCRYPTION_KEY` 原值和 `QQ_IMAGE_EXTERNAL_HELP=true`，不重新生成密钥或覆盖现有 Compose；保持测试站收费接收器关闭，同一机器人不能双开正式/测试收费服务。
4. 外部集成不能自行创建或兑换 `PAY-*` 内部充值凭证；使用非保留前缀。受限用户搜索仅邮箱/用户名，余额流水关联资源改为独立接口读取，完整管理员响应保持兼容。
5. 回退优先向前修复；不得删除已应用迁移或用旧数据库备份覆盖新增业务数据。从更早版本升级还需遵守对应福利/QQ发布记录的条件。

服务器运行版本本轮未在线复核；历史记录中的运行版本不代表本次部署。服务器由用户更新，Cloudflare 与独立智商检测站不随本镜像更新。

## 本机证据

工作树 `E:/sub2二次开发项目/.worktrees/upstream-v0.2.19-20261101`；本轮缓存 `.cache/publish-v0219-r1-20261101`，交付证据 `output/publish-v0.2.19-r1`。主目录原有 23 份 tracked WIP、5 份其他未跟踪文件及 293 份 dist 已备份并合并保全，不属于镜像源码。合并时以保留 stash、原文件备份和三方合并指纹验证保护原修改；仅将旧本机维护摘要标为历史记录，当前发布状态在交接顶部维护。
