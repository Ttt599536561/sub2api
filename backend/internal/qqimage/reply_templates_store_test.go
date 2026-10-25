package qqimage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReplyTemplateRevisionsConflictRollbackAndConfigIndependence(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	before, _ := s.GetConfig(ctx)
	initial, err := s.GetReplyTemplates(ctx)
	if err != nil || initial.Revision != 0 || len(initial.Overrides) != 0 {
		t.Fatalf("initial: %+v %v", initial, err)
	}
	one, err := s.SaveReplyTemplates(ctx, 0, map[string]string{"iq.ack": "您好\r\n{{group_title}}"}, 42)
	if err != nil || one.Revision != 1 || one.Overrides["iq.ack"] != "您好\n{{group_title}}" {
		t.Fatalf("save: %+v %v", one, err)
	}
	if _, err = s.SaveReplyTemplates(ctx, 0, map[string]string{}, 43); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if _, err = s.SaveReplyTemplates(ctx, 1, map[string]string{"iq.caption": "{{unknown}}"}, 43); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad save: %v", err)
	}
	two, err := s.SaveReplyTemplates(ctx, 1, map[string]string{}, 43)
	if err != nil || two.Revision != 2 {
		t.Fatal(err)
	}
	three, err := s.RollbackReplyTemplates(ctx, 2, 1, 44)
	if err != nil || three.Revision != 3 || three.Overrides["iq.ack"] != one.Overrides["iq.ack"] {
		t.Fatalf("rollback: %+v %v", three, err)
	}
	if _, err = s.RollbackReplyTemplates(ctx, 3, 99, 44); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing revision: %v", err)
	}
	history, err := s.ListReplyTemplateRevisions(ctx)
	if err != nil || len(history) != 3 || history[0].ActorID != 44 || history[0].RestoredFromRevision == nil || *history[0].RestoredFromRevision != 1 {
		t.Fatalf("history: %+v %v", history, err)
	}
	old, err := s.GetReplyTemplateRevision(ctx, 1)
	if err != nil || old.Overrides["iq.ack"] != one.Overrides["iq.ack"] {
		t.Fatal("prior revision mutated")
	}
	after, _ := s.GetConfig(ctx)
	if before.ID != after.ID {
		t.Fatal("reply edit changed runtime config/connection identity")
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM qq_image_audit WHERE action LIKE 'reply_templates_%'`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("audit=%d %v", count, err)
	}
	if _, err = s.db.Exec(`UPDATE qq_reply_template_revisions SET actor_id=99 WHERE revision=1`); err == nil {
		t.Fatal("immutable revision allowed update")
	}
}

func TestReplyTemplateConcurrentSaveHasOneWinner(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.SaveReplyTemplates(context.Background(), 0, map[string]string{}, 42)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
}

func TestReplyTemplateWritesIndependentOfBusinessLockAndHistoryBound(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(716709231248)`); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err = s.SaveReplyTemplates(short, 0, map[string]string{}, 42); err != nil {
		t.Fatalf("templates blocked by business lock: %v", err)
	}
	for revision := int64(1); revision < 52; revision++ {
		if _, err = s.SaveReplyTemplates(ctx, revision, map[string]string{}, 42); err != nil {
			t.Fatal(err)
		}
	}
	history, err := s.ListReplyTemplateRevisions(ctx)
	if err != nil || len(history) != 50 || history[0].Revision != 52 || history[49].Revision != 3 {
		t.Fatalf("history bound: %+v %v", history, err)
	}
}

