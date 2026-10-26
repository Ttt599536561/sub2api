# QQ功能开关与作品图库 Implementation Plan

> **For agentic workers:** Use subagent-driven-development, TDD and independent review. User approved this design before branch creation.

**Goal:** 提供独立QQ业务开关、可诊断的绑定过程与安全的图片作品图库。
**Architecture:** 现有settings键贯通管理/公开配置；QQ域层控制准入而保留账务与历史操作；图库通过既有认证Blob API并发受控加载。线上接收器归属先只读核查。
**Tech Stack:** Go/Gin/PostgreSQL、Vue3/TypeScript/pnpm9、PowerShell7；缓存仅主项目.cache。

## Task 1：隔离与契约
- [x] 用户批准推荐方案。
- [x] 从740c0c582建立codex/qq-controls-gallery-20261026，保存11份主目录WIP指纹。
- [x] 基线QQ后端和前端专项通过；复用pnpm9共享离线缓存。
- [x] 固定公共字段qq_binding_enabled/qq_image_enabled；QQStatus binding_enabled/image_enabled；保留旧enabled。

## Task 2：后端独立准入开关（独立代理）
Files: setting相关service/domain/admin/DTO；internal/qqimage binding/orders/store；qq_image_handler/wire；对应测试。
- [x] 先写并运行失败测试：默认兼容/false/旧PUT、三阶段绑定、4组合、新单不冻结、旧事件重放/旧操作可用。
- [x] 最小实现设置键和序列化/保存/公开/SSR；不新增数据库schema迁移（现有settings KV）。
- [x] 域层直接读取当前有效开关或等价安全provider，避免只挡HTTP；读取失败拒绝新业务，禁止混入缓存旧true绕过关闭。
- [x] Status回传有效两状态；simple与原Config.Enabled仍生效。
- [x] 跑设置/QQ服务handler和真实PG，保持既有财务断言，记录红绿证据。

## Task 3：作品图库（独立代理）
Files: QQOrdersView.vue、新图库组件/受控图片composable、图库专用CSS、对应新测试；不改绑定页或全局settings。
- [x] 先写可用图/晚到图/expiry、会话换号释放、并发<=2/缓存<=12张64MiB、迟到响应、原取消语义失败测试。
- [x] 实现3/2/1网格、状态占位、搜索筛选（明确最近100条）、详情抽屉/手机全屏与直接路由。
- [x] 原图鉴权、懒加载、有限轮询及Blob撤销；下载/复制提示词与重发指令保持现有能力，不引入公开链接/收费重试。
- [x] 保留现有QQOrderCard的管理员用法；新增图库组件避免波及后台列表。
- [x] 新词条放独立qqGallery locale模块并由root注册，避免与绑定词条冲突。
- [x] 专项测试、类型/lint/build后由root做桌面和移动端浏览器验证。

## Task 4：管理员UI与绑定错误（root）
Files: SettingsView.vue、frontend types/api settings/app store、featureFlags/sidebar、QQBindView.vue/utils qqImage、locale qqImage、api/qqImage.ts、对应测试。
- [x] 测试先行：开关显示/保存/public刷新/旧兼容；请求404明确站点提示，不把网络/500当确定失败。
- [x] 实现设置UI和有效状态说明；保留历史管理入口，关闭状态不拦解绑/旧图。
- [x] 补共享API字段website_status/binding_enabled/image_enabled；注册图库locale。
- [x] 查QQ群回复测试域名的配置原因，实际HTTP/配置/接收器状态采用只读核查，不伪造线上根因。

## Task 5：合并验证和交付
- [x] 各域集成后执行关键测试/真实PG，类型、lint、build及CI名单。
- [x] 本机真实组件/假数据视觉验证（桌面/移动、明暗、成功/处理中/失败/过期/空态）。
- [x] 独立规格审查和代码质量审查，修复所有阻断项。
- [x] 更新CHANGELOG/HANDOFF/changes与实际线上核查边界，检查链接/跟踪、主目录原WIP指纹。
- [x] 本地提交；发布/部署目标与是否执行另按本轮明确授权和最终事实记录，不代用户做真实QQ或收费生成。
