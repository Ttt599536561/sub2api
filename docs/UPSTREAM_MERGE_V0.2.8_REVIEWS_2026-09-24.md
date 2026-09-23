# v0.2.8 三轮独立审查记录

审查范围包括上游行为保留、二开兼容、数据库升级与发布来源。各轮初审及修复复核按实际记录保留；最终发布还须通过固定提交的 GitHub CI、安全扫描与镜像验证。


---

## Round 1: backend

# Review round 1 — backend runtime and upstream preservation

Reviewed frozen tree: cbe6249c9175a72e05576627b85d7161fd48a501
Official upstream: a3eb7ef302961cba716dc78b39b93b60c467db0e
Fork baseline: 713d2852e8ea8addce365f595d95823d26bb5aa2
Review mode: read only; no source, index, or commit changes.

## Findings

### [P2, resolved in round-1 recheck] Preserve official group rejection codes and telemetry after authoritative admission

Primary location: backend/internal/server/middleware/api_key_auth.go:237-245.
Related locations: backend/internal/server/middleware/api_key_auth_google.go:170-184; backend/internal/service/subscription_admission.go:17-25.

With the production SubscriptionService injected and a normal billed request, GetSubscriptionForAdmission returns ErrSubscriptionInvalid when the authoritative group is disabled or missing/deleted. The middleware returns that error before invoking abortIfAPIKeyGroupUnavailable. Consequently both standard and subscription groups now return SUBSCRIPTION_INVALID instead of the official GROUP_DISABLED/GROUP_DELETED response, and neither MarkOpsClientBusinessLimited(APIKeyGroupUnavailable) nor MarkIngressRejected(GroupDisabled/GroupDeleted) executes. Google likewise loses the official group-specific message and rejection metadata. The Google fresh exclusive-group authorization failure also omits the official MarkOpsClientBusinessLimited call.

This is observable for every billed request to an unavailable group and breaks the explicit requirement to retain official behavior/logic. The authoritative refresh is valuable; retain the group failure distinction and route it through the existing guards using current group state before emitting the subscription error. Add tests with a non-nil SubscriptionService for disabled/deleted groups in both protocols, asserting response contract and both telemetry markers. Existing api_key_auth_test.go group cases call NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg), while the inspected Google deleted-group case uses simple mode, so those tests do not exercise the affected production branch.

## Verdict

Round-1 backend review now has no unresolved P0/P1/P2 findings. The sole P2 was addressed and independently rechecked on the working tree after the frozen candidate. Backend scope is acceptable for continuation to the remaining review rounds and final verification gates. This does not certify release readiness before those gates finish.

## Inspected scope and positive evidence

- Compared all overlapping core/wiring/DTO/routes files with official upstream. DTO and route differences are additive custom fields/routes. Official ClaudeCode version sync, OpenCode usage, referral handlers, plugin services and lifecycle wiring remain present.
- billing_cache_service.go retains official DB-authoritative simple-mode key windows and quota/RPM logic; custom fresh admission preserves mode-change rejection and current daily/weekly/monthly checks.
- gateway_usage_billing.go retains official simple-mode command/error/cache boundaries; custom atomic fallback guard is scoped around it, and auto daily resets run only after committed consumption.
- Gateway Messages/chat/responses/Gemini/web-search/OpenAI slot and WebSocket revalidation paths, fallback currentSubscription handling and release cleanup.
- Exact-upstream redeem_service.go retained, including row lock before subscription reduction and remaining-hour preservation. Custom repository version increments surround official operations.
- Subscription reset admission, auto-reset scanner, usage window maintenance, renewal preferences, daily reset state and reset repository lock/retry flow inspected.
- Payment completion and refund welfare integration: completion accrual, reversal transaction boundaries, gateway-confirmed recovery metadata, retry CAS and refund precision checks.
- Balance generation fencing, authoritative low-balance refresh, durable invalidation outbox and lifecycle shutdown.

## Verification and limits

Executed successfully:

    go test ./internal/service ./internal/server/middleware ./internal/handler/dto ./cmd/server -run 'Test(WelfareAtomicGuardPreservesSimpleModeErrors|WelfareGatewayCannotFallbackWithoutAtomicRepository|.*SimpleMode.*|.*SubscriptionAdmission.*|.*BillingCache.*Subscription.*|.*ProvideCleanup.*|.*UserSubscription.*DTO.*)' -count=1

service: PASS; cmd/server: PASS. Middleware and DTO compiled successfully but reported no matching tests without the unit build tag. Root is independently running the full tagged unit suite, integration compile, embedded build and remaining gates; their results are not claimed here. This review did not execute live PostgreSQL/Redis concurrency or runtime wire startup. The finding is established by the production branch and official contract test comparison; no reproduction source file was added.

## Round-1 focused recheck — original P2 resolved

Recheck scope was limited to the delta in api_key_auth.go, api_key_auth_google.go and the new api_key_auth_admission_group_guard_test.go. This is a recheck of review round 1, not a new review round.

- Known authoritative groups, and confirmed absence represented by nil group plus ErrSubscriptionInvalid, now pass through the existing official group guards before subscription errors are emitted.
- Disabled/deleted/missing group errors therefore regain official response codes/messages and both business-limit and ingress-rejection telemetry.
- The extracted Google helpers preserve the original official message and marker behavior, including exclusive-group authorization denial.
- A nil group caused by a database error remains BILLING_SERVICE_UNAVAILABLE/503, even with a stale disabled auth snapshot. Active-group subscription lookup failures retain subscription error semantics.
- The new table-driven test covers 11 scenarios across both protocols (22 cases), asserting response shape, forwarding decision, current group authority and telemetry. Its missing/disabled/deleted group cases inject a real non-nil SubscriptionService, addressing the original coverage gap.

