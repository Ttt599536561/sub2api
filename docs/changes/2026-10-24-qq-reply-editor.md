# QQ 回复文案中文编辑与即时预览

日期：2026-10-24（Asia/Shanghai）。稳定变更主题：`feat: simplify QQ reply editing with localized variables and live preview`。用户明确授权简化后台并仅更新Sub2测试站；当前工作树实现、独立审查、本机测试、生产构建、模拟接口浏览器验收以及测试站应用更新和保全复核均已完成。源码未提交，私有测试部署不等于正式镜像发布。

## 范围与实现

沿用 `E:\sub2二次开发项目\.worktrees\qq-image-generation-20261023`，保留主目录WIP。本轮编辑器把已知变量显示为中文名称，例如 `{{智商查询指令}}`，并在“自动填写内容”中解释含义；点击名称可插入光标位置，必需内容已经存在时不重复插入。保存和预览仍转换回原有 `{{iq_commands}}` 协议，不改既有26个场景和用户文案。30个变量均有中文和英文说明；转换只处理当前场景允许的完整标记，单遍执行，未知标记与错误括号保留给原验证器拒绝。切换界面语言不会改变草稿的规范内容。

桌面并排显示编辑区与QQ消息气泡预览，窄屏上下排列。载入、换场景、改文字或恢复默认后自动预览，输入停顿350ms后合并请求；文字一变就撤下旧效果，请求取消及序号/会话校验阻止迟到结果覆盖。中文输入法组合期间不发半截预览，失败显示可重试状态，缺少必需内容时显示中文诊断。页面保留简单长度提示，隐藏UTF-16和展开预算等实现术语，原长度及固定事实验证仍有效。预览只用虚构数据，不展示真实订单或个人数据；只有明确点击保存才写文案，不自动保存、不发QQ、不调用模型、不存浏览器草稿。

本轮发现既有 `POST /api/v1/admin/qq-image/reply-templates/preview` 会继承通用POST审计，导致未保存草稿正文进入日志，并使自动预览增加大量成功审计行。因此增加最小隐私修复：仅该精确方法和路径省略审计请求体；通过管理员身份、同源和JSON解析后，纯预览跳过审计行。请求拒绝仍可记录安全元数据，不记录草稿；保存与回滚审计保持。假预览仍不访问模板存储、QQ或模型服务，无新迁移和API响应变化。

本轮仅更新Sub2测试应用。既有Cloudflare QQ Worker不重新部署，不改AI网站、Cron或生成服务。QQ凭据、价格、授权、WebSocket接入、图片PostgreSQL BYTEA存储及保留策略保持既有行为；不接R2。正式站、公开镜像、Git提交/合并/推送均不在本轮范围。

## 实际验证

以下计数对应各自范围，重叠测试不能相加，不代表全仓或真实群联调。

| 范围 | 当前实际结果 |
| --- | --- |
| 前端QQ回归 | 8文件153项通过，包含API、管理员页签、文案编辑器、用户卡片/页面、工具和语言目录；编辑器自身33项。 |
| 转换与语言专项 | 工具8项覆盖全部30个实际后端变量、全部默认文案往返、未知/继承对象名、错误括号、Unicode、单遍替换及切语言；语言目录3项，共11项已包含在153项中。 |
| 前端静态与生产构建 | 类型检查、定向ESLint通过；依次执行缓存pnpm的 `check:i18n`、`vue-tsc -b`、`vite build`，生产构建通过。沿用既有Browserslist数据年龄和大chunk提示，没有更新依赖。 |
| 后端隐私回归 | 先RED实际复现草稿泄漏及成功预览留审计行，再GREEN；专项15个pass事件覆盖真实middleware+handler、成功及模板校验反馈、坏JSON/未知字段、身份/同源拒绝、精确路由匹配和保存/回滚审计保留。 |
| 后端相关整包 | `go test ./internal/handler ./internal/server/middleware ./internal/server/routes -count=1 -json`：1,880个pass事件，0失败，仅既有 `TestDingTalkOAuthStart_Disabled` sentinel跳过；同三包 `go vet` 通过。显式配置本机PostgreSQL夹具。 |
| 独立审查 | 前端行为及后端审计两侧独立复核均无阻塞问题。 |
| 真实本机浏览器 | Headless Chrome加载真实组件/CSS/语言，使用全假API和会话并禁止外部请求；13项通过，pageErrors为0。1440px桌面并排、390px窄屏上下，深浅色均无横向溢出；自动预览、合并请求、缺项恢复、重试和显式保存规范变量通过。根代理实际查看桌面和窄屏截图。 |

浏览器使用的是本机模拟接口环境，不能等同测试站登录后完整联调。输入法回归使用浏览器合成composition事件，不宣称真实中文输入法硬件组合已验证。既有群内最终显示、送达和付费生成不在本次代测范围。

本机原始证据位于主项目 `.cache/qq-reply-editor-20261024`，包括 `qq-editor-frontend-tests.txt`、`build-i18n.log`、`build-types.log`、`build-vite.log`、`backend-audit-red.jsonl`、`backend-audit-green.jsonl`、`backend-related-full.jsonl`、`backend-related-vet.txt` 和 `backend-privacy-summary.json`。可视证据为 `output/qq-reply-editor-20261024/browser-qa-results.json` 与 `editor-*.png`。浏览器已关闭；测试过程中未访问真实外部服务。

## 测试站产物、部署与保全

北京时间2026-10-24 **02:09:50**，仅更新隔离实例 `sub2-qq-test` 的应用为 `0.2.17-qq-editor.11a2d245d32b`，没有重新部署Cloudflare Worker、升级正式站或发布公开镜像。源码父提交仍为 `c9a9209ea79163dbbf1fb20b5db2dc67c6b15394`；实际运行的是下表标识的未提交源码快照，不能声称父提交已包含本轮改动。

