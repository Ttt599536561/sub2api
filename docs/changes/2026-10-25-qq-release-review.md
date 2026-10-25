# QQ 功能发布前全面审查（2026-10-25）

稳定变更名称：`feat: add reviewed QQ image generation and binding`。本轮按用户要求审查完整 QQ 功能、整合主目录并发布 Linux 镜像；不是只检查最后一次指令解析修复。发布流程仍在进行，固定应用 SHA、GitHub 检查和最终镜像身份将在实际完成后补录。

## 范围与保护

输入为 `codex/qq-image-generation-20261023` 未提交工作区，父提交 `c9a9209ea`。包含双端绑定、邮箱补验、官方 WebSocket/HTTP 接收、单图生成、余额预留/结算/恢复、私有图片与QQ投递、后台配置/群识别/回复文案及本轮全部修复。主目录原11份tracked WIP和非构建untracked源码已备份；原 Codex/CCSwitch 等无关WIP不纳入本次发布。此前已提交但未推送的监控刷新修复随主线保留。

独立AI检测站的QQ菜单Worker单独维护，不复制代码或密钥到Sub2；本轮追加的裸@无回应已恢复且用户确认收到菜单，处理记录见后文。用户随后明确批准仅轮换QQ_ADMIN_TOKEN；新令牌已DPAPI加密保存且旧令牌失效，未修改BotSecret。只读状态确认CF网关长期reconnecting/gateway_alarm_failed，最后心跳在10月24日，授权管理令牌更新后actor重建及既有alarm自行恢复，17:46:44有新READY、17:48:06有新ACK；用户随后明确确认单独@已收到菜单。没有调用start/stop或修改重连源码，长期gateway_alarm_failed的内部异常根因尚不能证明，不把本次恢复描述为已修复所有重连异常。

## 已确认问题与修复

| 编号 | 复现与影响 | 最终修复 |
| --- | --- | --- |
| QQ-R1 | HTTPS终止反代、frontend_url空时，浏览器同源QQ写操作被误拒绝403 | 显式frontend_url优先；否则仅显式trusted_proxies中的直连代理可提供唯一合法X-Forwarded-Proto，不信任Forwarded Host或任意头。16场景先RED再GREEN。 |
| QQ-R2 | 管理页内外tab销毁未保存草稿，模板保存pending仍能切页abort | 首次访问后保留组件/草稿，显式刷新确认丢弃，busy向两层父tab传播；会话/角色切换继续清空敏感内容。13个永久回归。 |
| QQ-R3 | 前100条停用账号已存图永久挡住后续正常账号恢复 | 恢复候选先筛active且未删除owner，再照旧在事务内核验与结算。真实PG第101条恢复回归通过。 |
| QQ-R4 | 私有未发布图expires为NULL，账号长期不可用时无限保留 | 首次保存设置有界暂存上限；发布后仍重新给予完整保留期；历史NULL按配置快照与deadline保守清理。过期不可复写。 |
| QQ-R5 | 前100个queued都属于满额群时，其他群的空闲槽无法使用 | 在原领域锁内以(created_at,id)游标跨批扫描；原授权、资金、期限检查保持。真实PG跨群饥饿回归通过。 |
| QQ-R6 | 清除provider API Key会阻止已存图首次QQ投递及重发 | 投递仅读取历史bot凭据，仍校验bot撤销/当前App与群/grant revision/owner；生成仍拒绝被撤销provider key。真实Webhook重发不调用生成器、不重复扣款。 |

三路独立审查覆盖资金恢复、接线安全与前端交互；修复后做跨代理复核，主代理再核对关键代码和整体测试。没有新增SQL迁移（本功能原迁移248–250保留）、不修改历史SQL，不放宽授权、计费或已知未知结果的重发限制。

## 当前实际验证

- 隔离 native PostgreSQL 16.15，127.0.0.1:55465，数据和工具实际在E盘。QQ域整包400个PASS事件、0失败/跳过；相关handler/middleware/routes真实PG回归与vet通过。
- 完整后端unit最终覆盖59个有测试包、25,208个测试PASS事件，0失败，18个既有条件跳过。首次repository包3个pg_dump夹具因Windows进程PATH缺少sh失败；仅给该次复测进程加入已安装Git sh后，repository整包重新通过，未改应用或夹具源码。
- 完整前端442文件/3957项通过；全量ESLint、类型检查及生产构建通过。新增13项生命周期测试包含在总数中，不重复相加。
- 本轮已配置CI的独立QQ PostgreSQL服务，避免QQ数据库测试因无DSN被跳过；新增13个QQ/认证相关文件到前端关键门禁。全部自动CI及发布runner均为Linux，Apple工作流仍仅手动且本轮不触发。
- Docker构建上下文新增排除本机.cache、.worktrees、output、.codex。候选Git文件扫描未命中本机已知测试站/本地验收秘密；不将私有登录信息、凭据、缓存、构建目录纳入Git。

部署simple模式的本机等价检查仅适配Windows空env文件路径及已安装Compose插件位置，通过；原Linux脚本仍由CI执行。发布辅助10项本机尝试受Windows系统bash/WSL入口及POSIX执行位语义影响而失败，未修改这些源测试，须以精确提交的Linux CI结果为发布门禁。两份本轮残留夹具已核实归属并迁入主项目缓存，未触碰其他E盘临时目录。

上面的本机验证不等于GitHub Linux CI、镜像构建或生产部署。18个条件跳过及首次失败/复测身份保存在本机摘要；没有代发真实QQ/邮件或调用付费模型，也不以健康检查替代真实群送达。

## 整合和发布阶段

当前待完成：固定应用提交、保全并整合main、推送origin/main；精确SHA的CI与Security Scan通过后，以专用工作流发布新标签`0.2.17-r5`（已只读确认标签不存在）。仅linux/amd64、linux/arm64，原生runner构建并运行校验，不运行macOS。

镜像发布后应记录固定digest、两平台manifest/config、应用SHA和工作流控制SHA；升级需备份数据库并应用迁移248–250，功能默认关闭。QQ加密主密钥和实际机器人/上游配置由部署者设置，不能复制测试账号/余额/绑定到生产。启用正式站同一QQ机器人前，应先停止测试站对应接收器，避免两个独立Sub2实例重复处理同一指令。

本轮不部署生产。测试服务、缓存与原WIP在收尾逐项处理；被自动审批拒绝的清理目标保留、不绕过重试。必要证据在主项目 `.cache/qq-release-review-20261025` 与 `output/qq-release-review-20261025`。

## 首次整合与云端门禁

功能提交4efa5ae4e410f603c60dd84f8a15555e16aa1984已快进整合主目录并推送origin/main，原WIP备份/恢复核对完成。该SHA的Linux测试、前端、部署shell与发布辅助通过；安全/Go lint门禁发现的问题已单独修正，见[后续门禁修复](2026-10-25-r5-security-and-lint.md)。r5只会基于后续全部门禁通过的精确应用SHA构建，不使用失败的源快照。
