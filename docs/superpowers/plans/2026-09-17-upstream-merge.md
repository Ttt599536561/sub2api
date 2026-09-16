# Upstream 0.2.5 Integration Plan

> Execution: isolated worktree integration, bounded subagent checks, and independent final code review.

**Goal:** Merge Wei-Shaw/sub2api main at `881f3202694c6bc932446931a30c27d9675178b9` while preserving the subscription daily-reset feature and its ten existing review fixes.

**Architecture:** Preserve the existing group opt-in, user preference, transactional reset event, billing/admission hooks and UI recovery behavior. Integrate upstream schema and gateway changes without replacing either branch wholesale. Keep the historical reset migration unchanged because it may already have been applied.

**Stack:** Go 1.27, Ent, PostgreSQL, Redis, Vue 3, TypeScript, Vitest.

## Preservation and baseline

- [x] Fetch the upstream default branch directly from the user-specified repository; record the exact commit.
- [x] Save all modified and untracked files, a binary patch, and SHA-256 manifest outside the repository.
- [x] Preserve the existing review fixes and two untracked regression tests in commit `08d72d5bd`.
- [x] Run the original backend subscription/billing unit tests and four frontend subscription suites (39 tests).
- [x] Create branch `codex/merge-upstream-20260917` in an isolated worktree and attempt a normal merge.

## Resolve integration differences

- [x] Resolve `backend/internal/service/subscription_service.go` by retaining fresh-state reset/renewal safeguards and upstream subscription functionality.
- [x] Regenerate Ent code from the merged `backend/ent/schema` definitions, including the daily-reset fields and event entity, to resolve `backend/ent/runtime/runtime.go`. Generation completed outside the watched worktree to avoid Windows mapped-file interference; all generated Go files matched the final worktree after newline normalization.
- [x] Resolve `frontend/src/views/admin/__tests__/GroupsView.duplicate.spec.ts` while retaining upstream group-model behavior and daily-reset group configuration assertions.
- [x] Inspect automatically merged billing, gateway, admin group and dependency-injection changes; verify all custom hooks remain wired.
- [x] Verify migration identity/order against the upstream `235`–`238` additions, without rewriting any historical SQL file. Migration identity is the complete filename, so both `235` files remain intact.

## Validation and repair

- [x] Run `go test -p 1 -tags=unit ./internal/service -run 'DailyReset|Subscription|Billing|NegativeSubscription' -count=1` and full unit tests for affected handler, middleware, repository, DTO and route packages.
- [x] Run all feasible backend unit tests and compile `./cmd/server`; investigate failures against upstream when needed. Final full unit run: 57 passing packages, 19,842 passing test/subtest events, zero failures, 16 environment-dependent skips. Windows build and Linux amd64 build with embedded frontend both passed.
- [x] Run the daily-reset PostgreSQL/Redis integration tests only against isolated disposable test infrastructure, including concurrent reset/billing and lock-order regression cases. 13 top-level cases and 7 subcases passed; disposable containers were cleaned up.
- [x] Run frontend subscription/group regression suites, complete Vitest suite, TypeScript checks, lint, and production build. 288 files / 2194 tests passed after two upstream fixture corrections reproduced on the pure upstream baseline.
- [x] For any new behavioral integration failure, reproduce it with a focused regression before repairing the cause and rerunning the affected checks. Renewal response tests caught stale state; exact API fixtures and upstream timing/frontend fixtures were repaired without weakening assertions.
- [x] Have independent subagents review the merged backend and frontend against the documented acceptance matrix; resolve material findings and obtain re-review. Two independent final reviewers found no blocking issues.

## Completion

- [x] Update `docs/MONTHLY_SUBSCRIPTION_DAILY_RESET.md` and write an integration record listing actual custom changes, resolved conflicts, current verification and any limitations.
- [x] Confirm there are no unresolved conflicts or whitespace errors in changes relative to upstream, the exact upstream head is included, and historical migration bytes are unchanged. All 282 existing SQL migration blobs are unchanged. Preserve upstream prompt text verbatim even though its newly added lines contain inherited trailing spaces.
- [x] Commit the validated merge and fast-forward the user's original feature branch to it, preserving unrelated untracked files. Merge commit: `83fee7462cd0ba0ab6f12f341f8db6252cd750a1`.
- [x] Prepare the handoff with local commit IDs, review outcome, test results and deployment status. Details are recorded in `docs/UPSTREAM_MERGE_2026-09-17.md`; no push or deployment was performed.
