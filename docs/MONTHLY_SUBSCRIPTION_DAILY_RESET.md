# 月卡每日额度重置功能

## 文档信息

- 开发提交：`0be85e950`（`feat: add monthly subscription daily quota resets`）
- 开发分支：`feature/monthly-subscription-daily-reset`
- 开发日期：2026-09-06
- 记录更新日期：2026-09-17
- 当前状态：原两轮审查修复已保存为 `08d72d5bd`，并通过合并提交 `83fee7462` 与上游 `main` 的 `881f32026`（0.2.5）整合。月卡专项、真实数据库集成、后端与前端全量测试及构建已完成验证；本次同步详情见 [上游整合记录](UPSTREAM_MERGE_2026-09-17.md)。未推送或生产发布。
- 后续再次审查：补齐复制月卡分组的许可字段、自动扫描超时后的候选恢复、前端跨账号异步状态隔离和微秒响应排序；新增模块回归与真实数据库测试，详见 [合并后第二轮审查记录](POST_MERGE_REVIEW_2026-09-17.md)。

### 版本追溯

| 节点 | 提交或状态 | 说明 |
| --- | --- | --- |
| 线上源码基线 | `578785ee7fb35030b094b69624efe25670a36f5f` | 用户提供的运行版本为 `Sub2API 0.2.1`，构建时间为 `2026-09-05T09:34:19Z`。本地 Git 已确认该提交是当前分支的祖先。 |
| 版本文件同步 | `ab99d56e9` | 同步仓库 `VERSION` 到 0.2.1。 |
| 需求与设计记录 | `b0ba68f93` | 月卡重置规则及验收场景。 |
| 功能实现 | `0be85e950` | 月卡日额度重置的前后端、迁移与初始测试。 |
| 原功能文档 | `bff587089` | 本文初版。 |
| 两轮审查修复 | `08d72d5bd` | 保存下文 REV-01 至 REV-10 和两个原未跟踪回归测试。 |
| 2026-09-17 上游同步 | 合入 `881f32026`（0.2.5） | 保留上述二开功能与审查修复，详见上游整合记录。 |

与线上源码基线比较，历史数据库迁移文件未修改，仅新增 `235_subscription_daily_reset.sql`。该结论来自源码比较，尚未读取生产数据库的 `schema_migrations` 或执行生产升级演练。

以下两个原未跟踪回归测试已在 `08d72d5bd` 纳入提交：

- `backend/internal/handler/gateway_subscription_admission_regression_test.go`
- `backend/internal/repository/subscription_daily_reset_lock_order_integration_test.go`

## 功能概述

管理员可以在月卡分组上开启“允许提前重置每日额度”。用户购买或持有该分组的月卡后，可以在订阅卡片上手动清空当日已用额度，或开启自动重置。

每次成功重置会同时完成两件事：

1. 将当前订阅的每日已用额度清零。
2. 将订阅到期时间减少 24 小时。

周额度、月额度、历史消费记录和自然日刷新规则保留。重置资格由订阅所属分组和数据库中的当前状态决定，不根据套餐名称或价格猜测。

## 业务规则

- 只有 `subscription_type=subscription` 且管理员打开分组开关的月卡可用。
- 手动重置不要求每日额度用满，只要每日已用额度大于 0 即可；页面不弹确认框。
- 自动重置只在每日用量达到或超过当前分组每日上限时触发。开启开关后会立即检查，之后由服务端后台继续处理，用户无需保持页面打开。
- 每次成功只扣 24 小时；扣减后到期时间必须严格晚于服务端判定时间，剩余时间小于或等于 24 小时时拒绝重置。
- 每张订阅按系统时区自然日最多成功重置 100 次。失败、并发冲突、重复请求和自然刷新不计数。
- 周或月额度达到上限时，手动和自动重置都拒绝，不扣天、不计次数；页面按钮置灰且后端仍独立校验。
- 重置使用管理员当前配置的每日上限。事件保存本次上限快照，其他分组和历史记录不受影响。
- 自然日刷新先按原有规则维护日/周/月窗口，不产生本功能的扣天事件。
- 未到期续费保留自动开关；过期后重新开通会关闭自动开关，用户需要重新开启。
- 管理员仅延长订阅有效期时，保留已有暂停状态；已到期的暂停订阅不会因此自动恢复请求权限。
- 因次数或有效期限制暂时无法自动重置时，保留用户的开启偏好；条件恢复后后台继续检查，不额外显示暂停原因。
- 同一订阅的多个 API Key 共享每日额度和重置次数；不同订阅分别计算。

