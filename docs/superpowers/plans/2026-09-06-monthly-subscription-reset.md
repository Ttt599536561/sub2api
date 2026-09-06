# Monthly Subscription Daily Reset Implementation Plan

> For agentic workers: use subagent-driven-development or executing-plans, with focused verification after each task.

**Goal:** Implement the confirmed monthly subscription reset rules R01-R19.

**Architecture:** Persist reset events and subscription changes in a single PostgreSQL transaction. Use database state for subscription admission and share the atomic reset operation between manual requests, billing triggers, and a bounded recovery scanner. Vue controls use the server's eligibility and version, retaining operation IDs across uncertain responses.

**Tech Stack:** Go, Ent, PostgreSQL, Redis, Gin, Vue 3, TypeScript, Vitest.

Development was explicitly authorized in the previous task on 2026-09-06 ("开始开发"). Continue directly in the existing clean workspace; no deployment is authorized or required.

## Execution

- [x] Recover the approved rules and inspect subscription, billing, and UI entry points.
- [x] Domain: add subscription reset fields, natural-window maintenance helper, commands, events, errors, and eligibility tests in `backend/internal/service/subscription_daily_reset.go` and `subscription_daily_reset_test.go`.
- [x] Storage: add forward migration, Ent schemas/generated code, group/subscription mappings, and `backend/internal/repository/subscription_daily_reset_repo.go`. Exercise atomicity, concurrent rounds, operation replay, the 100-event cap, time boundaries, and window maintenance.
- [x] Service/API: add manual reset, preference compare-and-set, current state and operation lookup, then wire handlers/routes and administrator group validation. Lock administrator term adjustments and preserve renewal semantics.
- [x] Admission/automation: validate fresh database subscriptions before and after queues, trigger after committed billing, scan enabled subscriptions at startup and every 30 seconds, and stop the scanner on shutdown.
- [x] UI: add server-driven reset controls, per-card pending state, operation recovery, current-count display, group configuration, bilingual strings, and stale-response tests.
- [x] Integration: run targeted Go tests, repository integration tests where PostgreSQL is available, frontend tests/typecheck/build, inspect desktop/mobile rendering, and record any unavailable checks. Full-suite exceptions are recorded below; this is not a fully green service-suite claim.

## Shared Contracts

UserSubscription exposes `daily_reset` with `eligible`, `can_reset`, `auto_daily_reset_enabled`, `today_reset_count`, `daily_reset_limit`, `daily_reset_version`, `server_date`, `server_time`, and optional `reason`. It also exposes `preserve_calendar_daily_reset`. DTO serialization tests enforce the same names consumed by the frontend.

POST reset-daily accepts `Idempotency-Key` and `{expected_version, expected_date}`. PUT auto-daily-reset accepts `{expected_version, enabled}`. GET daily-reset-state returns the latest subscription DTO. POST and operation lookup return `{operation_id, replayed, subscription, reset_performed}`; PUT additionally returns `preference_saved` and optional `check_error`. A stale preference write never overwrites a newer choice; the client refreshes state.

Storage owns transaction/locking and writes; service owns server eligibility and automation; handlers map DTOs; frontend owns all files under `frontend/`. Database and generated changes are confined to the relevant subscription/group fields and reset event entity.

## Validation Commands

```text
cd backend
go generate ./ent
go test ./internal/service -run 'DailyReset|Subscription' -count=1
go test ./internal/handler/... ./internal/server/middleware/... ./internal/repository/... -count=1
go test -tags=integration ./internal/repository -run SubscriptionDailyReset -count=1
cd ../frontend
pnpm test:run
pnpm typecheck
pnpm build
```

Coverage maps R01-R04/R07-R14/R18-R19 to domain/storage/UI, R06/R09/R15 to transaction and concurrency checks, R05 to automation/billing/admission, and R16-R17 to renewal/preferences and recovery. Success requires checked command output; unexecuted checks remain explicitly recorded.

## Verification Log (2026-09-06)

- Generated server Wire bindings so the reset repository and recovery scanner are constructed at startup.
- Storage unit checks passed: `go test -p 4 -tags=unit ./internal/repository -run 'SubscriptionDailyReset|ResetVersion' -count=1 -v` (3 top-level tests).
- Storage integration checks passed on PostgreSQL 18.1 and Redis 8.4 with migrations applied: `go test -p 4 -tags=integration ./internal/repository -run 'SubscriptionDailyReset|ResetVersion' -count=1 -v` (12 top-level tests and 7 subtests).
- Review found and addressed user-status locking, expired administrator renewal preferences, negative subscription redemption locking, account-queue revalidation, and WebSocket turn admission. Feature regression checks passed: `go test -p 2 -tags=unit ./internal/service ./internal/handler/... ./internal/server/middleware -run 'DailyReset|Subscription|RevalidateSubscription|NegativeSubscription|AccountSlotRejects' -count=1`.
- Frontend: 257 test files / 1881 tests passed; 47 feature checks and 4 locale checks also passed in targeted runs. Typecheck, build, and changed-file ESLint passed. Desktop (1440x1000) and mobile (390x844) browser checks passed in English and Chinese using API mocks, including lost-response recovery. Screenshots are in `frontend/.dev/daily-reset-screenshots/`.
- Backend: `go build -p 1 ./cmd/server` and the DTO/client contract serialization test passed. Full unit runs passed for middleware, handler, admin handler, DTO, quota view, and repository packages.
- Full service suite exceptions: `TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig` timed out (also failed in five isolated repetitions), and four `TestPluginPackageInstaller*` cases failed because Windows could not rename an open archive. The four installer failures were reproduced in the unchanged `b0ba68f93` baseline. The moderation test passed once on that baseline; it uses a real 1 ns expiry and its timing failure remains unresolved. These unrelated files were not changed. Logs: `%TEMP%/sub2api-service-regression.log` and `%TEMP%/sub2api-baseline-regression.log`.
- The 104 acceptance scenarios remain a test specification, not a claim that 104 automated scenarios have passed. Production-scale load testing and deployment have not been performed.
