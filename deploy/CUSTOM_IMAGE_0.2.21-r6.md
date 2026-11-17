# 0.2.21-r6 Linux镜像发布

日期：2026-11-17，Asia/Shanghai。用户已授权将代码推送GitHub并构建、发布`0.2.21-r6`镜像。本记录处于发布准备阶段，镜像尚未发布；不代表服务器已升级。

## 固定来源与门禁

- 最终应用来源：`2516693b3ad777b13d7c93c25e9c41b9b00fd25a`，包括全量门禁发现的三项修正，已单独推送main；发布控制仅固定该源码。

- 前轮应用`a3dda81a022c559017715f2affe76dad53be2edd`、控制`f741ed3a73f42a58de7252fa1754f927c96ec9d7`完整unit、前端、lint、安全及锁/脱敏通过，但repository集成测试失败；尚未触发镜像构建。测试隔离修正后重新固定最终应用与控制。

- lint修正应用：`345db4cb5a95ca4ddc45e2b842e62237ced5e578`已单独推送main；首轮完整unit另发现订阅响应契约样例缺三个新增补额零值字段，已同步样例，完整11项API契约子用例通过。最终应用来源已在上方固定。

- 镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.21-r6`，仅原生`linux/amd64`与`linux/arm64`。
- 首轮应用：`b32ab38eb6670b5c9582920981f9cf48e4cddfc5`已单独推送main；完整CI发现废弃的`applyWelfareSupplement`未移除，现删除无调用实现及专用import，最终来源以后续完整门禁为准。功能提交为`256b3b70f974a298644207a18a7c513c77bd933a`。
- 首轮[CI](https://github.com/Ttt599536561/sub2api/actions/runs/44072726789)的unused错误不被忽略；[安全检查](https://github.com/Ttt599536561/sub2api/actions/runs/44072726812)已通过。本机完整生产lint（0 issues）、兑换专项及独立兼容性复审通过；修正后的应用SHA仍需重新通过全部门禁。
- 发布工作流：`.github/workflows/publish-custom-v0221-r6.yml`；发布控制SHA在该工作流提交后记录。
- 工作流要求应用SHA与控制SHA分别具备main分支最新成功的完整CI和Security Scan；不创建触发通用发行的`v*`标签，不覆盖已有正式镜像标签。
- 2026-11-17匿名预检确认r6标签不存在；正式发布前工作流会再次检查。

## 功能与升级

- 管理员生成订阅码时指定分组，可选一次性加额度或加时长，支持同组有效普通及福利月卡，不再要求邀请活动。
- 基础日/周/月额度优先消费，不足部分才扣补额；周期重置不重复赠送。加时保留已用量和剩余补额；原同组有效权益要求、失败零核销及旧已用历史保留。
- 未使用的旧福利补码兼容通用规则。新增独立补额账本、PG持久预占与异步视频创建时计费身份，防止缓存丢失重复放行或续期后扣错周期。
- 相对r5新增261/262/263迁移，321→324。升级前备份数据库和部署配置，仅替换Sub2应用镜像，保留PG/Redis卷、QQ_INVITE_ENCRYPTION_KEY、QQ_IMAGE_ENCRYPTION_KEY、NapCat网络与会话。
- 新效果和账本投入使用后，不盲目回退不理解新数据的旧应用。既有异步扣费故障没有通用持久重试队列，预占不等于完整收费故障恢复。

功能与本地验证详见[通用订阅补充码记录](../docs/changes/2026-11-17-subscription-supplements.md)。用户提供账号的线上具体数据未核实；本次镜像发布不操作服务器或真实用户余额。

首轮unit仅`TestAPIContracts/GET_/api/v1/subscriptions`失败：预期样例缺少`supplement_granted_usd`、`supplement_remaining_usd`、`supplement_used_usd`三个零值字段。已运行的repository、service、migrations成功；集成阶段被阻断未运行。只同步契约样例，保留全部原字段断言。

完整集成阶段[应用CI](https://github.com/Ttt599536561/sub2api/actions/runs/44074801518)和[控制CI](https://github.com/Ttt599536561/sub2api/actions/runs/44074888211)均在repository的同一3套件/23子用例失败，service集成通过。源头是`newDailyResetFixture`在续期触发261期次账本后，未清补额子表就DELETE订阅并吞错，导致后续用户列表/唯一性测试串扰。修正仅针对该夹具、按其订阅ID清理并断言错误，生产外键/触发器与业务逻辑保持。

本机修复前同进程复现用户串扰；修复后完整repository集成1868 PASS事件、0失败、41既有跳过，109.464秒、exit0，独立复审通过。首次本地整包另有3个备份测试因PATH无sh失败，补上当前进程Git bin后整包通过；没有为环境问题改代码。测试容器前后6个、差集为空，自动清理完成。证据`repository-cleanup-red-green-summary.json`及对应日志。

## 验收范围与证据

发布后核验两架构镜像版本、OCI源码与版本标签、Go1.27.4、运行资源、setup接口及前端静态资源；打包检查覆盖历史QQ/福利兼容和新增261–263补额/预占/视频身份。每架构provenance八个文件；五项必要artifact执行精确文件集、ZIP大小/hash/CRC与来源绑定验证，官方Docker辅助构建记录独立校验元数据。

随后匿名核验GHCR index、config、两架构digest绑定、全部运行层和最终tag稳定性，复用E盘旧层缓存后重新hash核对。发布完成前不填造digest或成功结论。

本机本轮证据目录：`.cache/publish-v0221-r6-20261117`。仅GitHub Linux runner构建；本机不运行Docker构建或新容器，下载、验收及缓存留在E盘。原主目录4个未跟踪文件保留；上一阶段被自动审批拒绝清理的文件保持原状。
