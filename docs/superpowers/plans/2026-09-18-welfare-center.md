# 福利中心 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. The original acceptance items below are retained for reference; current completion evidence is in the verification record.

**Goal:** 在本二次开发 Sub2api 上新增独立福利余额、每日签到、连续奖励、消费满额抽奖、手动兑换和可筛选流水，保证账务原子、即时发放抽奖资格及前端会话隔离。

**Architecture:** 新建福利领域 service/repository/handler 与一对一用户钱包。签到和抽奖只改变福利钱包；兑换事务同时修改福利钱包与原账户余额。普通扣费及批量图片 capture 在原结算事务内同步累计消费和次数，双缓存失效使用提交后执行与持久重试。

**Tech Stack:** Vue 3、TypeScript、Pinia、Vue Router、Tailwind、Vue i18n；Go、Gin、Ent/SQL、PostgreSQL、Redis、Wire；Vitest、Go unit/integration testcontainers。

---

状态：用户已确认[签到 v2](../specs/2026-09-19-welfare-reward-v2-proposal.md)并授权开发；2026-09-20 已完成本地实现、审查、回归与联调。功能在 `codex/welfare-center` 工作树开发，涵盖数据库、服务、计费接入、API 和用户/管理员页面。日奖以 v2 为准，旧 v1 仅为成本对照。下文保留原始任务边界和验收契约；实际完成情况及验证证据见 [收尾验证记录](../../WELFARE_CENTER_VERIFICATION_2026-09-19.md)。

连续签到奖励金额属于所有者／开发内部配置。领取前显示「神秘奖励」；固定额外奖励仍为 60／200／500 cents，消费抽奖奖表不变；日奖采用 v2 状态保护与 $1 内部上限，页面布局不变。

签到按钮下方采用「今日惊喜，签到揭晓」，页面、活动规则和用户 rules／overview 不预告每日金额范围；日奖上下限与概率仅保留在后端内部规则中。已到账历史与抽奖奖项正常显示。

资料保存说明：现有 `.gitignore:135` 忽略 `docs/*`，福利设计、计划、功能说明与验证记录需在功能提交时显式加入版本管理。

依据：`docs/superpowers/specs/2026-09-18-welfare-center-design.md` 与 `2026-09-18-welfare-reward-algorithms.md`。仓库基线 `f89835007`。当前 SQL 最大前缀为 238；计划新建 `239_welfare_center.sql`，执行当天如已占用则使用下一个未占用编号，不重命名历史迁移。

## Task 1：数据库、规则版本与钱包领域

**Create**

- `backend/migrations/239_welfare_center.sql`
- `backend/ent/schema/welfare_program.go`
- `backend/ent/schema/welfare_wallet.go`
- `backend/ent/schema/welfare_checkin.go`
- `backend/ent/schema/welfare_operation.go`
- `backend/ent/schema/welfare_ledger.go`
- `backend/ent/schema/welfare_spend_event.go`
- `backend/ent/schema/welfare_balance_outbox.go`
- `backend/internal/service/welfare.go`：领域类型、仓储接口、统一错误码。
- `backend/internal/repository/welfare_repo.go`：钱包初始化、锁、概览、幂等操作公共 SQL。
- `backend/internal/repository/welfare_repo_integration_test.go`

职责与字段按设计第 6 节执行。福利奖励以 BIGINT cents，消费净额以 NUMERIC(20,8)。新增 `CHECK balance_cents>=0`、`draws_used>=0`、`cycle_day BETWEEN 0 AND 30`、`daily_low_count BETWEEN 0 AND 3`；`daily_low_count` 默认 0，跨漏签／轮次持久保留，总天数只增；原始消费来源与退款来源各有唯一键。

钱包保留整体 `wallet_version`，另有仅福利余额变化才递增的 `welfare_balance_version`；兑换报价使用后者，不调整首版消费累计的存储位置。