No concrete remaining issue was found in the repair delta. No source/index edits or duplicate test suite were performed by this reviewer.

Additional verification evidence, reported by root from the implementer (no separate log file independently inspected):

    go test -tags=unit ./internal/server/middleware -run 'TestAPIKeyAuthAdmissionPreservesOfficialGroupGuards|TestAPIKeyAuthUsesDatabaseSubscriptionAdmission|TestReview3APIKeyAuthDeletedGroupIsForbidden' -count=1

Reported GREEN at 00:58:55: PASS, 0.747 seconds. Prior RED at 00:57:31 had 9 failing subcases. Root also reports the pre-repair full backend unit candidate passed 21,427 tests in 58 packages, with 17 conditional skips. Full middleware verification for the repaired tree and final root gates were still in progress when this recheck was recorded.


---

## Round 1: frontend

# Frontend merge review — Round 1
Frozen merged tree: cbe6249c9175a72e05576627b85d7161fd48a501
Fork baseline: 713d2852e8ea8addce365f595d95823d26bb5aa2
Official upstream: a3eb7ef302961cba716dc78b39b93b60c467db0e
Merge base: aea725f2e

Verdict: PASS for the reviewed frontend merge. No confirmed merge-introduced correctness defect or lost official feature/logic was found. No priority/file:line findings.

Scope:
- All 10 frontend overlap paths in docs/upstream-v0.2.8-file-comparison.csv: api/client.ts, api/__tests__/client.spec.ts, components/common/BaseDialog.vue, stores/announcements.ts, types/index.ts, both admin/overview locales, views/admin/GroupsView.vue, and the two GroupsView codexManifest/duplicate tests.
- Compared production and overlapping tests with both fork and upstream; examined the upstream changes since merge base.
- Inspected authSession.ts, tokenRefresh.ts, stores/auth.ts, api/auth.ts, App.vue, AnnouncementBell.vue, associated session tests, announcement fetch/identity/mark-all tests, BaseDialog focus/scroll-lock tests, and the opt-in trap-focus consumers.

Preservation and quality:
- All 132 upstream-only frontend files have exactly the official upstream Git blob ID (independent ls-tree comparison).
- Client retains the official network error code/fallback and its parameterized regression test, together with custom retry-after and request-session ownership checks. Existing cancellation and refresh paths remain intact.
- BaseDialog retains the official synchronous openDialogs Set and updateScrollLock transitions; custom dialogStack registration is separately deferred to nextTick. This avoids the prior class of lock loss while a replacement dialog registers. Unmount/disposal, hidden siblings, nested focus, and restored inert state have targeted tests.
- fetchAnnouncements matches official fetchGeneration logic; reset invalidates both official fetch ownership and custom session-scoped mutations/timers. Separate generations preserve same-session pending mark-read behavior when a forced fetch supersedes an earlier fetch.
- GroupsView keeps official reasoning-effort conversion, validation, and rendering unchanged. Custom reset permission is initialized, loaded, normalized during create/update, and cleared from new drafts. The merged tests retain upstream reasoning-effort assertions and test custom reset permission through real form controls, including combined persistence.
- Overlapping test differences do not drop official assertions. The codexManifest fixture adjustment uses the actual model_allowlist name and adds the custom store mock.

Verification evidence and limits:
- Reviewed the existing frontend-build.log: production build completed successfully.
- Parent supplied focused results of 11 files / 113 tests and successful lint/typecheck; this reviewer did not rerun those suites.
- Full frontend test suite was still running while reviewed; no full-suite pass is claimed.
- No real-browser keyboard/inert/transition verification or live-backend auth exercise was performed.
- Repository source/index were not edited.


---

## Round 1: migrations-release

# Review Round 1: migration safety and release pipeline

Reviewed frozen merged tree: cbe6249c9175a72e05576627b85d7161fd48a501.
Custom baseline: 713d2852e8ea8addce365f595d95823d26bb5aa2.
Upstream: a3eb7ef302961cba716dc78b39b93b60c467db0e.
This is round 1 only; it does not substitute for the remaining required rounds.

## Findings

No concrete P0, P1, or P2 defect identified in the reviewed scope.

## Migration evidence

- Git origin comparisons show all upstream SQL retained byte-for-byte; the only additions versus upstream are the three existing custom migrations. Versus the custom baseline, the only SQL differences are the three new upstream migrations. No historical SQL was edited.
- Independently recomputed custom baseline: 289 files, SHA256 47bd915b3a9b9baf8b145c432617b26082f138dcc34170ccfeabdd4079484648.
- Independently recomputed official baseline: 289 files, SHA256 6075250885f45555d7671005dc80db75c8848e9066a0cc3d291cd36a80c30947.
- The combined set has 292 migrations. migrations_runner.go:24-26 keys history by full filename, and :176-201 sorts/looks up complete names; shared numerical prefixes do not collide.
- subscription_daily_reset_v028_upgrade_integration_test.go:65-128 seed statements were checked against their SQL definitions. Required payment expires_at is present; reset version and 86400-second expiry deduction satisfy constraints; wallet balance equals earned minus redeemed; operation UUID and parent references are valid; spend debit permits null refund metadata; payment 100 CNY correctly yields two subscription draws; audit/outbox rows use valid fields and defaults.
- The new test verifies old rows and migration checksums, the intended legacy-to-generic pricing conversion, nullable new audit metadata, all 292 history records, and unchanged second startup. Existing upstream-upgrade coverage retains disabled-by-default custom features and original history.

## Release evidence and process condition

