# AI上下文卡：sub2api / qq-image

核对日期：2026-10-26。先读 [跨项目路由](AI_START_HERE.md) / [机器索引](PROJECT_INDEX.json)。本卡是定位资料，不是在线状态检查。

## Identity

- `repository_id=sub2api`；canonical root=`E:/sub2二次开发项目`；Go/Gin/Ent + Vue/pnpm；数据库PostgreSQL + Redis。
- `sub2-web`：账户、渠道、API代理、余额、福利与管理后台。`qq-image`：同一应用内的QQ群付费生图模块，随同一Docker镜像发布。
- 每次PowerShell先 `. 'E:\sub2二次开发项目\tools\project-env.ps1'`。工作树也用此入口；检查dirty后再决定隔离，不覆盖用户修改。
- QQ功能已合入main。`.worktrees/qq-image-generation-20261023` 是保留的同仓工作树，新任务默认主目录；明确指定工作树或已有任务时先核对其状态再继续。
- `C:/Users/Administrator/Desktop/Sub2api` 当前为指向E盘主目录的旧联接；侧栏“Sub2api”不代表独立仓库。不要在C盘路径构建或清理联接。

## Environments / publication

| 对象 | 身份 | 接手依据 |
| --- | --- | --- |
| 正式应用 | https://api.tangguo.xin | [HANDOFF](HANDOFF.md) 当前部署表；操作前重新核实 |
| QQ测试应用 | https://qq-test.tangguo.xin；远端 `/srv/sub2-qq-test` | [独立验收栈记录](changes/2026-10-23-qq-remote-acceptance.md) + HANDOFF后续更新 |
| 发布镜像 | `ghcr.io/ttt599536561/sub2api-custom` | [r5发布记录](../deploy/CUSTOM_IMAGE_0.2.17-r5.md)，只是2026-10-25发布证据 |

正式/测试最后记录为同宿主机但独立应用、PG、Redis、配置和数据；“同服务器”不代表共用账户/余额。最新发布镜像与正式/测试当前运行镜像必须分开核实。不要把r5已发布写成生产已升级。

## Task → source

| 任务 | 最短源码入口 |
| --- | --- |
| QQ命令、识别、绑定、授权、订单/计费、图片存取与发送 | `backend/internal/qqimage/`，先看 `qq_commands.go` |
| 生图侧QQ WebSocket、心跳、恢复 | `backend/internal/qqimage/gateway_supervisor.go`、`gateway_client.go`、`gateway.go`；HTTP入口 `backend/internal/handler/qq_image_runtime.go` |
| QQ管理/API/媒体 | `backend/internal/server/routes/qq_image.go`；`backend/internal/handler/qq_image*.go` |
| QQ后台配置、文案编辑 | `frontend/src/views/admin/WelfareSettingsView.vue`；`frontend/src/components/admin/QQImagePanel.vue`、`QQReplyTemplatesPanel.vue`；`frontend/src/api/qqImage.ts` |
| 用户绑定/订单页面 | `frontend/src/views/user/QQBindView.vue`、`QQOrdersView.vue` |
| 邮箱验证/换绑/SMTP | `backend/internal/service/auth_email_binding.go`；检索同目录 `SendQQImageEmailCode` / email；沿用普通账号认证 |
| 构建/发布/迁移 | `.github/workflows/`、`deploy/`、`backend/migrations/`；先读对应release记录 |

## Cross-project contract

- 当前联合模式下，CF `oneapi-observer-qq` 处理裸@、通用菜单和智商截图；Sub2处理 `/识别群 /绑定 /确认绑定 /生图帮助 /生图 /任务 /重发`。`/生图一个黑洞` 也有专门边界规则，改动时核对两侧。
- 测试栈启用 `QQ_IMAGE_EXTERNAL_HELP=true` 时通用帮助交给CF；独立部署默认false。不要盲目向正式环境复制该开关。
- CF对生图命令静默，不把收费请求代理到Go；两套独立QQ连接，一个online不代表另一个online。
- Sub2发布回复模板，CF从固定测试站读取六类菜单/智商文案。编辑内容先查后台发布版本；更改同步契约要审两仓。
- 用户/授权/余额/订单/生图图片属于选中的Sub2实例；当前图片保存在PostgreSQL，未启用检测站R2方案。QQ生成图片与检测站生成HTML不是同一执行器；该模块扣本实例 users.balance，不扣月卡/福利钱包。测试域名也不等于QQ平台sandbox。取消/解绑是网站能力，不杜撰 `/取消` 或 `/解绑` QQ命令。
- CF源码位于 `E:/CodexData/sites/ai-check-20261011/cloudflare-qq`；其部署、管理令牌和DO状态由独立仓交接，不随本镜像更新。

## Read next

读取 [HANDOFF](HANDOFF.md) 接手摘要、[CHANGELOG](../CHANGELOG.md) 最新条目与 [DEV_GUIDE](../DEV_GUIDE.md) 更新记录规范；只按任务追加 [统一菜单契约](changes/2026-10-23-qq-unified-menu.md)、[模板同步](changes/2026-10-24-qq-reply-templates.md)、[r5审查](changes/2026-10-25-qq-release-review.md)。历史规格中的“未合并/未部署”不能覆盖后续完成记录。索引不保存或授权使用实际凭据，不以真实QQ发送或收费生成作为常规测试。
