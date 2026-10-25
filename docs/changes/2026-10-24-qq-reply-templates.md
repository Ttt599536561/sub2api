# QQ 管理员回复模板设计与实施

日期2026-10-24。用户选择文字模板+变量，并明确取消R2：图片继续保存在服务器PostgreSQL，原保留天数、私有领取、QQ媒体权限与清理不变。不创建R2资源、不引入对象存储代码/密钥/迁移。仅实现后台回复设置并更新现有测试站与独立QQ Worker，正式Sub2网站和AI网站/Cron/生成服务保持不变。未授权提交/推送/公开镜像发布。

## 当前状态与范围

后端、管理员页面及独立QQ Worker模板逻辑已在各自工作区完成，并通过下列本机验证。Sub2测试站已更新为 **`0.2.17-qq-templates.1bdc7d36b035`**；截至北京时间2026-10-24 00:55:38，测试应用健康、WebSocket已连接且收到心跳ACK。新增迁移250已自动应用，模板仍为revision 0、全部默认，没有替用户保存自定义文案。测试站QQ配置ID仍为5，配置摘要保持一致，生图仍关闭。

**测试站只读功能验收和Cloudflare QQ Worker部署已完成。** 北京时间00:57:08测试站验证通过；Worker版本`db4cebad-9682-4717-9a29-7f7e36dbfadc`已读回100%流量，00:58:32受保护状态取得`source=remote`、revision 0、error为空。两侧同步状态不等于群内实际送达已经验收。未代发QQ群消息、截图或付费生图；新版管理员页面只做组件测试，当前没有可用真实浏览器完成视觉、控制台或移动端验收，不能沿用旧版本截图作为本轮证据。

本轮在 `E:\sub2二次开发项目\.worktrees\qq-image-generation-20261023` 继续开发，源码尚未提交、合并或推送；也没有公开发布镜像或升级正式Sub2站。正式发布基线仍是 `76ff7c88a` / `0.2.17-r4`。独立AI检测站仓库只涉及既有QQ Worker，不部署AI网站、检测Cron或生成执行器。

图片继续写入既有订单表的PostgreSQL `BYTEA`，默认保留7天、现有配置范围1～30天；原到期清理、仅本人领取与QQ短时媒体授权不变。测试实例继续使用独立2GiB数据盘，本轮没有新增R2资源、对象存储密钥、上传路径或存储迁移。

## 产品行为

在福利中心QQ生图下新增“回复文案”页签。管理员按场景选择模板，可编辑普通文字/换行/表情与{{variable}}，查看变量与不可删除的系统规则，使用假数据预览，保存并生效、恢复单项/全部默认、查看历史并回滚。草稿仅存在页面内存。存储、权限、价格与扣费逻辑不能被文案修改。模板与QQ配置分表，改文案不产生新QQ凭据配置ID、不重连WebSocket。

覆盖现有26种文本：common.menu；iq.ack、iq.caption、iq.error.capture/station/delivery_unknown；image.help、image.generate_usage；binding.usage/request/manual/confirm_usage/success；order.accepted/status_usage/status/redelivery_usage/redelivery；error.insufficient_balance/limit/expired/disabled/conflict/forbidden/invalid/unavailable。不新增自动群消息、异步成功文案、图片卡片、Markdown渲染、按群覆盖或命令别名。

纯文本单遍渲染，无表达式/循环/HTML执行。只接受固定键及该键允许变量；CRLF规范为LF，拒绝不完整占位符、非法控制符、未知变量和空白覆盖。重要规则占位符须恰好一次，保持本人授权、双端确认、订单价格/预留/结算、群图片可见、私有领取、不重复扣费及未知送达提示。变量不提供邮箱、QQ标识、核对码、Key、原提示词或上游错误。限制为最终2000个UTF-16单元；保存按变量最大预算检查，实际展开再次检查；超长或缺变量回到本场景默认/安全短兜底，不截断金额/ID/URL。变量内的{{...}}不递归展开。

保留U+200C/U+200D连接符以支持家庭、职业等组合表情，其他格式控制字符仍拒绝，仅连接符和空白也不算有效覆盖。动态目录给出各变量预算；CF统一菜单的分组名总预算为680、单组标题160，避免旧公开分组在新校验下误降级。`iq.ack`可选插入`group_title`，默认确认文字不变。生图用法明确引导`/生图帮助`查看模型、规格和价格；绑定用法的假数据预览已与真实“请勿替他人授权”规则对齐。