- Ordinary release.yml, backend-ci.yml, and Dockerfile exactly match upstream.
- publish-custom-v028.yml:21,38,46 fixes source to the event SHA and verifies checkout; :82-89 passes this source to binary metadata/OCI labels; :100-105 checks the published digest, native architecture, revision label, and binary version.
- The amd64/arm64 matrix uses native runners, checks setup/frontend availability and PostgreSQL clients/resources, and uploads a digest only after those checks pass. The final job consumes those recorded digests, rejects an existing final tag, and checks both platform names.
- The workflow's publish job depends only on build (:128-129); it does not independently require backend CI. This is not raised as a defect for the approved controlled procedure: push only codex/merge-upstream-v0.2.8 first, require successful CI plus Security Scan on the exact commit, then fast-forward the target and push the identical SHA to codex/publish-v0.2.8-r1 without source/config changes. Publishing any other SHA would invalidate this verdict.
- Anonymous registry requests returned HTTP 200 for the existing 0.2.7-r8 manifest and HTTP 404 for 0.2.8-r1. This supports current package public readability and an unused final tag; it is not evidence that the new image already exists or passes runtime checks.

## Verdict and limits

Static compliance/quality: pass for this scope, conditional on the stated exact-commit gate. Release readiness remains pending the mandatory GitHub CI integration execution, successful Security Scan, both native image runtime checks, and final manifest/public-pull verification.

No database integration tests or Docker builds were run locally because Docker is unavailable and existing workloads must remain running. SQL seed validation is static, not an executed PostgreSQL result. No workflows were triggered and no repository files were edited. Reviewed files still matched the frozen tree during verification.


---

## Round 2: backend

# Review round 2 — backend financial correctness and upstream preservation

Frozen tree: 178dc44ffe087cc8bcd837c76ea5ca95e1b5643c
Official upstream: a3eb7ef302961cba716dc78b39b93b60c467db0e
Fork baseline: 713d2852e8ea8addce365f595d95823d26bb5aa2
Merge base: aea725f2e

Independent read-only review. The backend working files matched the frozen tree when inspected. No source, index, or commit changes were made. One temporary regression test was executed through Go overlay outside the worktree.

## Finding

### [P2] Disable paid subscription resets in simple mode

Primary location: backend/internal/service/wire.go:23-24.
Related locations: backend/internal/service/subscription_daily_reset_service.go:176-190; backend/internal/repository/subscription_daily_reset_repo.go:194; README.md:731-733.

ProvideSubscriptionService always attaches dailyResetRepo and immediately starts its recovery scanner, including when cfg.RunMode is simple. If a deployment switches from standard to simple while an existing active subscription has automatic daily reset enabled and exhausted daily usage (for example, after an interrupted post-billing reset), the scanner calls TriggerAutoDailyReset and applies the paid reset. The repository subtracts 24 hours from ExpiresAt. This happens without a new billed request, even though the official simple-mode contract explicitly bypasses subscription debit and hides SaaS features. The direct custom reset/preference handlers also remain enabled through the attached repository.

Confirmed with a temporary Go overlay regression exercising the production provider in simple mode and a valid exhausted subscription. Result:

    --- FAIL: TestReview2SimpleModeDoesNotSpendSubscriptionValidity
    simple mode performed 1 paid reset(s); subscription validity changed
    from 2026-09-16 12:00:00 +0000 UTC to 2026-09-15 12:00:00 +0000 UTC

Suggested scope: in the custom provider, attach dailyResetRepo/start the scanner only outside simple mode. Keeping dailyResetRepo nil in simple mode also makes existing custom reset/preference methods reject and TriggerAutoDailyReset return without a debit. Preserve the standard-mode scanner and all official simple-mode key-window behavior.

Reproduction artifacts, in %TEMP%/sub2api-merge-v028-20260924/:

- review2_simple_mode_scanner_test.go
- review2-simple-mode-overlay.json
- review2-simple-mode-scanner-red.log

Command:

    go test -overlay <review2-simple-mode-overlay.json> ./internal/service -run '^TestReview2SimpleModeDoesNotSpendSubscriptionValidity$' -count=1

## Round-1 repair verification

The current authoritative admission group-guard repair is correct in the inspected paths:

- A present authoritative group always runs the official availability and exclusive-group guards before emitting a subscription admission error.
- Missing/deleted groups represented by nil plus ErrSubscriptionInvalid run the official deleted-group response and telemetry path.
- ErrBillingServiceUnavailable takes precedence over a nested ErrSubscriptionInvalid cause, preventing a database outage from being classified as group deletion.
- Standard and Google variants preserve official group messages, business-limited markers, and ingress rejection reasons.
- Simple mode and billing-exempt usage/billing/task-read requests keep their original guard path.
- The 24 new scenarios cover current and stale group state, wrapped errors, subscription lookup failures, response shape, forwarding, and both telemetry markers.

Independently read round1-group-guard-middleware-green.log: full tagged middleware suite PASS, exit code 0 (2026-09-24 01:01:39 +08:00). No duplicate broad suite was executed.

## Inspected areas and upstream compliance

Compared official-upstream-to-frozen-tree backend custom differences, prioritizing the overlap list from docs/upstream-v0.2.8-file-comparison.csv. Inspected:

- BillingCacheService authoritative subscription admission, cancelled probes, balance refill generation fences, API-key rate limits, user/platform quota and RPM boundaries.
- Official simple-mode DB-authoritative key spending windows and gateway usage command/finalization paths; the existing custom atomic-billing guard no longer masks the official simple-mode error or introduces other debit effects.
- Messages, chat, responses, Gemini, web search, OpenAI account-slot and WebSocket subscription revalidation, fallback subscription state, and release paths.
- Payment completion welfare accrual, confirmed refund reversal, recovery metadata, retry compare-and-swap, currency precision and transaction boundaries.
- Usage-billing welfare accrual and batch-image settlement amount alignment, wallet locking, durable balance invalidation outbox and auth cache refresh.
- Daily reset admission, preference changes, versioning, natural quota maintenance, preserved calendar behavior, auto-reset scanner, renewal/extension and subscription repository mutations.
- Service/repository/handler providers and generated wire construction; official version-sync, OpenCode usage, referral, plugin and cleanup wiring remains present. Group/DTO/routes/settings additions remain additive.
- redeem_service.go has no custom diff against official upstream, preserving the official reduction-lock and remaining-duration changes.

