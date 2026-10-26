# QQ图片生成成功但未送达：修复部署与送达验收

日期：2026-10-26（Asia/Shanghai）。用户报告任务 `b6074b3c…b5bb1a5` 已收到任务号，测试站后台可看图片，QQ群未收到图片。范围为Sub2 `qq-image`，不属于Cloudflare菜单/智商网关。

## 已取得的线上证据

经既有SSH严格主机校验进入已确认的测试栈，只读核对 `/srv/sub2-qq-test` owner、Compose project和单个订单；SQL使用READ ONLY事务，未读取图像内容、群/成员标识、原始提示词或媒体令牌。

| 北京时间 | 事件 |
| --- | --- |
| 2026-10-26 00:11:20.600 | 订单创建、冻结USD 0.04 |
| 00:11:53.446 | 约32.85秒后生成成功；图片落存、结算一次USD 0.04，建立投递记录 |
| 00:11:54.921 | 测试应用收到媒体GET，HTTP200、26ms；媒体授权fetches=1 |
| 00:12:03.390 | 投递结束为 `site_only`，原因仅存 `qq_http_400` |

2026-10-26 14:23只读状态：`succeeded / captured / site_only / available`；PNG为2,068,854字节、1086×1448，保存期限到10月31日00:11:53。只存在一次hold和一次capture，没有本次排查导致的扣费或重新生成。测试应用实际仍为 `sub2-qq-test:compact-05ab2f6c8396`，没有部署r5或此次修复。

该订单不是生成失败、5分钟被动回复超时或本实现5MiB限制导致的拒绝。媒体HTTP200不证明QQ的 `/files` 或最终 `/messages` 成功。旧代码对非2xx只存HTTP状态、不存失败阶段和业务code，因此**历史400无法还原到唯一根因**。QQ投递终态不会自动重试，持续等待也不会自动发出该图。

## 确认的协议缺陷及修复

