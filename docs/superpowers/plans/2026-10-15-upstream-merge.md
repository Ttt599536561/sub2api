# 2026-10-15 上游合并执行计划

**目标：** 合并官方最新固定提交 `698d142468ab15374bc22bd5247759e5b7352006` 与最新已提交二开 `11fe558f0b47862c64e75aed172277d6e6c624c6`，解决冲突、复审、验证并保留可继续开发的本地分支。

**方式：** 在 E 盘 `.worktrees/sub2api-upstream-20261015`、分支 `codex/merge-upstream-20261015` 执行；保留主目录全部未提交修改。官方 0.2.16 发布后主分支更新也包含在固定来源中。

- [x] 读取交接与更新日志；拉取 origin/upstream-official，确认 origin/main 是本地提交祖先，记录未提交状态。
- [x] 创建独立 E 盘工作树，检查三方合并冲突清单。
- [x] 基线验证：后端 `go test -tags=unit ./internal/service ./internal/handler/... ./internal/repository/... ./cmd/server`；前端 `corepack pnpm@9.15.9 install --frozen-lockfile` 后运行核心会话/福利/订阅回归。
- [x] `git merge --no-ff --no-commit 698d14246`，解决 billing_cache_service.go、ModelWhitelistSelector.vue，重新生成 wire_gen.go；检查所有自动合并的二开交点。
- [x] 并行审查财务/订阅/福利、前端会话、网关/迁移；修复已确认重要问题并添加相应回归测试。
- [x] 运行后端 unit 全量、前端完整测试/typecheck/lint/build、后端 embed 构建；确认 Docker 实际数据盘在 E 后运行可行的数据库集成验证。
- [x] 更新 CHANGELOG.md、docs/HANDOFF.md、docs/changes/2026-10-15-upstream-merge-review.md，核对文档链接与 git diff --check。
- [ ] 创建本地合并提交；确认两个来源都是祖先、无残留冲突且主目录原有修改未改变。报告结果、实际验证和剩余限制。