No other concrete P0/P1/P2 issue was established in these areas. Except for the simple-mode paid-reset side effect above, inspected official changes remain integrated.

## Verdict and limits

Frozen round-2 candidate requires the P2 correction above before backend approval. This is a new round-2 finding, independent of the resolved round-1 group-response defect.

This review does not claim live PostgreSQL/Redis concurrency validation or release readiness. Parent-reported full unit/build results were not rerun. The targeted reproduction exercises production provider/scanner logic with repository fixtures; it does not claim a live database run. Existing SQL proves the reset subtracts 24 hours when applied. Full migration and frontend review are assigned separately.

## Round-2 repair recheck — P2 resolved

Recheck performed on the source changes after tree 178dc44ffe087cc8bcd837c76ea5ca95e1b5643c. This is the resolution check for round 2, not round 3.

- Reviewed the provider diff independently: only the custom dailyResetRepo attachment and scanner startup are conditional on cfg being nil or RunMode not being simple. Official NewSubscriptionService construction and upstream billing logic remain intact.
- Production generated wiring already calls ProvideSubscriptionService; no generated wiring change is necessary for this guard to apply.
- In simple mode dailyResetRepo remains nil, so no scanner starts, TriggerAutoDailyReset returns without lookup or debit, and manual resets/preferences reject with ErrResetNotAllowed. Listing does not perform custom reset-state maintenance. Stored validity, usage and preferences remain intact for a subsequent standard-mode restart.
- Standard and nil-config construction retain the repository and both immediate and 30-second scans.
- Inspected subscription_daily_reset_provider_test.go. Its synctest cases assert actual Apply calls/24-hour expiry changes, a second periodic tick, direct trigger/manual/preference behavior and unchanged state. The standard/nil controls demonstrate the fixture is capable of performing the paid reset. The tests fail against the previous provider and pass with the guard; they do not merely assert implementation fields.
- Independently read fix-simple-mode-reset-red.log and fix-simple-mode-reset-green.log. All five affected simple-mode cases fail before the repair; standard/nil controls pass. The focused green run passes the new provider tests and surrounding simple-mode/atomic billing regressions (service package 1.823 seconds).
- Re-ran this reviewer's original temporary overlay reproduction unchanged against the fix. It now passes independently: service package PASS, 0.549 seconds, command exit code 0. Evidence: review2-simple-mode-scanner-recheck-green.log in the same audit directory.

Updated verdict: no unresolved P0/P1/P2 backend findings remain from round 2. The original simple-mode financial side effect is resolved, and inspected upstream behavior is preserved. Backend scope is acceptable for round 3 and final verification. Live PostgreSQL/Redis validation and final release gates remain outside this recheck. No source/index edits or broad duplicate suites were performed by this reviewer.


---

## Round 2: frontend

# Frontend review — round 2

## Verdict

No actionable P0/P1/P2/P3 findings identified in the reviewed frontend merge. The reviewed frontend scope is acceptable to proceed to round 3; this is not an approval of backend behavior, database migration execution, deployment, or release publication.

Requirement verdict: PASS for official frontend logic/function priority and adaptation of retained custom behavior in the reviewed overlap.
Quality verdict: PASS within static review plus the independently inspected existing test evidence. No source changes were made by this reviewer.

## Fixed inputs and independence

- Worktree: C:/Users/Administrator/.codex/worktrees/sub2api-upstream-v028/Sub2api
- Frozen candidate tree: 178dc44ffe087cc8bcd837c76ea5ca95e1b5643c
- Fork starting commit: 713d2852e8ea8addce365f595d95823d26bb5aa2
- Official upstream commit: a3eb7ef302961cba716dc78b39b93b60c467db0e
- Merge base: aea725f2ea644d5592d0bbb1d63b607efa7e200a
- This is round 2. I did not read the round-1 frontend report or use its verdict to determine mine.
- Read-only git comparison at completion found no frontend working-tree differences from the frozen candidate tree.

## What was checked

1. Independently classified frontend changes from the merge base. Official upstream changed 142 frontend paths, the fork changed 71, and the intersection was 10. All 132 official-only frontend paths are identical to the official upstream in the frozen candidate tree.
2. Reviewed all 10 intersection paths: API client and its test, BaseDialog, English/Chinese admin overview locale files, announcements store, central types, GroupsView, and the two affected GroupsView test files. Also followed the retained auth/session and UI callback paths in App, auth store/API, tokenRefresh, authSession, announcement bell, and admin compliance store/dialog.
3. BaseDialog.vue:57,170-173,207-214,223-278: the official ID-set scroll-lock registration remains synchronous at show time and is released on hide/unmount. The custom dialogStack is separate and registered after nextTick; disposing or superseding an opening prevents a stale focus entry. Nested focus/inert ownership, restoration of preexisting inert attributes, top-dialog focus restoration, listener cleanup, and the default opt-out behavior were inspected. The preserved official scroll-lock tests and the custom replacement-dialog regression cover the specific merge boundary.
4. announcements.ts:18-19,26-49,78-150: the official fetchGeneration protects newest-request data, failure/throttle handling, and loading state. reset invalidates fetchGeneration and the separate sessionGeneration. Retained mark-read, mark-all, and popup-delay operations use sessionGeneration, so a same-session forced fetch does not incorrectly cancel those operations. The candidate retains the official fetch tests and adds explicit forced-fetch/custom mutation intersection coverage.
5. client.ts:135-148,177-189,218-238,311-316 and authSession/tokenRefresh/auth store: inspected old-request 401 suppression, delayed compliance events, same-session refresh acceptance, new-session refresh isolation, stale logout/profile results, and request metadata ownership. The new official network error code is retained. The merge does not otherwise change the fork's auth store/session implementation.
6. GroupsView.vue:4402-4473,5892-5918,5939-5953,6084-6091,6255,6280-6294: official reasoning multiplier initialization, loading, validation, and serialization are retained. The custom day-reset permission remains a separate request field restricted to subscription groups with a positive daily limit; closing a create draft resets that permission. UI tests cover editing both multiplier and permission fields together, creation/reset of drafts, and copied group permissions.
7. Compared the modified official test files. Existing official assertions are preserved in the reviewed overlap; additions extend coverage. The codex manifest test changes are a required auth-store mock plus the current model_allowlist fixture field, without removing its original assertion.