## 管理员配置

管理员创建或编辑分组时使用字段：

```json
{
  "subscription_type": "subscription",
  "allow_subscription_day_reset": true,
  "daily_limit_usd": 100
}
```

启用开关时，后端要求分组是月卡类型，并且每日上限是有限的正数。标准分组、无限日额度或非正日额度不能开启该功能。前端配置入口位于管理端分组创建和编辑表单。

## 用户端接口

接口位于现有用户认证路由 `/api/v1/subscriptions` 下，所有接口按当前登录用户校验订阅归属。

### 查询状态

```http
GET /api/v1/subscriptions/:id/daily-reset-state
```

返回的订阅对象包含：

```json
{
  "daily_reset": {
    "eligible": true,
    "can_reset": true,
    "auto_daily_reset_enabled": false,
    "today_reset_count": 4,
    "daily_reset_limit": 100,
    "daily_reset_version": 9,
    "server_date": "2026-09-06",
    "server_time": "2026-09-06T12:00:00Z",
    "reason": ""
  }
}
```

`reason` 是后端状态码，供客户端判断和排障使用；自动暂停时页面不把它渲染为额外提示。

### 手动重置

```http
POST /api/v1/subscriptions/:id/reset-daily
Idempotency-Key: <operation-id>
Content-Type: application/json

{
  "expected_version": 9,
  "expected_date": "2026-09-06"
}
```

返回操作 ID、是否重放、是否实际重置以及最新订阅。客户端必须在结果明确前复用同一个 `Idempotency-Key`，不能因为网络超时创建第二个操作。

### 设置自动重置

```http
PUT /api/v1/subscriptions/:id/auto-daily-reset
Content-Type: application/json

{
  "expected_version": 9,
  "enabled": true
}
```

响应中的 `preference_saved` 表示开关是否已保存；如果保存成功但即时检查遇到技术错误，会同时返回 `check_error`，不能把开关保存误报为失败。

### 查询未确定操作

```http
GET /api/v1/subscriptions/:id/daily-reset-operations/:operationId
```

用于客户端刷新、断线或响应丢失后的结果核实。

## 数据库与事务

迁移文件为 `backend/migrations/235_subscription_daily_reset.sql`，包含：

- `groups.allow_subscription_day_reset`
- `user_subscriptions.auto_daily_reset_enabled`
- `user_subscriptions.daily_reset_version`
- `user_subscriptions.preserve_calendar_daily_reset`
- `subscription_daily_reset_events` 事件表

事件表记录订阅、用户、分组、来源、操作 ID、请求指纹、版本、系统时区、自然日序号、重置前后额度、每日上限快照、到期时间和判定时间。数据库约束保证：

- 每次只扣 86400 秒；
- 扣减后仍晚于判定时间；
- 每日序号只能是 1 到 100；
- 同一订阅的操作 ID、重置前版本和“自然日 + 序号”不能重复。

重置事务按“用户 → 分组 → 订阅”顺序加锁，并在同一事务中维护自然窗口、检查资格、清零每日额度、扣减到期时间和写入事件。用户停用、分组修改、管理员续期、退款扣天与重置之间有并发保护。

后续分组锁和订阅锁使用 `NOWAIT`。遇到 PostgreSQL `55P03` 锁竞争时，先回滚以释放已取得的锁，再等待 10 毫秒，用新事务重试取锁，最多尝试 100 次，并响应 context 取消。该重试仅覆盖取锁阶段，避免与“计费更新订阅后更新 API Key”和“删除用户先删除 API Key”形成锁环；尚未取得全部锁时不会执行扣天写入。达到重试上限仍可能返回锁竞争错误，并不保证所有并发调用都成功。