Go每个命令取一个模板快照，渲染后的主/手动备用回复继续与ProcessEvent业务同事务落库；重放不重新生成/扣款/发消息，未知数据库错误仍回滚而不是被错误模板吞掉。模板读取在业务事务前，读取异常可用默认，不能使已开始的SQL事务中毒。CF每个Job固定一个快照用于确认/截图说明/失败说明，序号与重试内容不变。统一菜单仍CF独占，七个生图指令仍只交给Go。

## 数据与API契约

新增250_qq_reply_templates.sql，仅建独立版本表qq_reply_template_revisions与单行状态表qq_reply_template_state；旧迁移248/249不改。保存、恢复和回滚均追加不可变版本，expected_revision比较+状态行锁+审计；不写qq_image_configs/资金/绑定。

GET /api/v1/admin/qq-image/reply-templates：{schema_version:1,catalog_version:1,revision:number,overrides:Record<string,string>,catalog:[{key,label,category,default_text,allowed_variables:string[],required_variables:string[],variable_limits:Record<string,number>}]}
PUT 同路径：{expected_revision:number,overrides:Record<string,string>}，返回同GET。
POST /preview：{key:string,text:string}，返回{valid:boolean,text:string,length:number,worst_length:number,max_length:2000,errors:string[]}；纯假数据，不发消息/截图/模型请求。
GET /revisions：[{revision,created_at,actor_id,restored_from_revision?}]（最多50）。
GET /revisions/:id：{revision,created_at,actor_id,overrides}。
POST /rollback：{expected_revision:number,revision:number}，返回新版本GET形状。
未知版本冲突HTTP409；保存格式无效HTTP400及受控reason。管理员鉴权与同源检查沿用既有QQ接口。

管理员JSON写入和预览使用独立256KiB上限，支持26项中文文案同时接近各自预算；其余QQ接口原64KiB限制不变。模板版本表禁止更新/删除，保存和回滚均以独立状态行锁原子追加版本与审计，不占用原业务域锁。管理员读取及机器发布读取均为`no-store`；预览只使用程序内虚构值，不读取账号、订单或外部服务。

GET /api/v1/qq-image/reply-templates/published：Bearer独立QQ_REPLY_TEMPLATE_SYNC_TOKEN；无token配置返回503，错token401，正确返回{schema_version:1,catalog_version:1,revision,published_at,templates:{仅六个CF键的有效文本}}。不附带目录/账号/群权限/凭据/订单。读接口不使用管理员cookie，token不复用QQ或JWT密钥。配置仅由部署者提供。

CF新部署变量QQ_REPLY_TEMPLATE_SOURCE为固定HTTPS origin+上述路径，secret QQ_REPLY_TEMPLATE_SYNC_TOKEN独立生成。禁跳转，2秒总期限，64KiB上限，严校验schema/键/变量/长度。DO缓存60秒，最近有效副本最长24小时，损坏/无配置/失败时默认文案回退；不因同步失败阻塞原菜单或截图。管理员页注明菜单/IQ约60秒生效，Go新命令即时。新增受同一只读token保护的GET /internal/reply-templates/status，读取DO缓存/同步状态的revision/source/fetched_at，不发QQ或截图，供接入验收；不公开秘密/模板原文。

## 实际验证

以下数字分别对应实际执行范围，重叠项不相加，也不代表全仓测试或真实业务联调。

| 范围 | 本轮实际结果 |
| --- | --- |
| Go三包整包 | `go test ./internal/qqimage ./internal/handler ./internal/server/routes -count=1 -json`：1,916个pass事件，0失败；1个既有`TestDingTalkOAuthStart_Disabled` sentinel跳过。QQ包272、handler包1,566、routes包78。使用显式本机真实PostgreSQL。 |
| 最后Go专项 | 生图帮助入口、绑定假预览两项后续修正后，模板/路由专项43个pass事件、0失败/跳过；与整包范围重叠，不能合计。 |
| Go静态检查与产物 | 同三包`go vet`通过；`go test ./internal/config -run QQImage -count=1`额外配置示例检查通过。当前源码Linux amd64构建通过，哈希与部署回执绑定见下表。 |
| 前端实施验证 | 7文件135项通过，类型检查、定向ESLint及i18n检查通过；Vite生产构建通过。既有Browserslist数据年龄及大chunk提示保留，未因此安装或更新依赖。 |
| 前端独立复核 | API、QQImagePanel、QQReplyTemplatesPanel三文件115项全部通过；验证模板保存与凭据配置隔离、会话/跨页登录和迟到响应保护、纯文本预览、冲突草稿及显式回滚。与135项范围重叠。 |
| 独立CF仓库 | 153项本地QQ/截图/模板测试、类型检查及部署dry-run通过；包括本地workerd路径。本机测试与下述Worker部署/只读同步验收分别记录。 |