## Verification evidence inspected

The report %TEMP%/sub2api-merge-v028-20260924/frontend-vitest.json reports:

- success: true
- 350 test result files
- 874/874 reported suites passed
- 2,687/2,687 individual assertions/tests passed
- 0 failed or pending tests
- Run: 2026-09-24 00:43:18.691 to 00:59:16.801 Asia/Shanghai

The individual test-result counts were independently summed and matched 2,687. Relevant passed files include BaseDialog.spec (8), BaseDialog.scrollLock.spec (4), announcements.fetch (6), announcements.identity (15), announcements.markAll (4), client.authSession (17), client.spec (33), auth.spec (21), auth.logoutIdentity (2), App.subscriptionIdentity (5), GroupsView.duplicate (13), and GroupsView.codexManifest (1).

I inspected the stored production build log ending in “built in 31.92s”; it contains the existing large-chunk warning. Lint/typecheck logs are empty, consistent with the parent-reported successful commands, but these files alone do not independently establish process exit status. This review did not rerun those gates or the full frontend suite, as requested. A fresh git diff --check on the fork-to-frozen-tree frontend delta returned exit 0 and no whitespace errors.

## Limits

This round is a frontend static review and inspection of already completed test/build evidence, not a browser execution pass. It does not establish native browser keyboard navigation, screen-reader behavior, or cross-tab integration beyond the code and existing tests. Test fixtures use mocks; successful tests alone do not prove backend authorization or pricing correctness. Those remain in the other review scopes and release gates. No blocker was found in the reviewed frontend integration.


---

## Round 2: migrations-release

# Round 2 independent review: migrations and release

Verdict: no actionable findings in the reviewed scope. This is round 2 of the required three rounds; it is not deployment or publication approval.

Reviewed candidate tree supplied by coordinator: `178dc44ffe087cc8bcd837c76ea5ca95e1b5643c`.
Old custom source: `713d2852e8ea8addce365f595d95823d26bb5aa2`.
Official source: `a3eb7ef302961cba716dc78b39b93b60c467db0e`.

## Independent evidence

- Read the migration runner, three new official SQL files, all three retained custom migrations, the existing official-upgrade test, the new deployed-custom upgrade test, and the pricing migration integration cases.
- Independently hashed raw Git blobs in sorted filename order using `filename + NUL + trimmed SQL + NUL`: old custom baseline has 289 SQL files and hash `47bd915b3a9b9baf8b145c432617b26082f138dcc34170ccfeabdd4079484648`; official baseline has 289 files and hash `6075250885f45555d7671005dc80db75c8848e9066a0cc3d291cd36a80c30947`. Both match the tests.
- Compared every baseline SQL blob against the index. Zero changed historical custom SQL files and zero differences from official SQL. Candidate contains 292 SQL files; the only additions relative to the deployed custom baseline are `238b_content_moderation_engine_meta.sql`, `239_channel_reasoning_effort_multipliers.sql`, and `240_affiliate_ledger_operation_id.sql`.
- Runner identity and checksum checks use complete filenames. It is unchanged from upstream. Shared numeric prefixes therefore do not replace an existing custom migration or suppress an official one.
- `backend/internal/repository/subscription_daily_reset_v028_upgrade_integration_test.go:41` constructs and pins the real deployed baseline before introducing the three official files. Lines 132-176 compare seeded business data, preserve original migration history including timestamps, assert the intended group pricing conversion and nullable new fields, and check every recorded checksum. Lines 179-189 compare the complete post-upgrade data and migration history across the next startup. Reviewed seed rows satisfy the retained custom schema constraints.
- The official-upgrade test independently pins its own baseline and checks that adding custom migrations does not opt existing users into paid resets or welfare. Upstream pricing integration cases cover preserving deliberately cleared maps and SQL replay.
- `.github/workflows/backend-ci.yml:42` invokes `make test-integration`; repository `TestMain` fails instead of silently skipping when `CI` is set and Docker is unavailable. The Linux `release-helpers` job remains enabled at line 86.
- Confirmed backend CI, the official release workflow, official release helpers, and the migration runner are byte-for-byte identical to upstream in the candidate index.
- Reviewed `.github/workflows/publish-custom-v028.yml`: event SHA is immutable per run and verified after checkout; the same SHA is injected into the binary and OCI revision label; native amd64/arm64 jobs pull and smoke the produced digests; the final manifest uses those verified digest artifacts and waits for both build jobs. Both architecture and final publication stages reject an existing final version. The ordinary official release workflow is tag-triggered, so the planned source branch push does not trigger it.
- Ran `go test ./migrations` successfully (cached), and `git diff --cached --check` passed. Confirmed no unstaged changes to the reviewed custom workflow or new upgrade test.

## Required evidence still outstanding

Actual PostgreSQL execution of the migration suites has not been performed in this review because the local Docker engine is unavailable. Integration compilation and static review do not substitute for execution. The official release-helper suite also requires the Linux CI environment. No CI run, publication, branch update, tag, container restart, code edit, staging operation, or commit was triggered by this review.