| 项目 | 固定值 |
| --- | --- |
| 未提交源码SHA256 | `11a2d245d32b9dca6a37f1115e80980057bdb3fbda2ac571c516733cded0fbd1` |
| 私有测试镜像 | `sub2-qq-test:editor-11a2d245d32b` |
| 平台 / 构建类型 | `linux/amd64` / `qq-acceptance-editor` |
| 二进制SHA256 | `1960ecfa04b573e251ada6bdcfad6bcb3872dfe1ff6616e81d26b11558027270` |
| 前端dist SHA256 | `98926220af9dd13ca1c6573b21caf220c5e4b07c541ddad8ac6a74c9cb516414` |
| 部署包SHA256 | `3bf65207d357cac6f9c96b96ac1d418dbfd2abc30ca71a474f89cbce670bc38e` |
| 保留的前一测试镜像 | `sub2-qq-test:templates-1bdc7d36b035` |
| 临停前备份 | `/srv/sub2-qq-test/backups/editor-switch-20261023-180855` |

当前二进制161,484,960字节，部署包46,469,446字节；备份数据库dump628,263字节，`pg_restore --list`验证通过，没有实际恢复。临停前检查进行中的订单，受控停止/恢复专用systemd stack；测试PG/Redis短暂停启但容器ID及数据保留，不宣称其启动时间未变。恢复后应用health为200，实际运行二进制摘要与部署产物一致。

仅替换应用镜像，既有专用同步token私有文件及全部compose环境保持，未重生成token或强写菜单开关。部署前后QQ配置ID5及完整摘要不变，receive_mode仍为websocket，生图仍关闭；已有迁移数305保持，本轮新增迁移0。模板状态和全部历史行摘要保留，实际观察到revision 0、历史0和空overrides；更新工具支持任意用户已有版本，不通过清空文案恢复该状态。

02:09:08建立真实WebSocket连接，02:09:49收到心跳ACK；02:10:24只读验收确认26项目录、当前有效统一菜单预览有效，展开157/2000、最坏1344/2000，状态及历史两表前后SHA256一致。预览不写模板，验收模板写请求/QQ请求/模型请求均0；心跳证明连接状态，不能替代群内送达验证。成功预览免审计和错误正文不入审计由本机真实middleware回归覆盖，不把线上只读检查夸大为完整日志落库验收。

最终 `preservation.json` 确认生产三个容器身份/运行字段、三份配置元数据和Nginx摘要不变；测试PG/Redis容器ID、业务摘要、模板两表摘要、全部compose环境和同步私有文件摘要保留；主目录11份tracked WIP哈希保持。测试站与正式站 `/health` 均在严格HTTPS证书校验下返回200。管理员登录可能更新活动时间，未把用户整行摘要宣称为完全不变。

公网 `/admin/welfare` 严格TLS返回200，当前入口 `index-BYdct-XU.js` 的SHA256为 `91fcb6d3aa35fb8448062f4fc0871a58744197db19488c1445a9d6013c9c9bd6`，福利页 `WelfareSettingsView-Cz_p0blx.js` 为 `3129c6ef962a4132e8ed3d97c9d6e238fef65fd4ab70a705183506152239b592`，均与打包dist字节相符，当前bundle确含中文变量与即时预览界面。线上未做登录后的真实浏览器视觉QA；本机Chrome真组件/假接口13项验证与公网资源核对分别记录，不能混为同一验证。

收尾再次比对当前 `backend` / `frontend` 共4,576份源码文件与部署manifest，哈希完全一致；最后仅补交接文档，没有再改运行时代码。

本轮部署证据保存在主项目 `output/qq-reply-editor-20261024` 下的 `artifact-summary.json`、`deployment.json`、`live-editor-verification.json`、`preservation.json` 和 `public-ui-assets.json`。需要回退时仅切回保留的前一测试应用镜像，保留当前环境、迁移250、模板历史和业务数据，不做down migration或清库；不以旧数据库备份覆盖新增用户操作。

## 完成清单

- [x] 中文变量名称/说明与规范协议往返，既有26场景、30变量兼容。
- [x] 即时预览、输入法/取消/会话保护、纯文本及错误恢复、显式保存。
- [x] 最小预览审计隐私修复及真实middleware回归；保存/回滚审计不变。
- [x] 前后端专项/相关整包、类型/lint/vet、独立审查和前端生产构建。
- [x] 真实本机Chrome模拟接口视觉与交互QA，边界已记录。
- [x] 当前源码打包、测试站备份及仅应用更新、线上只读验收与保全比较。
- [x] 补齐最终产物/部署证据、主目录WIP保全；本轮浏览器已关闭、独有Vite进程已停止，清理拒绝如实记录。

本轮 `browser-harness` 临时目录清理被自动审批以 `blocked by policy` 拒绝，未给出具体规则；目录保留，未重试或绕过。用于QA的浏览器已关闭，Vite已核验唯一进程后停止，截图和摘要保留，记录见 `output/qq-reply-editor-20261024/cleanup.json`。旧阶段被拒目录未触碰，不清理其他任务文件、登录资料、共享依赖缓存或回退备份。源码仍未提交、合并或推送，后续应将本记录、更新日志和交接与对应实现一并提交，不能以工作树父提交冒充本轮源码版本。

本轮远端未再使用的上传tar已在owner、绝对路径、归档及已安装二进制哈希核验后成功单文件清理，记录为 "output/qq-reply-editor-20261024/remote-upload-cleanup.json"。release目录、备份和旧镜像保留；该目标与被拒的本机browser-harness目录不同，没有重试被拒操作。
