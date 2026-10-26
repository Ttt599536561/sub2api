package qqimage

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func setBusinessControls(t *testing.T, s *Store, binding, image bool) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES('qq_binding_enabled',$1),('qq_image_enabled',$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, strconv.FormatBool(binding), strconv.FormatBool(image))
	require.NoError(t, err)
}

func TestQQControlsTechnicalConfigAndSimpleModeStillDenyAdmission(t *testing.T) {
	for _, restriction := range []string{"disabled", "group", "simple"} {
		for _, stage := range []string{"request", "authorize", "confirm", "order"} {
			t.Run(restriction+"/"+stage, func(t *testing.T) {
				s := testStore(t)
				ctx := context.Background()
				u := fixtureUser(t, s, "one@example.com", "3")
				i := fixtureIdentity("one", "group")
				r, err := s.CreateBindingRequest(ctx, i)
				require.NoError(t, err)
				var code string
				if stage == "confirm" || stage == "order" {
					a, e := s.AuthorizeBinding(ctx, r.ID, u, "session")
					require.NoError(t, e)
					code = a.ConfirmationCode
				}
				if stage == "order" {
					_, err = s.ConfirmBinding(ctx, i, code)
					require.NoError(t, err)
				}
				setBusinessControls(t, s, true, true)
				if restriction == "simple" {
					s = NewStore(s.db, s.key, StoreOptions{DisableNewAdmissions: true})
				} else {
					c, e := s.GetConfig(ctx)
					require.NoError(t, e)
					if restriction == "disabled" {
						c.Enabled = false
					} else {
						c.AllowedGroups = []string{"group2"}
					}
					_, err = s.SaveConfig(ctx, *c, 1)
					require.NoError(t, err)
				}
				switch stage {
				case "request":
					_, err = s.CreateBindingRequest(ctx, i)
				case "authorize":
					_, err = s.AuthorizeBinding(ctx, r.ID, u, "session")
				case "confirm":
					_, err = s.ConfirmBinding(ctx, i, code)
				case "order":
					_, err = s.CreateOrder(ctx, i, "new", "cat")
				}
				require.ErrorIs(t, err, ErrDisabled)
				checkWallet(t, s, u, "3.00000000", "0.00000000")
			})
		}
	}
}

func TestQQControlsQueuedAdmissionReadsCommittedClosure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "3")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	setBusinessControls(t, s, true, true)
	blocker, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	_, err = blocker.Exec(`SELECT pg_advisory_xact_lock(716709231248)`)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, e := s.CreateOrder(ctx, i, "new", "cat"); done <- e }()
	setBusinessControls(t, s, true, false)
	require.NoError(t, blocker.Commit())
	select {
	case err = <-done:
		require.ErrorIs(t, err, ErrDisabled)
	case <-time.After(5 * time.Second):
		t.Fatal("admission remained blocked")
	}
	checkWallet(t, s, u, "3.00000000", "0.00000000")
}

func TestQQControlsWorkerHistoryRedeliveryAndRefundContinue(t *testing.T) {
	s, user, order := workerDBOrder(t)
	ctx := context.Background()
	setBusinessControls(t, s, false, false)
	artifact, err := validateArtifact(fixturePNG(t))
	require.NoError(t, err)
	generator := &workerGenerator{artifact: artifact}
	sender := &workerSender{status: "site_only"}
	worker := NewWorker(s, generator, sender)
	require.NoError(t, worker.RunOnce(ctx))
	require.NoError(t, worker.RunOnce(ctx))
	require.Equal(t, 1, generator.calls)
	checkWallet(t, s, user, "1.00000000", "0.00000000")
	_, err = s.GetArtifact(ctx, order.ID, user)
	require.NoError(t, err)
	_, err = s.RequestRedelivery(ctx, order.Identity, order.ID, "manual-redelivery")
	require.NoError(t, err)
	require.NoError(t, worker.RunOnce(ctx))
	require.Equal(t, 1, generator.calls)
	checkWallet(t, s, user, "1.00000000", "0.00000000")
	for range 2 {
		_, err = s.RefundOrder(ctx, order.ID, 99, "operator refund")
		require.NoError(t, err)
	}
	checkWallet(t, s, user, "2.00000000", "0.00000000")
	var holds, captures, refunds int
	require.NoError(t, s.db.QueryRow(`SELECT count(*) FILTER (WHERE operation='hold'),count(*) FILTER (WHERE operation='capture'),count(*) FILTER (WHERE operation='refund') FROM qq_image_ledger WHERE order_id=$1`, order.ID).Scan(&holds, &captures, &refunds))
	require.Equal(t, 1, holds)
	require.Equal(t, 1, captures)
	require.Equal(t, 1, refunds)
}

