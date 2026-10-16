# OpenAI Group Ultrafast Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a mutually exclusive OpenAI-group Ultrafast switch that forces `gpt-6.1-sol`/`gpt-6-astra` to `service_tier=ultrafast` and charges only that enabled group at Standard effective pricing × 8.

**Architecture:** Persist a group-scoped boolean, carry it through the trusted group/API-key cache projection, and centralize outbound tier forcing beside the existing OpenAI Fast policy. Add a request-local Ultrafast multiplier override to the shared token-pricing path so the global model Ultrafast multiplier remains unchanged for groups without the switch. For an enabled target request, the global policy may block but may not filter or rewrite the forced Ultrafast tier.

**Tech Stack:** Go 1.27 + Gin + Ent + PostgreSQL migrations; Vue 3 + TypeScript + Vitest + pnpm; existing OpenAI HTTP/WebSocket gateway and billing resolver.

---

## Scope and file map

### Persistence and trusted group data

- Create: `backend/migrations/246_group_force_openai_ultrafast.sql`
- Create: `backend/migrations/group_force_openai_ultrafast_migration_test.go`
- Modify: `backend/ent/schema/group.go`
- Generate: `backend/ent/group.go`, `backend/ent/group/group.go`, `backend/ent/group_create.go`, `backend/ent/group_update.go`, `backend/ent/mutation.go`, `backend/ent/runtime/runtime.go`, and any Ent files changed by `go generate ./ent`
- Modify: `backend/internal/service/group.go`, `backend/internal/service/admin_group.go`, `backend/internal/service/admin_service.go`, `backend/internal/service/admin_group_duplicate.go`
- Modify: `backend/internal/repository/group_repo.go`, `backend/internal/repository/api_key_repo.go`
- Modify: `backend/internal/service/api_key_auth_cache.go`, `backend/internal/service/api_key_auth_cache_impl.go`
- Modify: `backend/internal/handler/admin/group_handler.go`, `backend/internal/handler/dto/types.go`, `backend/internal/handler/dto/mappers.go`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Modify/add: related group handler, mapper, admin service, duplicate, repository projection, and cache tests

### Gateway policy and billing

- Modify: `backend/internal/service/openai_gateway_request_body.go`
- Modify: `backend/internal/service/openai_gateway_forward.go`
- Modify: `backend/internal/service/openai_ws_forwarder_ingress.go` or the shared WS caller only where the existing helper is invoked
- Modify: `backend/internal/service/openai_ws_v2_passthrough_adapter.go` or the shared WS caller only where the existing helper is invoked
- Modify: `backend/internal/service/service_tier_billing.go` only if a shared target-model/billing helper belongs there; otherwise keep the helper in the OpenAI policy/billing file
- Modify: `backend/internal/service/billing_service.go`, `backend/internal/service/openai_gateway_usage.go`
- Test: `backend/internal/service/openai_fast_policy_test.go`, `backend/internal/service/openai_fast_policy_ws_test.go`, `backend/internal/service/openai_fast_service_tier_test.go`, `backend/internal/service/openai_gateway_record_usage_test.go`, `backend/internal/service/billing_service_test.go`, and focused gateway tests

### Admin UI

- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/admin/GroupsView.vue`
- Modify: `frontend/src/views/admin/groupsOpenAIFast.ts` or add a focused `groupsOpenAIUltrafast.ts` helper if keeping platform predicates separate is clearer
- Modify: `frontend/src/views/admin/__tests__/groupsOpenAIFast.spec.ts` for platform scope, localized labels, and mutual exclusion
- Modify: `frontend/src/i18n/locales/zh/admin/overview.ts`, `frontend/src/i18n/locales/en/admin/overview.ts`

### Project records

- Modify: `CHANGELOG.md`
- Modify: `docs/HANDOFF.md`
- Create: `docs/changes/2026-10-17-openai-group-ultrafast.md` if implementation/verification details exceed the changelog entry

---

## Task 1: Establish the migration and domain/API contract with failing tests first

**Files:**
- Test: `backend/migrations/group_force_openai_ultrafast_migration_test.go`
- Test: `backend/internal/repository/migrations_schema_integration_test.go`
- Test: existing group handler/mapper/admin service tests
- Modify: `backend/migrations/246_group_force_openai_ultrafast.sql`
- Modify: `backend/ent/schema/group.go`
- Modify: domain, handler DTO, service input, repository, cache, and duplicate files listed above

- [ ] **Step 1: Identify the next migration number and write the migration test.**

Run from the feature worktree:

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\.worktrees\openai-group-ultrafast-20261017\backend'
Get-ChildItem migrations -Filter '*.sql' | Sort-Object Name | Select-Object -Last 1 -ExpandProperty Name
```