- 为默认空钱包、唯一签到日期、唯一操作键、关联用户删除约束、流水索引写集成用例，先确认缺表失败。
- 写增量迁移与 Ent schema，允许未参与用户按需初始化；不全量扫描 usage_logs。
- 固化已确认 v2 日奖快照：状态 0–3 对应惊喜概率 20/30/50/100%；普通表总权重 400（1 分为 8，2–9 分各 49）；惊喜区间权重 65/25/9/1，区间为 10–20／21–35／36–50／51–100 分。消费抽奖仍为 1000 权重、里程碑仍为 60/200/500 cents；新规则创建新版本，不改历史快照。
- 在 backend 目录执行 `go generate ./ent`，检查生成差异仅为新模型。
- 运行 `go test -tags=integration ./internal/repository -run Welfare -count=1 -v`，确认 PostgreSQL 测试真实运行而非 skipped。

验收：金额字段没有混用 balance；空钱包为 0；迁移重复运行按项目 runner 规则安全；现有表及旧迁移 checksum 不改变。

## Task 2：奖励采样、日界线与连续签到

**Create**

- `backend/internal/service/welfare_random.go`、`welfare_random_test.go`
- `backend/internal/service/welfare_checkin.go`、`welfare_checkin_test.go`
- `backend/internal/repository/welfare_checkin_repo.go`
- `backend/internal/repository/welfare_checkin_integration_test.go`

使用算法文档中的整数奖表及 `crypto/rand.Int`，依赖注入随机源和时钟供测试。不要用按时间种子、前端 Math.random 或 float 权重。

```text
today = clock.now.in(Asia/Shanghai).date
BEGIN
  lock welfare_wallet
  if checkin exists(user,today): return saved result
  if last_date==today-1 AND cycle_day<30: new_day=cycle_day+1
  else: new_day=1; new cycle_id
  n = wallet.daily_low_count
  surprise = uniformInt(100) < [20,30,50,100][n]
  if surprise:
    tier = sample weights[65,25,9,1]
    daily = uniform cents within tier[10..20,21..35,36..50,51..100]
    next_low_count = 0
  else:
    daily = sample ordinary weights400(1:8, 2..9:49 each)
    next_low_count = n+1
  bonus = {7:60,15:200,30:500}[new_day] default 0
  wallet.balance_cents += daily + bonus
  wallet.total_checkin_days += 1
  wallet.last_checkin_date = today
  wallet.cycle_day = new_day; persist current/new cycle_id
  wallet.daily_low_count = next_low_count // 漏签/轮次只影响 new_day，不预先清零 n
  wallet.wallet_version += 1
  wallet.welfare_balance_version += 1
  insert deterministic operation(checkin,user,today) with private n_before/n_after/random_inputs/result
  insert checkin referencing operation; insert daily ledger; if bonus>0 insert bonus ledger
COMMIT
```

- 写边界测试：首次、同日重复、昨天、第 6→7、14→15、29→30、30→次日1、漏日、跨月跨年、北京时间23:59→00:00。
- 枚举四种状态各 100 个分支取样值，验证 20/30/50/100% 条件概率及最多 3 次连续普通；枚举普通 400 权重、惊喜 100 权重及区间端点／每分覆盖，复核分支期望 $0.0541／$0.21375。
- 用四状态 DP 验证初态 0 首 30 次惊喜数 11.1384311653、日奖期望 $3.4012505355；稳态日奖约 $0.11457348485。不把有保护状态的 30 次奖励当独立抽样，不用小样本波动断言。
- 验证漏签／自然月／第30→1天均保留 daily_low_count；普通日奖叠加里程碑不能误判为惊喜；新惊喜只清日奖计数，不重置周期天数。
- 实现事务签到；随机源失败则回滚，不能计签到天数、推进 daily_low_count 或发部分奖励。
- 所有钱包变更事务统一递增 wallet_version；仅福利余额变化才递增 welfare_balance_version。签到操作内部保存私有随机值与结果快照，所有流水引用同一操作，用户 DTO 不暴露 audit_u。
- 测试 100 个同用户同日并发请求只有 1 次签到和最多 2 条奖励流水。
- 执行 `go test ./internal/service -run Welfare -count=1` 与 `go test -tags=integration ./internal/repository -run WelfareCheckin -count=1 -v`。

验收：累计天数、周期天数与日奖保护计数三者区分；每日和额外奖励均存在；重试同日返回原金额、不重新随机或推进保护计数；过期连续进度在概览中按业务日期正确计算。

## Task 3：将消费满额机会接入实际扣费事务

**Modify**

