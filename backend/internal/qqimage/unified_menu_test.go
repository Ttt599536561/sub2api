package qqimage

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExternalGeneralHelpLeavesOneMenuOwner(t *testing.T) {
	for _, command := range []string{"", "   ", "/帮助", "/菜单", "/智商 月卡分组"} {
		t.Run(command, func(t *testing.T) {
			s := &commandStore{config: qqConfig()}
			s.config.ReceiveMode = ReceiveModeWebSocket
			sender := &commandSender{}
			h := NewQQCommandHandlerWithOptions(s, sender, QQCommandOptions{ExternalGeneralHelp: true})
			if e := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, commandPayload(command)); e != nil {
				t.Fatal(e)
			}
			if len(sender.messages) != 0 || len(s.hashes) != 0 || s.orders+s.binding+s.confirm+s.redelivery != 0 {
				t.Fatal("delegated menu created reply or business")
			}
		})
	}
}
func TestImageHelpDoesNotNeedProviderCredentials(t *testing.T) {
	s := &discoveryCommandStore{commandStore: commandStore{config: qqConfig()}}
	s.config.Enabled = false
	s.config.ReceiveMode = ReceiveModeWebSocket
	sender := &commandSender{result: QQSendResult{Status: "unknown"}}
	h := NewQQCommandHandlerWithOptions(s, sender, QQCommandOptions{ExternalGeneralHelp: true})
	raw := commandPayload("/生图帮助")
	for i := 0; i < 2; i++ {
		if e := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, raw); e != nil {
			t.Fatal(e)
		}
	}
	if s.runtimeCalls != 0 || len(sender.messages) != 1 || s.orders+s.binding+s.confirm+s.redelivery != 0 {
		t.Fatal("help decrypted upstream credentials or repeated business/send")
	}
	if !strings.Contains(sender.messages[0], "/生图") || !strings.Contains(sender.messages[0], "生图暂未开放") || strings.Contains(sender.messages[0], "/智商") {
		t.Fatal("invalid targeted help")
	}
}
func TestStandaloneGeneralHelpRemainsCompatible(t *testing.T) {
	s := &discoveryCommandStore{commandStore: commandStore{config: qqConfig()}}
	sender := &commandSender{}
	h := NewQQCommandHandler(s, sender)
	raw := commandPayload("/帮助")
	if _, e := h.HandleWebhook(context.Background(), signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), raw), raw); e != nil {
		t.Fatal(e)
	}
	if len(sender.messages) != 1 || s.runtimeCalls != 0 {
		t.Fatal("default help alias no longer works")
	}
}
func TestExternalHelpDoesNotRelaxImageCommandAdmission(t *testing.T) {
	s := &discoveryCommandStore{commandStore: commandStore{config: qqConfig()}}
	s.config.ReceiveMode = ReceiveModeWebSocket
	sender := &commandSender{}
	h := NewQQCommandHandlerWithOptions(s, sender, QQCommandOptions{ExternalGeneralHelp: true})
	if e := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, commandPayload("/生图 cat")); !errors.Is(e, ErrEncryptionUnavailable) {
		t.Fatalf("generation credential gate removed: %v", e)
	}
	if s.runtimeCalls != 1 || s.orders != 0 || len(sender.messages) != 0 {
		t.Fatal("generation escaped original gate")
	}
	s.config.AllowedGroups = nil
	if e := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, commandPayload("/生图帮助")); !errors.Is(e, ErrForbidden) {
		t.Fatalf("unapproved group replied: %v", e)
	}
	if len(sender.messages) != 0 {
		t.Fatal("unapproved help reply")
	}
}
func TestImageHelpOmitsUnsetModelAndPrice(t *testing.T) {
	s := &commandStore{config: qqConfig()}
	s.config.Model = ""
	s.config.Size = ""
	s.config.Price = ""
	s.config.Enabled = false
	s.config.ReceiveMode = ReceiveModeWebSocket
	sender := &commandSender{}
	h := NewQQCommandHandlerWithOptions(s, sender, QQCommandOptions{ExternalGeneralHelp: true})
	if e := h.HandleGatewayEvent(context.Background(), s.config.ID, s.config.AppID, commandPayload("/生图帮助")); e != nil {
		t.Fatal(e)
	}
	if len(sender.messages) != 1 || strings.Contains(sender.messages[0], "US$") || strings.Contains(sender.messages[0], "固定模型 ") {
		t.Fatal("blank configuration advertised as price/model")
	}
}
