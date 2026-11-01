# Sub2API 二开 0.2.19-r1 Linux 发布与升级记录

日期：2026-11-01（Asia/Shanghai）。**镜像已发布，双架构实际运行和匿名完整运行层校验通过。用户自行替换服务器镜像，本轮未部署或操作服务器。**

## 固定来源与平台

- 应用源码/OCI revision：`49fb2d996bb5d9cc035c6bdb89fb8236b316341c`。包含官方 v0.2.19 合并、全部审查修复及首轮 CI 后续修正。
- 发布控制提交：`11012a6a5294ac3f4c44f5971139a685ba04c001`；[专用工作流](../.github/workflows/publish-custom-v0219-r1.yml)固定上述完整应用 SHA，要求其最新 main/push CI 和 Security 成功，拒绝覆盖最终标签。
- 镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.19-r1`，仅原生 Ubuntu 构建 `linux/amd64` 和 `linux/arm64`。没有 macOS 构建、通用 Release 触发、`v*` Git 标签推送或 latest 标签更新。
- 最终 index digest：`sha256:f93ac626981d7baabd5fe6eb0f66cb63ef2aa56ff02d1324aa91fac7f8b5b6d7`。
- 主目录和 GitHub main 已整合修复及发布控制，最终记录随后提交；原23份tracked WIP、5份其他未跟踪文件及293份dist合并保全，不纳入镜像源码。

## 本版变化

合入官方 v0.2.19 的受限管理员密钥、图片按张计费、Grok 模型目录与诊断、Gemini 模型/重试等更新，保留 QQ 生图/独立开关/图库、签到 v3、抽奖 v4、月卡重置、Ultrafast、会话隔离及既有二开安全行为。

合并修复 auth snapshot v26 冲突、Gemini 交付计数调用、管理密钥跨会话与刷新竞态和历史迁移基线。后续关闭支付只读凭证暴露、PAY-通用兑换、零图SSE误收费、Messages省略图片仍收费、余额流水嵌套分组及搜索匹配隐藏用户凭证。首轮GitHub门禁还推动Vue I18n/Intlify11.4.14、decode-uri-component0.5.1、follow-redirects1.16.2及Node24更新，并修正历史升级快照对新增默认列的识别。

详细依据：[上游合并](../docs/changes/2026-11-01-upstream-v0219.md)、[前次修复](../docs/changes/2026-11-01-upstream-v0219-review-fixes.md)、[发布前加固](../docs/changes/2026-11-01-v0219-release-hardening.md)、[首轮CI修正](../docs/changes/2026-11-01-v0219-ci-followup.md)。首轮应用b2a9207d8的门禁未全部成功，未用于发布；本版固定为修正后的49fb2d996。

## 实际验证

| 项目 | 结果 |
| --- | --- |
| 精确应用CI | [41308542850](https://github.com/Ttt599536561/sub2api/actions/runs/41308542850)，5个job全部成功，含完整unit/integration、完整前端、lint及部署/发布辅助。 |
| 精确应用安全扫描 | [41308542829](https://github.com/Ttt599536561/sub2api/actions/runs/41308542829)，后端与前端均成功；仅保留既有xlsx两项高危豁免，没有新增豁免，不宣称零漏洞。 |
| 发布控制检查 | CI [41309233059](https://github.com/Ttt599536561/sub2api/actions/runs/41309233059) 与 Security [41309233040](https://github.com/Ttt599536561/sub2api/actions/runs/41309233040) 成功。 |
| Linux镜像发布 | [41310221743](https://github.com/Ttt599536561/sub2api/actions/runs/41310221743)，gate、amd64、arm64及publish全部成功。 |
| 两架构运行 | 版本0.2.19-r1、完整应用SHA、Go1.27.3、CGO0与对应GOARCH一致；psql/pg_dump、价格资源、setup状态和嵌入前端通过。 |
| GitHub产物 | 5份必要ZIP重新计算SHA256和大小，与API元数据一致；绑定同run/control/source，成员与解压字节一致。 |
| 匿名registry | index、runtime manifest、config的平台/来源/版本与架构artifact绑定通过；24个运行层共90,604,359字节全部SHA256和大小通过，最后重读tag未变化。 |

匿名核验时间：北京时间 **2026-11-01 21:57:01**。使用HTTP流式校验，没有向本机Docker引擎拉取镜像；运行验证在GitHub原生Linux任务执行。一次瞬时TLS读取中断后增加有界网络重试，摘要/大小不符仍立即失败。独立复核重新计算artifact与manifest摘要，并核对验证器、全部层日志与报告，不重复下载全部远端层；没有验证Buildx attestation内容、签名或服务器部署。

| 平台 | Runtime manifest | OCI config |
| --- | --- | --- |
| `linux/amd64` | `sha256:7dc4e8897569a14cc5d934fe1ec49e4e98795796cda45dc4d1d544ede66956af` | `sha256:b3a108b5e3a51045af5af50e6522ba82ba79fc731a088f55457993c44f2884d0` |
| `linux/arm64` | `sha256:53b608de28aae86b7c8c72ee913dc6c5926ef55e7cad13c4f820994b343fe3fd` | `sha256:ada51371262973fa058630ca35c81635580de3f6b7a37520d2763f10a1eabc93` |

本地功能验证与云端门禁分别记录：三项加固修复全Go60包、24,370叶通过，18条件跳过；最终前端450文件/4,203项、类型/lint/构建通过；历史升级三个测试及三个固定基线独立PG通过。最终GitHub门禁覆盖实际Docker integration，不能把不同范围数字相加。

## 镜像地址

```text
ghcr.io/ttt599536561/sub2api-custom:0.2.19-r1
```

固定内容地址：

```text
ghcr.io/ttt599536561/sub2api-custom@sha256:f93ac626981d7baabd5fe6eb0f66cb63ef2aa56ff02d1324aa91fac7f8b5b6d7
```

## 升级与回退

1. 替换前备份数据库、Compose/配置及原镜像。从0.2.18-r2升级新增247_scoped_admin_api_keys.sql和248_group_image_count_billing.sql，启动自动执行，历史308→310。按完整文件名区分二开相同数字前缀；不改写旧迁移或清空历史数据。
2. 新按张计费默认关闭；auth snapshot升至v26，协调替换所有应用节点，避免混合版本策略与缓存结构交错。保留现有福利状态及奖池规则，本次不调整奖励。
3. 保留现有 `QQ_IMAGE_ENCRYPTION_KEY` 原值和 `QQ_IMAGE_EXTERNAL_HELP=true`，不重新生成密钥或覆盖现有Compose。保持测试站收费接收器关闭，同一机器人不能双开正式/测试收费服务。
4. 外部集成不能自行创建或兑换PAY-*内部充值凭证，应使用非保留前缀。受限用户搜索仅邮箱/用户名；余额流水关联资源从独立接口读取；完整管理员兼容。
5. 回退优先向前修复，不删除已应用迁移或用旧数据库备份覆盖新增业务数据。从更早版本升级还须遵守对应福利/QQ发布记录。

服务器运行版本本轮未在线复核；镜像发布不代表生产已升级。服务器由用户替换，Cloudflare与独立智商检测站不随本镜像更新。

## 本机交接

证据：主项目 `output/publish-v0.2.19-r1`；缓存与WIP备份：`.cache/publish-v0219-r1-20261101`。主目录原23份tracked WIP、5份其他未跟踪文件及293份dist使用原字节备份、保留stash、三方合并和指纹验证保护；原本机维护摘要仅标注为历史，原内容保留。

测试PostgreSQL已正常停止，55463无监听。自动审批仅以 `blocked by policy` 拒绝删除本轮pgdata，未提供具体原因，未重试删除；保留目录及验证记录，复用E盘依赖缓存。没有服务器、QQ/邮件、CF或真实收费调用。
