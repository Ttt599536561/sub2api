# QQ 生图配置保存字段提示修复

日期：2026-10-24（Asia/Shanghai）。稳定变更名称：`fix: explain rejected QQ image configuration fields`。源码仍在 `E:\sub2二次开发项目\.worktrees\qq-image-generation-20261023` 的未提交工作区，父提交 `c9a9209ea79163dbbf1fb20b5db2dc67c6b15394` 不包含当前 QQ 功能；没有提交、合并、推送或公开镜像发布。

## 问题与已知证据

用户填写后台生图接口配置后点击保存，只看到“请求未能完成，请刷新后重试；不要据此认定操作已成功”。只读核查确认北京时间 03:04:52 的保存 HTTP 200，配置 ID 从 5 变为 6；随后 03:06:20、03:06:35、03:08:34 的保存均返回 HTTP 400，约 1～2 毫秒，未生成新配置。当时最后已保存配置为 ID 6、生图关闭、API Key 未配置，已有机器人凭据保留。新版上线后用户自行保存成功，最终配置 ID 7 状态见文末。

用户确认失败尝试时已经勾选开启生图，并填写新 API Key。但失败请求正文未留存，不能确定实际是哪个字段非法；不能把失败原因直接归结为漏填 Key、API 地址或网站 origin。`https://qq-test.tangguo.xin` 符合 origin 校验。配置保存路径不调用生图服务或 QQ，因此这组 400 也不代表上游连接失败。旧请求审计的正文省略标记已另行确认均为 true，见 `audit-body-omission.json`；早期 `audit-before.json` 的 false 来自对省略标记错误使用 null 判断，不能据此推断正文或凭据被记录。

已确定的界面问题是：后端多个字段校验统一返回 `QQ_IMAGE_INVALID`，前端又把 HTTP 400 转成通用失败。客户端此前只在启用时校验 URL/金额，并且未显示整数上限，未能提前指出服务端已有规则。测试夹具曾使用关闭状态下的完整 `/images/generations` 地址，模拟保存成功，没有覆盖后端拒绝行为。

## 改动与边界

- 后端保持所有现有校验、规范化、默认值和业务规则；为 `SaveConfig` 的校验失败返回带固定 `Field`/`Issue` 的错误，仍满足 `errors.Is(err, ErrInvalid)`。只有管理员配置保存接口新增 HTTP 400 的 `QQ_IMAGE_CONFIG_INVALID` 和 `{field, issue}` 元数据。
- 字段与问题类型采用固定组合白名单。解析错误仅返回 `request/invalid_request`，不会反射输入、凭据、未知字段名、底层错误原文或请求正文。加密不可用、冲突及内部错误保留原状态码；没有扩展日志或审计正文。
- 前端在对应字段显示中文/英文提示并定位焦点，显示原有数字上限；关闭生图时，已经填写的地址和金额也按已有约束检查。保留已有端口行为，不借此次提示修复新增或放宽服务端规则。
- 缺少凭据仍在后端现有事务与锁内核验；空凭据表示保留原值，显式清除仍关闭新申请并撤销对应旧凭据。前端失败后不重新读取配置、不清空草稿或新输入的 Key；会话变更仍及时清除敏感草稿，忽略迟到响应。
- 不替用户保存、启用或修改线上配置；不填写、复制、回显或测试真实 Key。不改变白名单、模板内容/版本、计费、订单、图片存储、WebSocket 或既有同步规则。

本轮仅更新 Sub2 独立测试应用；Cloudflare QQ Worker、AI 网站/Cron/生成服务和正式 Sub2 站均不在部署范围。无新迁移、部署环境或同步 secret 变更，无 QQ 消息、邮件或付费模型调用。用户后来自行保存 API Key 不属于代理部署变更。图片继续服务器 PostgreSQL BYTEA 存储，不接 R2。

## 实际验证

| 范围 | 已完成结果 |
| --- | --- |
| 后端 TDD | 先复现 URL、关闭状态完整端点、金额、保留天数、提示词和 JSON 解析错误仅返回笼统原因，再验证固定字段元数据与不回显输入。 |
| Go 真实 PostgreSQL | `go test ./internal/qqimage ./internal/handler ./internal/server/routes -count=1 -json`：2,020 个测试 pass 事件、0 fail、1 个既有 `TestDingTalkOAuthStart_Disabled` skip。计数包含子测试，不等同文件数。 |
| 配置与事务回归 | 缺凭据、关闭状态错误 URL、保留 31 天、提示词 2001 等失败前后，配置、审计、凭据撤销、群识别和业务表摘要不变。已保存 BotSecret 密文＋新假 API Key＋开启生图＋正确测试 origin 成功，原密文保留且返回脱敏。 |
| 补充真实 PG 回归 | 全量后新增的最终 `config.save` 审计故障测试单独通过：新配置插入、凭据撤销与识别窗口关闭全部回滚，原凭据仍可用。该单测不重复计入前述全量数字。 |
| 后端静态检查 | 最后三包 `go vet` 退出 0，无诊断；格式与针对改动的 `git diff --check` 通过。 |
| 前端 | 9 文件 196 项通过，`vue-tsc -b` 与定向 ESLint 通过。覆盖字段上限、关闭状态非空校验、受控错误组合、凭据保留/清除、失败草稿保留、页面权限和会话变化。 |
| 独立审查 | 后端与前端交叉独立复核均无阻塞问题；安全元数据、原校验规则、事务/凭据语义和会话隔离已核对。 |
| 本机 Chrome | 真实父组件、全假 API/身份的 10 项检查通过，包含桌面/窄屏字段错误、草稿/新 Key 保留和服务端错误展示。页面错误 0；离线夹具不访问真实 API，根代理实际核看截图。 |

