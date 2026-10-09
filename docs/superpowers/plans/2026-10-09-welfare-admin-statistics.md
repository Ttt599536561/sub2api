# Welfare Admin Statistics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Provide administrators date-filtered welfare totals, daily participation, user lifetime earnings, and auditable reward details.

**Architecture:** Query immutable welfare reward facts using business_date and precise cent sums. Add isolated statistics repository/service/handler files and an admin UI panel while preserving original activity controls. Frontend and backend work on disjoint paths against the API contract in the approved design.

**Tech Stack:** Go, PostgreSQL, Gin, Vue3, TypeScript, Vitest, pnpm.

## Environment
All shell calls begin with `. 'E:\sub2二次开发项目\tools\project-env.ps1'`. Worktree: E:\sub2二次开发项目\.worktrees\welfare-admin-stats-20261009. Shared cache/logs: E:\sub2二次开发项目\.cache\welfare-admin-stats-20261009. No new C-drive workspace or cache. Validate Docker host storage before container tests.

## Task 1: Baseline and shared contract
- [x] Run `go test -tags unit ./internal/service ./internal/handler ./internal/repository -run Welfare -count=1` from backend and existing welfare Vitest tests from frontend; save logs.
- [x] Read design API contract and confirm amount/count/date semantics. Do not mutate existing reward facts or wallet values.

## Task 2: Backend statistics
Files: create backend/internal/service/welfare_statistics.go + tests; backend/internal/repository/welfare_statistics.go + tests; backend/internal/handler/welfare_statistics_handler.go + tests; modify backend/internal/server/routes/admin.go. Add indexes only if query plan evidence warrants them.
- [x] First write service parameter tests and repository fixtures: A has daily=10,streak=60,draw=50 twice on 2026-10-08; B has daily=20 same day; A has daily=10 on 2026-10-07; A redeems50. Expected Oct8 daily=0.30,streak=0.60,draw=1.00,total=1.90,checkin_users=2,draw_users=1,draw_count=2,participating_users=2. A lifetime total=1.80 and period=1.70. Oct9 is zero. Verify RED before implementation.
- [x] Implement validated inclusive business-date filters, defaults, pagination/sorting allowlists, exact decimal rendering, read-only consistent snapshots and parameterized user search.
- [x] Register three admin GET routes and enforce admin role before query. Test unauthenticated/normal-user denial and invalid date/user/type/sort HTTP400. Ensure GET while paused works.
- [x] Run focused unit tests and real PostgreSQL statistics integration tests. Verify type totals reconcile and historical/deleted-user rewards are not dropped.

## Task 3: Frontend statistics
Files: modify frontend/src/api/admin/welfare.ts and locale zh/en welfare.ts; create frontend/src/components/admin/welfare/WelfareStatistics.vue (+ smaller focused components/utilities as appropriate); modify frontend/src/views/admin/WelfareSettingsView.vue; create tests under views/admin/__tests__ and api/__tests__.
- [x] First write failing UI tests: statistic tab default, setting tab retains toggle/save, apply dates and user query to all endpoints, click daily/user to reveal filtered details, server-side pagination/sort, request race handling and error/empty states. Verify RED.
- [x] Implement shared API types exactly matching design, explicit Shanghai date presets, bounded custom date validation, responsive summary/cards/tables, server-side sort/pagination and reward detail labels.
- [x] Preserve amount strings through presentation; label period and lifetime clearly. Invalid filters show a useful message without losing existing input.
- [x] Run welfare tests, new stats tests, typecheck, changed-file ESLint, locale-key completeness and production build.

## Task 4: Review and delivery
- [x] Independently review approved requirement coverage first, then code correctness/security/performance; resolve actionable issues and re-review.
- [x] Run relevant backend/frontend regression suites and browser QA (desktop/mobile, filters/details/settings), using local test data only.
- [x] Save concise command/results evidence and screenshots to task cache/output. Clean only task-created unneeded helpers and stop task servers.
- [x] Commit only feature/spec/plan/test changes on codex/welfare-admin-stats-20261009. Preserve all original dirty main-worktree changes. Report branch, result and validation without claiming a release or deployment.