## 自动触发与请求准入

自动重置共用手动重置的原子事务，不另写扣天逻辑。触发点包括：

- 用户开启自动开关后立即检查；
- 用量结算提交成功后检查；
- 请求进入计费准入时检查一次并最多重新判断一次；
- 服务启动时和之后每 30 秒扫描开启自动重置的有效订阅。

请求在等待用户或账号并发槽位后，会再次读取数据库中的订阅状态、分组权限、额度和有效期。OpenAI、Anthropic、Gemini、Web Search 以及 OpenAI WebSocket 后续 turn 均接入了这层复核，避免排队期间到期或额度耗尽后仍转发请求。

审查后补充的兼容行为：

- 订阅准入向计费熔断器回报检查结果；到期或额度耗尽等业务拒绝表示数据库读取成功，技术故障仍计入失败。余额请求在余额查询完成后再结算本次探测结果。
- Web Search 在取得账号槽位后单独处理订阅复核结果，返回现有计费错误，释放槽位并终止账号重试；现有 handler 对订阅额度错误使用 403，中间件的 429 映射保持原样。
- Anthropic 请求进入余额分组回退后，复核当前尝试的 `currentSubscription`，不会继续检查原订阅。
- 标准和 Gemini 鉴权错误响应使用 `ApplicationError.Message`，不把内部错误链的 `cause` 返回给客户端。

## 前端行为

用户订阅卡片提供：

- 每日额度旁的“重置”按钮；
- 自动重置每日额度开关；
- 当日 `N/100` 次成功次数；
- 请求未确定时的“正在核实”状态。

只有 `subscription_type=subscription` 且获得分组许可的订阅显示完整控件；本功能称其为月卡，不根据套餐名称或具体期限推断资格。未获得许可的其他分组不显示完整控件。周/月额度耗尽时手动按钮真实禁用，但用户仍可关闭自动开关。前端将操作 ID 写入 `sessionStorage`，页面重载后先查询操作结果，再决定是否复用原请求。

关闭分组许可后，如果仍有已开启的自动偏好或待核实操作，会保留对应控制区域。普通 HTTP/IP 访问环境缺少 `crypto.randomUUID()` 时，使用项目已有形式的操作 ID 回退。浏览器存储写入失败仍允许关闭自动重置；手动重置和开启自动重置仍要求先保存恢复状态，防止未确定的扣天操作丢失。

重置相关接口按本次数据库判定时间修正已到期订阅的响应状态，不修改仓储快照。“有效订阅”接口会剔除补充查询时已确认到期、暂停或撤销的订阅，避免影响顶部订阅数量及续费页面。

当前已覆盖中文和英文，并检查了桌面 1440×1000 与移动 390×844 布局。

## 验证记录

### 初始开发验证（2026-09-06）

以下为初始实现阶段的历史结果，不能替代后续修复版本的验证范围：

- PostgreSQL 18.1 + Redis 8.4 集成测试：并发重置、幂等重放、每日 100 次上限、事务回滚、窗口维护和用户锁。
- 后端月卡专项回归：领域、服务、准入、槽位复核、退款并发和接口测试。
- `go build -p 1 ./cmd/server`。
- DTO 与前端字段契约测试。
- 前端全量测试：257 个测试文件、1881 项测试通过。
- 前端 `typecheck`、生产构建、变更文件 ESLint 和 `git diff --check`。
- API Mock 页面验收：中文/英文、桌面/移动端、响应丢失后的单操作 ID恢复。

完整验证明细见 [实施计划与验证日志](superpowers/plans/2026-09-06-monthly-subscription-reset.md)。

### 两轮审查后的验证（2026-09-06）

