# 2026-10-27 QQ独立开关、正式站接管与作品图库

## 授权与来源

用户要求管理员功能开关可分别控制QQ绑定与生图、排查正式站绑定失败、优化用户作品展示。先完成只读调查及方案展示，用户明确回复“按推荐方案开发”后才从740c0c582创建codex/qq-controls-gallery-20261026工作树。用户随后明确选择“正式站接管现有机器人，测试站停止接收”。主目录11份既有WIP保留。

设计见[已批准方案](../superpowers/specs/2026-10-26-qq-controls-gallery-design.md)，计划见[实现计划](../superpowers/plans/2026-10-26-qq-controls-gallery.md)。交付沿用此前仅Linux镜像、用户自行更新服务器的方式；本轮线上接管只重新加载原r7环境，没有部署本分支新界面。

## 代码变化

- 系统设置→功能开关新增qq_binding_enabled、qq_image_enabled；缺失默认true，原QQ技术配置默认关闭/凭据/群限制不变。管理GET/PUT、public settings、SSR注入、审计和前端刷新贯通；旧客户端省略/null字段不参与写入，避免覆盖并发关闭。
- 域层取得原事务锁后直接读取settings：绑定新申请/网站授权/QQ确认三阶段均受控，新单先去重再检查开关、再冻结余额；读取失败拒绝。已受理订单、历史查询/图片/重发、取消、解绑及账务处置保留。simple模式在runtime启动前禁止新业务。关闭不是断开全部QQ连接或撤销全部授权。
- Status保留原enabled，增加binding_enabled/image_enabled有效值；用户页面保留历史入口并显示暂停说明。已观察到暂停时统一清理确认码和同意状态，模板也检查有效开关。
- 请求不存在时显示当前站点/原始链接域名提示；网络及5xx仍保留结果不确定提醒，不自动再次授权、不从错误message解析敏感内容。
- 订单变为作品网格（桌面3列/平板2列/手机1列）、大图详情抽屉、当前最近100条搜索筛选、空态/骨架及状态占位；原详情URL保留。生成/资金/投递三态独立，报价不冒充实际支出。
- 认证原图Blob按视口加载，最多2并发、12张/64MiB缓存；过期/离屏/会话失效/卸载撤销URL并丢弃迟到响应，不持久化私图。website_status和有效期决定图片可用性，资金释放后的晚到图仍可免费领取。仅有效且captured的图显示QQ重发指令；原群原身份权限仍由后端核验。
- 进行中订单每5秒只读轮询，最多60次；隐藏页面暂停，被打断的首次读取恢复可见后重取。读取序号与页面/会话归属分离，避免取消POST被并行GET或隐藏事件卡死；路由/会话变化仍丢弃迟到结果。
- 基础VERSION对齐官方0.2.18发布基线，后续二开镜像使用0.2.18-r1；未额外合并上游未发布main。

## 正式绑定故障的实际核查与接管

用户确认群内/绑定回复指向qq-test.tangguo.xin。只读核查正式r7已运行，但QQ专用加密密钥未入环境、配置0条、授权/在途订单0；测试站仍是inline私有版本，配置7开启WebSocket且PublicBaseURL为测试域名。因此测试站解绑不会把申请编号迁到正式站。没有截取用户当次HTTP响应，不能声称逐笔证明其通用文案的唯一后端错误码；源码404映射缺陷已单独修复。

经用户明确选择接管后，先准备阶段脚本和回退并完成独立审查。审查发现verify原本未要求生产lease=1，先以0/2锁负向复现，再补门禁；最终脚本SHA256为3926a8c960d8a73658ccf68f7195d5f96b64e1e93cb0dfa8336958a71cd124a5，10项本机构造验证通过。

实际执行按阶段完成：

1. 私有备份位于服务器/root/sub2api-deploy/qq-cutover-backup-20261026，目录0700/文件0600。两库pg_dump经pg_restore --list核验；production.dump为633,110,990字节、test.dump为14,033,936字节，未恢复或下载到本机。
2. 正式实例生成独立32字节主密钥；仅新增QQ_IMAGE_ENCRYPTION_KEY和QQ_IMAGE_EXTERNAL_HELP=true两项环境。生产应用以原0.2.17-r7镜像重建，原PG/Redis容器与其余配置语义保全，health200。
3. 测试站追加配置8：enabled=false、app_id为空，其余原配置/密文保留，没有clear_secret或凭据撤销。先提交停收，再认证读取waiting_config并确认网关锁释放；不改旧配置/账本/图片。
4. 在测试停态fence下，正式站追加配置1：原App/群/价格/上游设置保留，凭据在服务器内存解密后以正式新密钥重新加密，PublicBaseURL为https://api.tangguo.xin。测试锁0、生产锁1、两站health200；锁本身不冒充connected/心跳证据。
5. 用户随后明确确认“已是正式站链接，绑定成功”。只读复核正式active_grants=1、pending_requests=0，测试active_grants=0；两站无active_orders。由用户完成真实QQ/网站授权，代理没有发QQ消息、邮件或收费生成。

