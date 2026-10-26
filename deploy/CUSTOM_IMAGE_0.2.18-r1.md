# Sub2API 二开 0.2.18-r1 Linux 发布与升级记录

日期：2026-10-27（Asia/Shanghai）。状态：源码已整合主目录main并推送，正在等待精确源码CI/安全门禁；镜像尚未发布。新界面将由用户自行替换镜像上线。

## 固定来源与功能

- 应用源码/OCI revision：`b048bc329a06b227ced2324af6001fc1960e44c8`，基于740c0c582，继承官方v0.2.18对应ac1b8f1及r7签到v3，不额外拉取上游未发布main。
- 本次基础VERSION已同步0.2.18，二开发行号为0.2.18-r1；源码身份按Git SHA判断，历史0.2.17-r7已包含官方0.2.18基线。
- [专用工作流](../.github/workflows/publish-custom-v0218-r1.yml)固定完整应用SHA，仅Ubuntu原生linux/amd64和linux/arm64；要求同SHA最新main/push CI和Security Scan成功，最终标签拒绝覆盖，不触发macOS/通用Release。
- 预定镜像：`ghcr.io/ttt599536561/sub2api-custom:0.2.18-r1`。
- 功能：管理员系统设置的两个QQ业务开关、完整后端准入/旧PUT兼容、绑定404站点指引、图片作品网格/详情和有界私图加载。历史订单、解绑、免费晚到图、资金/投递状态保持；详情仅为有效已结算图片提供QQ重发指令。

## 已完成的开发与实际运行边界

本地四包相关回归3,456 PASS事件/3,149叶，完整QQ真实PG514 PASS事件/474叶，均0失败0skip；SSR129 PASS事件/103叶，五包vet/CI模式Linux lint0 issues。前端全量445文件4,010项后，追加邮箱验证暂停清码回归先RED后GREEN，最终相关67项及类型/lint/build通过；所有新增关键测试纳入CI。桌面/平板/手机、明暗和关键状态已通过真实组件/纯假API浏览器验收。独立后端/前端及操作脚本审查发现项已修复并复核；详见[实现与接管记录](../docs/changes/2026-10-27-qq-controls-gallery.md)。

正式QQ接管是另一个已完成的配置操作：正式仍运行原0.2.17-r7，新增独立QQ密钥和EXTERNAL_HELP=true；测试配置8已停收，正式配置1链接api.tangguo.xin。用户确认正式链接绑定成功，正式active_grants=1；未迁移测试用户/余额/绑定/订单，不改CF菜单/智商服务。新界面不因配置接管自动上线，需更新本次镜像。

## 用户升级与回退

1. 用户自行更新镜像，保留数据库、配置、Compose与现有QQ密钥备份。正式配置中的QQ_IMAGE_ENCRYPTION_KEY和QQ_IMAGE_EXTERNAL_HELP=true必须保留；不要重新生成/替换密钥，否则现有凭据无法解密。
2. r7→本版没有新增SQL schema迁移；两个settings键缺失默认true以兼容现状，原QQ技术配置/白名单/邮箱证明仍生效。管理员在系统设置→功能开关分别控制新绑定和新生图，旧订单/授权管理入口保留。
3. 关闭业务开关不会断开全部QQ连接；测试站已用独立停收配置停用。不得同时恢复测试接收器和保持正式接收器，避免同一指令被两站处理。
4. 从更早版本升级仍需核对[r7及之前的迁移/密钥说明](CUSTOM_IMAGE_0.2.17-r7.md#用户升级与回退)。不要修改历史迁移/checksum或用旧数据库覆盖新账务。
5. 回退旧镜像会失去本版新开关的后端执行与新界面，必要时先按旧QQ技术配置停新业务；接收器回退必须先停正式、等在途处理和连接释放，再恢复测试，保留资金事实。

## 发布证据

首轮4ae的完整unit发现两份管理设置JSON契约遗漏新增字段；只补4行精确期望、保持JSONEq，11契约子例/server20叶/Linux unit-tag lint通过。现固定b048并重新等待其自己的CI/安全门禁，旧失败候选不用于发布。

- 精确源码CI：[40364270141](https://github.com/Ttt599536561/sub2api/actions/runs/40364270141)。
- 精确源码Security Scan：[40364269888](https://github.com/Ttt599536561/sub2api/actions/runs/40364269888)。

精确CI/安全/发布run、最终digest和匿名artifact/运行层核验在实际完成后补充。原始发布证据放`output/publish-v0.2.18-r1`，过程缓存和WIP保护放`.cache/publish-qq-v0218-r1-20261027`。主目录11份WIP、4份其他untracked及293份原dist文件已保全，不进入发布源码。
