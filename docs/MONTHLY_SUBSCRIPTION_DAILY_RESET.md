# 月卡每日额度重置功能

## 文档信息

- 开发提交：`0be85e950`（`feat: add monthly subscription daily quota resets`）
- 开发分支：`feature/monthly-subscription-daily-reset`
- 开发日期：2026-09-06
- 当前状态：代码已完成本地实现和专项验证，尚未执行生产部署。

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

## 自动触发与请求准入

自动重置共用手动重置的原子事务，不另写扣天逻辑。触发点包括：

- 用户开启自动开关后立即检查；
- 用量结算提交成功后检查；
- 请求进入计费准入时检查一次并最多重新判断一次；
- 服务启动时和之后每 30 秒扫描开启自动重置的有效订阅。

请求在等待用户或账号并发槽位后，会再次读取数据库中的订阅状态、分组权限、额度和有效期。OpenAI、Anthropic、Gemini、Web Search 以及 OpenAI WebSocket 后续 turn 均接入了这层复核，避免排队期间到期或额度耗尽后仍转发请求。

## 前端行为

用户订阅卡片提供：

- 每日额度旁的“重置”按钮；
- 自动重置每日额度开关；
- 当日 `N/100` 次成功次数；
- 请求未确定时的“正在核实”状态。

只有获得分组许可的月卡显示完整控件。周卡、季卡和其他分组不显示。周/月额度耗尽时手动按钮真实禁用，但用户仍可关闭自动开关。前端将操作 ID 写入 `sessionStorage`，页面重载后先查询操作结果，再决定是否复用原请求。

当前已覆盖中文和英文，并检查了桌面 1440×1000 与移动 390×844 布局。

## 验证记录

已通过：

- PostgreSQL 18.1 + Redis 8.4 集成测试：并发重置、幂等重放、每日 100 次上限、事务回滚、窗口维护和用户锁。
- 后端月卡专项回归：领域、服务、准入、槽位复核、退款并发和接口测试。
- `go build -p 1 ./cmd/server`。
- DTO 与前端字段契约测试。
- 前端全量测试：257 个测试文件、1881 项测试通过。
- 前端 `typecheck`、生产构建、变更文件 ESLint 和 `git diff --check`。
- API Mock 页面验收：中文/英文、桌面/移动端、响应丢失后的单操作 ID恢复。

完整验证明细见 [实施计划与验证日志](superpowers/plans/2026-09-06-monthly-subscription-reset.md)。

## 部署前注意事项

- 当前只完成代码和本地验证，迁移尚未对生产数据库执行。
- 上线前先备份数据库，在所有实例完成升级后再按分组逐步打开许可。
- 需要在目标环境运行迁移、PostgreSQL 集成测试和数据库负载验证。
- 全量服务测试中仍有 5 项与本功能无关的失败：4 项 Windows 插件包文件占用错误已在原始基线复现；1 项内容审核缓存定时用例仍需单独处理。不能将全量服务测试描述为完全通过。
