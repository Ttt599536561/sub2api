package qqimage

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestQQCompactImageCommandsUseExistingBalanceAndDedupe(t *testing.T) {
	cases := []struct{ name, input, prompt string }{
		{"compact Chinese", "/生图一个黑洞", "一个黑洞"},
		{"compact English", "/生图a black hole", "a black hole"},
		{"ordinary space", "/生图 一个黑洞", "一个黑洞"},
		{"ideographic space", "/生图\u3000一个黑洞", "一个黑洞"},
		{"nonbreaking space", "/生图\u00a0一个黑洞", "一个黑洞"},
		{"next line separator", "/生图\u0085一个黑洞", "一个黑洞"},
		{"internal whitespace", "/生图一个 黑洞\n蓝色\t背景", "一个 黑洞\n蓝色\t背景"},
		{"outer whitespace", "\u3000/生图一个黑洞\u3000", "一个黑洞"},
		{"existing BOM in prompt", "/生图 \ufeff一个黑洞", "\ufeff一个黑洞"},
	}
	for _, transport := range []string{"websocket", "webhook"} {
		for _, tc := range cases {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				store := &commandStore{config: qqConfig(), err: ErrInsufficientBalance}
				sender := &commandSender{}
				handler := NewQQCommandHandler(store, sender)
				raw := compactCommandPayload(t, tc.input)
				for i := 0; i < 2; i++ {
					var err error
					if transport == "websocket" {
						store.config.ReceiveMode = ReceiveModeWebSocket
						err = handler.HandleGatewayEvent(context.Background(), store.config.ID, store.config.AppID, raw)
					} else {
						store.config.ReceiveMode = ReceiveModeWebhook
						_, err = handler.HandleWebhook(context.Background(), signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), raw), raw)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if store.orders != 1 || store.prompt != tc.prompt || len(store.hashes) != 1 || len(sender.messages) != 1 {
					t.Fatalf("order attempts=%d prompt=%q events=%d replies=%d", store.orders, store.prompt, len(store.hashes), len(sender.messages))
				}
				if sender.messages[0] != "可用余额不足，本次未生成；请登录原网站检查余额。" {
					t.Fatalf("unexpected insufficient-balance reply: %q", sender.messages[0])
				}
			})
		}
	}
}

func TestQQCompactImageCommandsPreserveHelpAndUnknownOwnership(t *testing.T) {
	cases := []struct {
		name, input string
		reply       bool
	}{
		{"help", "/生图帮助", true},
		{"help copied menu line", "/生图帮助 查看说明", true},
		{"help ideographic space", "/生图帮助\u3000查看说明", true},
		{"help next line separator", "/生图帮助\u0085查看说明", true},
		{"empty prompt", "/生图", true},
		{"whitespace prompt", "/生图\u3000", true},
		{"unknown help suffix", "/生图帮助xxx", false},
		{"help BOM suffix", "/生图帮助\ufeff查看说明", false},
		{"IQ ownership", "/智商月卡", false},
		{"other command unicode boundary", "/绑定\u3000x", false},
		{"fullwidth slash", "／生图 一个黑洞", false},
		{"BOM before command", "\ufeff/生图 一个黑洞", false},
		{"BOM compact suffix", "/生图\ufeff一个黑洞", false},
		{"zero width compact suffix", "/生图\u200b一个黑洞", false},
		{"joiner compact suffix", "/生图\u200d一个黑洞", false},
		{"control compact suffix", "/生图\x00一个黑洞", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &commandStore{config: qqConfig()}
			store.config.ReceiveMode = ReceiveModeWebSocket
			sender := &commandSender{}
			handler := NewQQCommandHandler(store, sender)
			if err := handler.HandleGatewayEvent(context.Background(), store.config.ID, store.config.AppID, compactCommandPayload(t, tc.input)); err != nil {
				t.Fatal(err)
			}
			if store.orders+store.binding+store.confirm+store.redelivery != 0 {
				t.Fatal("help/unknown command attempted a mutation")
			}
			if tc.reply {
				if len(store.hashes) != 1 || len(sender.messages) != 1 || strings.TrimSpace(sender.messages[0]) == "" {
					t.Fatal("missing help/usage reply")
				}
			} else if len(store.hashes) != 0 || len(sender.messages) != 0 {
				t.Fatal("unknown command ownership changed")
			}
		})
	}
}

func TestQQCompactImageCommandsKeepGroupAuthorization(t *testing.T) {
	store := &commandStore{config: qqConfig()}
	store.config.ReceiveMode = ReceiveModeWebSocket
	store.config.AllowedGroups = nil
	sender := &commandSender{}
	handler := NewQQCommandHandler(store, sender)
	err := handler.HandleGatewayEvent(context.Background(), store.config.ID, store.config.AppID, compactCommandPayload(t, "/生图一个黑洞"))
	if !errors.Is(err, ErrForbidden) || store.orders != 0 || len(store.hashes) != 0 || len(sender.messages) != 0 {
		t.Fatalf("unauthorized group admitted: %v", err)
	}
}

func compactCommandPayload(t *testing.T, content string) []byte {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(commandPayload("fixture"), &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["d"].(map[string]any)["content"] = content
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
