# 月卡与兑换码审查

2026-11-18，北京时间。审查基线为本地 `0ed0ee3b9` 和已发布的 `0.2.21-r6` 应用源码 `2516693b3`。两者的月卡与兑换码后端代码一致。结论来自本地源码和测试，线上订单与账户数据待按订单单独核对。

## $1,120 月卡续费

本地以已付的同分组 30 天订单复现。旧卡还有效且已用满 $1,120 时，订单进入 `COMPLETED`，到期时间延长 30 天，月用量仍为 $1,120，余额为 $0。日额度重置被月限额拦下。到期当刻及到期后履约会开启新一期，月用量清零，月上限仍为 $1,120。独立验证覆盖付款回调重试、原月窗口自然刷新和额度准入。[付款履约](../../backend/internal/service/payment_fulfillment.go#L629) · [续期](../../backend/internal/service/subscription_service.go#L332) · [日重置](../../backend/internal/service/subscription_daily_reset.go#L142)

原测试分别覆盖有效期延长、自动开关保留、重复回调和月额度拒绝。验收缺少「月额度用尽 → 用户付款成功 → 立即发起请求」的联动场景，购买页也没有给出额度生效时间。首次开通的月窗口可从首次使用起算，复测中它比原到期时间晚两小时刷新。[原验收](../superpowers/specs/2026-09-06-monthly-subscription-reset-tests.md#L142) · [窗口初始化](../../backend/internal/service/subscription_service.go#L971) · [月窗口刷新](../../backend/internal/service/user_subscription.go#L196)

建议用单一续费按钮。相同普通月卡已结算的基础月余额和一次性补额都用尽时，付款生效后立即开启新一期。旧期仍有额度时，续费在旧期结束后生效，付款前展示时间，已经预付的后续期次继续保留。普通订阅码是否采用相同生效规则、历史受影响订单如何补偿、部分退款如何撤销原订单权益，需单独定口径。

## 其他审查发现

| 优先级 | 问题和影响 | 证据 |
| --- | --- | --- |
| 高 | 退款按用户和分组扣当前有效订阅。复现了老订单退款 $1，却扣除旧订单的完整天数并撤销后来购买的新卡。 | [退款扣减](../../backend/internal/service/payment_refund.go#L290) |
| 高 | 已核销兑换码缺少统一的服务器端不可变约束。后台过期、删除及批量更新可破坏历史，并有重复发放和使用过时分组或有效期的风险。 | [后台操作](../../backend/internal/service/admin_user.go#L1327) · [批量更新](../../backend/internal/repository/redeem_code_repo.go#L295) · [核销](../../backend/internal/service/redeem_service.go#L448) |
| 高 | 管理员对从未使用的订阅只重置部分窗口后，其余窗口可能没有起点，后续累积用量无法按期自然刷新。 | [窗口激活](../../backend/internal/service/user_subscription.go#L106) · [部分重置](../../backend/internal/repository/user_subscription_repo.go#L412) |
| 高 | 请求跨日、周、月窗口或人工重置时，扣费归属受结算和窗口推进的先后顺序影响。期次标识可区分新旧订阅期，窗口归属还需要统一。 | [计费身份](../../backend/internal/service/gateway_usage_billing.go#L337) · [用量分摊](../../backend/internal/repository/subscription_supplement_billing.go#L43) |
| 高 | 已受理的 Grok 或 Seedance 视频仍会在查询时检查当前额度。订阅耗尽或到期可能卡住查询、结算及预占释放，即使任务已经绑定原计费期次。 | [查询准入](../../backend/internal/handler/grok_media.go#L166) · [结算](../../backend/internal/handler/grok_media.go#L524) |
| 中 | 超出过期宽限期的成功付款回调可被接收并记审计，但订单仍停在过期且普通重试无法履约，已在本地复现。首次购买同组两笔订单的并行创建还有一笔撞唯一索引后等待重试的路径。 | [迟到回调](../../backend/internal/service/payment_fulfillment.go#L217) · [首次履约事务](../../backend/internal/service/payment_fulfillment.go#L629) |
| 中 | 固定码的恢复路径未比对金额、类型、分组和天数。普通批量发码逐张提交，断线重试可能留下部分批次或额外发码。普通订阅码生成接口对负数天数的实现与接口说明不一致。 | [固定码](../../backend/internal/handler/admin/redeem_handler.go#L213) · [批量发码](../../backend/internal/service/admin_user.go#L1298) |
| 中 | 兑现成功后的响应仍依赖一次收据查询。查询失败时界面可能报失败，再试只返回已使用。第二张 QQ 福利卡实际叠加额度，兑换成功和历史却可显示开通 30 天。 | [返还收据](../../backend/internal/service/redeem_service.go#L612) · [QQ 授予](../../backend/internal/service/welfare_subscription.go#L110) · [兑换结果](../../frontend/src/views/user/RedeemView.vue#L113) |
| 中 | API Key 用量页的剩余额度仍只算基础额度。预占失败也可能把日、周或在途容量不足报成月额度不足。同组福利只剩少量额度时，部分请求会先选福利，随后因报价超出而拒绝，即使普通卡有足额余额。 | [用量响应](../../backend/internal/handler/gateway_handler.go#L1859) · [预占](../../backend/internal/repository/subscription_supplement_reservation_repo.go#L67) · [权益选择](../../backend/internal/service/subscription_admission.go#L44) |

另外核对了标准无补额订阅的并发计费、QQ 福利追回、同组普通与福利订阅隔离、老码迁移以及同一期管理员恢复。普通无补额可变费用目前为准入后按实际结算，并发在途可能超过阈值。管理员恢复未关闭的旧福利期限时会保留原期用量和剩余补额。后续可分别确认硬上限与历史恢复的产品口径。[在途计费](../../backend/internal/handler/gateway_inflight_reservation.go#L87) · [福利调整](../../backend/internal/service/welfare_subscription_adjustment.go#L72) · [旧码兼容](2026-11-17-subscription-supplements.md)

## 本轮展示调整

按最新要求，将有效且设有月限额的订阅补额合并到「我的订阅」与顶栏订阅提示的原月额度行，只显示一个月剩余金额。该金额为基础月余额加上本期未用补额。月度余额按月限额展示，日或周限额更紧时，当前可用额可能更少。后台和兑换历史仍保留授予与消费明细。

以上业务规则和账务发现待决策。本轮实施范围为前端展示。

本地验证覆盖五个后端包的订阅、兑换码、重置及视频绑定单元测试。付款与退款的复现使用 SQLite 测试订单及本地订阅仓储夹具。前端定向测试覆盖支付、兑换、订阅、API Key 用量及词条完整性，共 30 文件、320 项通过。类型检查、变更文件 ESLint 和生产构建通过。线上订单、实际运营数据及真实支付渠道未参与本轮验证。