Retain the controlled release sequence: after all three review rounds and local gates, commit/push only `codex/merge-upstream-v0.2.8`; require successful CI and Security Scan for that exact source SHA, including actual integration and release-helper execution; only then fast-forward the feature branch and push the identical SHA to `codex/publish-v0.2.8-r1`. The publishing workflow binds output to its event SHA, while the prerequisite CI authorization is supplied by this controlled process.


---

## Round 3: backend

# Independent review round 3 — backend

## Verdict

**Code review ready for the exact-SHA GitHub validation gate. No actionable P0, P1, or P2 findings were found in this review.** This is a fresh independent review of the frozen merged backend; the previous review verdicts were not used as substitutes.

This verdict does not authorize target-branch integration or publication before the requested GitHub integration, lint, security, and build checks pass on the actual candidate commit.

## Reviewed identity and preservation

- Worktree: C:/Users/Administrator/.codex/worktrees/sub2api-upstream-v028/Sub2api
- Frozen staged tree: 045353a21ddaeecbd2898ceb113abeba9e079997
- Fork commit: 713d2852e8ea8addce365f595d95823d26bb5aa2
- Official upstream: a3eb7ef302961cba716dc78b39b93b60c467db0e
- Merge base: aea725f2ea644d5592d0bbb1d63b607efa7e200a

Independently compared tree entries for every upstream-only path identified in file-comparison.json: **441 checked, zero differing blobs or modes**. Independently confirmed backend/internal/service/redeem_service.go is identical to official upstream, preserving the new locked reread and calendar-day reduction semantics. git diff --cached --check returned exit 0; no unstaged paths were present.

Reviewed the backend overlap changes against both the official upstream snapshot and its changes from the base: generated/server/provider wiring and cleanup, handler wiring/routes and DTO mappings, gateway handlers and WebSocket turns, billing_cache_service.go, gateway_usage_billing.go, setting_service.go/settings_view.go, and redeem_service.go. Also inspected the custom admission, daily reset repository/service, subscription repository/versioning, balance cache fencing, atomic usage/welfare accounting, and payment completion/refund transactions that those overlaps invoke.

## Specific conclusions

1. **Round 1 group guard fix:** Standard and Google middleware apply official group-unavailable/exclusive-group guards using the authoritative group returned by admission. A confirmed missing group produces the official deleted-group response and ingress/business-limited metadata. Infrastructure errors are not misclassified as deletion, including wrapped subscription causes. Skip-billing and simple-mode requests retain the official cached group guard path.

2. **Round 2 simple-mode fix:** ProvideSubscriptionService attaches the custom reset repository and starts its scanner only for nil/standard configuration. In simple mode the service has no reset repository: the scanner cannot start, automatic triggers cannot spend validity, and manual/preference endpoints reject without mutation. Standard and nil configurations retain immediate and periodic recovery scans. Official simple-mode API-key monetary-window opt-in admission, atomic settlement, and WebSocket turn rechecks remain present.

3. **Subscription compatibility:** Admission uses current group/subscription data and reruns eligibility after concurrency waits. Weekly/monthly exhaustion prevents a paid daily reset; reset application serializes current state and checks version/date, operation identity, remaining validity, and eligibility before charging 24 hours. Window/version updates and reset flags are carried through repository mapping and renewal. Upstream group/bulk operations and redemption logic remain wired into the adapted repositories.

4. **Financial paths:** Usage and batch-image welfare accrual run inside the debit/capture transaction and retain idempotent source identity. Balance refill generation checks prevent stale queued refills from resurrecting pre-credit data. Payment completion couples its lease claim and welfare reward accrual in one Ent transaction. Refund completion couples order state, welfare reversal, and audit; confirmed external refund recovery persists metadata atomically and skips a second provider request or entitlement deduction. The reviewed CAS predicates guard concurrent finalizers and stale recovery attempts.

5. **Runtime wiring:** New official Claude version sync, OpenCode Go usage, plugin account directory, referral/quota, dashboard setting dependencies, and cleanup paths are retained. Custom reset and welfare providers are additive to that graph. Paid reset cancellation and welfare outbox shutdown are included before infrastructure close.

## Fresh verification

Only focused regressions were rerun by this reviewer; no broad duplicate suite was run.

- `go test -tags=unit ./internal/service -run '^TestProvideSubscriptionService(DailyResetScannerRunMode|SimpleModeDailyResetEntryPoints)$' -count=1` — exit 0; service package passed in 0.725s. Includes simple/standard/nil startup and periodic scan controls and simple-mode automatic/manual/preference entry points.
- `go test -tags=unit ./internal/server/middleware -run '^TestAPIKeyAuthAdmissionPreservesOfficialGroupGuards$' -count=1` — exit 0; middleware package passed in 0.561s. Covers the 24 standard/Google scenarios.
- Independently parsed completed backend-final-affected.jsonl: **15,524 test/subtest passes, 3 package passes, 4 conditional test skips, zero failure records**. Package results are service, cmd/server, and middleware.
- Final embedded build exit 0 and version 0.2.8-r1 with premerge-review-final label were reported by the integrating agent. The reviewer verified that the final build log exists and the rebuilt sub2api.exe exists; an empty build log alone was not treated as independent proof of the build command exit status.

## Testing limits and remaining gate

The final affected-package skips are TestAuthPendingIdentityService_UpsertAdoptionDecision_ClearsLegacyNullSessionReference, TestContentModerationTypeSafeLive, TestEstimateOpenAIInputTokens_CompareWithOpenAIAPI, and TestPluginRuntimeIntegration.

