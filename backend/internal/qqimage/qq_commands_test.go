package qqimage

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type commandStore struct {
	hashes                                      map[string]string
	config                                      *RuntimeConfig
	events                                      map[string]bool
	binding, confirm, orders, reads, redelivery int
	identity                                    Identity
	eventID, messageID, prompt                  string
	err                                         error
}

func (s *commandStore) BotRuntimeConfig(context.Context) (*RuntimeConfig, error) {
	return s.config, nil
}
func (s *commandStore) RecordDiscovery(context.Context, int64, *QQEvent, string) error { return nil }
func (s *commandStore) GetConfig(context.Context) (*Config, error)                     { return &s.config.Config, nil }
func (s *commandStore) RuntimeConfig(context.Context, int64) (*RuntimeConfig, error) {
	return s.config, nil
}
func (s *commandStore) ClaimEvent(_ context.Context, key string) (bool, error) {
	if s.events == nil {
		s.events = map[string]bool{}
	}
	if s.events[key] {
		return false, nil
	}
	s.events[key] = true
	return true, nil
}
func (s *commandStore) CreateBindingRequest(_ context.Context, i Identity) (*BindingRequest, error) {
	s.binding++
	s.identity = i
	return &BindingRequest{ID: "request1"}, s.err
}
func (s *commandStore) ConfirmBinding(_ context.Context, i Identity, code string) (*Grant, error) {
	s.confirm++
	s.identity = i
	return &Grant{}, s.err
}
func (s *commandStore) CreateOrderFromEvent(_ context.Context, i Identity, eventID, messageID string, at time.Time, prompt string) (*Order, error) {
	s.orders++
	s.identity = i
	s.eventID = eventID
	s.messageID = messageID
	s.prompt = prompt
	return &Order{ID: "order1", Status: "queued", Price: "1.00000000"}, s.err
}
func (s *commandStore) GetOrderForIdentity(_ context.Context, i Identity, id string) (*Order, error) {
	s.reads++
	s.identity = i
	return &Order{ID: id, Status: "succeeded", FundsStatus: "captured", DeliveryStatus: "site_only"}, s.err
}
func (s *commandStore) RequestRedelivery(_ context.Context, i Identity, id, msg string) (*Order, error) {
	s.redelivery++
	s.identity = i
	s.messageID = msg
	return &Order{ID: id}, s.err
}

type commandSender struct {
	messages  []string
	sequences []int
	result    QQSendResult
}

func (s *commandSender) SendText(_ context.Context, c *RuntimeConfig, e *QQEvent, text string, sequence int) QQSendResult {
	s.messages = append(s.messages, text)
	s.sequences = append(s.sequences, sequence)
	if s.result.Status != "" {
		return s.result
	}
	return QQSendResult{Status: "sent"}
}
func commandPayload(command string) []byte {
	return []byte(fmt.Sprintf(`{"op":0,"id":"event1","t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"message1","group_openid":"group-1","author":{"member_openid":"member1","user_id":"victim"},"content":%q,"timestamp":%q}}`, command, time.Now().Format(time.RFC3339)))
}
func TestQQCommandsTrustedIdentityAndDeduplication(t *testing.T) {
	s := &commandStore{config: qqConfig()}
	sender := &commandSender{}
	handler := NewQQCommandHandler(s, sender)
	body := commandPayload("/生图 tree")
	headers := signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), body)
	for i := 0; i < 2; i++ {
		if _, err := handler.HandleWebhook(context.Background(), headers, body); err != nil {
			t.Fatal(err)
		}
	}
	if s.orders != 1 || s.identity.SubjectID != "member1" || s.messageID != "message1" || s.prompt != "tree" || len(sender.messages) != 1 {
		t.Fatalf("dedup/identity incorrect %+v %+v", s, sender)
	}
	if strings.Contains(sender.messages[0], "tree") || strings.Contains(sender.messages[0], "victim") {
		t.Fatal("private information leaked into group")
	}
}
func TestQQCommandsBindingHelpStatusAndRedelivery(t *testing.T) {
	for _, command := range []string{"/绑定", "/确认绑定 ABCD1234", "/帮助", "/生图帮助", "/任务 order1", "/重发 order1"} {
		t.Run(command, func(t *testing.T) {
			s := &commandStore{config: qqConfig()}
			sender := &commandSender{}
			handler := NewQQCommandHandler(s, sender)
			body := commandPayload(command)
			_, err := handler.HandleWebhook(context.Background(), signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), body), body)
			if err != nil || len(sender.messages) != 1 {
				t.Fatalf("command=%s err=%v messages=%v", command, err, sender.messages)
			}
			if strings.HasPrefix(command, "/重发") && (s.orders != 0 || s.redelivery != 1) {
				t.Fatal("redelivery created new generation")
			}
			if strings.Contains(sender.messages[0], "ABCD1234") {
				t.Fatal("confirmation code echoed")
			}
		})
	}
}
func TestQQCommandsRejectUnsignedAndDisallowedGroup(t *testing.T) {
	s := &commandStore{config: qqConfig()}
	sender := &commandSender{}
	handler := NewQQCommandHandler(s, sender)
	body := commandPayload("/生图 tree")
	if _, err := handler.HandleWebhook(context.Background(), http.Header{}, body); err == nil {
		t.Fatal("unsigned command accepted")
	}
	s.config.AllowedGroups = []string{"another"}
	if _, err := handler.HandleWebhook(context.Background(), signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), body), body); err == nil {
		t.Fatal("wrong group accepted")
	}
	if s.orders != 0 || len(sender.messages) != 0 {
		t.Fatal("rejected event caused side effect")
	}
}
func TestQQBindingKnownLinkFailureUsesManualCodeFallback(t *testing.T) {
	s := &commandStore{config: qqConfig()}
	sender := &commandSender{result: QQSendResult{Status: "failed"}}
	body := commandPayload("/绑定")
	handler := NewQQCommandHandler(s, sender)
	_, err := handler.HandleWebhook(context.Background(), signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), body), body)
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 2 || strings.Contains(sender.messages[1], "https://") || !strings.Contains(sender.messages[1], "request1") {
		t.Fatalf("no safe manual fallback: %+v", sender)
	}
}
func TestQQBindingUnknownReplyIsNotRetried(t *testing.T) {
	s := &commandStore{config: qqConfig()}
	sender := &commandSender{result: QQSendResult{Status: "unknown"}}
	body := commandPayload("/绑定")
	handler := NewQQCommandHandler(s, sender)
	_, _ = handler.HandleWebhook(context.Background(), signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), body), body)
	if len(sender.messages) != 1 {
		t.Fatal("unknown QQ reply blindly retried")
	}
}

