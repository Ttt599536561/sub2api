package qqimage

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

type discoveryCommandStore struct {
	commandStore
	discoveryCalls, runtimeCalls int
}

func (s *discoveryCommandStore) RuntimeConfig(context.Context, int64) (*RuntimeConfig, error) {
	s.runtimeCalls++
	return nil, ErrEncryptionUnavailable
}
func (s *discoveryCommandStore) BotRuntimeConfig(context.Context) (*RuntimeConfig, error) {
	return s.config, nil
}
func (s *discoveryCommandStore) RecordDiscovery(context.Context, int64, *QQEvent, string) error {
	s.discoveryCalls++
	return nil
}
func TestDiscoveryWebhookBeforeWhitelistWithoutUpstreamCredentials(t *testing.T) {
	s := &discoveryCommandStore{commandStore: commandStore{config: qqConfig()}}
	s.config.Enabled = false
	s.config.AllowedGroups = nil
	sender := &commandSender{}
	body := commandPayload("/识别群 " + strings.Repeat("a", 48))
	_, err := NewQQCommandHandler(s, sender).HandleWebhook(context.Background(), signQQ(t, "secret", strconv.FormatInt(time.Now().Unix(), 10), body), body)
	if err != nil || s.discoveryCalls != 1 || s.runtimeCalls != 0 {
		t.Fatalf("discovery blocked by generation settings: err=%v discovery=%d generation_config=%d", err, s.discoveryCalls, s.runtimeCalls)
	}
	if len(sender.messages) != 0 || s.orders+s.binding+s.confirm+s.redelivery != 0 {
		t.Fatal("discovery produced business side effects")
	}
}
func TestDiscoveryBotOnlyConfigurationCanBeSaved(t *testing.T) {
	s := testStore(t)
	c, err := s.SaveConfig(context.Background(), Config{AppID: "only-bot", BotSecret: "new-bot-secret", ClearAPIKey: true}, 1)
	if err != nil {
		t.Fatalf("bot-only disabled configuration rejected: %v", err)
	}
	if c.Enabled || !c.BotSecretConfigured || c.APIKeyConfigured {
		t.Fatalf("unexpected configuration: %+v", c)
	}
}
func TestDiscoveryWebhookRejectsBadSignatureWithoutRecording(t *testing.T) {
	s := &discoveryCommandStore{commandStore: commandStore{config: qqConfig()}}
	body := commandPayload("/识别群 " + strings.Repeat("a", 48))
	_, err := NewQQCommandHandler(s, &commandSender{}).HandleWebhook(context.Background(), signQQ(t, "wrong-secret", strconv.FormatInt(time.Now().Unix(), 10), body), body)
	if !errors.Is(err, ErrQQAuthentication) || s.discoveryCalls != 0 {
		t.Fatalf("invalid signature: err=%v discovery=%d", err, s.discoveryCalls)
	}
}