func TestQQControlsReconcileAndReleaseContinueAfterClosure(t *testing.T) {
	s, user, order := workerDBOrder(t)
	ctx := context.Background()
	claim, err := s.ClaimOrder(ctx, "worker", time.Minute)
	require.NoError(t, err)
	setBusinessControls(t, s, false, false)
	require.NoError(t, s.MarkReconciling(ctx, order.ID, claim.LeaseToken, "provider_result_unknown"))
	require.NoError(t, s.RequestReconcile(ctx, order.ID, 99))
	_, err = s.ResolveOrderFailure(ctx, order.ID, 99, "provider confirmed failure")
	require.NoError(t, err)
	checkWallet(t, s, user, "2.00000000", "0.00000000")
	late, err := s.CompleteGeneration(ctx, order.ID, claim.LeaseToken, Artifact{Data: []byte("image"), MIME: "image/png", Width: 1, Height: 1})
	require.NoError(t, err)
	require.Equal(t, "released", late.FundsStatus)
	require.Equal(t, "available", late.WebsiteStatus)
	_, err = s.GetArtifact(ctx, order.ID, user)
	require.NoError(t, err)
	checkWallet(t, s, user, "2.00000000", "0.00000000")
}

func TestQQControlsGatewayCannotBypassClosureAndReplaysOldEvent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "3")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	c, err := s.GetConfig(ctx)
	require.NoError(t, err)
	c.ReceiveMode = ReceiveModeWebSocket
	c, err = s.SaveConfig(ctx, *c, 1)
	require.NoError(t, err)
	sender := &commandSender{}
	h := NewQQCommandHandler(s, sender)
	payload := func(id, content string) []byte {
		raw, e := json.Marshal(map[string]any{"op": 0, "id": "event-" + id, "t": "GROUP_AT_MESSAGE_CREATE", "d": map[string]any{"id": id, "group_openid": "group", "content": content, "timestamp": time.Now().Format(time.RFC3339), "author": map[string]string{"member_openid": "one"}}})
		require.NoError(t, e)
		return raw
	}
	old := payload("old", "/生图 cat")
	require.NoError(t, h.HandleGatewayEvent(ctx, c.ID, c.AppID, old))
	setBusinessControls(t, s, false, false)
	require.NoError(t, h.HandleGatewayEvent(ctx, c.ID, c.AppID, old))
	require.Len(t, sender.messages, 1, "old event cannot send or freeze again")
	for n, command := range []string{"/绑定", "/确认绑定 ABCD1234", "/生图new cat"} {
		require.NoError(t, h.HandleGatewayEvent(ctx, c.ID, c.AppID, payload(strconv.Itoa(n), command)))
		require.Contains(t, sender.messages[len(sender.messages)-1], "未开放")
	}
	var orders, requests, grants, holds int
	require.NoError(t, s.db.QueryRow(`SELECT (SELECT count(*) FROM qq_image_orders),(SELECT count(*) FROM qq_image_binding_requests),(SELECT count(*) FROM qq_image_grants),(SELECT count(*) FROM qq_image_ledger WHERE operation='hold')`).Scan(&orders, &requests, &grants, &holds))
	require.Equal(t, 1, orders)
	require.Equal(t, 1, requests)
	require.Equal(t, 1, grants)
	require.Equal(t, 1, holds)
	checkWallet(t, s, u, "2.00000000", "1.00000000")
	_, err = s.db.Exec(`ALTER TABLE settings RENAME TO settings_unavailable`)
	require.NoError(t, err)
	require.NoError(t, h.HandleGatewayEvent(ctx, c.ID, c.AppID, old))
	failed := payload("database-failure", "/生图 cat")
	require.Error(t, h.HandleGatewayEvent(ctx, c.ID, c.AppID, failed))
	var events int
	require.NoError(t, s.db.QueryRow(`SELECT count(*) FROM qq_image_events`).Scan(&events))
	require.Equal(t, 4, events, "database error must not commit event")
	checkWallet(t, s, u, "2.00000000", "1.00000000")
}

func TestQQControlsBindingThreeStages(t *testing.T) {
	for _, stage := range []string{"request", "authorize", "confirm"} {
		t.Run(stage, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			u := fixtureUser(t, s, "one@example.com", "3")
			i := fixtureIdentity("one", "group")
			var id, code string
			if stage != "request" {
				r, err := s.CreateBindingRequest(ctx, i)
				require.NoError(t, err)
				id = r.ID
			}
			if stage == "confirm" {
				a, err := s.AuthorizeBinding(ctx, id, u, "session")
				require.NoError(t, err)
				code = a.ConfirmationCode
			}
			setBusinessControls(t, s, false, true)
			var err error
			switch stage {
			case "request":
				_, err = s.CreateBindingRequest(ctx, i)
			case "authorize":
				_, err = s.AuthorizeBinding(ctx, id, u, "session")
			case "confirm":
				_, err = s.ConfirmBinding(ctx, i, code)
			}
			require.ErrorIs(t, err, ErrDisabled)
			var grants int
			require.NoError(t, s.db.QueryRow(`SELECT count(*) FROM qq_image_grants`).Scan(&grants))
			require.Zero(t, grants)
			checkWallet(t, s, u, "3.00000000", "0.00000000")
			if id != "" {
				r, err := s.GetBindingRequest(ctx, id)
				require.NoError(t, err)
				require.NotEqual(t, "completed", r.Status)
			}
			// Reopening admits the same authorization without consuming its code.
			setBusinessControls(t, s, true, true)
			if stage == "confirm" {
				_, err = s.ConfirmBinding(ctx, i, code)
				require.NoError(t, err)
			}
		})
	}
}

