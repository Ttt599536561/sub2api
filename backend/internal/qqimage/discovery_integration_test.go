package qqimage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func discoveryFixture(t *testing.T) (*Store, *Discovery, string) {
	t.Helper()
	s := testStore(t)
	c := DefaultConfig()
	c.AppID = "app"
	c.BotSecret = "qq-secret"
	if _, err := s.SaveConfig(context.Background(), c, 1); err != nil {
		t.Fatal(err)
	}
	d, err := s.StartDiscovery(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	code := strings.TrimPrefix(d.Command, "@机器人 /识别群 ")
	if len(code) != 48 || d.Status != "waiting" || d.Candidate != nil || time.Until(d.ExpiresAt) > 5*time.Minute {
		t.Fatalf("invalid window: %+v", d)
	}
	return s, d, code
}
func discoveryEvent(group, eventID string) *QQEvent {
	return &QQEvent{EventID: eventID, MessageID: "msg-" + eventID, Identity: fixtureIdentity("private-member", group), Content: "private-content", CreatedAt: time.Now()}
}
func discoveryRecord(t *testing.T, s *Store, e *QQEvent, code string) {
	t.Helper()
	c, err := s.GetConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecordDiscovery(context.Background(), c.ID, e, code); err != nil {
		t.Fatal(err)
	}
}
func TestDiscoveryRequiresExplicitConfirmationAndStoresOnlyMinimalCandidate(t *testing.T) {
	s, d, code := discoveryFixture(t)
	ctx := context.Background()
	discoveryRecord(t, s, discoveryEvent("unknown-group", "event-private"), strings.Repeat("b", 48))
	current, err := s.GetDiscovery(ctx)
	if err != nil || current.Status != "waiting" {
		t.Fatalf("wrong code consumed window: %+v %v", current, err)
	}
	discoveryRecord(t, s, discoveryEvent("unknown-group", "event-private"), code)
	current, err = s.GetDiscovery(ctx)
	if err != nil || current.Status != "found" || current.Command != "" || current.Candidate.GroupOpenID != "unknown-group" {
		t.Fatalf("bad candidate: %+v %v", current, err)
	}
	config, _ := s.GetConfig(ctx)
	if config.Enabled || len(config.AllowedGroups) != 0 {
		t.Fatal("candidate enabled generation or changed whitelist")
	}
	var raw string
	if err = s.db.QueryRow(`SELECT row_to_json(d)::text FROM qq_image_discoveries d WHERE id=$1`, d.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{code, "private-member", "private-content"} {
		if strings.Contains(raw, private) {
			t.Fatalf("sensitive data persisted: %s", private)
		}
	}
	encoded, _ := json.Marshal(current)
	if strings.Contains(string(encoded), "code") || strings.Contains(string(encoded), "command") {
		t.Fatalf("read leaked one-time code: %s", encoded)
	}
	confirmed, err := s.ConfirmDiscovery(ctx, d.ID, 99)
	if err != nil || confirmed.Status != "confirmed" || confirmed.Command != "" {
		t.Fatalf("confirm: %+v %v", confirmed, err)
	}
	config, _ = s.GetConfig(ctx)
	if config.Enabled || len(config.AllowedGroups) != 1 || config.AllowedGroups[0] != "unknown-group" {
		t.Fatalf("wrong whitelist: %+v", config)
	}
	prior := config.ID
	if _, err = s.ConfirmDiscovery(ctx, d.ID, 99); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeat confirmation accepted: %v", err)
	}
	config, _ = s.GetConfig(ctx)
	if prior != config.ID {
		t.Fatal("replay wrote another config")
	}
	for _, table := range []string{"qq_image_binding_requests", "qq_image_grants", "qq_image_orders", "qq_image_holds", "qq_image_ledger", "qq_image_balance_outbox", "qq_image_deliveries"} {
		var n int
		if err = s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("business side effect %s=%d err=%v", table, n, err)
		}
	}
}
func TestDiscoveryConcurrentEventsAndConfirmsCannotOverwriteOrDuplicate(t *testing.T) {
	s, d, code := discoveryFixture(t)
	ctx := context.Background()
	c, _ := s.GetConfig(ctx)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.RecordDiscovery(ctx, c.ID, discoveryEvent(fmt.Sprintf("group-%d", i), fmt.Sprintf("event-%d", i)), code)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	before, _ := s.GetDiscovery(ctx)
	if before.Status != "found" {
		t.Fatal("no candidate")
	}
	discoveryRecord(t, s, discoveryEvent("overwrite", "new-event"), code)
	after, _ := s.GetDiscovery(ctx)
	if after.Candidate.GroupOpenID != before.Candidate.GroupOpenID {
		t.Fatal("candidate overwritten")
	}
	successes := make(chan bool, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.ConfirmDiscovery(ctx, d.ID, 99); successes <- err == nil }()
	}
	wg.Wait()
	close(successes)
	n := 0
	for ok := range successes {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("confirmation successes=%d", n)
	}
}
func TestDiscoveryClosingExpiryAndNewWindowInvalidateOldCode(t *testing.T) {
	for _, scenario := range []string{"close", "expire", "restart"} {
		t.Run(scenario, func(t *testing.T) {
			s, d, code := discoveryFixture(t)
			ctx := context.Background()
			switch scenario {
			case "close":
				if _, err := s.CloseDiscovery(ctx, d.ID, 1); err != nil {
					t.Fatal(err)
				}
			case "expire":
				if _, err := s.db.Exec(`UPDATE qq_image_discoveries SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, d.ID); err != nil {
					t.Fatal(err)
				}
			case "restart":
				if _, err := s.StartDiscovery(ctx, 1); err != nil {
					t.Fatal(err)
				}
			}
			discoveryRecord(t, s, discoveryEvent("group", "event1"), code)
			current, _ := s.GetDiscovery(ctx)
			if current.Candidate != nil {
				t.Fatal("closed/expired code admitted candidate")
			}
			if _, err := s.ConfirmDiscovery(ctx, d.ID, 1); err == nil {
				t.Fatal("invalid window confirmed")
			}
		})
	}
}
func TestDiscoveryConfigurationChangesRejectCandidatesAndStaleVerifiedEvents(t *testing.T) {
	for _, field := range []string{"app", "bot", "other"} {
		t.Run(field, func(t *testing.T) {
			s, d, code := discoveryFixture(t)
			ctx := context.Background()
			old, _ := s.GetConfig(ctx)
			discoveryRecord(t, s, discoveryEvent("candidate", "event1"), code)
			next := *old
			switch field {
			case "app":
				next.AppID = "new-app"
			case "bot":
				next.BotSecret = "new-secret"
			case "other":
				next.Model = "changed"
			}
			if _, err := s.SaveConfig(ctx, next, 1); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ConfirmDiscovery(ctx, d.ID, 1); !errors.Is(err, ErrConflict) {
				t.Fatalf("config change confirmed: %v", err)
			}
			current, _ := s.GetDiscovery(ctx)
			if current.Status != "closed" {
				t.Fatalf("stale status=%s", current.Status)
			}
			if err := s.RecordDiscovery(ctx, old.ID, discoveryEvent("overwrite", "event2"), code); err != nil && !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
			config, _ := s.GetConfig(ctx)
			if len(config.AllowedGroups) != 0 || config.Enabled {
				t.Fatal("stale discovery changed configuration")
			}
		})
	}
}
func TestDiscoveryEventReplayCannotPopulateNewWindow(t *testing.T) {
	s, d, code := discoveryFixture(t)
	ctx := context.Background()
	event := discoveryEvent("first-group", "same-event")
	discoveryRecord(t, s, event, code)
	if _, err := s.CloseDiscovery(ctx, d.ID, 1); err != nil {
		t.Fatal(err)
	}
	next, err := s.StartDiscovery(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	err = s.RecordDiscovery(ctx, func() int64 { c, _ := s.GetConfig(ctx); return c.ID }(), event, strings.TrimPrefix(next.Command, "@机器人 /识别群 "))
	if err != nil && !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	current, _ := s.GetDiscovery(ctx)
	if current.Status != "waiting" || current.Candidate != nil {
		t.Fatal("replayed event populated another window")
	}
}
func TestDiscoveryBotRuntimeIgnoresUndecryptableUpstreamKey(t *testing.T) {
	s, _, _ := discoveryFixture(t)
	ctx := context.Background()
	c, _ := s.GetConfig(ctx)
	if _, err := s.db.Exec(`UPDATE qq_image_configs SET api_key='bad-old-key'::bytea WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RuntimeConfig(ctx, c.ID); !errors.Is(err, ErrEncryptionUnavailable) {
		t.Fatalf("test requires invalid upstream key: %v", err)
	}
	r, err := s.BotRuntimeConfig(ctx)
	if err != nil || r.BotSecret != "qq-secret" || r.APIKey != "" {
		t.Fatalf("bot-only config: %+v %v", r, err)
	}
	if _, err = s.StartDiscovery(ctx, 1); err != nil {
		t.Fatal(err)
	}
}
func TestDiscoveryWebhookRealPostgresAuthenticatesAndHasNoReplyOrMoneyEffects(t *testing.T) {
	s, d, code := discoveryFixture(t)
	ctx := context.Background()
	sender := &commandSender{}
	h := NewQQCommandHandler(s, sender)
	payload := func(id, group, command string, at time.Time) []byte {
		return []byte(fmt.Sprintf(`{"op":0,"id":%q,"t":"GROUP_AT_MESSAGE_CREATE","d":{"id":%q,"group_openid":%q,"author":{"member_openid":"private-member"},"content":%q,"timestamp":%q}}`, id, "msg-"+id, group, command, at.Format(time.RFC3339)))
	}
	raw := payload("event1", "new-group", "<@!app> /识别群 "+code, time.Now())
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	for _, headers := range []http.Header{{}, signQQ(t, "wrong", stamp, raw), signQQ(t, "qq-secret", strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10), raw)} {
		if _, err := h.HandleWebhook(ctx, headers, raw); err == nil {
			t.Fatal("unauthenticated discovery accepted")
		}
	}
	stale := payload("stale", "new-group", "/识别群 "+code, time.Now().Add(-10*time.Minute))
	if _, err := h.HandleWebhook(ctx, signQQ(t, "qq-secret", stamp, stale), stale); !errors.Is(err, ErrExpired) {
		t.Fatalf("stale event accepted: %v", err)
	}
	current, _ := s.GetDiscovery(ctx)
	if current.Status != "waiting" {
		t.Fatal("rejected callback mutated window")
	}
	for i := 0; i < 2; i++ {
		if _, err := h.HandleWebhook(ctx, signQQ(t, "qq-secret", stamp, raw), raw); err != nil {
			t.Fatal(err)
		}
	}
	current, _ = s.GetDiscovery(ctx)
	if current.Status != "found" || current.Candidate.GroupOpenID != "new-group" || current.ID != d.ID {
		t.Fatalf("no candidate: %+v", current)
	}
	if len(sender.messages) != 0 {
		t.Fatal("discovery sent QQ reply")
	}
	generation := payload("generate", "new-group", "/生图 secret-prompt", time.Now())
	if _, err := h.HandleWebhook(ctx, signQQ(t, "qq-secret", stamp, generation), generation); !errors.Is(err, ErrForbidden) {
		t.Fatalf("discovery bypassed whitelist for generation: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT (SELECT count(*) FROM qq_image_orders)+(SELECT count(*) FROM qq_image_ledger)+(SELECT count(*) FROM qq_image_binding_requests)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("business side effects=%d %v", n, err)
	}
}
