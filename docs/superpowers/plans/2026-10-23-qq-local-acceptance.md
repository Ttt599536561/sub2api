# QQ本地验收准备 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox tracking.

**Goal:** 补齐安全群标识发现和可访问的本机真实应用验收入口，不接公网或真实业务发送。
**Architecture:** 管理员开启有界识别窗口，官方验签事件中的一次性码仅产生待确认group_openid候选；确认才写当前配置白名单。独立本地DB/Redis/配置运行当前分支应用，业务保持关闭。
**Tech Stack:** 现有Go/Gin/PostgreSQL/Vue3与PowerShell，复用E盘工具与依赖。

## 范围与接口合同

用户已授权本地验收准备。沿用现有工作树，不提交/推送/合主目录。不改DNS、反代、隧道、生产或公网；不发送QQ/真实邮件/付费图。此前被拒的Linux二进制删除与旧PG关闭保持原位，不重试。

- `POST /api/v1/admin/qq-image/discovery/start {}`：显式开启5分钟一次性识别窗口。需要已保存当前QQ AppID+Secret，不需要生图Key或生图开关开启。返回 `{id,status,app_id,expires_at,command}`，command为 `@机器人 /识别群 <高熵随机码>`，只此响应含明文码。
- `GET /api/v1/admin/qq-image/discovery`：返回最近窗口或null；`{id,status,app_id,expires_at,candidate?}`；candidate仅 `{group_openid,event_id,received_at}`。不返回码/正文/成员信息。
- `POST /api/v1/admin/qq-image/discovery/:id/close {}`：主动关闭窗口。
- `POST /api/v1/admin/qq-image/discovery/:id/confirm {}`：管理员确认候选，将精确group_openid加入当前App白名单，保留生图开关为原值（默认关闭），不自动启用。
- 状态waiting/found/confirmed/closed/expired；开新窗口使前一未终态失效。当前App/QQ凭据/配置版本改变后旧候选不得确认。有效签名、时间和事件去重仍是必需条件；错误码不失效他人的窗口，不匹配事件不回QQ消息、不记录正文。识别在群白名单判断之前处理，其余命令仍按原规则拒绝未授权群。
- 保存只包含QQ凭据的关闭配置必须可行。启用生成仍要求现有完整生成配置。不能放宽任何HTTP/平台验签。
- UI明确填写的是group_openid而非普通群号；提示机器人不会在群里回复识别结果，需回后台刷新。仅管理员可见，确认有独立按钮。状态文案补site_only/closed_unknown中英。

## Task 1：发现域/HTTP/回调（backend agent）

- [x] TDD新增迁移249（先确认编号）、发现窗口与候选/去重/到期/关闭/配置变更/显式确认真实PG测试。
- [x] 分离Bot-only凭据读取，验签后处理识别事件，无生图Key且功能关闭可识别，不走sender/订单/绑定/钱包。
- [x] 接上管理员四接口、角色/Origin校验，输入限长和审计；不返回或持久明文码。
- [x] 执行相关核心/handler/route回归；测试证明错误签名/过期/重放/错码不产生资金或业务副作用。

## Task 2：管理员与文案（frontend agent）

- [x] 按合同加API及识别窗口面板，先保存QQ配置再开启，显示倒计时/只显示一次的命令/候选/确认/关闭。
- [x] 会话变化清理命令，候选不自动入白名单，生图功能不被打开。
- [x] site_only与closed_unknown双语映射和必要组件回归；类型、相关lint/build。

## Task 3：真实本地运行（runtime agent）

- [x] 只读盘点E盘PG/Redis工具和端口。新验收实例使用新DB/Redis数据、独立端口，配置仅loopback；不停止旧PG55439或清理旧编译文件。
- [x] 在主.cache/qq-image-acceptance-20261023保存私有本地配置/稳定JWT与QQ主密钥及测试管理员凭据，不进Git、不输出秘密。使用真实当前应用及数据库迁移、Redis，不复用生产数据。
- [x] 产出仓库内可复用PowerShell启动/status脚本（自检E盘路径、固定工作树/本机绑定；后台Start-Process Hidden）。仅新资源，禁止隐式公网访问和真实发送，默认QQ/SMTP/生图均关闭。
- [x] 实际启动、健康/API登录及管理接口只读验证；日志脱敏，提供私有凭据文件路径。缺关键依赖则准确报告，不伪称已运行。

## Task 4：收口（root + independent reviewer/browser agent）

- [x] 独立安全复核发现窗口的授权/签名/重放/无资金副作用；修复后专项复测。
- [x] 浏览器由子代理检查实际本地应用，保护私有密码不入聊天/截图/日志；必要时只检查公共登录页，管理接口由本地脚本验证，不伪称浏览器已登录。
- [x] 更新CHANGELOG/HANDOFF/实现记录及简短本地验收说明，明确公网HTTPS入口仍未选定、真实联调未完成；保留原WIP，不处理旧拒绝清理目标。
## 实际收口

代码、脚本、依赖和真实应用启动已完成；健康与私有管理员登录成功。管理读取因既有合规确认返回423，保留用户决定、未绕过，浏览器仅检查匿名页面/守卫。公网HTTPS与真实外部联调尚未授权。见[验收记录](../../changes/2026-10-23-qq-local-acceptance.md)。