# 福利签到 v3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox syntax for tracking.

**Goal:** 实现设计中的低额签到、随机连续奖励和v3兼容升级。
**Architecture:** 保留事务/钱包/幂等边界，拆分连续进度推进与连续奖励抽样；纯算法集中于welfare_rewards.go。元数据迁移和前端文案独立实施，最后集成验证。
**Tech Stack:** Go、PostgreSQL、Vue/TypeScript、pnpm、PowerShell 7；所有缓存复用主项目.cache。

## Task 1: 隔离与基线

- [x] 从8e5c5d32e建立codex/welfare-v3-20261026，位于E盘.worktrees/welfare-v3-20261026。
- [x] 保存主目录修改文件SHA256到主项目.cache/welfare-v3-20261026/main-protection.json。
- [x] 在新工作树执行现有TestWelfare服务测试，记录基线。

## Task 2: 日奖、连续奖与服务接线（TDD）

Files: backend/internal/service/welfare_rewards.go, welfare_service.go, welfare.go, welfare_test.go, welfare_rewards_distribution_test.go, welfare_public_contract_test.go；backend/internal/repository/welfare_repo_integration_test.go。

- [x] 测试先行：日奖联合分布期望以1/2分70/30和3/4/5分70/25/5独立定义；第7/15/30奖区间/概率、非节点不抽样、随机失败、保底独立、版本与历史重放。
- [x] 执行go test -p 1 ./internal/service -run TestWelfare -count=1，确认因旧算法失败。
- [x] 实现每日分支随机数阈值；advanceWelfareCheckin仅推进周期返回error；sampleWelfareStreak(day,random)独立返回金额及审计，非节点零金额。

算法契约：
```text
p = [20,30,50,100][low]
surprise = random(100) < p
r = random(100)
ordinary: amount = r<70 ? 1 : 2
surprise: amount = r<70 ? 3 : r<95 ? 4 : 5
streak day7 = 3+random(6); day15 = 8+random(13)
day30: r=random(100); [min,max] = r<80 ? [20,50] : r<98 ? [51,100] : [101,195]
streak = min+random(max-min+1)
```

- [x] CheckIn同事务依次推进日期、抽日奖、抽连续奖，累加并保存v3私有审计；返回原DTO。
- [x] WelfareRulesVersion=3集中引用。draw:v2和抽奖审计v2保持。
- [x] 同步真实PG夹具金额，并新增旧v2重放及连续奖随机失败回滚回归。
- [x] 执行上述服务测试至通过，保留失败/成功日志。

## Task 3: 元数据迁移与回归

Files: backend/migrations/251_welfare_checkin_rules_v3.sql; backend/internal/repository/welfare_repo.go; repository迁移/福利相关测试。

- [x] 新增真实PG升级/全新安装测试：版本为v3且旧钱包/历史JSON、首次启用时间及活动状态原样保留，新增约束拒绝未知版本。
- [x] 先运行确认旧实现失败；新增迁移更新版本约束、默认值、现有行，不改updated_at或资金。
- [x] 仓储对外规则版本引用service常量；旧版本历史结果保持。
- [x] 固定r6历史迁移测试目标到<=250，维持306条原语义；其他升级保全测试仅明确转换program版本期望。
- [x] 运行全部福利真实PG专项及新增迁移测试，无跳过。

## Task 4: 文案与交接

Files: frontend/src/i18n/locales/{zh,en}/welfare.ts; CHANGELOG.md; docs/HANDOFF.md; docs/changes/2026-10-26-welfare-checkin-v3.md; .gitignore精准例外。

- [x] 中英文ruleCheck明确“小额随机使用补贴，主要用量按需充值”，不透露未领金额/概率；ruleStreak明确随机。
- [x] 同步版本fixture（保留有意覆盖旧结果的fixture）；运行福利相关测试、类型、lint、构建。
- [x] 变更记录列明范围、数学成本、迁移/混跑/回退和验证边界；旧设计文档保持历史身份。

## Task 5: 最终验证与交付

- [x] 独立规格符合性审查，再代码质量审查，修复所有阻断问题。
- [x] Go福利service/handler/repository/migrations测试与vet，真实PG事务/迁移；前端相关测试、类型、lint、构建。
- [x] 校验文档链接/跟踪状态、git diff --check、主目录HEAD和原WIP指纹。
- [x] 将代码、测试、CHANGELOG和交接一起提交到功能分支；不合并、不推送、不部署。
- [x] 停止本轮专属测试PG，保留验证摘要及工作树。
- [ ] 清理专属pgdata、构建dist和临时联接：自动审批在启动前以blocked by policy拒绝，实际删除0文件，按要求保留且不重试。