Use the next number after the current maximum (the inspected baseline ends at `245`). Create the test with the same pattern as `group_free_openai_fast_migration_test.go` and assert both:

```go
require.Contains(t, sql,
    "ADD COLUMN IF NOT EXISTS force_openai_ultrafast BOOLEAN NOT NULL DEFAULT FALSE")
require.Contains(t, sql, "COMMENT ON COLUMN groups.force_openai_ultrafast")
```

Add a schema-parity assertion in `backend/internal/repository/migrations_schema_integration_test.go`:

```go
requireColumn(t, tx, "groups", "force_openai_ultrafast", "boolean", 0, false)
```

- [ ] **Step 2: Run the new migration test before adding the SQL or schema.**

```powershell
go test ./migrations -run 'TestGroupForceOpenAIUltrafastMigration' -count=1
```

Expected: FAIL because the migration file is absent.

- [ ] **Step 3: Add the append-only SQL migration and Ent schema field.**

Use this exact migration shape in `backend/migrations/246_group_force_openai_ultrafast.sql`:

```sql
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS force_openai_ultrafast BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN groups.force_openai_ultrafast IS
    'Force OpenAI group target models gpt-6.1-sol and gpt-6-astra to use service_tier=ultrafast';
```

Add beside the existing Fast fields in `backend/ent/schema/group.go`:

```go
field.Bool("force_openai_ultrafast").
    Default(false).
    Comment("是否强制此 OpenAI 分组的 GPT-6.1 Sol/Astra 请求使用 service_tier=ultrafast"),
```

- [ ] **Step 4: Generate Ent code and rerun migration tests.**

```powershell
go generate ./ent
 go test ./migrations -run 'TestGroupForceOpenAIUltrafastMigration' -count=1
```

Expected: the focused migration test passes and generated Ent code contains `ForceOpenaiUltrafast` plus the `force_openai_ultrafast` field constant.

- [ ] **Step 5: Add failing contract tests for the new field.**

Extend the existing group request decode test to assert create/update decoding:

```go
require.NoError(t, json.Unmarshal(
    []byte(`{"name":"ultra","force_openai_ultrafast":true}`), &createReq,
))
require.True(t, createReq.ForceOpenAIUltrafast)

require.NoError(t, json.Unmarshal(
    []byte(`{"force_openai_ultrafast":false}`), &updateReq,
))
require.NotNil(t, updateReq.ForceOpenAIUltrafast)
require.False(t, *updateReq.ForceOpenAIUltrafast)
```

Extend the admin mapper test to verify the field is present in the admin DTO and absent from user-facing group JSON, matching `force_openai_fast` privacy behavior. Extend the group create/update service test so an OpenAI group persists the field and a non-OpenAI group normalizes it to false.

Add a mutual-exclusion assertion to the admin service test:

```go
input := &UpdateGroupInput{
    ForceOpenAIFast:       boolPtr(true),
    ForceOpenAIUltrafast:  boolPtr(true),
}
// after normalization/update: ForceOpenAIUltrafast=true, ForceOpenAIFast=false
```

- [ ] **Step 6: Run the new contract tests to verify they fail for the missing field.**

```powershell
go test ./internal/handler/admin ./internal/handler/dto ./internal/service -run 'Group.*(OpenAIFast|Ultrafast)|Mapper.*Ultrafast' -count=1
```

Expected: compile failures mentioning missing `ForceOpenAIUltrafast`/`force_openai_ultrafast` fields.

Before running the cache contract tests, increment `apiKeyAuthSnapshotVersion` from `24` to `25` and extend `api_key_auth_cache_openai_fast_test.go` (or add a new adjacent test) to round-trip `ForceOpenAIUltrafast`; the version bump is required so existing Redis snapshots are refreshed instead of silently losing the new flag.

- [ ] **Step 7: Implement the domain/API/cache/repository plumbing.**

Add `ForceOpenAIUltrafast bool` to `service.Group`, the create/update inputs, admin handler request structs, admin DTO mapper, and API-key auth cache/snapshot structs. Add Ent setters/fields through generation rather than hand-editing generated code. Set/read the field in:

