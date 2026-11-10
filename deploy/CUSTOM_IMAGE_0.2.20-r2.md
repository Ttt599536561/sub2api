# Sub2API 二开 0.2.20-r2 Linux 镜像

## 发布准备（2026-11-10）

本版本包含NapCat普通福利QQ账号接入、后台扫码重登和邀请月卡全流程；实现见[功能记录](../docs/changes/2026-11-10-napcat-invite.md)。应用固定源码 `1eadf763b01fb19a3875530ce4cd5aa31bf2512b`，包含功能提交 `896adb3bf`、CI覆盖补齐及cleanup测试夹具修复。

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

原应用f3817b57e的[CI 42841786296](https://github.com/Ttt599536561/sub2api/actions/runs/42841786296)仅旧v0.2.20升级/新装夹具失败：它们应用了新增253后的312项迁移，却仍按历史311项断言。原NapCat/邀请/服务测试、前端、lint和Security通过，生产迁移实际成功。修正固定原310→311历史目标，保持原断言，新增完整311→312新装/升级/数据保全与码保护测试；没有修改生产SQL或放宽断言。五项本地真实PG unit专项及独立复核通过（当时本机repository integration跳过，见后文更正），repository默认lint通过。发布SOURCE已转向包含该修复的42e6baebd，等待新精确CI复验，不重跑错误历史SHA来代替修复。

## PostgreSQL 18测试兼容

42e6baebd的[CI 42844605361](https://github.com/Ttt599536561/sub2api/actions/runs/42844605361)仅新保护测试的错误码断言失败：同一RESTRICT删除保护在本地PG16报告23503，云端PG18报告23001，实际都阻止了删除。修正仅接受这两种明确完整性SQLSTATE，核对真实FK名称，并新增码仍unused与池引用仍完整的断言；原SQL不变。PG16的五项unit专项与lint通过（当时本机repository integration跳过），CI新增PG18迁移专项前置。当前固定SOURCE已更新为70eff145，重新等待完整门禁，尚未执行镜像发布。

## 2026-11-11订阅扣减夹具与验证边界

70eff145的[CI 42847377473](https://github.com/Ttt599536561/sub2api/actions/runs/42847377473)通过PG18迁移专项、完整Unit、前端、lint和Security，但Integration发现两个旧订阅扣减测试假设：备注仍要求明文码，以及双读取屏障位于新增串行锁之后。仅修测试，使其验实际码ID/无明文，并把并发屏障移到真实Use之后、用户锁之前；保留外部续期不覆盖、累计扣两天、两码used/UsedBy/UsedAt和两次锁读取断言。新增互斥tag夹具让同一断言可在本地显式loopback PG与原Docker套件运行，未改生产逻辑或全仓TestMain。实际PG16两项RED→GREEN，实际Docker PG18两项逐条RUN/PASS已确认；固定SOURCE转为1eadf763，等待最终全量CI。

早期本机repository integration命令因Docker未就绪在TestMain中以0退出，不能算执行通过；对应记录更正为跳过。之后确认实际Docker VHDX在E盘并验证daemon可用，补跑真实全仓integration，首轮仅3项旧pg_dump模拟测试因Windows PATH缺少sh失败；补充本次进程PATH后专项通过，受影响repository包补PATH后完整复验已通过：1894个PASS事件、1项条件跳过、0失败；其余包首轮无失败。最终完整Linux CI与两架构镜像运行结果将独立记录，不用这些本机边界替代发布门禁。


活动创建后奖励订阅分组固定；更换月卡种类需新建活动。若QQ要求好友验证，需在QQ端处理后私信领取；本版不自动审批好友申请。真实QQ扫码、各入群路径与私信可达性仍需部署后验收。
