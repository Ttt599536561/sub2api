# Welfare Redeem Effects Implementation Plan

> **For agentic workers:** Use subagent-driven-development with scoped file ownership, test-first changes, and independent spec then quality reviews. User approved the design and implementation in chat.

**Goal:** Allow an invitation welfare entitlement to receive either a fixed quota supplement or a duration supplement through admin-generated codes, while protecting campaign groups and preserving legacy behavior.

**Architecture:** Explicit supplement metadata on redeem codes, an independent immutable receipt ledger, campaign-derived target groups, and transactional group lifecycle guards. Existing standard and assigned QQ redemption paths remain the default for legacy codes.

**Tech Stack:** Go/Gin/Ent, PostgreSQL, Vue3/TypeScript, Vitest, pnpm.

## Tasks and ownership

- [ ] Backend supplement: schema/Ent, migration259, service/repository/DTO/admin generate and campaign-option route, redemption, independent receipts; Go unit and integration tests. New logic in focused welfare_redeem files; avoid group lifecycle files and frontend. Contract is the approved design document.
- [ ] Group lifecycle: migration260, group repository/service/handler protection, qqinvite campaign/inventory integration and tests. Cooperate with supplement owner about schema and SQL locking. Do not edit supplement259 or Ent generated files.
- [ ] Frontend: typed API contract, generation modal (ordinary vs welfare, campaign→read-only group, effect/amount/days), list/result/history and both locales; meaningful Vitest tests.
- [ ] Integration: isolated local PostgreSQL and frontend dependencies on E drive, baseline then changed-code checks, migration upgrade and concurrency verification, independent spec/quality reviews, fix findings.
- [ ] Documentation and delivery: changelog, dated change record and handoff with actual verification; local commit after checks. Keep production and remote publication untouched.

## Commands and evidence

Every PowerShell invocation starts `. 'E:\sub2二次开发项目\tools\project-env.ps1'`. Working tree: E:/sub2二次开发项目/.worktrees/welfare-redeem-effects-20261116. Cache and test databases: E:/sub2二次开发项目/.cache/welfare-redeem-effects-20261116. Reuse existing toolchains and dependency caches.

1. Establish baseline with `go test -tags=unit ./internal/service -run 'TestWelfare' -count=1` and existing redeem/NapCat frontend tests.
2. Write behavior regressions, observe the expected failure, then implement and rerun. Real database tests use local isolated TEST_DATABASE_DSN following existing helpers; never production.
3. Run changed Go packages with appropriate unit/integration tags, frontend targeted Vitest plus typecheck/build, migration upgrade fixtures, and `git diff --check`.
4. Review requirements and then correctness/concurrency/compatibility independently; do not accept unaddressed blocking findings.

Acceptance cases and concrete request/response fields are defined in ../specs/2026-11-16-welfare-redeem-effects-design.md. Checkboxes and actual evidence are updated during implementation.