- `backend/internal/repository/group_repo.go` create/update builders;
- `backend/internal/repository/api_key_repo.go` group projection;
- `backend/internal/service/api_key_auth_cache_impl.go` cache materialization and snapshot restore;
- `backend/internal/service/admin_group_duplicate.go` group duplication;
- `backend/internal/service/admin_group.go` create/update and platform normalization.

Use a new OpenAI-only normalizer for the new field and preserve the existing Fast/Free Fast platform behavior:

```go
func sanitizeGroupOpenAIUltrafast(group *Group) {
    if group == nil {
        return
    }
    if group.Platform != PlatformOpenAI {
        group.ForceOpenAIUltrafast = false
        return
    }
    if group.ForceOpenAIUltrafast {
        group.ForceOpenAIFast = false
    }
}
```

Call this alongside the existing `sanitizeGroupOpenAIFast` without changing that helper's OpenAI/Composite behavior. Keep `FreeOpenAIFast` independent; do not clear it merely because Ultrafast is enabled on an OpenAI group.

- [ ] **Step 8: Run contract, repository, and migration tests.**

```powershell
go test ./migrations ./internal/handler/admin ./internal/handler/dto ./internal/repository ./internal/service -run 'Group.*(OpenAIFast|Ultrafast)|Mapper.*Ultrafast|APIKey.*OpenAIFast|Migration' -count=1
```

Expected: all focused tests pass.

- [ ] **Step 9: Commit the persistence/API slice.**

```powershell
git add backend/ent backend/migrations backend/internal/handler backend/internal/repository backend/internal/service
 git commit -m "feat: persist group-scoped OpenAI ultrafast mode"
```

---

## Task 2: Add the admin UI switch and mutually exclusive form behavior

**Files:**
- Test: `frontend/src/views/admin/__tests__/groupsOpenAIFast.spec.ts` and focused GroupsView test
- Modify: `frontend/src/types/index.ts`, `frontend/src/views/admin/GroupsView.vue`, `frontend/src/views/admin/groupsOpenAIFast.ts`, `frontend/src/i18n/locales/zh/admin/overview.ts`, `frontend/src/i18n/locales/en/admin/overview.ts`

- [ ] **Step 1: Add failing helper and locale assertions.**

Extend the helper tests with the approved platform boundary:

```ts
expect(supportsGroupOpenAIUltrafast("openai")).toBe(true)
expect(supportsGroupOpenAIUltrafast("composite")).toBe(false)
expect(normalizeGroupOpenAIUltrafast("openai", true)).toBe(true)
expect(normalizeGroupOpenAIUltrafast("composite", true)).toBe(false)
```

Assert both locales include `ultrafast`, `8`, `gpt-6.1-sol`, `gpt-6-astra`, and the mutual-exclusion wording. Run:

```powershell
Set-Location 'E:\sub2二次开发项目\.worktrees\openai-group-ultrafast-20261017\frontend'
pnpm exec vitest run src/views/admin/__tests__/groupsOpenAIFast.spec.ts
```

Expected: FAIL because the helper/key does not exist.

- [ ] **Step 2: Add types, helper, and locale keys.**

Add `force_openai_ultrafast: boolean` to `AdminGroup`, and optional create/update request fields. Add a helper with the exact platform rule:

```ts
export function supportsGroupOpenAIUltrafast(platform: string): boolean {
  return platform === "openai"
}

export function normalizeGroupOpenAIUltrafast(
  platform: string,
  enabled: boolean,
): boolean {
  return supportsGroupOpenAIUltrafast(platform) && enabled
}
```

Add `admin.groups.openaiFast.ultrafast` and `ultrafastHint` in Chinese and English. The hint must say it affects only GPT-6.1 Sol/GPT-6 Astra, uses 8× Standard pricing for this group, is mutually exclusive with forced Fast, does not apply Free Fast, and does not promise account capability fallback.

- [ ] **Step 3: Add the switch to create/edit forms and enforce mutual exclusion in the UI.**

Add the new toggle between the existing Force Fast and Free Fast rows. Keep the existing Fast section container and gate the new toggle itself by `supportsGroupOpenAIUltrafast(platform)` so Composite does not display it. Add form defaults, reset, load-from-group, and create/update payload normalization.

Use explicit update handlers rather than relying on two-way binding alone:

```ts
function toggleGroupOpenAIUltrafast(form: GroupForm, enabled: boolean) {
  form.force_openai_ultrafast = enabled
  if (enabled) form.force_openai_fast = false
}

function toggleGroupOpenAIFast(form: GroupForm, enabled: boolean) {
  form.force_openai_fast = enabled
  if (enabled) form.force_openai_ultrafast = false
}
```