TDD记录覆盖语法与未知变量、UTF-16及组合表情、保存最大展开预算、非递归替换、固定事实、缺值/超长回退、只读同步鉴权、假数据预览、版本竞争/回滚/不可变历史和审计。真实PG还验证模板写入不受业务锁阻塞、不改变QQ配置ID；命令回复在事务内落库，改模板后的旧事件不重发/不重复预留，模板表读取失败不使业务事务中毒，未知业务数据库错误仍回滚。资金回归使用原语义：余额3预留1后可用余额2、冻结1，模板编辑与事件重放均保持2/1；本轮未修改资金算法。

完整原始日志保留在主项目 `.cache/qq-reply-templates-20261024`，关键文件为`backend-full.jsonl`、`backend-targeted-green.jsonl`、`frontend-focused.log`、`frontend-independent-review-tests.json`及构建摘要。前端独立复核时，系统pnpm启动器因已有lockfile校验拒绝运行，随后直接调用已安装的本地Vitest完成115项；没有重装依赖或修改前端。实现阶段的pnpm 10.11.0入口和类型/lint证据见同目录摘要。

## 测试站产物、部署与回退

此次部署是服务器私有测试镜像，不是GitHub/GHCR正式发布。`source_head`仅代表工作树父提交；下面的源码SHA256才标识包含未提交QQ实现的构建快照，不得将父提交误写成已包含本轮功能。

收尾再次比对当前`backend`/`frontend`共4,574份源码文件与部署manifest，哈希仍一致；其后本轮仅补交接文档，没有重新修改运行时代码或重复测试。

| 项目 | 固定值 |
| --- | --- |
| 工作树父提交 | `c9a9209ea79163dbbf1fb20b5db2dc67c6b15394` |
| 未提交源码快照SHA256 | `1bdc7d36b03509934ad9d2c6d0fce98b0d25938b13020e11a6849273dd2603aa` |
| 构建版本 / 平台 | `0.2.17-qq-templates.1bdc7d36b035` / `linux/amd64` |
| 私有测试镜像 | `sub2-qq-test:templates-1bdc7d36b035` |
| 二进制SHA256 | `740adaaffd999af3412093e8cab7b5c11fe1a97009b86c1819b7e6fb54712e9d` |
| 前端dist SHA256 | `a72b381f869ceb493583f7bd2f01f77cd552efef67178954035d985ac8af1e6a` |
| 部署包SHA256 | `826e5d1ede491f4a8eb11bc04d1010034c6ac4821ce0509a0b15603da85b0706` |
| 保留的旧测试镜像 | `sub2-qq-test:menu-523a797dcb9d` |

北京时间00:55:38部署回执记录健康接口200、真实WebSocket connected，连接时间00:54:57.318500317、心跳ACK推进到00:55:38.379730332。配置ID5与配置digest未变，`QQ_IMAGE_EXTERNAL_HELP=true`保留，生图关闭；独立同步token已配置且日志扫描未发现登录秘密或同步token。迁移250新增模板表，初始revision 0，没有覆盖文案。不将连接或心跳当作群消息送达验证。

临停前备份位于`/srv/sub2-qq-test/backups/templates-switch-20261023-165444`，数据库dump为622,264字节；前置备份也保留，`pg_restore --list`通过但未实际恢复。部署证据为主项目`output/qq-reply-templates-20261024/deployment.json`与`artifact-summary.json`。正式Sub2站不在本次更新范围，最终前后保全核对通过，具体范围如下。

北京时间00:57:08的应用验收经固定Host/Origin的loopback21089执行并拒绝重定向：管理员目录26项、schema/catalog均1、revision 0、历史0；独立token机器读取200且仅六个CF键，全部默认与管理目录一致；匿名管理/历史/同步及错误token均401，跨源预览403。虚构预览保留家庭/职业组合表情，CRLF规范化、最终UTF-16长度21/最坏177，非法模板被拒绝。迁移250实际记录1条、状态行1、不可变触发器1、版本和模板审计均0；验收的模板写请求、QQ请求和模型请求均0，配置/余额/订单/绑定/资金摘要不变。非管理员权限另由本机测试覆盖，线上没有为此新建用户。