func TestReplyTemplatePostgresCommandSnapshotFundsAndMissingTableFallback(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := fixtureUser(t, s, "template@example.test", "3")
	identity := fixtureIdentity("template-user", "group")
	fixtureGrant(t, s, uid, identity)
	const customReceipt = "已收到，正在安排图片。"
	_, err := s.SaveReplyTemplates(ctx, 0, map[string]string{"order.accepted": customReceipt}, 42)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := s.RuntimeConfig(ctx, cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	sender := &commandSender{}
	h := NewQQCommandHandler(s, sender)
	event := &QQEvent{Identity: identity, EventID: "template-event", MessageID: "template-message", CreatedAt: time.Now(), Content: "/生图 a private prompt"}
	admit := func(context.Context) error { return nil }
	if _, err = h.handleEvent(ctx, runtime, event, admit); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 || sender.messages[0] != customReceipt {
		t.Fatalf("snapshot: %+v", sender)
	}
	var persisted string
	if err = s.db.QueryRow(`SELECT result FROM qq_image_events LIMIT 1`).Scan(&persisted); err != nil || !strings.Contains(persisted, customReceipt) {
		t.Fatalf("persisted: %q %v", persisted, err)
	}
	checkWallet(t, s, uid, "2.00000000", "1.00000000")
	if _, err = s.SaveReplyTemplates(ctx, 1, map[string]string{}, 42); err != nil {
		t.Fatal(err)
	}
	if _, err = h.handleEvent(ctx, runtime, event, admit); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 {
		t.Fatal("replay after template edit sent again")
	}
	checkWallet(t, s, uid, "2.00000000", "1.00000000")
	// A read failure occurs before ProcessEvent and must not leave its SQL
	// transaction aborted. The default help result still commits successfully.
	if _, err = s.db.Exec(`ALTER TABLE qq_reply_template_state RENAME TO qq_reply_template_state_unavailable`); err != nil {
		t.Fatal(err)
	}
	event.MessageID = "fallback-message"
	event.Content = "/生图帮助"
	if _, err = h.handleEvent(ctx, runtime, event, admit); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 2 || !strings.Contains(sender.messages[1], "双端绑定") {
		t.Fatal("table read failure poisoned transaction")
	}
	checkWallet(t, s, uid, "2.00000000", "1.00000000")
}

func TestReplyTemplatePostgresOptionalFieldsPublishedAndRollback(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	before, err := s.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Preserve existing administrator text and old-format templates without
	// rewriting defaults or any stored revision when editing a different field.
	legacy := "原有欢迎语\n{{group_title}}\n截图时间：{{captured_at}}（{{timezone}}）\n{{query_rules}}{{schedule_rules}}"
	first, err := s.SaveReplyTemplates(ctx, 0, map[string]string{"iq.ack": "原有欢迎语", "iq.caption": legacy}, 42)
	if err != nil {
		t.Fatal(err)
	}
	custom := map[string]string{"iq.ack": "原有欢迎语"}
	for _, def := range ReplyTemplateCatalog()[:6] {
		if def.Key != "iq.ack" {
			custom[def.Key] = "糖果小站：自定义回复。"
		}
	}
	custom["iq.caption"] = "{{group_title}}：{{captured_at}}\n{{group_title}}"
	second, err := s.SaveReplyTemplates(ctx, first.Revision, custom, 43)
	if err != nil || second.Revision != 2 {
		t.Fatalf("free text save: %+v %v", second, err)
	}
	loaded, err := s.GetReplyTemplates(ctx)
	if err != nil || loaded.Overrides["iq.ack"] != "原有欢迎语" || loaded.Overrides["iq.caption"] != custom["iq.caption"] {
		t.Fatalf("optional template read: %+v %v", loaded, err)
	}
	published := loaded.Published()
	if len(published.Templates) != 6 || published.Revision != 2 {
		t.Fatalf("published shape: %+v", published)
	}
	for key, text := range custom {
		if published.Templates[key] != text {
			t.Fatalf("published text unexpectedly replaced: %s %q", key, published.Templates[key])
		}
	}
	third, err := s.RollbackReplyTemplates(ctx, 2, 1, 44)
	if err != nil || third.Revision != 3 || third.Overrides["iq.caption"] != legacy || third.Overrides["iq.ack"] != "原有欢迎语" {
		t.Fatalf("legacy rollback: %+v %v", third, err)
	}
	fourth, err := s.RollbackReplyTemplates(ctx, 3, 2, 45)
	if err != nil || fourth.Revision != 4 || fourth.Overrides["iq.caption"] != custom["iq.caption"] {
		t.Fatalf("optional-field rollback: %+v %v", fourth, err)
	}
	after, err := s.GetConfig(ctx)
	if err != nil || before.ID != after.ID {
		t.Fatalf("reply revisions changed runtime configuration: %+v %v", after, err)
	}
}

func TestReplyTemplatePostgresPlainRepliesDoNotAuthorizeOrCreateCredit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := fixtureUser(t, s, "template-no-credit@example.test", "0")
	identity := fixtureIdentity("template-no-credit", "group")
	fixtureGrant(t, s, uid, identity)
	_, err := s.SaveReplyTemplates(ctx, 0, map[string]string{
		"order.accepted":             "图片安排中。",
		"error.insufficient_balance": "请联系管理员。",
		"error.forbidden":            "请先完成绑定。",
	}, 42)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := s.RuntimeConfig(ctx, cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	sender := &commandSender{}
	h := NewQQCommandHandler(s, sender)
	admit := func(context.Context) error { return nil }
	event := &QQEvent{Identity: identity, EventID: "no-credit-event", MessageID: "no-credit-message", CreatedAt: time.Now(), Content: "/生图 a private prompt"}
	if _, err = h.handleEvent(ctx, runtime, event, admit); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 || sender.messages[0] != "请联系管理员。" {
		t.Fatalf("insufficient balance reply not customized: %+v", sender.messages)
	}
	event.Identity = fixtureIdentity("template-unbound", "group")
	event.EventID, event.MessageID = "unbound-event", "unbound-message"
	if _, err = h.handleEvent(ctx, runtime, event, admit); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 2 || sender.messages[1] != "请先完成绑定。" {
		t.Fatalf("unbound reply not customized: %+v", sender.messages)
	}
	event.Identity.GroupID = "not-allowed-group"
	event.EventID, event.MessageID = "unauthorized-group-event", "unauthorized-group-message"
	if _, err = h.handleEvent(ctx, runtime, event, admit); !errors.Is(err, ErrForbidden) {
		t.Fatalf("template bypassed group authorization: %v", err)
	}
	if len(sender.messages) != 2 {
		t.Fatal("unauthorized group received a reply")
	}
	var orders int
	if err = s.db.QueryRow(`SELECT count(*) FROM qq_image_orders`).Scan(&orders); err != nil || orders != 0 {
		t.Fatalf("template created unauthorized order: %d %v", orders, err)
	}
	checkWallet(t, s, uid, "0.00000000", "0.00000000")
}