测试站网站与原用户私图历史保留；CF菜单/智商Worker仍运行并从测试站读取原published模板，当前测试模板revision13无硬编码测试域名；生产使用自身默认模板，本次未复制模板历史或改CF同步源。禁止两个Sub2接收器同时处理同一收费指令。

后续替换正式镜像必须保留本次新增QQ环境变量及原密钥。回退也必须先停正式、等连接释放并处理在途订单，再追加恢复测试配置；不倒灌数据库、不覆盖用户资金。脚本在未知配置漂移、新在途资金或生产容器停止时保守暂停，不自动双开。

## 验证和审查

- 后端四包相关回归：3,456个PASS事件/3,149叶、0失败0跳过；完整QQ真实PG：514 PASS事件/474叶、0失败0跳过，范围重叠不相加。
- 五包vet及Linux目标CI默认标签golangci-lint2.14通过0 issues；额外unit标签lint有78项，全部来自46个未改动旧测试文件，未扩大修复范围。
- SSR/public注入与缓存刷新：完整web包129 PASS事件/103叶、0失败0跳过。
- 前端全量曾完成445文件4,010项；最后邮箱验证期间暂停绑定的追加回归先RED后GREEN，相关67项通过。追加回归后的类型/lint/build再次通过；新源码的GitHub完整门禁与镜像状态在发布记录补充，不把旧数字冒充新快照。
- 本机真实Vue组件/假身份/假API浏览器验证1280/768/390断点，无横向溢出；验证明暗、详情/键盘关闭、空态、筛选搜索、免费晚到图、暂停历史、跨站错误与暂停解绑，所查console无warn/error。演示PNG为本地Canvas假图，不是真实用户图片。
- 独立后端审查无阻断；图库两项P2（初次隐藏不恢复、取消/GET竞争）先复现再修复并复核关闭；绑定邮箱验证后的暂停清码P2同样红绿修复并复核关闭。浏览器补正不可重发图片的指令入口和旧结算提示，原图下载仍保留。

原始证据、回退脚本、只读核查在主项目.cache/qq-controls-gallery-20261026；图片预览在output/qq-controls-gallery-20261026。必要结论归档本文件，秘密只在服务器私有备份中。后台启动/停止脚本组合曾被自动审批拒绝；改用仅监听127.0.0.1的受控前台工具会话完成QA，未重试被拒绝的后台/停止脚本。预览已关闭；没有清理先前被拒绝的旧目录。

参考：[Midjourney作品管理](https://docs.midjourney.com/hc/en-us/articles/33329462451469-Organizing-Your-Creations)、[Firefly生成历史](https://helpx.adobe.com/in/firefly/mobile/view-generation-history/generation-history-overview.html)。

本地QA前台受控进程和专属测试PostgreSQL已停止，端口13926/54394均无监听；没有重试删除先前被审批拒绝的旧数据目录。


## GitHub精确门禁的契约补充

应用4ae80f8ad的CI 40362114869中前端、Linux lint、shell与release helpers成功，完整unit的TestAPIContracts发现两份管理设置wantJSON遗漏qq_binding_enabled/qq_image_enabled，307与309字段不匹配，integration随前置失败跳过。新增字段本身返回正确，现只给两份精确JSON期望各补两个true字段（4行），不改产品逻辑或弱化断言。本地先RED复现，随后11个契约子例、server完整20叶/0失败0skip与Linux目标unit-tag server lint0 issues通过；后续新源码必须重新通过自身CI/安全门禁。


## Linux镜像最终交付

本轮源码与契约修正已合入main并推送，应用固定b048bc329a06b227ced2324af6001fc1960e44c8，控制c612676d61a5167b68808acd8a0d2d11e8a476fb；精确CI/安全和发布40366486180全成功。ghcr.io/ttt599536561/sub2api-custom:0.2.18-r1的5份必要artifact及24层/90,240,959字节匿名核验完成，digest `sha256:899cdd8b73f64853c7421ea9f1a441fb2d544c4f47f26488b59f0db9c73f2de9`。新镜像未部署服务器，用户自行更新；正式QQ配置接管仍在原r7上，用户已确认绑定成功。后续保留QQ密钥和EXTERNAL_HELP环境，详见[完整发布记录](../../deploy/CUSTOM_IMAGE_0.2.18-r1.md)。
