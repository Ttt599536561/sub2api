# QQ 本地验收入口与群标识识别

日期：2026-10-23（Asia/Shanghai）。本文保留同一独立工作树本地验收准备阶段的记录；未提交、推送、合入主目录或更新正式站。该阶段尚未选择公网入口。后续已获授权新增独立 HTTPS 站点，当前远端入口、账号文件与验证状态见[公网隔离验收记录](2026-10-23-qq-remote-acceptance.md)；本机18089及其私有配置继续保留，没有迁移或重置。

## 本地入口与登录（继续保留）

1. 打开 **http://127.0.0.1:18089/login**。这是本机真实Sub2应用，不是mock页面，也不是公网测试域名。
2. 本机测试管理员信息在 `E:\sub2二次开发项目\.cache\qq-image-acceptance-20261023\private.json`，使用其中 `admin_email` 和 `admin_password`。它不是正式站账号；不要将密码、JWT或加密主密钥发到聊天或提交Git。
3. 初始化验证时，管理API要求“部署与运营合规确认”（HTTP423 / ADMIN_COMPLIANCE_ACK_REQUIRED）。需要用户本人阅读和操作；没有代确认或数据库绕过。确认记录按管理员保存在本机隔离数据库，不修改正式站；用户后续自行操作后，以页面当前状态为准。来源：[既有门禁](../../backend/internal/server/middleware/admin_compliance.go)、[确认服务](../../backend/internal/service/admin_compliance.go)。
4. 自行完成首次确认后，进入“福利中心 → QQ 群生图”，填写并保存QQ AppID/App Secret。先保持生图开关关闭，可以不填生图Key；公网origin、模型及价格留待后续配置。

**localhost不能接收QQ服务器回调。** 本地阶段用于页面查看与配置；后续获授权的新 HTTPS 验收站是独立实例，入口与回调地址见[远端记录](2026-10-23-qq-remote-acceptance.md)。原本地凭据和配置没有迁移到远端，真实 QQ 回调仍需在测试站完成相应配置与平台联调。

## HTTPS入口接好后识别测试群

- 管理员点击开启5分钟识别，获取一次性口令；仅本次页面显示，刷新后无法重新读取。重新开启会使旧窗口失效。
- 在目标群用QQ真正的@功能选择机器人，再发送 `/识别群` 和后台口令。展示中的“@机器人”表示实际提及，不要再当重复普通文字粘贴；不要向其他群转发口令。
- 机器人不会在群里回复。回后台刷新，核对候选group_openid、事件ID和时间；明确点击确认后才加入授权群，不自动开启生图。
- 白名单用官方 `group_openid`，不是普通数字QQ群号。错码不取消别人的窗口；过期、关闭、重开、配置/App/密钥变化使旧窗口无效，重放不能再确认。

识别只用当前QQ凭据验签，无生图Key或功能关闭也可用；不创建绑定、订单、冻结、扣费或QQ回复。授权群不等于授权用户消费，仍需原站邮箱与双端绑定。后续真实生图须另配置真实Key和固定USD价；即使网站测试余额是虚拟的，服务商也可能收费。

## 启动与状态

当前服务已运行，直接打开URL即可。脚本：[qq-acceptance.ps1](../../tools/qq-acceptance.ps1)。在PowerShell7中检查状态：

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
& 'E:\sub2二次开发项目\.worktrees\qq-image-generation-20261023\tools\qq-acceptance.ps1' -Action Status
```

若服务未运行，将Status换成Start。脚本只启动/复用本任务资源；Docker Desktop本地Linux引擎须可用，脚本会再次核实其实际E盘磁盘。Verify仅用于初始空库安全基线；手动配置或新增用户后使用Status，不清库/重置配置来使Verify通过。

| 资源 | 本轮实际状态 |
| --- | --- |
| 应用 | Windows原生Go embed，只监听127.0.0.1:18089；核对时PID60164。 |
| PG | 新PostgreSQL16，127.0.0.1:55449，数据库qq_image_acceptance_20261023。 |
| Redis | 独立容器sub2-qq-acceptance-20261023-redis，127.0.0.1:16389，使用已缓存redis:8.4-alpine。 |
| 私有配置 | 主.cache/qq-image-acceptance-20261023；private.json只有当前用户/SYSTEM权限，JWT及QQ主密钥稳定保留，不进Git。 |
| 初始数据 | 1测试管理员；余额0；上游账户、QQ订单、授权、QQ配置均0；SMTP空，生成默认关闭。此为初始化核对，之后保留用户自己的操作。 |

全部新数据实际在E盘。WindowsPG使用已验证指向主.cache的既有ASCII junction；Docker实际VHDX已核实在E盘。旧PG55439与旧Linux编译文件未停止/清理，没有重试此前被拒目标。新应用、PG、Redis保留运行供用户继续验收。

## 代码、测试及限制

新增[迁移249](../../backend/migrations/249_qq_image_group_discovery.sql)，历史248未改；四个管理员发现接口和验签回调已接线。候选只留必要群ID/事件ID/时间，识别码仅摘要持久化，命令不进入浏览器持久存储。补齐site_only/closed_unknown双语状态，并修复登录失效先改变认证标志时清理敏感数据、旧中文@字面量编译问题。

实际验证：qqimage整包126个测试通过事件，0失败/跳过；相关HTTP/路由/核心批次156个pass事件含包（不相加计数）。独立复核真实PG下12个顶层场景、21个通过事件，无P1/P2阻断项。前端10文件140项、类型与相关ESLint通过；最终构建成功，Vite20.71秒，既有大chunk/Browserslist警告保留。脚本解析0错误、依赖/Start幂等复用检查通过。

真实应用health200、私有测试管理员登录成功，304项迁移含249已应用；只读SQL完成上表隔离基线，日志逐值扫描未发现私有密码/Key/token。Windows启动补齐ZONEINFO到E盘Go时区资源并修正PowerShell日期比较，只改本机脚本。

**管理API是部分验证**：初始化核对的3个管理读取接口均被原有首次合规确认拦截，记录partial_admin_ack_required、admin_config_read=false，没有冒称真实管理写操作通过。浏览器子代理验证真实登录页、QQ绑定登录提示及管理员匿名跳转；1280×720和390×720无横溢出，所查控制台warn/error为空。未读取密码、未浏览器登录管理员或代确认条款；后台识别操作以组件/HTTP/真实PG回归验证。截图仅工具内证据，未落盘。

证据目录：主.cache/qq-image-acceptance-20261023，含verification-summary.json、database-baseline.json、dependency-verification.json、build-evidence.json、discovery-backend-summary.txt、discovery-qqimage-full.jsonl、frontend-build.log。没有真实QQ群回调、邮件或付费生图联调；后续资金验收见[首版记录](2026-10-23-qq-image-generation.md)。