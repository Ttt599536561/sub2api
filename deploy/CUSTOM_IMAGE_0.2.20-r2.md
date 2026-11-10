# Sub2API 二开 0.2.20-r2 Linux 镜像

## 发布准备（2026-11-10）

本版本包含NapCat普通福利QQ账号接入、后台扫码重登和邀请月卡全流程；实现见[功能记录](../docs/changes/2026-11-10-napcat-invite.md)。应用固定源码 `42e6baebd7b33869b227876ee7a4c5bfbce67b37`，包含功能提交 `896adb3bf`、CI覆盖补齐及cleanup测试夹具修复。

**当前尚未完成发布验证，不应以本准备记录认定镜像已存在。** 计划发布 `ghcr.io/ttt599536561/sub2api-custom:0.2.20-r2`，仅Linux amd64/arm64。专用[发布工作流](../.github/workflows/publish-custom-v0220-r2.yml)固定源码，要求该SHA的main/push CI和Security成功；双原生runner验证运行及provenance后才创建不可覆盖最终tag。

## 首次部署变化

1. 更新包含本功能的Sub2镜像；启动时增加迁移253，仓库SQL迁移文件数311→312。先备份数据库及配置。
2. 继续保留原 `QQ_IMAGE_ENCRYPTION_KEY` 与 `QQ_IMAGE_EXTERNAL_HELP`，原官方生图/绑定和独立CF智商服务不变。
3. 在服务器当前Compose项目叠加[独立NapCat服务](docker-compose.napcat.yml)，执行[首次初始化](NAPCAT_SETUP.md)，生成专用 `QQ_INVITE_ENCRYPTION_KEY` 与三种独立连接凭据。保留原DB/Redis卷，不另建生产数据库。
4. 新邀请功能默认关闭。后台“福利中心 → QQ邀请奖励”扫码，确认指定福利QQ账号在线；设置群/文案/套餐、码库及成员基线后启用。
5. 同一邀请活动全群按QQ号去重，等级>10才发码；普通入群审批不自动推断邀请者。未知等级/归属待核验，未知送达只显式原码重试。

仅替换Sub2镜像会获得新后台，但需要NapCat容器和扫码才能实际收发福利消息。用户自行在服务器更新；本轮发布不等于生产已升级或QQ已登录。

## 回退

先关闭邀请奖励和投递/停止NapCat，保留数据库账本、码池FK/trigger保护、私有env及QQ会话。旧应用不能管理邀请奖励，不删除新表、不解除未分配码保护、不改变现有加密key。已发月卡和兑换记录保持。

## 验证状态

本地相关Go整包unit、真实PG RC/RR并发、前端59专项/i18n测试/类型/生产构建、变更lint及Linux双架构编译已完成。GitHub CI、实际镜像运行、最终digest、artifact与匿名拉取结果将在发布收尾后补入本页。浏览器预览启动被自动审批拒绝（blocked by policy），未重试；真实QQ扫码、入群、私聊验收需部署后执行。

## 首轮CI迁移夹具修正

原应用f3817b57e的[CI 42841786296](https://github.com/Ttt599536561/sub2api/actions/runs/42841786296)仅旧v0.2.20升级/新装夹具失败：它们应用了新增253后的312项迁移，却仍按历史311项断言。原NapCat/邀请/服务测试、前端、lint和Security通过，生产迁移实际成功。修正固定原310→311历史目标，保持原断言，新增完整311→312新装/升级/数据保全与码保护测试；没有修改生产SQL或放宽断言。五项真实PG专项在unit/integration及独立复核通过，repository默认lint通过。发布SOURCE已转向包含该修复的42e6baebd，等待新精确CI复验，不重跑错误历史SHA来代替修复。