This Windows review did not execute PostgreSQL/Redis integration transactions, Linux lint/security checks, live provider tests, or the GitHub container build. Existing integration tests were inspected where relevant, but compilation and unit/mocked transaction coverage do not substitute for running them. The integrating agent's broader earlier unit run is supplementary evidence; it was not represented as a fresh full-suite run on this final tree.

**Required next step:** commit the reviewed candidate and require the exact commit's GitHub integration, lint, security, and build results before target-branch integration or publication.

No source files were edited, staged, or committed by this reviewer. Only this report was written outside the worktree.


---

## Round 3: frontend

# Independent review round 3 — frontend

- Date: 2026-09-24
- Reviewer: review3_frontend (independent source inspection; prior review conclusions were not used as evidence)
- Worktree: C:/Users/Administrator/.codex/worktrees/sub2api-upstream-v028/Sub2api
- Reviewed staged tree: 045353a21ddaeecbd2898ceb113abeba9e079997
- Fork: 713d2852e8ea8addce365f595d95823d26bb5aa2
- Upstream: a3eb7ef302961cba716dc78b39b93b60c467db0e
- Common ancestor: aea725f2ea644d5592d0bbb1d63b607efa7e200a

## Findings and verdict

No concrete P0, P1, or P2 findings in the reviewed frontend merge. The frontend changes are code-ready within this review scope. Official behavior has been preserved while the existing custom features remain connected through additive fields and separate state ownership mechanisms.

This is a frontend source and verification-evidence verdict. It does not approve backend runtime behavior, database upgrade execution, release CI, production deployment, or image publication. Those gates remain owned by the coordinating review.

## Scope and contract review

All 10 frontend paths changed on both branches were inspected against both upstream and the fork:

1. frontend/src/api/__tests__/client.spec.ts
2. frontend/src/api/client.ts
3. frontend/src/components/common/BaseDialog.vue
4. frontend/src/i18n/locales/en/admin/overview.ts
5. frontend/src/i18n/locales/zh/admin/overview.ts
6. frontend/src/stores/announcements.ts
7. frontend/src/types/index.ts
8. frontend/src/views/admin/GroupsView.vue
9. frontend/src/views/admin/__tests__/GroupsView.codexManifest.spec.ts
10. frontend/src/views/admin/__tests__/GroupsView.duplicate.spec.ts

Additional source and tests were inspected for auth sessions, token refresh, App.vue store invalidation, welfare API ownership and useWelfare lifecycle handling, group reasoning multiplier serialization, dialog scroll locking, focus trapping, and announcement fetch ownership.

- BaseDialog.vue: upstream synchronous Set-based scroll-lock registration and removal remains intact. The custom dialogStack manages focus/inert state separately. Hidden siblings, nested dialogs, unmounting and replacement dialogs cannot remove another open dialog's scroll registration. Trap focus stays opt-in. Generation/disposed guards prevent late nextTick work from reviving a closed component.
- announcements.ts: upstream fetchGeneration remains the owner of fetch success, failure, loading and throttle state. reset increments it. Custom sessionGeneration independently guards pending read mutations and popup timers, so a same-session refresh does not invalidate a legitimate mark-read and an account switch invalidates old work. App.vue synchronously resets the stores on user/session transitions.
- client.ts and supporting auth/welfare code: the official network error code change remains present. Custom request ownership guards stale 401 retries/session clearing and compliance events, while same-session token refresh remains valid. Welfare requests capture identity before dispatch; the request interceptor rejects changed ownership, and the composable aborts/ignores outdated responses and retains scoped idempotency recovery state.
- GroupsView.vue: official reasoning_effort_multipliers initialization, API-to-form copying, form-to-API serialization, and validation remain present for create/update. Custom day-reset permission is initialized, loaded, reset and normalized at submit; its payload does not overwrite model_pricing. Duplicate editing retains the server-provided permission. The combined test explicitly saves reasoning pricing and reset permission in one update.
- Types and locales: upstream OpenCode Go, Codex credits/referrals and pricing-related changes remain present. Custom welfare and subscription-reset types/labels are additive. The codex manifest test adaptation matches the current auth dependency and model_allowlist contract.

## Independent verification and existing evidence

- Recomputed the frontend branch overlap: 10 files.
- Independently compared staged blobs against upstream for every frontend path changed only upstream: 132 files, 0 mismatches.
- Confirmed the staged tree equals the requested review tree.
- git diff --exit-code -- frontend: exit 0 (no unstaged frontend differences).
- git diff --exit-code 178dc44ffe087cc8bcd837c76ea5ca95e1b5643c 045353a21ddaeecbd2898ceb113abeba9e079997 -- frontend: exit 0 (frontend unchanged since the recorded round-2 tree).
- git diff --cached --check: exit 0.
- Parsed frontend-vitest.json directly: 350 test files passed; 2,687 tests passed; 0 failed; 0 pending; success=true. Vitest also reports 874 nested suites; that is not the file count.
- Confirmed passing recorded cases for BaseDialog (14 across three files), announcement fetch/identity/mark-all (25), API client/auth-session/retry-after (51), GroupsView duplicate/pricing/reset permission (13), GroupsView manifest (1), welfare API (2), useWelfare (33), WelfareRedemption (5), and WelfareView (24).
- Read the lint/typecheck/build evidence. Lint and typecheck logs are empty; their successful exit status is recorded in verification-summary.json. The build log explicitly ends with a successful build. Its existing bundle-size warning is not a newly identified merge regression.

No broad test suite was rerun, and no source, index, branch or commit was changed by this review. Only this report was written. Browser execution and new end-to-end validation were outside this review; the existing automated evidence was assessed together with the independent code comparison.


---

## Round 3: migrations-release

# Round 3 independent review: migrations and release provenance