When switching away from OpenAI, normalize the new field to false. Preserve `free_openai_fast` independently.

- [ ] **Step 4: Run frontend focused tests and typecheck.**

```powershell
pnpm exec vitest run src/views/admin/__tests__/groupsOpenAIFast.spec.ts
pnpm exec vue-tsc --noEmit
```

Expected: focused tests and typecheck pass.

- [ ] **Step 5: Commit the admin UI slice.**

```powershell
git add frontend/src/types/index.ts frontend/src/views/admin frontend/src/i18n/locales/zh/admin/overview.ts frontend/src/i18n/locales/en/admin/overview.ts
git commit -m "feat: add OpenAI group ultrafast switch"
```

---

## Task 3: Add failing tests and implement group-scoped request forcing

**Files:**
- Test: `backend/internal/service/openai_fast_policy_test.go`, `backend/internal/service/openai_fast_policy_ws_test.go`, `backend/internal/service/openai_fast_service_tier_test.go`, relevant forwarder tests
- Modify: `backend/internal/service/openai_gateway_request_body.go`, `backend/internal/service/openai_gateway_forward.go`
- No WS caller changes: both existing WS entry points already call the shared `applyOpenAIFastPolicyToWSResponseCreate` helper

- [ ] **Step 1: Add pure helper tests for target models and platform scope.**

Add tests for a helper such as `openAIGroupForcesUltrafast(ctx, account, model)`:

```go
for _, model := range []string{"gpt-6.1-sol", "gpt-6-astra"} {
    require.True(t, openAIGroupForcesUltrafast(ctxWithOpenAIUltrafastGroup, openAIAccount, model))
}
for _, model := range []string{"gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.5"} {
    require.False(t, openAIGroupForcesUltrafast(ctxWithOpenAIUltrafastGroup, openAIAccount, model))
}
require.False(t, openAIGroupForcesUltrafast(ctxWithCompositeUltrafastGroup, compositeAccount, "gpt-6-astra"))
require.False(t, openAIGroupForcesUltrafast(ctxWithDisabledGroup, openAIAccount, "gpt-6-astra"))
```

Run the focused test before implementation and expect a compile failure for the helper/field.

- [ ] **Step 2: Add HTTP body tests for precedence.**

Cover these exact cases for `applyOpenAIFastPolicyToBody`:

1. Enabled OpenAI group + target model + missing tier → `service_tier=ultrafast`.
2. Enabled OpenAI group + target model + `priority` → `ultrafast`.
3. Enabled OpenAI group + target model + `flex` → `ultrafast`.
4. Enabled OpenAI group + non-target → original tier unchanged.
5. Disabled group + target + explicit `ultrafast` → existing policy path unchanged.
6. Global policy `filter` matching Ultrafast + forced target → keep `ultrafast`.
7. Global policy `force_priority` matching Ultrafast + forced target → keep `ultrafast`.
8. Global policy `block` matching Ultrafast + forced target → return `OpenAIFastBlockedError`.

Run the test and verify the new cases fail before implementation.

- [ ] **Step 3: Implement one centralized forced-tier resolver and apply it to HTTP/WS paths.**

Add a resolver that returns the group-forced tier with Ultrafast precedence:

```go
func openAIGroupForcedServiceTier(ctx context.Context, account *Account, model string) string {
    if openAIGroupForcesUltrafast(ctx, account, model) {
        return OpenAIFastTierUltrafast
    }
    if openAIGroupForcesFast(ctx, account) {
        return OpenAIFastTierPriority
    }
    return ""
}
```

`openAIGroupForcesUltrafast` must require a trusted OpenAI group, `ForceOpenAIUltrafast`, and the existing canonical model helpers for exactly Astra/6.1 Sol. It must not inspect account capability metadata.

In `applyOpenAIFastPolicyToBody`, set the forced tier before policy evaluation. When the forced tier is Ultrafast, evaluate the existing global rule only to honor `block`; ignore `filter` and `force_priority` outputs so the body remains Ultrafast. Keep the current Fast behavior unchanged when the forced tier is priority. Apply the same rule to `applyOpenAIFastPolicyToWSResponseCreate`.

Update the inline `openAIGroupForcesFast` branch in `openai_gateway_forward.go` to use the centralized forced-tier resolver and the same block-only precedence. Do not add account capability detection or automatic fallback.

- [ ] **Step 4: Run HTTP/WS policy tests.**

