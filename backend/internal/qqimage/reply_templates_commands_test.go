package qqimage

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type templateCommandStore struct {
	commandStore
	readsTemplates                  int
	inTransaction                   bool
	snapshot                        *ReplyTemplateSnapshot
	templateError                   error
	committedReply, committedManual string
}

func (s *templateCommandStore) GetReplyTemplates(context.Context) (*ReplyTemplateSnapshot, error) {
	if s.inTransaction {
		panic("template read inside business transaction")
	}
	s.readsTemplates++
	return s.snapshot, s.templateError
}
func (s *templateCommandStore) ProcessEvent(ctx context.Context, key, hash string, execute func(context.Context) (string, string, error)) (string, string, bool, error) {
	return s.commandStore.ProcessEvent(ctx, key, hash, func(ctx context.Context) (string, string, error) {
		s.inTransaction = true
		defer func() { s.inTransaction = false }()
		r, m, e := execute(ctx)
		if e == nil {
			s.committedReply = r
			s.committedManual = m
		}
		return r, m, e
	})
}
func TestReplyTemplateCommandsSnapshotAndReplay(t *testing.T) {
	s := &templateCommandStore{commandStore: commandStore{config: qqConfig()}, snapshot: newReplyTemplateSnapshot(1, map[string]string{"order.accepted": "自定义 {{order_id}} US${{price}} {{settlement_rules}} {{order_url}}"}, nil)}
	s.config.ReceiveMode = ReceiveModeWebSocket
	sender := &commandSender{}
	h := NewQQCommandHandler(s, sender)
	raw := commandPayload("/生图 private prompt")
	if err := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, raw); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 || !strings.HasPrefix(sender.messages[0], "自定义 order1 US$1.00000000") || s.committedReply != sender.messages[0] {
		t.Fatalf("snapshot not committed: %+v %+v", s, sender)
	}
	s.snapshot.Overrides["order.accepted"] = "{{order_id}}已改 {{price}}{{settlement_rules}}{{order_url}}"
	if err := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, raw); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 || s.orders != 1 || s.readsTemplates != 2 {
		t.Fatal("replayed template caused resend or business")
	}
}
func TestReplyTemplateCommandsReadFailureAndBusinessFailure(t *testing.T) {
	s := &templateCommandStore{commandStore: commandStore{config: qqConfig(), err: errors.New("business DB down")}, templateError: errors.New("template DB down")}
	s.config.ReceiveMode = ReceiveModeWebSocket
	sender := &commandSender{}
	h := NewQQCommandHandler(s, sender)
	raw := commandPayload("/生图 private")
	if err := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, raw); err == nil {
		t.Fatal("template fallback consumed failed business transaction")
	}
	if len(sender.messages) != 0 || len(s.hashes) != 0 {
		t.Fatal("failed business accepted")
	}
	s.err = nil
	if err := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, raw); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "已预留 US$1.00000000") {
		t.Fatal("default unavailable")
	}
}
func TestReplyTemplateBindingManualAndLongLegacyConfig(t *testing.T) {
	s := &templateCommandStore{commandStore: commandStore{config: qqConfig()}, snapshot: newReplyTemplateSnapshot(1, map[string]string{"binding.manual": "备用 {{request_id}} {{binding_rules}}"}, nil)}
	s.config.ReceiveMode = ReceiveModeWebSocket
	s.config.PublicBaseURL = "https://example.test/" + strings.Repeat("x", 3000)
	sender := &commandSender{result: QQSendResult{Status: "failed"}}
	h := NewQQCommandHandler(s, sender)
	if err := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, commandPayload("/绑定")); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 2 || !strings.HasPrefix(sender.messages[1], "备用 request1") || s.committedManual != sender.messages[1] {
		t.Fatalf("manual snapshot %+v", sender)
	}
	for _, text := range sender.messages {
		if replyUTF16Length(text) > 2000 || strings.Contains(text, "https://") {
			t.Fatal("unsafe URL truncation or length")
		}
	}
}

func TestReplyTemplateGenerateUsagePointsToImageHelp(t *testing.T) {
	s := &commandStore{config: qqConfig()}
	s.config.ReceiveMode = ReceiveModeWebSocket
	sender := &commandSender{}
	h := NewQQCommandHandlerWithOptions(s, sender, QQCommandOptions{ExternalGeneralHelp: true})
	if err := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, commandPayload("/生图")); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "/生图帮助 查看固定价格") {
		t.Fatalf("wrong help owner: %+v", sender.messages)
	}
	if s.orders != 0 {
		t.Fatal("usage created an order")
	}
}
