# 通用订阅补充码 Implementation Plan

> 执行：subagent-driven-development；先回归复现，再实现，最后独立需求/质量审查。主代理负责共享接口和集成，分工以文件所有权避免冲突。

**Goal:** 管理员按分组发放通用订阅加额/加时码；两类月卡均可兑换，一次性额度可靠进入计费。

**Architecture:** 独立个人补充额度与账本；服务准入和事务结算统一基础/补充分摊，兑换保持原子核销。新补充效果与旧月限额效果区分，历史记录保全。

**Tech Stack:** Go/Gin/Ent、PostgreSQL、Redis、Vue3/TypeScript/Vitest。

## 1. 基线与现场诊断（主代理）

- [x] 核对仓库、源码HEAD、Git dirty和用户明确选择。
- [x] 建立E盘隔离工作树，保留原主目录未跟踪文件。
- [ ] 复用E盘缓存；执行对应包现有基础测试，记录原始失败。
- [ ] 现场只读核对账号与活动；SSH不可达时明确保留未知，不修改生产数据。

## 2. 数据模型与扣费（计费实现代理）

涉及 `backend/migrations/261_*`、`backend/ent/schema/user_subscription.go`及生成文件、`internal/service/user_subscription.go`、订阅准入/缓存/预占、`internal/repository/usage_billing_repo.go`及订阅映射。

- [ ] 先建立真实行为红测：基础不足补额通过、部分费用拆分、重复请求只扣一次、并发不丢扣费、重置不增加补额、过期新周期不复活。
- [ ] 新增兼容迁移和状态；NUMERIC结算在原去重事务内完成，记录总费用和补充分摊，保留已发生超用事实。
- [ ] 更新有效额度、固定费用预占、所有HTTP/WS/图片计费入口和缓存一致性，补额用完后拒绝准入。
- [ ] 定向unit及真实PG计费回归，记录命令和结果。

## 3. 兑换与API（兑换实现代理）

涉及 `internal/service/redeem_service.go`、新 `subscription_supplement_redeem.go`、admin handler、DTO、redeem repository及补码约束/账本迁移。

- [ ] 红测普通/福利、同组双权益优先级、无活动生成、错组/无效订阅零核销和旧未用码兼容。
- [ ] 新效果 `add_balance` / `extend_subscription`，保留旧记录的 `add_quota` / `extend_duration` 快照。新码仅固定分组；码和权益同事务更新。
- [ ] 生成及兑换校验金额正数/精度/上限、天数正数/期限上限；加时不重置用量；原续期和QQ自动发奖码保留。
- [ ] 兑换结果和历史提供目标订阅、效果、金额/天数及变更快照，失败码保持未使用。
- [ ] 定向unit和真实PG并发/迁移回归。

## 4. 前端（前端实现代理）

涉及 `frontend/src/components/admin/RedeemGenerateForm.vue`、admin/user redeem API/types、订阅与兑换结果展示、i18n和对应测试。

- [ ] 先验证新操作无需活动、发送分组与效果、旧普通码行为保持的红测。
- [ ] 改为管理员选择分组及效果；加额度表单说明一次性、基础优先且不按周期重复增加；加时说明保留用量。
- [ ] 展示实际订阅及独立补充剩余额度；历史旧码效果保持准确，错误给出同组有效订阅规则。
- [ ] 运行受影响Vitest、i18n、vue-tsc和生产构建。

## 5. 整合与审查（主代理）

- [ ] 独立需求审查核对用户选择和旧码修复，再做独立质量审查覆盖计费事务、周期、缓存和并发。
- [ ] 运行受影响完整后端包及真实PG迁移/数据保全；核对前后端接口、build、diff check。
- [ ] 同步CHANGELOG、HANDOFF与实现记录；本地提交/整合并明确未发布、未部署状态。保护主目录已有修改，清理本次失效临时文件，保留验证摘要和依赖缓存。
