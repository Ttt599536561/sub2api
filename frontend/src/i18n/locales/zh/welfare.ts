export default {
  welfare: {
    recovering: '正在核对上次操作结果…', pendingHint: '上次操作结果尚未确认。重试会继续原操作，不会重复扣款。', pendingDraw: '待确认的抽奖', retryDraw: '重试上次抽奖', pendingRedemption: '待确认的兑换：{amount}', checkResult: '查询操作结果',
    title: '福利中心', subtitle: '每天一点积累，让每一次使用多一份回馈。',
    balance: '福利余额', balanceHint: '可随时 1:1 兑换账户余额', draws: '剩余抽奖次数', drawsUnit: '次', totalDays: '累计签到', daysUnit: '天', cycle: '本轮连续 {days} 天', threshold: 'API 余额每消费 {amount} 获得 1 次',
    subscriptionThreshold: '套餐单笔实付每满 ¥{amount} 送 1 次', subscriptionHint: '仅人民币订单；每笔独立计算，余数不累计。',
    rules: '活动规则', probabilities: '奖项概率', close: '我知道了', loading: '正在加载…', retry: '重试', processing: '处理中…', paused: '签到与抽奖暂时暂停，已有福利余额仍可兑换。',
    checkIn: '每日签到', checkNow: '立即签到', checked: '今日已签到', surprise: '今日惊喜，签到揭晓', calendar: '签到日历', prevMonth: '上个月', nextMonth: '下个月', today: '今天', signed: '已签到', notSigned: '未签到', calendarTimezone: '按北京时间更新', weekdays: '一 二 三 四 五 六 日', milestone: '连续 {days} 天', mystery: '神秘奖励', claimed: '已到账', unlocked: '签到揭晓', remainingDays: '还差 {days} 天',
    checkSuccess: '签到成功！每日奖励 {daily}，连续签到奖励 {streak}，合计 {total} 已存入福利余额。', checkDailySuccess: '签到成功！{amount} 已存入福利余额。',
    lottery: '免费抽奖', availableDraws: '可用抽奖机会', guaranteed: '每次抽奖必得福利余额', progress: 'API 余额消费进度', spendRemaining: 'API 余额再消费 {amount}，即可获得 1 次抽奖', drawNow: '免费抽奖 · 消耗 1 次', noDraws: '暂无抽奖机会', prizePool: '奖池一览', debt: '待抵扣次数：{count}', drawResult: '抽奖结果', received: '本次获得福利余额', drawSuccess: '已存入福利余额，剩余 {count} 次抽奖机会', accept: '收下奖励',
    redemption: '福利兑换', redeemable: '可兑换福利余额', redeem: '兑换余额', exchangeNote: '1:1 转入账户余额 · 无手续费', redemptionTitle: '兑换账户余额', amount: '兑换金额', all: '全部', quote: '查看兑换金额', confirm: '确认兑换', cancel: '取消', rate: '兑换比例', rateValue: '1:1 · 无手续费', accountAfter: '兑换后账户余额', redemptionHint: '支持任意可用金额，精确到 $0.01。确认后立即转入账户余额。', invalidAmount: '请输入大于 0 的金额，最多两位小数。', insufficient: '兑换金额不能超过福利余额。', redemptionSuccess: '{amount} 已转入账户余额。', retryTransfer: '重试本次兑换', uncertainTransfer: '暂未收到兑换结果。请重试确认本次兑换，系统不会重复扣款。',
    records: '福利记录', totalDraws: '累计抽奖 {count} 次', recordType: '记录类型', from: '开始日期', to: '结束日期', reset: '重置', time: '时间', type: '类型', detail: '明细', change: '福利余额变动', recordCount: '共 {count} 条记录', prevPage: '上一页', nextPage: '下一页', empty: '该条件下暂无福利记录', footer: '30 天为一轮 · 断签重新累计 · 不支持补签 · 福利余额 1:1 兑换账户余额',
    types: { all: '全部类型', daily: '每日签到', streak: '连续签到奖励', draw: '抽奖', redeem: '兑换余额' },
    ruleCheck: '每天签到一次，领取当日惊喜。以北京时间 00:00 划分日期，不支持补签。', ruleStreak: '连续第 7、15、30 天解锁神秘奖励，达成后自动到账并揭晓金额。30 天完成一轮，次日开始新一轮；断签仅重置连续进度，累计签到天数保留。', ruleSpend: '活动开放期间，API 实际账户余额扣款每累计满 {amount}，获得 1 次抽奖机会，未满部分继续累计。充值本身及订阅额度内的 API 用量不计入。',
    ruleSubscription: '月卡、周卡等人民币订阅订单付款并完成后，单笔实付每满 ¥{amount} 赠送 1 次抽奖。每笔独立计算，余数不累计，也不与 API 美元消费合并；两种来源的抽奖次数可一起使用。',
    ruleSubscriptionEligibility: '套餐赠送仅适用于活动开放期间完成的新订单。首次开放前付款、历史已完成订单及暂停期间完成的订单不补发。',
    ruleSubscriptionRefund: '退款确认后，按该订单剩余实付金额重新计算赠送次数。多用的次数从后续获得的次数中抵扣，已获得的福利余额不收回。',
    ruleRedeem: '签到与抽奖奖励先进入福利余额，可手动按 1:1 兑换账户余额，支持部分或全部兑换，无手续费。', probabilityHint: '每次抽奖独立随机，奖励均存入福利余额。',
    errors: { WELFARE_STORAGE_UNAVAILABLE: '无法保存操作进度，请允许浏览器存储后重试。', WELFARE_OPERATION_UNRESOLVED: '上次兑换结果尚未确认，请先重试原兑换。', network: '暂时无法获取最新结果，请重试。', dateRange: '开始日期不能晚于结束日期。', WELFARE_PAUSED: '活动暂时暂停，已有福利余额仍可兑换。', WELFARE_NOT_LAUNCHED: '福利活动尚未开放。', WELFARE_NO_TICKETS: '暂无可用抽奖机会，请刷新后重试。', WELFARE_QUOTE_STALE: '福利余额已变化，请重新查看兑换金额后确认。', WELFARE_INSUFFICIENT_BALANCE: '可用福利余额不足，请重新查看兑换金额。', WELFARE_IDEMPOTENCY_CONFLICT: '本次操作信息不一致，请刷新后重试。', WELFARE_INVALID_AMOUNT: '请输入有效的兑换金额。', WELFARE_INVALID_REQUEST: '请求无效，请刷新后重试。' },
    admin: {
      title: '福利管理', description: '查看签到与抽奖奖励统计，管理福利活动开放状态', statisticsTab: '福利统计', settingsTab: '活动设置', program: '活动状态', enabledLabel: '开放签到、抽奖与消费送次数', enabledHint: '暂停期间的 API 消费和完成的套餐订单不赠送次数；用户仍可兑换已有福利余额并查看记录。', launchAt: '首次开放时间', notLaunched: '尚未开放', timezone: '活动时区', rulesVersion: '奖励规则版本', launchHint: '首次开放后，API 余额消费与人民币套餐购买分别按规则赠送次数。历史消费、历史已完成订单和首次开放前付款的订单不补发；重新开放保留已有权益。', saved: '福利设置已保存', loadFailed: '福利设置加载失败', saveFailed: '福利设置保存失败',
      stats: {
        overview: '期间奖励汇总', filterTitle: '统计筛选', timezoneHint: '北京时间（Asia/Shanghai）· 最多 366 天', range: '日期范围', search: '用户邮箱', searchHint: '输入邮箱片段', userId: '用户 ID', userIdHint: '输入完整用户 ID', apply: '应用筛选', appliedRange: '已应用：',
        presets: { today: '今天', yesterday: '昨天', last7: '近 7 天', last30: '近 30 天', month: '本月', custom: '自定义' },
        sortBy: '排序字段', sortOrder: '排序顺序', ascending: '升序（从小到大）', descending: '降序（从大到小）',
        invalidRange: '请输入有效日期，开始日期不得晚于结束日期，范围最多 366 天。', invalidUserId: '用户 ID 必须为有效的正整数。', loadFailed: '福利统计加载失败，请重试。',
        daily_amount: '基础签到奖励', streak_amount: '连续签到额外奖励', checkin_amount: '签到奖励合计', draw_amount: '抽奖奖励', total_amount: '奖励总额',
        checkinUsers: '签到人数', checkinCount: '签到次数', streakUsers: '连续奖励人数', drawUsers: '抽奖人数', drawCount: '抽奖次数', date: '业务日期',
        dailyTitle: '每日奖励', dailyHint: '日期按北京时间划分，无奖励日期显示为零。点击日期查看当日明细。', dailyUnavailable: '每日数据暂不可用，请重试汇总数据。',
        usersTitle: '用户奖励', usersHint: '期间奖励按上方筛选计算。历史累计为用户全部历史奖励，不随日期范围缩减。点击用户查看其奖励明细。', user: '用户', period: '期间', periodCheckinCount: '期间签到次数', lifetimeCheckinDays: '历史累计签到天数', periodTotal: '期间奖励合计', lifetimeRewards: '历史累计奖励分类', lifetimeTotal: '历史累计奖励总额', emptyUsers: '该条件下暂无参与用户',
        recordsTitle: '奖励明细', recordsHint: '仅展示签到和抽奖发放的奖励。明细日期可在总览日期范围内进一步缩小；筛选总览会重置明细条件。', detailScope: '当前明细范围：', clearScope: '清除明细定位', detailRangeError: '明细日期必须有效且位于已应用的总览日期范围内。', rewardAmount: '奖励金额', cycleDay: '本轮连续第几天', emptyRecords: '该条件下暂无奖励明细'
      }
    }
  }
}