```powershell
Set-Location 'E:\sub2二次开发项目\.worktrees\openai-group-ultrafast-20261017\backend'
go test ./internal/service -run 'OpenAIFastPolicy|OpenAI.*ServiceTier|OpenAI.*WS' -count=1
```

Expected: all focused policy tests pass, including the new filter/force/block precedence cases.

- [ ] **Step 5: Commit the request policy slice.**

```powershell
git add backend/internal/service/openai_gateway_request_body.go backend/internal/service/openai_gateway_forward.go backend/internal/service/openai_ws_forwarder_ingress.go backend/internal/service/openai_ws_v2_passthrough_adapter.go backend/internal/service/*fast*test.go
git commit -m "feat: force ultrafast for selected OpenAI group models"
```

---

## Task 4: Add the group-scoped 8× billing override with red-green tests

**Files:**
- Test: `backend/internal/service/billing_service_test.go`, `backend/internal/service/openai_gateway_record_usage_test.go`, `backend/internal/service/openai_fast_service_tier_test.go`
- Modify: `backend/internal/service/billing_service.go`, `backend/internal/service/openai_gateway_usage.go`

- [ ] **Step 1: Add a failing pure billing test.**

Add a test that constructs the same effective Standard pricing for an enabled and disabled group and asserts:

```go
standard, _ := billing.CalculateCostUnified(CostInput{
    Model: "gpt-6-astra", Group: enabledGroup, GroupID: &enabledGroup.ID,
    Tokens: tokens, RateMultiplier: 1, ServiceTier: "",
    Resolver: resolver,
})
ultrafast8x, _ := billing.CalculateCostUnified(CostInput{
    Model: "gpt-6-astra", Group: enabledGroup, GroupID: &enabledGroup.ID,
    Tokens: tokens, RateMultiplier: 1, ServiceTier: "ultrafast",
    UltrafastMultiplierOverride: &eight, Resolver: resolver,
})
require.InDelta(t, standard.TotalCost*8, ultrafast8x.TotalCost, 1e-12)
```

Add companion assertions that the same `ultrafast` request without the request-local flag retains the existing model multiplier, and that a non-target model with the flag does not change price.

- [ ] **Step 2: Run the pure billing test before adding the new field/logic.**

```powershell
go test ./internal/service -run 'Ultrafast.*(Billing|Cost)|CalculateCostUnified.*Ultrafast' -count=1
```

Expected: compile failure because `UltrafastMultiplierOverride` is not yet defined.

- [ ] **Step 3: Add the request-local billing flag and apply it only to cloned effective token pricing.**

Add `UltrafastMultiplierOverride *float64` to the shared `CostInput` type. In `openai_gateway_usage.go`, derive a pointer to `8.0` only when the trusted `apiKey.Group` is OpenAI with `ForceOpenAIUltrafast`, the final settled service tier is `ultrafast`, and `result.UpstreamModel` (falling back to `result.Model`) canonicalizes to `gpt-6.1-sol` or `gpt-6-astra`. Do not expose this override through `TokenCostRequest` or user-controlled request fields.

In both `CalculateCostUnified` resolver and no-resolver token branches, after obtaining the effective Standard/interval pricing and after existing model-specific policy adjustments, apply a helper that clones the `ModelPricing` only when the override pointer is non-nil and sets only:

```go
pricing.UltrafastMultiplier = 8
```

Apply this clone only when all of these are true:

```go
input.UltrafastMultiplierOverride != nil
```

Do not mutate shared fallback/channel/group pricing. Do not change `ModelPricing.UltrafastMultiplier` defaults. Do not apply this override to per-request/image/video modes.

Derive the override inside `calculateOpenAIRecordUsageCost` from the final tier and actual upstream model, then pass it into `calculateOpenAIRecordUsageTokenCost`. Make both resolver and no-resolver billing paths call `CalculateCostUnified` so the request-local override is honored consistently. Response-model re-evaluation recomputes the same group-scoped override from the same outbound result. Keep `groupBillsOpenAIFastAtStandard` unchanged so Free Fast does not match Ultrafast.

- [ ] **Step 4: Add end-to-end usage billing tests.**

Extend `openai_gateway_record_usage_test.go` with these cases:

- enabled group + `gpt-6.1-sol` + final Ultrafast → customer/actual cost is Standard × 8 × existing group multiplier;
- enabled group + `gpt-6-astra` + final Ultrafast → same;
- disabled group + either target + final Ultrafast → existing old multiplier, not 8×;
- enabled group + target + priority/flex/empty final tier → no 8×;
- enabled group + non-target + Ultrafast → no new 8×;
- enabled group + Ultrafast + `FreeOpenAIFast=true` → Ultrafast remains billable, not free;
- upstream downgrade to a cheaper tier → existing downgrade resolution removes the 8× condition.

- [ ] **Step 5: Run billing tests and the focused gateway package.**

```powershell
go test ./internal/service -run 'Ultrafast|OpenAIGatewayServiceRecordUsage|ServiceTierBilling' -count=1
```

Expected: all focused billing and tier-resolution tests pass.

- [ ] **Step 6: Commit the billing slice.**

```powershell
git add backend/internal/service/billing_service.go backend/internal/service/billing_token_cost_request.go backend/internal/service/openai_gateway_usage.go backend/internal/service/*billing*test.go backend/internal/service/openai_gateway_record_usage_test.go
 git commit -m "feat: apply group-scoped ultrafast pricing"
```

---

## Task 5: Add regression coverage for migration/cache/UI contracts and update project records

**Files:**
- Test: repository/cache/duplicate/group integration tests touched by Tasks 1–4
- Modify: `CHANGELOG.md`, `docs/HANDOFF.md`
- Create: `docs/changes/2026-10-17-openai-group-ultrafast.md`

- [ ] **Step 1: Run all focused backend tests from the feature worktree.**

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\.worktrees\openai-group-ultrafast-20261017\backend'
go test ./migrations ./internal/handler/admin ./internal/handler/dto ./internal/repository ./internal/service -count=1
```

Expected: exit code 0. If an integration test requires PostgreSQL/Redis unavailable in the local environment, record the exact skipped command and reason rather than claiming it passed.

- [ ] **Step 2: Run the focused frontend suite and production checks.**

```powershell
. 'E:\sub2二次开发项目\tools\project-env.ps1'
Set-Location 'E:\sub2二次开发项目\.worktrees\openai-group-ultrafast-20261017\frontend'
pnpm exec vitest run src/views/admin/__tests__/groupsOpenAIFast.spec.ts
pnpm run typecheck
pnpm run build
```

Expected: all commands exit 0; `build` must include the i18n completeness check and Vite build.

- [ ] **Step 3: Update the changelog and handoff records.**

Add a dated `2026-10-17` changelog entry describing:

- the OpenAI-only group switch;
- mutual exclusion with forced Fast;
- exact target models;
- group-scoped Standard × 8 billing;
- unchanged behavior for disabled groups and other models;
- migration and tests actually run.

Add the new feature to `docs/HANDOFF.md` under current work/retained custom capabilities, explicitly distinguishing the unmerged feature branch from the published `0.2.16-r1` image and stating that no production deployment occurred.

If the verification details are lengthy, create `docs/changes/2026-10-17-openai-group-ultrafast.md` and link it from both records. Do not claim a release, image build, or production upgrade.

- [ ] **Step 4: Run repository hygiene checks.**

```powershell
Set-Location 'E:\sub2二次开发项目\.worktrees\openai-group-ultrafast-20261017'
git diff --check
git status --short --branch
git ls-files --error-unmatch docs/superpowers/specs/2026-10-17-openai-group-ultrafast-design.md
git diff HEAD~1 --stat
```

Expected: no whitespace errors, all intended source/docs files tracked, and no generated `output/`/`.cache/` files included.

- [ ] **Step 5: Commit project records.**

```powershell
git add CHANGELOG.md docs/HANDOFF.md docs/changes
 git commit -m "docs: record OpenAI group ultrafast feature"
```

---

## Final verification checklist

- [ ] `go test` focused backend packages pass on the feature worktree.
- [ ] Frontend focused Vitest, `pnpm run typecheck`, and `pnpm run build` pass.
- [ ] Migration is append-only and schema parity covers the new column.
- [ ] Only OpenAI groups expose/persist the switch; Composite and other platforms normalize false.
- [ ] Fast and Ultrafast force switches are mutually exclusive in UI and backend.
- [ ] Force behavior is target-model-only and capability-agnostic; unsupported upstreams return existing errors.
- [ ] Global policy can block forced Ultrafast but cannot filter or rewrite it to another tier.
- [ ] Only enabled groups with target model + final Ultrafast receive Standard × 8; disabled groups retain prior behavior.
- [ ] Free Fast does not make Ultrafast free.
- [ ] Existing user changes in the primary worktree are untouched.