| 阶段 | 已执行的验证 | 结果及范围 |
| --- | --- | --- |
| 第一轮审查 | 前端全量 Vitest | 258 个测试文件、1885 项测试通过。这是第一轮阶段的历史全量结果。 |
| 第一轮审查 | 7 项修复回归、后端计费/订阅专项、handler/鉴权/仓储单元测试、数据库集成测试 | 通过；新增问题先复现失败，再验证修复。 |
| 第二轮审查 | 前端 store、API、订阅页专项 | 4 个测试文件、39 项测试通过，含存储失败关闭开关及扣天操作恢复保护。 |
| 第二轮审查 | 前端类型检查、变更文件 ESLint、`git diff --check` | 通过。 |
| 第二轮审查 | 后端计费/订阅专项；handler、admin handler、DTO、quota view、鉴权、仓储包的完整 unit 测试 | 通过，覆盖响应脱敏和有效列表状态边界。服务包使用专项筛选，未宣称服务包全量通过。 |
| 第二轮审查 | PostgreSQL 18.1 + Redis 8.4 专项集成测试 | 通过，包含死锁复现回归、并发幂等、计费用量守恒、次数约束、自然窗口和事件失败回滚。 |

第二轮修复当时未重新运行前端全量测试或前端生产构建，不能将 1885 项历史全量结果视为最终工作区的全量结果。该轮文档更新仅记录当时已有证据；2026-09-17 上游整合后的重新验证见 [上游整合记录](UPSTREAM_MERGE_2026-09-17.md)。

第二轮实际执行命令如下：

```bash
cd backend
go test -p 1 -tags=unit ./internal/service -run 'DailyReset|Subscription|Billing|NegativeSubscription' -count=1
go test -p 1 -tags=unit ./internal/handler/... ./internal/server/middleware ./internal/repository -count=1
go test -p 1 -tags=integration ./internal/repository -run 'SubscriptionDailyReset|ResetVersion' -count=1 -v

cd ../frontend
node node_modules/vitest/vitest.mjs run src/stores/__tests__/subscriptionDailyReset.spec.ts src/stores/__tests__/subscriptions.spec.ts src/views/user/__tests__/SubscriptionsView.spec.ts src/api/__tests__/subscriptions.dailyReset.spec.ts
node node_modules/vue-tsc/bin/vue-tsc.js --noEmit
node node_modules/eslint/bin/eslint.js src/stores/subscriptions.ts src/stores/__tests__/subscriptionDailyReset.spec.ts
```

本机全局 pnpm 11 曾尝试迁移依赖安装；验证时使用已安装工具，第一轮还使用过本机缓存的 pnpm 10.11.0，没有为此修改项目依赖或锁文件。

## 审查修复索引

以下 10 项均由初始月卡新增代码引入，已修复并在 `08d72d5bd` 保存；2026-09-17 上游整合继续保留这些修复。编号用于问题单和回归定位。

### 第一轮：7 项