- `backend/internal/repository/usage_billing_repo.go`：普通 `applyUsageBillingEffects` 与批量图片 `captureUsageBillingBatchImageBalance`。
- `backend/internal/service/usage_billing.go`：复用已有量化契约，必要时在 command 补充稳定结算来源／活动批次。
- `backend/internal/service/gateway_usage_billing.go`：约束旧 fallback，保证启用福利时生产使用统一仓储。
- `backend/internal/service/usage_service.go`：明确内部旧路径不得绕过消费事件。

**Create**

- `backend/internal/repository/welfare_spend_repo.go`：接收现有 `*sql.Tx`，禁止自行开第二个事务。
- `backend/internal/repository/welfare_spend_integration_test.go`
- `backend/internal/service/welfare_billing_coverage_test.go`

```text
existing settlement transaction:
  claim existing billing dedup key
  apply actual user balance deduction / image capture
  if settled_at>=launch_at and reward accrual enabled and actual_cost>0:
    insert welfare_spend_event with unique source
    lock/update welfare_wallet after users row lock
    S += exact database-quantized actual_cost
    E = floor(S/50)
  commit original transaction
```

- 写精度用例：`49.99999999 + 0.00000001 => 1`；`$126 => 2` 次及 `$26` 余数；100 次 `$0.00000001` 不丢精度。
- 加普通扣费接点，仅认真实 `BalanceCost`；订阅额度、配额扣减、充值、免费请求不计。
- 加批量图片 capture 接点，仅认量化后的 `ActualAmount`；例如冻结 $60、实际消费 $45、返还 $15，合格消费是 $45，不是 -$15 或 $60。
- 用同一请求重复／并发、dedup archive 命中、结算回滚验证机会只发一次。
- 验证异步视频完成后首次结算计一次；创建任务／轮询本身不计。
- 从生产 Wire 注入与调用图验证所有实际余额扣费路径已覆盖，无法确认的 fallback 不允许开启福利；只读 UsageService 遗留路径加显式约束或统一接入。
- 验证上线边界以服务端结算时刻为准：上线前创建而上线后结算的异步任务计入，历史已结算请求重试不补发。
- 执行 `go test -tags=integration ./internal/repository -run 'WelfareSpend|UsageBilling' -count=1 -v`。

验收：扣费提交后的概览立刻读到资格；消费累计不用非可靠日志；事务内不访问 Redis、不发网络请求；锁顺序 users→wallet，压测比较启用前后计费延迟与锁等待。

## Task 4：抽奖原子操作与财务流水

**Create**

- `backend/internal/service/welfare_draw.go`、`welfare_draw_test.go`
- `backend/internal/repository/welfare_draw_repo.go`
- `backend/internal/repository/welfare_draw_integration_test.go`
- `backend/internal/repository/welfare_ledger_repo.go`

事务内先检查幂等结果，再锁钱包、算 `available=max(floor(S/50)-draws_used,0)`，验证次数，采样，记录已用次数和中奖结果，增加福利余额，写抽奖流水。操作幂等键+请求摘要必须一致。

- 枚举 1000 个随机整数验证奖表边界和 98%≤$5；无次数不能调用随机源。
- 一次抽奖在操作表保存内部随机整数、规则版本、奖项、金额、次数及余额快照；只向用户返回公开结果。
- 测试多标签页 1 次机会并发抽 10 次，最多一个新操作成功；相同操作重复返回同一奖品。
- 测试断网重试、随机错误、提交失败、事务中断时不重复发奖或漏扣次数。
- 执行 `go test ./internal/service -run WelfareDraw -count=1` 和 `go test -tags=integration ./internal/repository -run WelfareDraw -count=1 -v`。

验收：每次抽奖一个结果／一条财务流水；原型演示序列不会进入真实实现；中奖概率对所有账户一致。

## Task 5：手动兑换、确认报价与可靠缓存失效

**Create**

- `backend/internal/service/welfare_redemption.go`、`welfare_redemption_test.go`
- `backend/internal/repository/welfare_redemption_repo.go`
- `backend/internal/repository/welfare_redemption_integration_test.go`
- `backend/internal/service/welfare_balance_outbox.go`、`welfare_balance_outbox_test.go`
- `backend/internal/repository/welfare_balance_outbox_repo.go`

**Reference without reuse of recharge semantics**

