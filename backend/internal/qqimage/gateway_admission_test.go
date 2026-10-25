package qqimage

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGatewayAdmissionModesAndStaleConfig(t *testing.T) {
	s := &commandStore{config: qqConfig()}
	h := NewQQCommandHandler(s, &commandSender{})
	raw := commandPayload("/生图 tree")
	if e := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, raw); !errors.Is(e, ErrDisabled) {
		t.Fatalf("legacy mode admitted WS: %v", e)
	}
	s.config.ReceiveMode = ReceiveModeWebSocket
	for _, p := range []struct {
		id  int64
		app string
	}{{s.config.ID + 1, s.config.AppID}, {s.config.ID, "other"}} {
		if e := h.HandleGatewayEvent(context.Background(), p.id, p.app, raw); !errors.Is(e, ErrConflict) {
			t.Fatalf("stale socket: %v", e)
		}
	}
	if _, e := h.HandleWebhook(context.Background(), http.Header{}, raw); !errors.Is(e, ErrDisabled) {
		t.Fatalf("HTTP remained active: %v", e)
	}
	if s.orders != 0 {
		t.Fatal("invalid admission created order")
	}
}
func TestGatewayAndWebhookShareMessageDedupe(t *testing.T) {
	for _, firstWS := range []bool{false, true} {
		t.Run(strconv.FormatBool(firstWS), func(t *testing.T) {
			s := &commandStore{config: qqConfig()}
			sender := &commandSender{}
			h := NewQQCommandHandler(s, sender)
			raw := commandPayload("/生图 tree")
			ws := func() {
				s.config.ReceiveMode = ReceiveModeWebSocket
				if e := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, raw); e != nil {
					t.Fatal(e)
				}
			}
			hook := func() {
				s.config.ReceiveMode = ReceiveModeWebhook
				if _, e := h.HandleWebhook(context.Background(), signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), raw), raw); e != nil {
					t.Fatal(e)
				}
			}
			if firstWS {
				ws()
				hook()
			} else {
				hook()
				ws()
			}
			ws()
			if s.orders != 1 || len(sender.messages) != 1 {
				t.Fatal("duplicate business/reply")
			}
		})
	}
}
func TestGatewayDiscoveryWithoutGenerationAndNoBusiness(t *testing.T) {
	s := &discoveryCommandStore{commandStore: commandStore{config: qqConfig()}}
	s.config.ReceiveMode = ReceiveModeWebSocket
	s.config.Enabled = false
	s.config.AllowedGroups = nil
	sender := &commandSender{}
	e := NewQQCommandHandler(s, sender).HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, commandPayload("/识别群 "+strings.Repeat("a", 48)))
	if e != nil || s.discoveryCalls != 1 || s.runtimeCalls != 0 || s.orders+s.binding+s.confirm+s.redelivery != 0 || len(sender.messages) != 0 {
		t.Fatalf("discovery failure/side effect: %v", e)
	}
}

type rotatingGatewayStore struct {
	commandStore
	rotate bool
}

func (s *rotatingGatewayStore) ProcessEvent(ctx context.Context, key, hash string, f func(context.Context) (string, string, error)) (string, string, bool, error) {
	if s.rotate {
		s.config.ID++
	}
	return s.commandStore.ProcessEvent(ctx, key, hash, f)
}
func TestGatewayRechecksConfigInsideBusinessTransaction(t *testing.T) {
	s := &rotatingGatewayStore{commandStore: commandStore{config: qqConfig()}, rotate: true}
	s.config.ReceiveMode = ReceiveModeWebSocket
	sender := &commandSender{}
	e := NewQQCommandHandler(s, sender).HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, commandPayload("/生图 tree"))
	if !errors.Is(e, ErrConflict) || s.orders != 0 || len(sender.messages) != 0 {
		t.Fatalf("stale transaction admitted: %v", e)
	}
}
func TestGatewayEventEnvelopeFallbackAndValidation(t *testing.T) {
	s := &commandStore{config: qqConfig()}
	s.config.ReceiveMode = ReceiveModeWebSocket
	h := NewQQCommandHandler(s, &commandSender{})
	raw := strings.Replace(string(commandPayload("/生图 tree")), `"id":"event1",`, "", 1)
	if e := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, []byte(raw)); e != nil {
		t.Fatal(e)
	}
	if s.eventID != "message1" {
		t.Fatal("missing stable event fallback")
	}
	for _, raw := range []string{`{"op":13,"d":{}}`, `{"op":0,"t":"C2C_MESSAGE_CREATE","d":{}}`, strings.Repeat("x", maxQQEventBytes+1)} {
		if e := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, []byte(raw)); e == nil {
			t.Fatal("invalid event accepted")
		}
	}
	s.config.ReceiveMode = ""
	if _, e := h.HandleWebhook(context.Background(), http.Header{}, commandPayload("/生图 tree")); !errors.Is(e, ErrQQAuthentication) {
		t.Fatal("HTTP signature bypassed")
	}
}
func TestGatewayReceiveModeConfigCompatibility(t *testing.T) {
	if DefaultConfig().ReceiveMode != ReceiveModeWebhook || receiveMode("") != ReceiveModeWebhook {
		t.Fatal("legacy default changed")
	}
	s := NewStore(nil, nil)
	if _, e := s.SaveConfig(context.Background(), Config{ReceiveMode: "invalid"}, 1); !errors.Is(e, ErrInvalid) {
		t.Fatalf("invalid mode not rejected: %v", e)
	}
}
