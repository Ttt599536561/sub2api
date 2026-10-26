# Welfare lottery v4 implementation plan

> **For agentic workers:** Use subagent-driven-development to implement the bounded tasks, followed by independent specification and quality review.

**Goal:** Reduce lottery expected rewards by 97.8445% while preserving all existing wallet and operation state.
**Architecture:** Update the existing integer-cent prize table and public version, retain request fingerprints, migrate only rule metadata, and keep the frontend driven by backend rules.
**Tech Stack:** Go, PostgreSQL migrations, Vue/TypeScript, Vitest, pnpm.

- [x] Backend service: run existing Welfare tests as baseline; change exact distribution expectation to `{1:450,2:300,5:180,10:50,20:15,50:4,100:1}` and observe RED. Update `welfare_rewards.go` amounts, `welfare.go` public version4, Draw private audit v4 while preserving draw:v2. Test sum=3050 cents/1000 samples, 930 samples<=5 cents, maximum100, public probabilities, wallet credit, old result replay and rollback. Run `go test ./internal/service -run Welfare -count=1`.
- [x] Repository: add failing file/schema/upgrade tests for `252_welfare_lottery_rules_v4.sql`. SQL drops rules-version constraint, sets v4 default, changes v3→v4 and restores v4-only constraint. Keep 251 unchanged and pin its tests to historical migration FS. Test actual migration preservation, fresh install, retry/concurrency and replay; update current settings version expectations. Run welfare repository tests against isolated local PostgreSQL using project cache and existing native binary.
- [x] Frontend: test v3→v4 rule reload (both polling and accepted mutation responses) and seven small prizes in probabilities/result UI; update bilingual lottery explanation to small supplement without changing signup. Use existing fixtures intentionally for historical high-prize recovery. Run welfare Vitest suite, `pnpm typecheck`, eslint changed source, and `pnpm build` in isolated worktree.
- [x] Review: inspect changes for spec compliance, then independent code-quality review. Run targeted backend vet and `git diff --check`. Record actual results and deployment caveats in CHANGELOG/HANDOFF and `docs/changes/2026-10-27-welfare-lottery-v4.md`; verify main WIP hashes unchanged and commit only task files. Stop any test database created by task; preserve shared caches and concise evidence.