- `backend/internal/service/affiliate_service.go` 双缓存失效。
- `backend/internal/repository/affiliate_repo.go` SQL 转账结构，但不能带入 total_recharged 更新。
- `backend/internal/service/redeem_service.go` 邀请返利逻辑必须避免调用。

**Modify for cache correctness**

- `backend/internal/service/api_key_auth_cache_impl.go`：可靠 worker 可调用的 error-returning 删除／广播路径，不能仅复用吞错的 void wrapper。
- `backend/internal/repository/billing_cache.go`、`backend/internal/service/billing_cache_service.go`：新增余额缓存 generation／CAS 回填保护，并检查包含资金快照的 auth cache 相同竞态。

- 写测试：$0.01、全部、部分、0、负数、超过余额、3 位小数、巨大输入；拒绝指数输入和非有限值。
- 实现无资金副作用的报价，返回正整数分对应金额字符串和 welfare_balance_version；兑换确认携带同一余额版本及金额。wallet_version 仍只作页面状态同步，不能用纯消费更新使报价过期。
- 实现 users→wallet 锁顺序，先检查已提交的幂等结果再检查 welfare_balance_version，避免已成功操作重试时被版本冲突遮蔽。测试报价后持续 API 扣费仍可兑换，福利余额实际变化才要求重新报价。
- 一个 SQL 事务执行福利扣分、账户增精确十进制、兑换流水、幂等结果、缓存 outbox；账户冻结余额不变。
- 提交后立即尝试双缓存失效；失败仍返回已成功转账结果并后台重试，避免把缓存失败当账务失败。worker 使用可返回 error 的删键／跨实例广播接口，全部完成才标记 outbox 成功；不能凭 `InvalidateAuthCacheByUserID` 的 void 返回确认成功。
- 新增资金缓存 generation 栅栏：读 DB 前获取 generation，回填用原子 CAS；失效先提升 generation 再删键，旧读回填遇代次变化即丢弃并重取。当前订阅／平台配额版本不等于余额版本，不宣称能直接复用。测试持有旧 DB 读→兑换提交→释放旧读、Redis失败、outbox重试及跨实例认证缓存失效。
- 测试不会增加 `total_recharged`、不会触发邀请返利、不会直接增加消费进度；真实消费兑换所得资金时才计入。
- 执行 `go test ./internal/service -run 'WelfareRedemption|WelfareBalanceOutbox' -count=1` 和相关 PostgreSQL/Redis 集成测试。

验收：任意并发与重试下，两种余额金额守恒；福利余额不为负；账户实时可用金额最终与数据库一致。

## Task 6：消费退款修正与对账

**Create**

- `backend/internal/service/welfare_spend_adjustment.go`、`welfare_spend_adjustment_test.go`
- `backend/internal/repository/welfare_spend_adjustment_repo.go`
- `backend/internal/repository/welfare_reconciliation_integration_test.go`

首版先建立被明确调用的内部退款／修正能力，不扩展用户退款入口或另做复杂运营后台。未发生费用退款时不触发。

- 接口参数包括原消费事件、退款金额、操作员、理由、幂等键；累计退款不得超过原事件实际金额。
- 在账户费用退款同事务写负向消费事实、递增钱包版本、写 `welfare_balance_outbox`，重算可获次数；提交后立即尝试双缓存失效及后台可靠重试。冻结释放和外部充值退款不能调用此接口。
- 测试 `$100 已抽2次 → 退$20`：S=$80、可用0、待抵扣1；再消费$20后待抵扣0但可用仍0，再消费$50可用1。
- 核对 `钱包余额 = Σ福利流水delta`、`净消费 = Σ合格消费事件`、`draws_used = 成功抽奖操作数`、`总签到天数 = 唯一签到记录数`。
- 已抽奖励默认保留，调整资格通过消费事件审计，不擅自给用户四种福利财务类型增加一种用户未要求的入口。

验收：退款幂等、原始事件可追溯，不会通过重复消费退款保留无限抽奖机会。退款默认政策随设计一起审阅。

## Task 7：API、鉴权、开关和依赖注入

**Create / Modify**

- Create `backend/internal/handler/welfare_handler.go`、`welfare_handler_test.go`。
- Create `backend/internal/handler/dto/welfare.go`。
- Modify `backend/internal/server/routes/user.go`、`backend/internal/handler/wire.go`、`backend/internal/service/wire.go`、`backend/internal/repository/wire.go`，并按当前 `Handlers` 容器和 `cmd/server` Provider 接入。
- Modify 设置链路：`service/domain_constants.go`、`service/settings_view.go`、`service/setting_service.go`、`handler/dto/settings.go`、`handler/setting_handler.go`、`handler/admin/setting_handler.go`。