最终`preservation.json`确认生产三容器身份/运行字段、三份配置元数据和Nginx SHA256不变；测试PG/Redis容器ID不变（部署时曾随隔离stack暂停/恢复，不宣称启动时间未变），测试业务摘要保持，主目录11份tracked WIP逐一保全。测试站及正式站`/health`均通过严格HTTPS证书校验并返回200。用户整行摘要会随管理员登录活动变化，不据此宣称整行完全未变。应用只读证据为`live-templates-verification.json`，与`server-after.json`、`preservation.json`同处上述output目录。

公网`/admin/welfare`在严格TLS下返回200，当前入口`index-CDzu_wXH.js`与福利页`WelfareSettingsView-Dgq5WqOW.js`的字节SHA256分别为`43e68e3ed7d976864a8a2bad9056a5c4f8df1626b755851b2232adfe87040dda`和`9d2bd977612fd0e6e0a291541aacc8dc63a2f9b6d59d253d9d43d7b46faecc9e`，均与打包dist一致，确认线上当前bundle包含回复模板页面。证据`public-ui-assets.json`明确`browser_visual_qa=false`；资源匹配不代表真实浏览器中的视觉与交互已经验收。

QQ Worker最终部署版本为`db4cebad-9682-4717-9a29-7f7e36dbfadc`，管理接口读回100%流量。北京时间00:58:32受保护`/internal/reply-templates/status`读取到远端revision 0，`source=remote`、同步error为空，匿名请求401；这一检查不发送QQ或截图。独立AI仓库`output/qq-reply-templates-20261024/deployment-verification.json`保存回执，不把它误列为Sub2正式发布。首次验收脚本的局部名称遮蔽和Cloudflare对默认Python User-Agent返回1010均已在诊断脚本修正后重验，未修改产品代码，也不把首次失败列为功能通过。

需要回退测试应用时，使用保留的`sub2-qq-test:menu-523a797dcb9d`和相应部署配置；**保留迁移250与模板历史，不做down migration、不清库、不回退用户余额/绑定/订单数据**。旧应用忽略新增模板表。Worker独立回退至其仓库保留的前一部署版本，不能用Sub2镜像替代Worker回退；切回旧应用后新机器端点可能不可用，新Worker按已实现的有效缓存/默认文案机制降级，不能据此删除数据库中的版本记录。

## 保留项与后续交接

本轮尝试清理独立AI仓库的3个Miniflare临时目录被自动审批以`blocked by policy`拒绝，未提供具体规则；目录已原位保留，不重试或绕过。旧阶段被拒绝的清理目标也未触碰。源码、依赖共享缓存、部署备份、回退镜像与必要验证摘要继续保留，不清理用户配置、凭据或运行数据。

本轮远端`incoming`上传tar在核对owner、绝对路径和SHA256后已按单文件清理成功；release二进制、备份和旧测试镜像保留。该成功清理与上述被拒的Miniflare目录是不同目标，未重试被拒操作；回执为`output/qq-reply-templates-20261024/cleanup.json`。

本轮已完成授权范围内的实现、本机验证、隔离测试应用及QQ Worker部署和只读保全验收。剩余边界为新版真实浏览器视觉QA、真实群内最终显示/送达及付费生图联调，未将这些列为已验收；如需群内确认，由用户自行发起新指令。提交、推送、正式镜像发布及生产升级未执行，后续应把本报告与代码及更新日志一并纳入相应提交，不能使用当前父提交冒充本轮源码版本。

## 实施与验证清单

- [x] 用户确认文字模板+变量；取消R2，保留本机服务器存储。
- [x] 后端目录/纯渲染/版本存储/迁移/管理员API和机器读取，Go命令接入；真实PG/TDD覆盖模板、事务、去重与资金不变。
- [x] 前端独立回复页签、变量说明/编辑/预览/默认/历史回滚；会话归属与迟到响应保护，纯文本及仅内存草稿。
- [x] CF六类模板、同步缓存与同Job快照实现；本地测试、类型和dry-run通过。
- [x] 独立审查、Go/前端验证和当前源码Linux产物绑定；真实浏览器视觉QA未执行。
- [x] 保全备份、仅更新Sub2测试应用、配置专用同步token、自动应用迁移250；默认revision 0与QQ配置ID5保留。
- [x] 测试站线上只读功能验收、CF QQ Worker部署及远端revision 0同步状态、最终正式站/测试数据保全核对完成。
- [x] 当前阶段交接/日志/实施报告已更新；必要证据与被拒清理目标保留，旧拒绝目标不重试。