func (s *commandStore) ClaimEventPayload(ctx context.Context, key, hash string) (bool, error) {
	if s.hashes == nil {
		s.hashes = map[string]string{}
	}
	if old, exists := s.hashes[key]; exists {
		if old != hash {
			return false, ErrConflict
		}
		return false, nil
	}
	s.hashes[key] = hash
	return true, nil
}
func TestQQCommandsConflictingDuplicateIsRejected(t *testing.T) {
	s := &commandStore{config: qqConfig()}
	sender := &commandSender{}
	handler := NewQQCommandHandler(s, sender)
	for index, command := range []string{"/生图 tree", "/生图 different"} {
		body := commandPayload(command)
		_, err := handler.HandleWebhook(context.Background(), signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), body), body)
		if index == 0 && err != nil {
			t.Fatal(err)
		}
		if index == 1 && err != ErrConflict {
			t.Fatalf("conflicting duplicate not rejected: %v", err)
		}
	}
	if s.orders != 1 || len(sender.messages) != 1 {
		t.Fatal("conflicting duplicate caused a second side effect")
	}
}
func TestQQCommandsDatabaseFailureDoesNotConsumeEvent(t *testing.T) {
	s := &commandStore{config: qqConfig(), err: fmt.Errorf("database unavailable")}
	sender := &commandSender{}
	handler := NewQQCommandHandler(s, sender)
	body := commandPayload("/生图 tree")
	headers := signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), body)
	if _, err := handler.HandleWebhook(context.Background(), headers, body); err == nil {
		t.Fatal("transient DB failure must roll back event claim for platform retry")
	}
	if len(sender.messages) != 0 {
		t.Fatal("uncommitted command must not send a receipt")
	}
	s.err = nil
	if _, err := handler.HandleWebhook(context.Background(), headers, body); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.HandleWebhook(context.Background(), headers, body); err != nil {
		t.Fatal(err)
	}
	if s.orders != 2 || len(sender.messages) != 1 {
		t.Fatalf("want failed attempt plus one success, once reply; calls=%d sends=%d", s.orders, len(sender.messages))
	}
}

func (s *commandStore) ProcessEvent(ctx context.Context, key, hash string, execute func(context.Context) (string, string, error)) (string, string, bool, error) {
	if s.hashes == nil {
		s.hashes = map[string]string{}
	}
	if prior, ok := s.hashes[key]; ok {
		if prior != hash {
			return "", "", false, ErrConflict
		}
		return "", "", false, nil
	}
	reply, manual, err := execute(ctx)
	if err != nil {
		return "", "", false, err
	}
	s.hashes[key] = hash
	return reply, manual, true, nil
}
