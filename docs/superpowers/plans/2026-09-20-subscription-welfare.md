# Subscription Purchase Draws Implementation Plan

> For agentic workers: use subagent-driven-development for bounded tasks. Shared service contracts belong to the coordinating agent.

**Goal:** Grant floor(single CNY payment / 50) draws on subscription fulfillment.
**Architecture:** Transaction-only WelfarePaymentRepository reads payment snapshots, writes order-unique reward records and wallet.subscription_draws. Entitlement = floor(API USD spend/50) + subscription_draws; order remainder never enters API spend.
**Tech Stack:** Go, Ent, PostgreSQL, Vue, Vitest.

## Tasks

- [ ] Repository worker: failing TestWelfarePayment* integration cases first; migration240 adds subscription_draws and welfare_subscription_rewards; implement NewWelfarePaymentRepository and transaction-only AccrueSubscriptionPurchase/ReverseSubscriptionPurchase. Read locked persisted order, use CNY pay_amount, lock user then wallet; guard launch/PaidAt/paused. Refund recomputes remaining draws, preserves balance/rewards/draws_used. Test 49.99/50/99/100/200, two25/two75, currencies, retry, concurrency, rollback, historical/paused, partial/full refund.
- [ ] Coordinator: add service interface/field, tests for markCompleted repository invocation/transaction/error/replay, then implement atomic completion CAS plus accrual. Wire production constructor in repository/service providers and existing assembly.
- [ ] Coordinator: tests for subscription-only draws, combined entitlement/debt/API remainder, then update welfareOverview/Draw and wallet read paths. Expose subscription_draws and per-order CNY threshold in public rules.
- [ ] Refund worker: tests first, then make immediate markRefundOk transactional and share markRefundOkTx with pending refund. CAS and reverse in same transaction. Persist confirmed-gateway local recovery metadata to avoid repeat refund or rights deduction.
- [ ] Frontend worker: Chinese/English per-order CNY rules separately from API USD progress, examples and refund explanation; types and component display. Run direct local Vitest welfare+i18n and typecheck.
- [ ] Review independently for specification then money/locking/recovery correctness. Run backend unit, real DB welfare/payment integration, frontend tests/lint/typecheck/build, backend build and diff checks. Integrate verified work into feature branch while preserving unrelated files.

Repository command: CI=true go test -p 1 -tags=integration ./internal/repository -run TestWelfarePayment -count=1 -timeout=10m.
Service command: go test -p 1 -tags=unit ./internal/service -run 'Test(PaymentWelfare|Welfare|.*Refund)' -count=1.