| 编号 | 问题与修复 | 代码位置 | 回归证据 |
| --- | --- | --- | --- |
| REV-01 | 订阅准入遗漏熔断回调，故障恢复后可能耗尽半开探测并持续拒绝请求；补齐成功、业务拒绝和技术失败的回报。 | [billing_cache_service.go](../backend/internal/service/billing_cache_service.go)，`CheckBillingEligibility` | [billing_cache_subscription_admission_test.go](../backend/internal/service/billing_cache_subscription_admission_test.go)：`TestBillingSubscriptionAdmissionCompletesCircuitBreakerProbe` |
| REV-02 | 延长已到期的暂停订阅会意外激活；续期初始化时保留暂停状态。 | [subscription_service.go](../backend/internal/service/subscription_service.go)，`ExtendSubscription` | [subscription_renewal_lock_test.go](../backend/internal/service/subscription_renewal_lock_test.go)：`TestExtendSubscriptionExpiredKeepsSuspension` |
| REV-03 | 重置、计费与删除用户形成三事务锁环；使用后续锁 `NOWAIT` 和释放锁后的有限重试。 | [subscription_daily_reset_repo.go](../backend/internal/repository/subscription_daily_reset_repo.go)，`beginLocked`、`lock` | [锁序集成测试](../backend/internal/repository/subscription_daily_reset_lock_order_integration_test.go)：`TestSubscriptionDailyResetRepository_UserDeletionAndBillingLockOrder`；[仓储单元测试](../backend/internal/repository/subscription_daily_reset_repo_unit_test.go)：`TestSubscriptionDailyResetRepository_RetriesContendedLocksInNewTransaction` |
| REV-04 | 余额分组回退仍复核原订阅，可能被原订阅到期拦截；改用当前尝试的订阅。 | [gateway_handler.go](../backend/internal/handler/gateway_handler.go)，`Messages` | [gateway_subscription_admission_regression_test.go](../backend/internal/handler/gateway_subscription_admission_regression_test.go)：`TestMessagesFallbackUsesCurrentAttemptSubscription` |
| REV-05 | Web Search 将取得槽位后的订阅拒绝误报为并发故障或继续账号重试；独立返回计费错误并释放槽位。 | [gateway_web_search.go](../backend/internal/handler/gateway_web_search.go)，`WebSearch` | [gateway_subscription_admission_regression_test.go](../backend/internal/handler/gateway_subscription_admission_regression_test.go)：`TestWebSearchSubscriptionRejectionAfterAccountAcquisition` |
| REV-06 | 普通 HTTP/IP 环境缺少 `crypto.randomUUID()` 时无法提交；增加操作 ID 回退并保持重试 ID 不变。 | [subscriptions.ts](../frontend/src/stores/subscriptions.ts)，`startDailyReset` | [subscriptionDailyReset.spec.ts](../frontend/src/stores/__tests__/subscriptionDailyReset.spec.ts)：缺少 `randomUUID` 时的 manual/auto 提交及重试用例 |
| REV-07 | 到期后关闭自动重置可能返回原始 active 状态；用数据库判定时间修正响应，保留仓储快照不变。 | [subscription_daily_reset_service.go](../backend/internal/service/subscription_daily_reset_service.go)，`resetStateSubscription` | [subscription_daily_reset_service_test.go](../backend/internal/service/subscription_daily_reset_service_test.go)：`TestDailyResetServiceResponseNormalizesExpiryAtServerTime` |

### 第二轮：3 项

| 编号 | 问题与修复 | 代码位置 | 回归证据 |
| --- | --- | --- | --- |
| REV-08 | 鉴权响应直接返回带 `cause` 的错误字符串，可能暴露数据库内部信息；改为标准对外消息。 | [api_key_auth.go](../backend/internal/server/middleware/api_key_auth.go)、[api_key_auth_google.go](../backend/internal/server/middleware/api_key_auth_google.go) | [api_key_auth_subscription_admission_test.go](../backend/internal/server/middleware/api_key_auth_subscription_admission_test.go)：`TestAPIKeyAuthUsesDatabaseSubscriptionAdmission`，覆盖两种协议的分组/订阅数据库错误，并断言不包含内部信息 |
| REV-09 | 浏览器存储写入失败导致自动开关无法关闭；允许无存储的关闭请求，保持手动重置和开启请求的持久化前置保护。 | [subscriptions.ts](../frontend/src/stores/subscriptions.ts)，`startDailyReset` | [subscriptionDailyReset.spec.ts](../frontend/src/stores/__tests__/subscriptionDailyReset.spec.ts)：存储失败时可关闭，以及 manual/enable 无恢复状态不提交的用例 |
| REV-10 | 列表初查后到期、暂停或撤销的订阅仍出现在有效列表；按补充查询的状态过滤。 | [subscription_service.go](../backend/internal/service/subscription_service.go)，`ListActiveUserSubscriptions` | [subscription_daily_reset_service_test.go](../backend/internal/service/subscription_daily_reset_service_test.go)：`TestDailyResetActiveListFiltersRefreshedInactiveSubscriptions` |

## 本地预览记录（2026-09-07）

已使用当前完整工作区构建 Windows 后端，并启动真实前后端与独立数据库。此次预览没有使用 API Mock，也未连接生产数据库。

| 组件 | 本地地址或资源 |
| --- | --- |
| Vue/Vite 页面 | `http://127.0.0.1:3000/subscriptions` |
| Go 后端 | `http://127.0.0.1:8080`，启动验收时 `/health` 返回 `{"status":"ok"}` |
| PostgreSQL | 容器 `sub2api-design-postgres`，镜像 `postgres:18.1-alpine3.23`，本机端口 `15432`，数据库 `sub2api_preview` |
| PostgreSQL 数据卷 | `sub2api-design-postgres-data` |
| Redis | 容器 `sub2api-design-redis`，镜像 `redis:8.4-alpine`，本机端口 `16379` |
| 演示用户 | `preview@example.test`，普通用户角色；仅供本地预览 |

