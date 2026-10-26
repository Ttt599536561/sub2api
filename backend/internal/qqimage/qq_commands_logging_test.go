package qqimage

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestQQCommandReplyFailureLogsArePrivateAndDoNotRepeatBusiness(t *testing.T) {
	for _, tt := range []struct {
		name, command, status, reason, safeReason string
		sends, logs                               int
	}{
		{"generation_failed", "/生图 private-prompt", "failed", "qq_message_send_http_400_code_40034004", "qq_message_send_http_400_code_40034004", 1, 1},
		{"redelivery_unknown", "/重发 private-order", "unknown", "qq_result_unknown", "qq_result_unknown", 1, 1},
		{"binding_manual_failed", "/绑定", "failed", "qq_error_40034004", "qq_error_40034004", 2, 2},
		{"binding_unknown", "/绑定", "unknown", "qq_invalid_response", "qq_invalid_response", 1, 1},
		{"sent_is_quiet", "/重发 private-order", "sent", "secret-sent-detail", "", 1, 0},
		{"unsafe_reason", "/重发 private-order", "failed", "qq_message_send_http_400_code_40034004\nhttps://private.invalid/token-secret", "qq_unclassified", 1, 1},
		{"unknown_stage", "/重发 private-order", "failed", "qq_external_http_400_code_1", "qq_unclassified", 1, 1},
		{"overflow_code", "/重发 private-order", "failed", "qq_message_send_http_400_code_9223372036854775808", "qq_unclassified", 1, 1},
		{"success_http_reason", "/重发 private-order", "failed", "qq_message_send_http_200_code_1", "qq_unclassified", 1, 1},
		{"fixed_reason", "/重发 private-order", "failed", "qq_invalid_token_response", "qq_invalid_token_response", 1, 1},
		{"token_stage", "/重发 private-order", "failed", "qq_token_http_401_code_11244", "qq_token_http_401_code_11244", 1, 1},
		{"media_stage", "/重发 private-order", "failed", "qq_media_upload_http_400", "qq_media_upload_http_400", 1, 1},
		{"missing_reason", "/重发 private-order", "failed", "", "qq_unclassified", 1, 1},
		{"missing_receipt", "/重发 private-order", "unknown", "qq_missing_receipt", "qq_missing_receipt", 1, 1},
		{"negative_code", "/重发 private-order", "failed", "qq_error_-1", "qq_error_-1", 1, 1},
		{"unknown_command", "/private-command private-token", "failed", "qq_invalid_request", "", 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			prior := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			t.Cleanup(func() { slog.SetDefault(prior) })
			s := &commandStore{config: qqConfig()}
			s.config.ReceiveMode = ReceiveModeWebSocket
			sender := &commandSender{result: QQSendResult{Status: tt.status, Reason: tt.reason}}
			handler := NewQQCommandHandler(s, sender)
			body := commandPayload(tt.command)
			for i := 0; i < 2; i++ {
				if err := handler.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, body); err != nil {
					t.Fatal(err)
				}
			}
			if len(sender.messages) != tt.sends {
				t.Fatalf("logging changed sends: got %d want %d", len(sender.messages), tt.sends)
			}
			wantBusiness := 1
			if tt.sends == 0 {
				wantBusiness = 0
			}
			if s.orders+s.redelivery+s.binding != wantBusiness || (strings.HasPrefix(tt.command, "/重发") && s.orders != 0) {
				t.Fatalf("logging repeated business or generated on redelivery: orders=%d redelivery=%d binding=%d", s.orders, s.redelivery, s.binding)
			}
			for _, forbidden := range []string{"private-", "secret", "order1", "request1", "message1", "event1", "group-1", "member1", "https://", "site.example.com", "已有图片", "预留"} {
				if strings.Contains(output.String(), forbidden) {
					t.Fatalf("private field leaked into logs: %q", forbidden)
				}
			}
			decoder := json.NewDecoder(&output)
			count := 0
			for decoder.More() {
				var record struct {
					Command       string `json:"command"`
					ReplySequence int    `json:"reply_sequence"`
					Status        string `json:"status"`
					Reason        string `json:"reason"`
					HasURL        bool   `json:"has_url"`
					Length        int    `json:"length"`
				}
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				if count >= len(sender.messages) {
					t.Fatal("extra reply diagnostic")
				}
				if record.Command != strings.Fields(tt.command)[0] || record.ReplySequence != sender.sequences[count] || record.Status != tt.status || record.Reason != tt.safeReason || record.Length != utf8.RuneCountInString(sender.messages[count]) || record.HasURL != strings.Contains(sender.messages[count], "https://") {
					t.Fatalf("incorrect reply diagnostic: %+v", record)
				}
				count++
			}
			if count != tt.logs {
				t.Fatalf("diagnostic count=%d want=%d", count, tt.logs)
			}
		})
	}
}
