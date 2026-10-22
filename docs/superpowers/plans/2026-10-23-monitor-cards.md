# V2 渠道状态卡片移植计划

> 用户于 2026-10-23 批准本方案及实施，最初要求验收前不提交、不推送；现已完成本地验收并明确授权整合 main、检查、提交和推送。主目录原 WIP 单独保留，不纳入提交。

**Goal:** V2 默认展示参考截图的厂商卡片，保留卡片/详细切换，使用现有后端健康状态，提供本地可检查地址。
**Architecture:** 新增纯数据适配与卡片组件；现有 V2 页面保留详细内容、权限和筛选，通过查询参数 view=cards/detail 选择展示。卡片使用固定 platform_group 矩阵，按厂商和有效基础/专属倍率排序；后台 health.overall 同时驱动卡片徽章和时间条，保留 unknown/最低样本语义。V1 不变。
**Tech Stack:** Vue 3 / TypeScript / Tailwind / Vitest / 现有 V2 API。

## 已批准设计及边界

- 参考 zhoushun98/sub2api commit 80f7f2dce0ec103ff2c22988fb7d2e052daa20ff 与用户截图：厂商标题、1/2/3/4列响应式卡片，分组、平台与倍率、缓存率/可用率/首 Token 三格、底部历史色条和悬停详情。
- 使用 backend health.overall/error_rate/ttft/cache，不复制固定70/30%与10/30s算法。卡片和历史条用同一健康来源；灰色表示健康未确定，区分无流量和样本不足。
- 支持90m/24h/7d/30d，按coverage补空桶；格式为近N个时间段。保持回填/覆盖不足提示、手动刷新与倒计时。
- 默认cards；保留详细页的筛选、趋势/矩阵、模型/错误/用户明细和隐私开关。URL保留时间范围及详细筛选；卡片固定platform_group，不让详细分组模式污染卡片。
- 无数据分组由matrix/dimensions补齐；筛选后不补回筛选外组。倍率custom ?? base，保留0；加载失败不伪造默认或1x。组名优先用户分组本地化名称。
- 所有监控、倍率与详情请求绑定会话/用户/角色/序号；换号、重新登录、角色变化、切换语言/范围、卸载清理；隐藏页面暂停自动刷新。不修改认证实现或后端计费。
- 不新增后端接口、数据库迁移、生产演示数据、依赖或发布操作。真实后端若不可用，本地fixture仅保存在.cache并明确标为演示数据。

## 实施任务

- [x] 1. 基线：隔离工作树、复用pnpm缓存、记录主目录WIP哈希；运行现有模式/API/监控关键套件。
- [x] 2. 数据与卡片：先补并运行失败用例，再实现health复用、无流量/低样本、忽略错误、稀疏桶、倍率0/失败、平台别名和排序；实现截图风格组件、键盘提示与减弱动画。
- [x] 3. 页面与请求：先补默认cards/切换/固定维度/乱序及身份变化回归；接入视图、倍率signal、成功率类型、会话归属与倒计时；保持详细页能力。
- [x] 4. 文案与验证：中英文完整性、组件/页面/API测试、全量前端测试、vue-tsc、lint:check及生产构建；独立需求复核和代码质量复核，修复确认问题。
- [x] 5. 本地验收：仅loopback启动预览；浏览器检查暗/亮色、宽/窄屏、切换/悬停/范围；给用户实际可访问URL及数据性质，保持服务可用。
- [x] 6. 交接：更新CHANGELOG与HANDOFF、docs/changes记录实际验证；核对主目录原WIP哈希未变；验收前阶段不commit/push，等待用户检查（已完成；后续整合授权见下节）。

## 主要文件

新增 frontend/src/features/channel-monitor-v2/cards/{monitorCards.ts,MonitorCards.vue,MonitorCard.vue,MonitorBars.vue} 及行为测试。
修改 frontend/src/views/user/ChannelStatusV2View.vue、api/{groups.ts,channelMonitorV2.ts}、i18n/locales/{zh,en}/channelMonitorV2.ts；必要时拆分请求composable并给独立测试。
验证命令统一先加载主项目tools/project-env.ps1，在工作树frontend运行corepack pnpm@10.12.3。

## 验收后整合

用户已确认样式及功能均无问题。按授权在主项目 main 整合；本轮检查与源码提交身份见[整合记录](../../changes/2026-10-23-monitor-cards.md)。保留仍服务本地预览的工作树，镜像和生产状态不变。