- 按设计第 7 节实现 overview/calendar/check-in/draw/redemption-quote/redeem/operations/records/rules，复用 JWT/限流/审计。
- 为公开规则和用户 DTO 建立白名单：rules 的里程碑只有 day／神秘奖励标签，overview 另含状态，不下发未到账金额或内部奖表／预算。成功签到、本人操作结果及历史流水只披露已提交的实际到账金额，不附带未来档位金额；calendar 不提供连续奖励金额预告。
- 增加 API 响应测试：覆盖全未领取、部分已领取、成功到账与幂等重放；断言 rules／overview／公开设置均无里程碑金额泄露，个人已到账结果与账本金额准确且不能越权读取。
- 金额 JSON 全部字符串；财务流水强制当前 subject，测试别人的 operation ID、恶意 user_id、未知 type、逆序日期、超大 page_size。
- 概览用一致性数据库快照或单查询，避免分别读金额和次数时跨事务混合；操作响应携带新 wallet_version。
- 活动首次启用记录不可后移的 launch_at。暂停获奖后仍保留已有权益查看／兑换，UI 访问和发奖开关分开表达。
- 首次未上线且无权益时入口隐藏；停止活动有权益时保留入口并标注已暂停。SSR 注入和 API 返回开关保持一致。
- 执行 `go generate ./cmd/server` 后检查 Wire 差异；运行 handler、routes 和 `public_settings_injection_schema_test` 相关测试。

验收：只有服务端能决定资格和金额；菜单隐藏不能替代服务端鉴权；停活动不会锁住既有余额。

## Task 8：前端页面、按钮状态和移动端

**Create**

- `frontend/src/views/user/WelfareView.vue`
- `frontend/src/api/welfare.ts`
- `frontend/src/types/welfare.ts`
- `frontend/src/composables/useWelfare.ts`：页面级状态、会话代次、操作幂等键和刷新；首版避免不必要全局 store。
- `frontend/src/components/welfare/WelfareSummary.vue`
- `frontend/src/components/welfare/CheckInCard.vue`
- `frontend/src/components/welfare/CheckInCalendar.vue`
- `frontend/src/components/welfare/LotteryCard.vue`
- `frontend/src/components/welfare/RedemptionDialog.vue`
- `frontend/src/components/welfare/RewardHistory.vue`
- `frontend/src/components/welfare/WelfareRulesDialog.vue`
- `frontend/src/i18n/locales/zh/welfare.ts` 与 `en/welfare.ts`

**Modify**

- `frontend/src/components/layout/AppSidebar.vue`：`buildSelfNavItems` 插入福利中心。
- `frontend/src/router/index.ts`、`router/meta.d.ts`、`utils/featureFlags.ts`。
- `frontend/src/types/index.ts`、`api/admin/settings.ts`、`views/admin/SettingsView.vue` 的必要开关，不新增整套运营系统。
- `frontend/src/i18n/locales/zh/index.ts`、`en/index.ts` 及 `common.ts` 导航词条。

- 先写接口类型和页面状态契约；页面放进现有 AppLayout，不复制原型的静态 sidebar/header。
- 复用 BaseDialog、EmptyState、LoadingSpinner、Pagination、既有按钮与暗色 token，实现顶部指标、实际月份日历、奖励进度、抽奖、流水。在免费抽奖卡片的奖池下方放独立「福利兑换」区，包含细分隔线、实时可兑换余额及整行「兑换余额」按钮；手机端保持相同顺序。页头只保留活动规则，兑换资格不依赖签到或抽奖，抽奖次数为 0 仍可兑换。
- 保持 7／15／30 天卡片布局，仅展示天数、「神秘奖励」及未达成／已获得状态；活动规则不预告金额，已获得状态不泄露未来档位。前端常量、i18n、SSR、隐藏 DOM、tooltip、ARIA 均不得内置未到账连续奖励金额，不能以 CSS 隐藏代替接口裁剪。
- 抽奖结果仅展示已提交响应；签到成功增加福利余额和天数，**账户余额只有兑换时才改变**。
- 兑换弹窗支持全部报价／手动金额／过期报价；不在浏览器浮点相加来决定实际余额。
- URL query 保存流水过滤和页码，变化后回到第一页；先按单选类型实现「全部／四类」，按原需求理解“筛选”为单选。
- pending／成功／空态／网络失败／提交成功刷新失败分开；同一操作重试复用 key。
- 页面 focus、可见性恢复、业务日变化刷新状态；旧会话响应与低 wallet_version 响应丢弃。
- 移动端顺序为余额、两项统计、签到、抽奖、流水；完成浅色／暗色及键盘焦点验收。