[腾讯官方发送消息文档](https://github.com/tencent-connect/bot-docs/blob/main/docs/develop/api-v2/server-inter/message/send-receive/send.md) 将群聊 `content` 列为必填，并说明富媒体消息也需提供值。旧 `QQClient.SendDelivery` 的 `msg_type=7` 请求没有该字段；现补固定 `content: " "`，不添加用户可见的自定义说明、不覆盖已发布文字模板。这个明确缺项与400相符，但不据此声称已经证明该笔历史错误的唯一原因。

同时，非2xx诊断加入固定阶段 `token/media_upload/message_send`、HTTP状态，以及64KiB有界响应中合法整数 `code`。只持久化固定枚举及数字，不保存响应message/body、file_info、媒体URL、群成员信息或凭据。HTTP200业务码处理、既有序号1/2、5分钟窗口、15秒HTTP超时与35秒投递期限保持；最终发送的网络异常、408、5xx仍为unknown，不能自动重发。资金与订单生成逻辑、数据库结构均未改。

## 首轮最初的本地验证（历史阶段）

- 独立工作树 `E:/sub2二次开发项目/.worktrees/qq-image-delivery-20261026`，分支 `codex/qq-image-delivery-20261026`，基线 `cda70a59b584932d3de120423112a9b4feb707e5`。先验证旧QQClient基线，再以新测试复现缺字段和诊断丢失，最后通过。
- QQClient专项35个PASS事件（31个叶场景）、0失败/跳过；包含阶段区分、敏感字段不泄漏、HTML/截断/超大响应、非法code、不确定结果不重发，以及4个慢错误体超时场景。
- 最终 `go test -tags=unit ./internal/qqimage -count=1 -json`：345个PASS事件、0失败、77个真实PG条件跳过（未配置 `QQ_IMAGE_TEST_DATABASE_URL`）。不将其声明为本轮真实数据库事务回归。
- `go vet`、差异检查和独立复核通过。测试仅调用本地httptest，不调用真实QQ或模型。原始日志与脱敏只读订单证据在主仓 `.cache/qq-delivery-b6074b3c-20261026`。
- 复核后两份源码已同步主目录，原17个工作区修改在同步前后指纹一致；只修改 `qq_client.go` 与 `qq_client_test.go`，另更新本轮文档。未提交/推送。

## 第一轮部署与新增实证（2026-10-26）

用户14:48:34主动重发后，旧compact应用新增投递，14:48:44仍为qq_http_400；hold/capture仍各一次。实际初次确认和重发确认都含测试站URL，不能仅凭模板链接推断文字被QQ拒绝。

第一轮增加仅失败/unknown的文字确认诊断（固定命令、序号、状态、安全原因、含URL布尔值、长度，不记录内容或身份）；保持原绑定备用回复行为，不为重发添加seq2备用以免与图片冲突。最终QQ包362 PASS事件、0失败、77真实PG条件跳过，vet/独立审查通过。前端在同HEAD干净工作树复用依赖重新构建，类型/i18n/build通过。

北京时间15:22已部署测试私有镜像 **sub2-qq-test:delivery-0a7873079d51**，应用版本 **0.2.17-qq-delivery.0a7873079d51**。基于cda70a59b（已审查r5源码）与本轮四文件改动、新构建前端，在已核验r4 runtime上覆盖静态Linux amd64程序；不能把它称为完整r5公开镜像。源码快照0a7873079d51d8feb5ed31244e41a25eb8013418ad3034f12ca2a0bf8d64a871；二进制186,441,888字节、SHA256 528920624f993555e4298f0c0f8b174491956cfad656c243d21731f906f37ced。首轮原始包51,662,244字节；SSH大流中断后改用35块文本编码传输2,273,501字节BSDIFF包，native与独立解码逐字节还原验证通过。所有源码与产物指纹保存在本轮缓存manifest。

切换前取得QQ域持久事务锁并核对无活动/冻结订单和pending/sending投递，保存数据库备份7721383字节（pg_restore --list通过，未恢复），备份位于 `/srv/sub2-qq-test/backups/delivery-switch-20261026-072058`。systemd协调停/起测试栈，仅更改app镜像，保留PG/Redis容器及数据；旧镜像保留回退。15:23新ACK推进，两域TLS/health200、5份当前引用资源核验通过；27项保全检查确认正式三容器/配置/Nginx，以及测试订单、投递、ledger、余额、绑定、模板revision12、配置7、305迁移和环境保持。正常管理员登录活动字段可变化。

**用户15:24:25再次主动重发后仍未收到图片。** 新投递在15:24:35结束为 `qq_media_upload_http_400_code_850027`，尚未调用最终messages接口；媒体GET200、19ms、fetches1，资金流水仍各一次。当前[腾讯官网群文件接口](https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_files.post.html)将850027定义为发送数据超时，850026另指原始文件下载失败，不能把850027唯一等同于回源下载失败。因此第一轮缺content修复不等于此订单已送达。

## 第二轮直传验证、部署与送达验收

使用原订单的原始PNG做一次严格限定的仅上传诊断：只读取得图片/授权，凭据仅在服务器内存解密，访问固定腾讯token及群files端点，明确srv_send_msg=false，从未调用messages或生成接口。原图2,068,854字节，JSON请求2,758,547字节；**数据直传HTTP200、收到file_info、耗时3.463秒**。未新增订单、扣费或数据库业务写入，原图未下载到本机或记录到日志。

当前腾讯官网（页面更新2026-10-09）及官方SDK支持file_data与file_name；旧GitHub rich-media文档仍写暂未支持，其最近对应文件更新为2024-03-18，不据此否认新接口。第二轮已实现并部署：不超过3,500,000字节的PNG/JPEG使用普通Base64直传，固定image.png/image.jpg、srv_send_msg=false、最终content为空格；复用原CreateMediaGrant→ReadMedia的授权/撤销/期限校验，不直接绕过仓储读取私图。领取时同次查询可信MIME，读取后再次核对；只读临时图片及MIME字段标json:"-"。大图/WebP保持URL路径，WebP不提前消耗内部ReadMedia，原3次公网读取额度保留；读失败/取消/尺寸或MIME不符立即停止，不自动换路径或二次上传，不改原图内容。35秒覆盖配置/授权/读取/上传/消息发送，完成状态另有5秒收尾。

新增真实Worker→QQClient链路回归先RED后GREEN；最终带隔离PostgreSQL16.15的完整QQ包 **483 PASS事件、0失败、0跳过**，包含授权撤销、令牌/图片过期、账号停用、租约失效、MIME变化、WebP读取预算、原图/余额/ledger保持。独立离线36 PASS与真实PG14 PASS也通过，vet与差异检查通过。测试PG仅127.0.0.1:55465/qq_release_review，运行后已正常停止、端口释放；数据保留，未尝试删除或绕过历史清理拒绝。

北京时间 **16:09** 仅更新测试站为 **sub2-qq-test:inline-74148a5aae1e** / **0.2.17-qq-inline.74148a5aae1e**，基线仍cda70a59b、静态linux/amd64。源码快照 `74148a5aae1ec83aeae7d5213e20996ba9ff1f2810da45b87c2c576b460ccc96`；二进制186,441,888字节，SHA256 `8d2789bf596b1a0e1cf835e204e6ed6eb97cbdeb23d11cafa2fb198512cbdca1`。前端与首轮哈希相同，本轮未重建。差分包1,351,585字节，native与独立还原一致，经21块文本上传校验；远端完整重建包SHA256 `3868865f144bd81155f734a9fa0e21d5a483bf79b3fa244bcff35bdf8e996dc7`。

沿用已审持久QQ事务锁与0在途/0待发送检查，数据库备份7,723,007字节，路径 `/srv/sub2-qq-test/backups/inline-switch-20261026-080816`；pg_restore --list通过但没有恢复数据库。切换只改测试应用镜像，PG/Redis容器与数据保留，旧delivery镜像作为回退；正式站三容器/配置/Nginx未变，测试订单/投递/ledger/余额/授权/SMTP设置/文案revision12/配置7/迁移305/环境27项检查通过。16:10新ACK推进、两站TLS/health200及5份活动前端资源核验通过。

**实际送达已完成**：用户16:11:16在原群发送新的重发，16:11:23.144记录 `delivery.sent`（约7.14秒），用户明确确认“收到图片了”。16:12:32只读复核目标订单 `succeeded/captured/sent/available`，原PNG仍2,068,854字节、1086×1448，hold/capture保持最初两条，实际扣款仅USD0.04一次；没有重新生成、重复扣费或代用户发送QQ。直传中的media_grant fetches=1来自服务内部授权读取，不能再据此推断发生了公网媒体GET。

两轮源码均已同步主目录，原用户WIP保留；隔离工作树和分支继续保留，尚未提交/推送或发布新的公开镜像。完整二轮证据在 `.cache/qq-inline-upload-20261026`，脱敏送达回执在 `output/qq-inline-upload-20261026/delivery-receipt.json`。已保留原release、回退镜像/数据库备份和本地验证记录。送达验收后，逐文件大小/哈希、规范路径、进程打开句柄检查通过，清理本次服务器incoming内64个上传/重建副本及4个空目录，释放485,818,524字节；一个仍有打开句柄的零字节旧.part被保留。没有触碰此前被拒的本地清理目标。

后续类似站内成功但未送达的订单，应由原用户在原群真正@机器人发送 `/重发 原任务ID`，使用新消息提供被动回复上下文；该路径仅重新投递已保存图片，不生成新图、不再次扣费。不能用旧消息盲目自动补发，不能代用户发送QQ或通过退款/重建订单掩盖投递故障。若仍失败，新诊断可区分上传与发送并保留QQ业务code，再继续定位；历史400原文未记录，不能事后编造。
