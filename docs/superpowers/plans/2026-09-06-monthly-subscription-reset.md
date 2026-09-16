# Monthly Subscription Daily Reset Implementation Plan

> For agentic workers: use subagent-driven-development or executing-plans, with focused verification after each task.

**Goal:** Implement the confirmed monthly subscription reset rules R01-R19.

**Architecture:** Persist reset events and subscription changes in a single PostgreSQL transaction. Use database state for subscription admission and share the atomic reset operation between manual requests, billing triggers, and a bounded recovery scanner. Vue controls use the server's eligibility and version, retaining operation IDs across uncertain responses.

**Tech Stack:** Go, Ent, PostgreSQL, Redis, Gin, Vue 3, TypeScript, Vitest.

Development was explicitly authorized in the previous task on 2026-09-06 ("开始开发"). The feature is committed, while the two review rounds' fixes remain uncommitted in the shared workspace and must be preserved. Production deployment has not been performed; the user requested further review and a local UI preview first.

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

## Review Follow-Up (2026-09-06)

The canonical change record is [月卡每日额度重置功能](../../MONTHLY_SUBSCRIPTION_DAILY_RESET.md), updated on 2026-09-07. Its version table, REV-01 through REV-10 repair index, test links, command history, and local preview record describe the current workspace. The initial verification log above remains historical.

- Baseline: the server-reported `0.2.1` build uses commit `578785ee7fb35030b094b69624efe25670a36f5f`, an ancestor of this branch. The intervening commits are VERSION synchronization, requirements, feature implementation, and documentation. Relative to that source baseline, migration 235 is the only added SQL migration; earlier migration files are unchanged. The live database migration history was not inspected.
- First review: fixed billing circuit-breaker completion, suspension preservation during expired term extension, the reset/billing/user-deletion lock cycle, subscription checks during balance fallback, Web Search error handling, HTTP-context operation ID generation, and expired response status. Each confirmed defect was reproduced before its fix.
- Second review: fixed leaked database error causes in both auth protocols, disabling automatic reset when session storage writes fail, and inactive subscriptions retained after fresh state attachment to the active list. Regression tests cover both the corrected behavior and adjacent controls.
- The repairs are not part of `0be85e950` or current HEAD `bff587089`; they remain working-tree changes. Two new regression files also remain untracked: `backend/internal/handler/gateway_subscription_admission_regression_test.go` and `backend/internal/repository/subscription_daily_reset_lock_order_integration_test.go`. Include them when making the eventual repair commit and record that commit in the canonical document.
- First review frontend full run: 258 files / 1885 tests passed. Final second-review frontend run: four focused files / 39 tests, typecheck, and changed-file ESLint passed. No frontend full run or production frontend build was repeated after the second-review edits.
- Final second-review backend verification: billing/subscription service checks passed, as did full unit tests for handler, admin handler, DTO, quota view, middleware, and repository packages. PostgreSQL 18.1 + Redis 8.4 reset/version integration checks passed, including the lock-cycle regression and existing idempotency/usage-conservation checks. This does not override the recorded full-service-suite exceptions.

## Local Preview (2026-09-07)

- Built the current backend with `go build -p 1 -o ../frontend/.dev/sub2api-local.exe ./cmd/server` and ran it at `127.0.0.1:8080`; Vite runs at `127.0.0.1:3000`. The checked application route is `/subscriptions`.
- Used independent local containers `sub2api-design-postgres` (host port 15432) and `sub2api-design-redis` (host port 16379), with database `sub2api_preview`. Seeded a preview member and OpenAI/Claude monthly groups plus a Gemini weekly group. No production database was accessed.
- Actual API/browser checks passed in Chinese at 1440x1000 and 390x844: two reset controls and two automatic switches, no horizontal overflow or page JavaScript exceptions, and successful on/off PUT requests for the OpenAI monthly subscription. The manual reset HTTP flow was not exercised in this live preview.
- The local startup, seed, browser-check scripts, logs, screenshots, executable, data directory, and browser profile live under Git-ignored `frontend/.dev/`. These machine-local artifacts are not committed. Credentials are intentionally omitted from the tracked change record. The current `.dockerignore` does not exclude the directory as a whole; explicitly exclude it before a release build so local configuration and browser sessions do not enter the build context. This documentation update records the pending work without modifying `.dockerignore`.
- Remaining release verification: exclude local preview artifacts, build and validate the final Linux/amd64 image, rehearse migration against an isolated copy of production data, and measure target-load behavior before enabling the feature. No release or production migration has been performed.

This follow-up records earlier command results. The documentation update itself does not rerun the test suites.