func TestQQControlsFourCombinationsAndNoExtraFreeze(t *testing.T) {
	for _, binding := range []bool{false, true} {
		for _, image := range []bool{false, true} {
			t.Run(strconv.FormatBool(binding)+"/"+strconv.FormatBool(image), func(t *testing.T) {
				s := testStore(t)
				ctx := context.Background()
				u := fixtureUser(t, s, "one@example.com", "3")
				i := fixtureIdentity("one", "group")
				g := fixtureGrant(t, s, u, i)
				setBusinessControls(t, s, binding, image)
				_, err := s.CreateBindingRequest(ctx, fixtureIdentity("two", "group"))
				if binding {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, ErrDisabled)
				}
				_, err = s.CreateOrder(ctx, i, "event", "a cat")
				if image {
					require.NoError(t, err)
					checkWallet(t, s, u, "2.00000000", "1.00000000")
				} else {
					require.ErrorIs(t, err, ErrDisabled)
					checkWallet(t, s, u, "3.00000000", "0.00000000")
				}
				grants, err := s.ListGrants(ctx, u)
				require.NoError(t, err)
				require.Len(t, grants, 1)
				require.NoError(t, s.MarkEmailVerified(ctx, u, "one@example.com"))
				require.NoError(t, s.RevokeGrant(ctx, g.ID, u, "user_revoked"))
				checkWallet(t, s, u, "3.00000000", "0.00000000")
			})
		}
	}
}

func TestQQControlsOldOrderReplayAndCancellationSurviveClosureAndSettingsFailure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "one@example.com", "3")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	order, err := s.CreateOrder(ctx, i, "event", "a cat")
	require.NoError(t, err)
	setBusinessControls(t, s, false, false)
	for _, unavailable := range []bool{false, true} {
		if unavailable {
			_, err = s.db.Exec(`ALTER TABLE settings RENAME TO settings_unavailable`)
			require.NoError(t, err)
		}
		replay, err := s.CreateOrder(ctx, i, "event", "a cat")
		require.NoError(t, err)
		require.Equal(t, order.ID, replay.ID)
		_, err = s.CreateOrder(ctx, i, "new-event", "a cat")
		require.Error(t, err)
		checkWallet(t, s, u, "2.00000000", "1.00000000")
		var count int
		require.NoError(t, s.db.QueryRow(`SELECT count(*) FROM qq_image_ledger WHERE operation='hold'`).Scan(&count))
		require.Equal(t, 1, count)
	}
	_, err = s.CancelOrder(ctx, order.ID, u)
	require.NoError(t, err)
	checkWallet(t, s, u, "3.00000000", "0.00000000")
}

func TestQQControlsSettingsFailureRollsBackBinding(t *testing.T) {
	for _, stage := range []string{"request", "authorize", "confirm"} {
		t.Run(stage, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			u := fixtureUser(t, s, "one@example.com", "3")
			i := fixtureIdentity("one", "group")
			r, err := s.CreateBindingRequest(ctx, i)
			require.NoError(t, err)
			var code string
			if stage == "confirm" {
				a, e := s.AuthorizeBinding(ctx, r.ID, u, "session")
				require.NoError(t, e)
				code = a.ConfirmationCode
			}
			before, err := s.GetBindingRequest(ctx, r.ID)
			require.NoError(t, err)
			_, err = s.db.Exec(`ALTER TABLE settings RENAME TO settings_unavailable`)
			require.NoError(t, err)
			switch stage {
			case "request":
				_, err = s.CreateBindingRequest(ctx, fixtureIdentity("two", "group"))
			case "authorize":
				_, err = s.AuthorizeBinding(ctx, r.ID, u, "session")
			case "confirm":
				_, err = s.ConfirmBinding(ctx, i, code)
			}
			require.Error(t, err)
			after, err := s.GetBindingRequest(ctx, r.ID)
			require.NoError(t, err)
			require.Equal(t, before.Status, after.Status)
			require.Equal(t, before.Revision, after.Revision)
			checkWallet(t, s, u, "3.00000000", "0.00000000")
		})
	}
}