验收：卡片数据来自后端；累计签到不等于连续天数；签到和中奖不误加账户余额；第7天只有一次签到但两条财务流水。

## Task 9：端到端验证与灰度上线

**Create / Extend Tests**

- `frontend/src/views/user/__tests__/WelfareView.spec.ts`
- `frontend/src/components/welfare/__tests__/RedemptionDialog.spec.ts`
- `frontend/src/composables/__tests__/useWelfare.spec.ts`
- Extend `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`
- Extend `frontend/src/router/__tests__/feature-access.spec.ts`

- 先跑修改模块定向测试，修复失败后再跑完整必要检查；不依赖蒙特卡洛小样本碰运气判概率。
- frontend 目录：`pnpm test:run src/views/user/__tests__/WelfareView.spec.ts src/components/welfare/__tests__/RedemptionDialog.spec.ts src/composables/__tests__/useWelfare.spec.ts src/components/layout/__tests__/AppSidebar.spec.ts src/router/__tests__/feature-access.spec.ts`。
- frontend 目录：`pnpm typecheck`、`pnpm lint:check`、`pnpm build`；build 会校验 i18n 键完整性。
- backend 目录：`go test ./...`、`go test -tags=unit ./...`、`go test -tags=integration ./internal/repository -run 'Welfare|UsageBilling' -count=1 -v`；检查 testcontainers 未跳过，执行项目既有 lint 检查。
- 在隔离测试账户验证：初始0 → 每日签到 → 第7天 → API 消费过$50 → 抽奖 → 部分兑换 → 全部兑换 → API再次扣款 → 退款修正；每一步核对数据库流水与 UI。
- 前端与 API 联合验证神秘奖励：领取前及部分已领取时检查页面、规则弹窗、网络响应和隐藏属性无未来里程碑金额及每日金额上下限；成功后仅实际结果／本人流水揭示已到账额。按签到规则语义检查泄露，不误删签到历史金额和抽奖中合法公开的同额奖项。
- 多标签页并发签到／抽奖／兑换；跨午夜；网络超时；Redis停机；账号切换；320/390/768/1024px；暗色模式；日期端点。
- 比较同步资格累计前后的同用户并发计费延迟、锁等待、死锁计数；未达既有计费 SLO 则优化事务后复验。
- 部署兼容迁移及默认关闭代码；记录实际 launch_at、已确认 v2 日奖版本、原消费抽奖奖表、灰度账户与发放成本，再开放入口。
- 准备关闭获奖、保留兑换和账本的回退开关；禁止删钱包／流水来回滚。

## 验收总表

| 用户需求 | 任务 |
|---|---|
| 侧栏准确位置、新页面、美观交互 | 8、9 |
| 独立福利余额／累计天数／抽奖次数 | 1、2、7、8 |
| 日奖范围与概率、连续7/15/30额外奖励 | 2 |
| 30天循环、断签重置、无补签 | 2 |
| 上线后真实消费满50即刻获得机会 | 3 |
| 七个奖项、至少70%≤5且优先控成本 | 4 |
| 无门槛部分／全部兑换、原账户余额到账 | 5 |
| 四类流水、日期与类型筛选、次数 | 4、7、8 |
| 资金安全、重试、缓存和退款一致性 | 1、3、4、5、6、9 |

原始建议实现顺序：1→2；3 可与 2 的纯奖励规则开发并行，但同一计费文件仅一个任务持有写权限；4、5 在钱包接口确定后并行；7 汇总后接 8，最终统一执行 9。Task 1–8 的功能已实现，Task 9 的本地回归、真实数据库验证和页面联调已完成；生产部署、容量验收与开放活动属于后续发布操作。
