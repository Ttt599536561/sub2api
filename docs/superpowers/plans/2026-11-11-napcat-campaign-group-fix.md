# NapCat草稿活动修改订阅分组修复计划

> 使用subagent-driven-development分别实现后端、前端；主协调者复核集成及部署。

**Goal:** 用户可修改未开始使用的草稿活动月卡分组，保存后能继续编辑，且不会使历史邀请或兑换库存错配。

**Architecture:** 保留乐观版本检查，仅解除从未启用、无库存且无邀请记录的草稿分组限制。SaveCampaign在原活动互斥锁和行锁内检查；导入库存也先锁活动再读取分组，生成库存沿用现有行锁。曾启用的历史版本禁止换组，防止迟到事件使用旧分组快照。拒绝换组使用专用错误码及准确提示。前端直接接纳保存返回的新版本，不依赖二次列表读取成功。

**Tech Stack:** Go/sql PostgreSQL，Vue/TypeScript/Vitest。

## 范围与执行

- [x] 后端先加真实PostgreSQL回归：草稿改组成功且版本/分组名更新；旧版本仍冲突；库存非空、曾启用（含已暂停）、邀请历史拒绝；并发导入与换组无错配。运行看到预期失败后再实现。
- [x] 修改backend/internal/qqinvite/campaigns.go、inventory.go、types.go；不改迁移和兑换码，不清除库存或历史，不启停用户活动。handler映射QQ_INVITE_CAMPAIGN_GROUP_LOCKED。
- [x] 前端先测试草稿改组提交、专用错误、保存成功后列表不可用仍使用新version；再修改NapCatCampaignPanel、qqInvite错误映射及中英文文案。
- [ ] 使用既有E盘PostgreSQL测试实例和依赖缓存，实际执行qqinvite与handler相关测试、前端相关测试、类型检查及必要构建；独立复核并发和状态边界。
- [ ] 同步CHANGELOG/HANDOFF和修复记录，保留主仓全部既有WIP；整合已核验修复并沿用正式Linux镜像发布门禁，部署前重新核对活动/任务状态并备份，保留NapCat会话与密钥。

## 验收边界

正式站只读已确认当前活动version=1，未启用、无基线/库存/邀请记录；失败由后端无条件分组不可变判断触发。不得通过直接改生产数据库绕过业务保护。真实奖励消息无需用于此修复验收；用户修改的目标分组由用户选择。
