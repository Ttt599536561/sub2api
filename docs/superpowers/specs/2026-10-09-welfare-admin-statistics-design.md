# 福利管理统计设计（2026-10-09）

用户已确认：在二开 0.2.14-r1 基础上新增管理员福利统计；“额外赠送”是连续签到额外奖励。2026-10-09 已授权实施。

## 范围与口径
- /admin/welfare 保留入口，默认展示“福利统计”，另一页签“活动设置”保留原功能。
- 筛选支持北京时间今天、昨天、近7/30天、本月、自定义起止日；按用户邮箱/ID搜索。统计采用保存的 business_date，包含起止日。
- 默认近7天；单次日期范围最多366天，历史累计始终涵盖全历史。
- 所选期间奖励总览、补零每日统计、分页用户累计列表、分页奖励明细；点击日期定位当日明细、点击用户定位该用户明细。
- daily=基础签到，streak=连续签到额外，draw=抽奖；签到奖励=daily+streak；福利总奖励=daily+streak+draw。redeem 是余额转移，不计发放。
- 人数 distinct user_id；签到次数/天数数 daily；抽奖次数数 draw；参与人数将三类奖励用户合并去重。每日签到人数不因 streak 多算。
- 用户列表同时展示 period（日期范围）及 lifetime（全历史）指标；历史累计不被日期过滤缩减。只展示有历史奖励记录的用户，保留已删除用户的既有财务事实。
- 仅管理员可查询，暂停活动仍可查询，不改变发奖、抽奖、兑换或活动规则；所有金额以美元两位小数字符串传输，SQL分单位整数/精确数汇总。
- 列表允许以 total_amount、period_total_amount、checkin_count 排序，稳定用户ID次排序；分页最大100。

## API合同
GET /api/v1/admin/welfare/statistics
GET /api/v1/admin/welfare/statistics/users
GET /api/v1/admin/welfare/statistics/records
共同query: date_from/date_to (YYYY-MM-DD), search (邮箱片段或完整用户ID), user_id (可选正整数)。列表另有 page/page_size；用户列表另有 sort_by=total_amount|period_total_amount|checkin_count 和 sort_order=asc|desc，默认total_amount desc；明细另有 type=all|daily|streak|draw。

RewardTotals: daily_amount, streak_amount, checkin_amount, draw_amount, total_amount（均string）；checkin_users, checkin_count, streak_users, draw_users, draw_count, participating_users（均number）。
统计响应: {date_from,date_to,timezone:'Asia/Shanghai',summary:RewardTotals,daily:Array<RewardTotals & {date:string}>}。
用户响应: {items:Array<{user_id:number,email:string,period:RewardTotals,lifetime:RewardTotals}>,total,page,page_size}。
明细响应: {items:Array<{id:string,user_id:number,email:string,type:'daily'|'streak'|'draw',created_at:string,business_date:string,amount:string,cycle_day?:number}>,total,page,page_size}。
空结果使用空数组/金额0.00/计数0；daily仍补齐所选所有日期。

## 验收
1. 同一天同用户daily+streak只计1名签到用户，多次draw分别计次数、人数去重。
2. 用户期间收益与历史累计收益分开；redeem不影响累计发放。
3. 按北京时间业务日含起止两天；无数据日补零；无效日期/反向范围/超限/无效用户、类型和排序参数返回400。
4. 普通用户/未登录用户无法访问；暂停不阻断历史统计；接口不泄漏操作内部审计或随机信息。
5. 分页排序稳定；邮箱关键字参数化且按字面匹配；金额汇总准确；切换筛选不显示过期请求的结果。
6. 后端服务/HTTP/真实数据库统计测试、前端行为/类型/i18n/构建通过；浏览器检查桌面、手机、日期筛选、明细与设置页签。

## 后续功能
趋势图与CSV导出留待后续，不包含充值赠送、管理员手动赠送或修改奖励规则。