初始化演示数据包含 OpenAI 月卡（日用量 60/100，可手动重置、自动默认关闭）、Claude 月卡（周用量 700/700，手动禁用、自动偏好开启）和 Gemini 周卡（无月卡重置控件）。页面交互会改变本地数据，以上数值是初始化状态。

本机辅助文件均位于被 Git 忽略的 `frontend/.dev/`，不属于已提交源码；克隆仓库不会自动得到这些文件。当前 `.dockerignore` 尚未整体排除此目录，后续构建发布镜像前需显式排除，Git 忽略规则不能替代 Docker 构建上下文排除：

- `start-local.ps1`：启动前检查 8080/3000 端口，设置仅本地使用的环境并启动后端与 Vite。
- `seed-local.sql`：向独立预览库写入演示用户、分组及订阅，已存在的演示订阅不会被重置。
- `preview-local.cjs`：实连 API 登录、桌面/移动端检查，并通过 `--show` 打开独立 Chrome 预览会话。
- `sub2api-local.exe`：由 `go build -p 1 -o ../frontend/.dev/sub2api-local.exe ./cmd/server` 生成的本机后端。
- `local-data/`、`local-browser-profile/`：本地配置及浏览器会话数据，包含凭据，不应加入 Git 或发布包。
- `local-backend*.log`、`local-vite*.log`、`local-browser*.log`：本地运行日志。
- `local-preview-1440.png`、`local-preview-390.png`：本次实连页面截图。

实连预览已检查中文桌面 1440×1000 和移动 390×844，未观察到横向溢出或页面 JavaScript 异常。OpenAI 演示月卡的自动开关完成开启、关闭各一次，两个 PUT 请求均为 200，最终恢复关闭；此次实连浏览器检查未执行手动扣天，相关正确性证据见后端与数据库专项测试。

本机已有上述辅助文件和独立容器时，可按以下顺序重新启动；脚本会拒绝占用中的应用端口，应先核对已有进程：

```powershell
docker start sub2api-design-postgres sub2api-design-redis
# 在项目根目录执行；源码改变后先按上文命令重新构建本地后端。
& .\frontend\.dev\start-local.ps1
# 执行有限的页面检查，或加 --show 打开可操作的浏览器。
node .\frontend\.dev\preview-local.cjs
```

## 部署前注意事项

- 当前只完成代码和本地验证，迁移尚未对生产数据库执行。
- 上线前先备份数据库，在所有实例完成升级后再按分组逐步打开许可。
- 需要在目标环境运行迁移、PostgreSQL 集成测试和数据库负载验证。
- 初始开发曾记录 5 项范围外服务测试失败：4 项 Windows 插件包文件占用错误及 1 项内容审核缓存定时用例。这是历史执行记录；2026-09-17 整合版本的重新验证范围与结果以整合记录为准。
- 本次已完成 Linux amd64、CGO=0、内嵌前端资源的完整二进制交叉编译。发布容器镜像、生产数据库备份副本升级演练及生产规模负载测试尚未验证；本地构建不替代这些验证。
- 打包前将 `frontend/.dev/` 从 Docker 构建上下文整体排除，尤其是 `local-data/` 和 `local-browser-profile/`；本次只记录该待办，尚未修改 `.dockerignore`。
- 服务器报告使用 Docker Compose、`linux/amd64`、PostgreSQL 18、Redis 8。升级应保留原部署配置、密钥和数据卷，仅替换经过验证的自定义应用镜像。
- 当前默认镜像和程序在线更新源仍指向作者版本；后续维护二开功能应使用自己的发布镜像，避免被作者版本覆盖。
- 已发生付费重置后，替换旧二进制不会恢复扣天或用量；旧版本不理解日历日保留字段。关闭分组许可可停止新的付费重置，数据回滚仍需单独核对，恢复旧备份会丢弃备份后的业务写入。