Reviewed frozen tree: `045353a21ddaeecbd2898ceb113abeba9e079997`.
Custom parent: `713d2852e8ea8addce365f595d95823d26bb5aa2`.
Official parent: `a3eb7ef302961cba716dc78b39b93b60c467db0e`.
Merge base: `aea725f2ea644d5592d0bbb1d63b607efa7e200a`.
Reviewer scope: SQL/history compatibility, both upgrade integration tests, CI execution gates, custom image publication provenance, and audit documentation/CSV accuracy. Read-only review; the only file written is this external evidence report.

## Verdict

No actionable P0, P1, or P2 finding in this scope. Code/configuration is ready for the controlled CI gate. This is conditional approval, not a claim that database execution, GitHub CI, Security Scan, or image publication has succeeded.

Required sequence remains: create the reviewed merge commit, push it only to `codex/merge-upstream-v0.2.8`, require successful CI and Security Scan for that exact SHA, then fast-forward the remote `feature/monthly-subscription-daily-reset` branch and push the same SHA to `codex/publish-v0.2.8-r1`. Do not push `v*` tags. Preserve the original dirty checkout.

## Independent source evidence

- `git diff --cached 045353a21ddaeecbd2898ceb113abeba9e079997 --name-only` and `git diff --name-only` produced no differences at review time. `git diff --cached --check` passed.
- Enumerated Git blobs from both parents and the frozen tree. All 289 official SQL files and all 289 deployed custom SQL files are byte-identical to their corresponding source. The merged tree has 292 SQL files. No historical filename/content was replaced.
- Independently recomputed each ordered filename/NUL/trimmed-content/NUL SHA-256 directly from Git blobs, and confirmed again from worktree bytes:
  - Official: `6075250885f45555d7671005dc80db75c8848e9066a0cc3d291cd36a80c30947`.
  - Deployed custom: `47bd915b3a9b9baf8b145c432617b26082f138dcc34170ccfeabdd4079484648`.
- Independently confirmed 473 upstream changes, 256 custom changes, 32 overlapping paths, 697 source-delta paths, and 441 upstream-only paths. All 441 upstream-only paths match the official blobs exactly.
- Checked all 704 CSV rows against Git trees using the CSV's actual classifications (`upstream-only`, `fork-only`, `overlap`, `integration-only`). No missing source-delta path, incorrect class, or incorrect upstream/fork equality flag. The additional seven rows are the explicitly classified integration files.
- Official release source `fd80b08c90b55edcad5b00171b53f08721d30da1` differs from the official synchronization point only in `backend/cmd/server/VERSION`, consistent with the audit report.
- Migration runner and official CI, Security Scan, and Release workflows match the official source. No custom change to historical checksum compatibility rules or official release triggers was introduced.

## Migration and upgrade review

- `backend/internal/repository/migrations_runner.go:25,180,201` uses full filename primary keys, lexicographic ordering, and per-file SHA-256 checking. The duplicated numeric prefixes at 235, 239 and 240 remain distinct migration records. Existing applied rows are validated and skipped; their checksums/applied timestamps are not rewritten.
- `subscription_daily_reset_upgrade_review3_integration_test.go:24,61,100,140` pins the complete official 289-file baseline, inserts existing customers/subscriptions/keys, then applies the custom files. It compares existing data/history, verifies resets and welfare are opt-in by default, checks both filenames for shared prefixes, and verifies a second startup preserves customer choices and migration history.
- `subscription_daily_reset_v028_upgrade_integration_test.go:23,60,160,169,186` pins the deployed custom 289-file baseline. The fixture includes users, group pricing, subscriptions, reset events, keys, payment orders, welfare settings/wallets/operations/checkins/ledger/spend/rewards/outbox, moderation and affiliate history. It compares existing rows and old migration history, verifies the official pricing conversion and new nullable fields, verifies all 292 recorded checksums, then repeats startup and compares the complete state again.
- Read the three new official SQL files and three historical custom SQL files. The official additions retain their upstream semantics; none conflicts with the custom tables or fields.
- `backend/internal/repository/integration_harness_test.go:54` exits unsuccessfully when Docker is unavailable in CI. `.github/workflows/backend-ci.yml:42` executes `make test-integration`, which includes both tagged upgrade tests. An unavailable CI Docker daemon cannot silently count as successful database validation.

## Publication provenance review

- `.github/workflows/publish-custom-v028.yml:5,20,39,46` binds automatic publication to the dedicated branch and checks out/verifies the immutable event SHA. The binary COMMIT build argument and OCI revision label use that same value.
- Native Ubuntu amd64 and arm64 runners build the images. The smoke checks at line 93 pull by digest, validate OS/architecture/revision, inspect embedded binary version/commit, run PostgreSQL client binaries, check runtime resources, and exercise setup status and embedded frontend serving.
- Successful architecture jobs upload their verified digests. The dependent publication job validates digest syntax and creates the final manifest from those digests, then confirms both architectures.
- Existing final version detection fails closed both before architecture builds and before final manifest creation. Workflow concurrency prevents overlapping runs of this publication workflow.
- The publication workflow does not itself query prior CI results; the explicitly controlled push sequence above supplies that gate. No publication branch push or manual dispatch is authorized by this review until CI and Security Scan succeed for the reviewed SHA.
- Ordinary `v*` release workflow is the exact upstream version and will not be triggered by the prescribed branch pushes.

## Evidence limits and final delivery requirements

This reviewer did not execute PostgreSQL/Docker tests or trigger any remote action. Local Docker is reported unavailable with existing workloads, and the supplied release-helper log records Windows/WSL execution and file-mode failures; these are accurately treated as pending Linux CI gates rather than passing checks. Audit documentation explicitly avoids claiming completed CI, migration execution, or publication.

After gates and image publication actually succeed, record the final source SHA, exact-SHA CI and Security Scan run URLs, publication run URL, GHCR version tag and multi-platform digest. Finalization of the pre-execution plan/checklists must reflect those observed results. No production deployment is included.