本机 Chrome 验证不等于测试站登录后的浏览器验收，也不等于用户真实配置保存成功。没有发送群消息或生图；无法用这组验证推断真实 Key 的有效性或生图服务兼容性。

必要证据在主项目 `.cache/qq-config-save-20261024` 与 `output/qq-config-save-20261024`，包括后端 RED/GREEN/全量/vet/审计回滚日志、前端测试/类型/lint、`browser-qa.json`、`browser-qa-lifecycle.json` 和三张截图。原始证据不代替本文中的关键结论。

## 构建与部署状态

北京时间 **03:25:31** 已仅更新独立测试应用。正式发布仍为 r4，本轮没有更新正式站或 Cloudflare。

| 项目 | 固定值 |
| --- | --- |
| 已部署版本 / 私有镜像 | `0.2.17-qq-config.503f54c85dd8` / `sub2-qq-test:config-503f54c85dd8` |
| 未提交源码 SHA256 | `503f54c85dd84cb855b3f3108419491192f3f83bdeea042fe7901e09f310553a` |
| 平台 / 构建类型 | `linux/amd64` / `qq-acceptance-config` |
| 二进制 SHA256 / 大小 | `9dbd6576df39df4a9925b6c394eee31bbfb8e3c6c60a63778472320c5c50cf12` / 172,671,136 字节 |
| 前端 dist SHA256 | `3ff7362ba5999edc72b7c0b0aa0a2e7c242189e5fcbbefb3d7af7648c0844744` |
| 部署包 SHA256 / 大小 | `9c5b043288e54c4ca97e5cb1e446a896ae6e985ce60e036cdaa11c381c06e827` / 48,939,558 字节 |
| 保留的前一应用镜像 | `sub2-qq-test:flexible-19c444be294b` |
| 临停前备份 | `/srv/sub2-qq-test/backups/config-switch-20261023-192435`，数据库 dump 631,903 字节 |

03:26:37 验收记录确认配置仍为 ID 6、WebSocket 模式、生图关闭；03:26:11 有真实心跳 ACK。用户在旧阶段后已编辑文案，本次基线是模板 **revision 12、历史 12 条、自定义覆盖 12 项**，不是早期记录中的 revision 0；完整状态/历史摘要及全部用户文案保持。迁移仍为 305，环境与同步 secret 均保留，无模板写入、QQ 或模型调用。

在线除读取状态和假数据预览外，执行过一次受控失败配置探针：`Content-Type: text/plain` 的非法 PUT，不含真实凭据，解码阶段即拒绝，未调用 Store。返回 HTTP 400、`QQ_IMAGE_CONFIG_INVALID`、`{field: request, issue: invalid_request}`，完整配置摘要前后相等，有效配置保存请求数为 0。这是明确不写配置的失败探针，不应称为“全部纯只读 GET”，也不证明用户实际输入已经保存成功。

`preservation.json` 确认生产三容器、生产配置元数据、Nginx 摘要、测试 PG/Redis 容器 ID、业务/配置/模板摘要、compose 环境、同步 secret 文件和主目录 11 份 tracked WIP 均保留；两站严格 HTTPS `/health` 均 200。测试 PG/Redis 随专用 stack 暂停/恢复，不宣称启动时间不变；常规登录活动与审计元数据可能变化。

公网后台页面 200，当前入口 `index-Fmc9TaRS.js` SHA256 `62e182cbfd9b3fcf223041535e3aa7907ba3c15cb1e92a911a03723881b37c3c`，福利页 `WelfareSettingsView-B7AeVGVf.js` SHA256 `ad6fef2c104a5f333e973bb72d22a165cc6bdda3db84894b00108574f0c43a16`，均匹配本次 dist，包含新字段提示。线上登录后的真实浏览器视觉 QA 未执行，不能以本机 10 项或静态资源匹配替代。上述证据保存在本轮 output 的 `artifact-summary.json`、`deployment.json`、`live-config-verification.json`、`preservation.json` 和 `public-ui-assets.json`。

本轮本机 QA 浏览器上下文与端口 18120 服务已关闭，必要夹具和截图保留，未尝试删除夹具目录；旧阶段被拒绝的清理目标未触碰。本轮远端闲置上传包已单独清理，`remote-upload-cleanup.json` 确认 release、备份和回退镜像保留。收尾复核 4,581 份 backend/frontend 源码与构建 manifest 一致。无迁移，回退时只使用前一测试应用镜像并保留当前数据库，不以旧备份覆盖用户后续配置和模板。

## 用户重试结果

用户自行在新版后台重新填写并保存，确认收到“配置已保存，历史订单继续使用原始快照”。北京时间 **03:30:06** 只读核验确认配置 **ID 6→7**、生图已开启、API Key 与 BotSecret 均已配置、origin 匹配测试站、允许群 1 个、售价 USD 0.04；WebSocket connected，03:29:35 有心跳 ACK。证据为 `user-save-confirmation.json`。这是用户操作导致的新配置版本，与部署期间保持 ID 6 不矛盾；核验代理的配置修改、上游调用、QQ 消息均为 0。

当前保存问题已解决，但最初失败的具体字段未能复现，不能推断为 Key 错误或某个地址错误。成功保存也不验证真实 Key 或上游生图能力。继续真实群内生图前，仍需完成 SMTP、真实邮箱验证、余额与绑定：此时 SMTP 未配置，现有账号邮箱未验证、有效绑定 0，用户 1 个且余额合计 0。本轮没有代开邮件、加余额、绑定或调用付费生图。
