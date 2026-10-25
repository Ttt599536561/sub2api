package qqimage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func fixtureSavedPrivateOrder(t *testing.T, s *Store, seq int) (int64, *Order) {
	t.Helper()
	ctx := context.Background()
	u := fixtureUser(t, s, fmt.Sprintf("review-%d@example.com", seq), "2")
	i := fixtureIdentity(fmt.Sprintf("review-%d", seq), "group")
	fixtureGrant(t, s, u, i)
	o, err := s.CreateOrder(ctx, i, fmt.Sprintf("event-%d", seq), "cat")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimOrder(ctx, "review", time.Minute)
	if err != nil || claimed == nil || claimed.ID != o.ID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if _, err = s.db.Exec(`UPDATE users SET status='disabled' WHERE id=$1`, u); err != nil {
		t.Fatal(err)
	}
	artifact, err := validateArtifact(fixturePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CompleteGeneration(ctx, o.ID, claimed.LeaseToken, artifact)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("save private image: %v", err)
	}
	return u, o
}

func TestOrderRecoverySkipsUnavailableOwnersBeyondBatch(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for n := 0; n < 100; n++ {
		_, o := fixtureSavedPrivateOrder(t, s, n)
		if _, err := s.db.Exec(`UPDATE qq_image_orders SET deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, o.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.recoverExpired(ctx); err != nil {
			t.Fatal(err)
		}
	}
	u, wanted := fixtureSavedPrivateOrder(t, s, 101)
	if _, err := s.db.Exec(`UPDATE users SET status='active' WHERE id=$1`, u); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		if err := s.RecoverExpired(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetOrder(ctx, wanted.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	if got.FundsStatus != "captured" || got.WebsiteStatus != "available" {
		t.Fatalf("eligible 101st saved result starved after 3 recovery runs: funds=%s website=%s", got.FundsStatus, got.WebsiteStatus)
	}
}

func TestOrderClaimReachesOtherGroupBeyondFirstBatch(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c, err := s.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.QueueLimit, c.GroupConcurrency, c.GlobalConcurrency = 200, 1, 2
	if _, err = s.SaveConfig(ctx, *c, 1); err != nil {
		t.Fatal(err)
	}
	var wanted string
	for n := 0; n < 102; n++ {
		group := "group"
		if n == 101 {
			group = "group2"
		}
		u := fixtureUser(t, s, fmt.Sprintf("queue-%d@example.com", n), "2")
		i := fixtureIdentity(fmt.Sprintf("queue-%d", n), group)
		fixtureGrant(t, s, u, i)
		o, err := s.CreateOrder(ctx, i, fmt.Sprintf("queue-event-%d", n), "cat")
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			got, err := s.ClaimOrder(ctx, "review", time.Minute)
			if err != nil || got == nil || got.ID != o.ID {
				t.Fatalf("first claim: %+v %v", got, err)
			}
		}
		if n == 101 {
			wanted = o.ID
		}
	}
	got, err := s.ClaimOrder(ctx, "review", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != wanted {
		t.Fatalf("idle global/group2 capacity could not claim eligible order after 100 blocked group1 rows: %+v", got)
	}
}

func TestWorkerProviderRevocationPreservesCapturedDelivery(t *testing.T) {
	s, u, order := workerDBOrder(t)
	ctx := context.Background()
	claimed, err := s.ClaimOrder(ctx, "review", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	artifact, err := validateArtifact(fixturePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteGeneration(ctx, order.ID, claimed.LeaseToken, artifact); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.ClearAPIKey = true
	if _, err = s.SaveConfig(ctx, *c, 1); err != nil {
		t.Fatal(err)
	}
	generator := &workerGenerator{}
	sender := &workerSender{status: "sent"}
	if err = NewWorker(s, generator, sender).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOrder(ctx, order.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 || got.DeliveryStatus != "sent" {
		t.Fatalf("valid bot and stored captured image blocked by unrelated provider credential: sends=%d delivery=%s", sender.calls, got.DeliveryStatus)
	}
	body := []byte(fmt.Sprintf(`{"op":0,"id":"event-redelivery","t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"msg-redelivery","group_openid":"group","author":{"member_openid":"worker"},"content":%q,"timestamp":%q}}`, "/重发 "+order.ID, time.Now().Format(time.RFC3339)))
	textSender := &commandSender{}
	if _, err = NewQQCommandHandler(s, textSender).HandleWebhook(ctx, signQQ(t, "qq-secret", fmt.Sprint(time.Now().Unix()), body), body); err != nil {
		t.Fatal(err)
	}
	if len(textSender.messages) != 1 {
		t.Fatal("redelivery acknowledgement missing")
	}
	if err = NewWorker(s, generator, sender).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetOrder(ctx, order.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	if sender.calls != 2 || got.DeliveryStatus != "sent" {
		t.Fatalf("authenticated redelivery blocked: sends=%d status=%s", sender.calls, got.DeliveryStatus)
	}
	if generator.calls != 0 {
		t.Fatal("saved delivery regenerated")
	}
	checkWallet(t, s, u, "1.00000000", "0.00000000")
}

func TestOrderPrivateArtifactStagingRetention(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_%v", legacy), func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			c, err := s.GetConfig(ctx)
			if err != nil {
				t.Fatal(err)
			}
			c.RetentionDays = 1
			if _, err = s.SaveConfig(ctx, *c, 1); err != nil {
				t.Fatal(err)
			}
			u, order := fixtureSavedPrivateOrder(t, s, 1)
			stored, err := s.GetOrder(ctx, order.ID, u)
			if err != nil {
				t.Fatal(err)
			}
			if !legacy && stored.ArtifactExpiresAt == nil {
				t.Error("unpublished image has no bounded staging retention")
			}
			var sql string
			if legacy {
				sql = `UPDATE qq_image_orders SET deadline=clock_timestamp()-interval '2 days',artifact_expires_at=NULL WHERE id=$1`
			} else {
				sql = `UPDATE qq_image_orders SET deadline=clock_timestamp()-interval '1 second',artifact_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`
			}
			if _, err = s.db.Exec(sql, order.ID); err != nil {
				t.Fatal(err)
			}
			if err = s.RecoverExpired(ctx); err != nil {
				t.Fatal(err)
			}
			stored, err = s.GetOrder(ctx, order.ID, u)
			if err != nil {
				t.Fatal(err)
			}
			var removed bool
			if err = s.db.QueryRow(`SELECT artifact IS NULL FROM qq_image_orders WHERE id=$1`, order.ID).Scan(&removed); err != nil {
				t.Fatal(err)
			}
			if !removed || stored.WebsiteStatus != "expired" || stored.FundsStatus != "released" {
				t.Fatalf("private expired image remains: removed=%v website=%s funds=%s", removed, stored.WebsiteStatus, stored.FundsStatus)
			}
			if _, err = s.db.Exec(`UPDATE users SET status='active' WHERE id=$1`, u); err != nil {
				t.Fatal(err)
			}
			if err = s.RecoverExpired(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err = s.GetArtifact(ctx, order.ID, u); !errors.Is(err, ErrNotFound) {
				t.Fatalf("expired staged image became available: %v", err)
			}
			artifact, err := validateArtifact(fixturePNG(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.CompleteGeneration(ctx, order.ID, stored.LeaseToken, artifact); !errors.Is(err, ErrExpired) {
				t.Fatalf("expired staged image accepted repeated result: %v", err)
			}
			checkWallet(t, s, u, "2.00000000", "0.00000000")
		})
	}
}

func TestOrderRestoredOwnerGetsFullPublishedRetention(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late_%v", late), func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			u, order := fixtureSavedPrivateOrder(t, s, 1)
			if _, err := s.db.Exec(`UPDATE qq_image_orders SET artifact_expires_at=clock_timestamp()+interval '1 minute' WHERE id=$1`, order.ID); err != nil {
				t.Fatal(err)
			}
			if late {
				if _, err := s.db.Exec(`UPDATE qq_image_orders SET deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, order.ID); err != nil {
					t.Fatal(err)
				}
				if err := s.RecoverExpired(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.db.Exec(`UPDATE users SET status='active' WHERE id=$1`, u); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			if err := s.RecoverExpired(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetOrder(ctx, order.ID, u)
			if err != nil {
				t.Fatal(err)
			}
			if got.WebsiteStatus != "available" || got.ArtifactExpiresAt == nil || got.ArtifactExpiresAt.Before(start.Add(7*24*time.Hour-time.Second)) {
				t.Fatalf("restored owner's promised retention shortened: %+v", got)
			}
			want := "captured"
			if late {
				want = "released"
			}
			if got.FundsStatus != want {
				t.Fatalf("restored owner funds=%s want=%s", got.FundsStatus, want)
			}
			if _, err = s.GetArtifact(ctx, order.ID, u); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQQRedeliveryDoesNotRequireProviderCredentials(t *testing.T) {
	s := &discoveryCommandStore{commandStore: commandStore{config: qqConfig()}}
	sender := &commandSender{}
	body := commandPayload("/重发 order1")
	_, err := NewQQCommandHandler(s, sender).HandleWebhook(context.Background(), signQQ(t, "secret", fmt.Sprint(time.Now().Unix()), body), body)
	if err != nil || s.redelivery != 1 || s.runtimeCalls != 0 || s.orders != 0 {
		t.Fatalf("redelivery depended on generation credentials: err=%v redelivery=%d provider_config=%d generations=%d", err, s.redelivery, s.runtimeCalls, s.orders)
	}
}

func TestWorkerProviderRevocationStillBlocksGeneration(t *testing.T) {
	s, u, order := workerDBOrder(t)
	ctx := context.Background()
	c, err := s.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.ClearAPIKey = true
	if _, err = s.SaveConfig(ctx, *c, 1); err != nil {
		t.Fatal(err)
	}
	generator := &workerGenerator{}
	if err = NewWorker(s, generator, &workerSender{status: "sent"}).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOrder(ctx, order.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	if generator.calls != 0 || got.Status != "reconciling" || got.FundsStatus != "held" {
		t.Fatalf("provider revocation bypassed: calls=%d status=%s funds=%s", generator.calls, got.Status, got.FundsStatus)
	}
}

func TestWorkerCapturedDeliveryStillChecksBotAndGrant(t *testing.T) {
	for _, revoke := range []string{"bot", "grant", "group"} {
		t.Run(revoke, func(t *testing.T) {
			s, u, order := workerDBOrder(t)
			ctx := context.Background()
			claimed, err := s.ClaimOrder(ctx, "review", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			a, err := validateArtifact(fixturePNG(t))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.CompleteGeneration(ctx, order.ID, claimed.LeaseToken, a); err != nil {
				t.Fatal(err)
			}
			if revoke == "grant" {
				if err = s.RevokeGrant(ctx, order.GrantID, u, ""); err != nil {
					t.Fatal(err)
				}
			} else {
				c, err := s.GetConfig(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if revoke == "bot" {
					c.ClearBotSecret = true
				} else {
					c.AllowedGroups = []string{"group2"}
				}
				if _, err = s.SaveConfig(ctx, *c, 1); err != nil {
					t.Fatal(err)
				}
			}
			generator := &workerGenerator{}
			sender := &workerSender{status: "sent"}
			if err = NewWorker(s, generator, sender).RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if sender.calls != 0 || generator.calls != 0 {
				t.Fatalf("revoked %s still permitted delivery", revoke)
			}
			if _, err = s.RequestRedelivery(ctx, order.Identity, order.ID, "new-context"); !errors.Is(err, ErrForbidden) {
				t.Fatalf("revoked %s allowed redelivery: %v", revoke, err)
			}
			if _, err = s.GetArtifact(ctx, order.ID, u); err != nil {
				t.Fatalf("revocation removed original owner's image: %v", err)
			}
		})
	}
}
