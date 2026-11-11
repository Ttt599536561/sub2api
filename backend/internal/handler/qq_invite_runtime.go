package handler

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/napcat"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/Wei-Shaw/sub2api/internal/qqinvite"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

type qqInviteBotClient interface {
	Status(context.Context) (napcat.Status, error)
	GroupMembers(context.Context, string) ([]string, error)
	SendGroup(context.Context, string, string, string) (string, error)
	SendPrivate(context.Context, string, string) (string, error)
}
type qqInviteBotAdapter struct {
	client     qqInviteBotClient
	expectedQQ string
}

func (b qqInviteBotAdapter) check(ctx context.Context) error {
	s, err := b.client.Status(ctx)
	if err != nil {
		return err
	}
	if !s.Online || s.QQ != b.expectedQQ {
		return napcat.ErrOffline
	}
	return nil
}
func (b qqInviteBotAdapter) CheckAccount(ctx context.Context) error {
	return b.check(ctx)
}
func (b qqInviteBotAdapter) GroupMembers(ctx context.Context, group string) ([]string, error) {
	if err := b.check(ctx); err != nil {
		return nil, err
	}
	return b.client.GroupMembers(ctx, group)
}
func inviteSendError(err error) error {
	if errors.Is(err, napcat.ErrDeliveryUnknown) {
		return qqinvite.ErrDeliveryUnknown
	}
	return err
}
func (b qqInviteBotAdapter) SendGroup(ctx context.Context, group, text, mention string) (string, error) {
	if err := b.check(ctx); err != nil {
		return "", err
	}
	id, err := b.client.SendGroup(ctx, group, text, mention)
	return id, inviteSendError(err)
}
func (b qqInviteBotAdapter) SendPrivate(ctx context.Context, qq, text string) (string, error) {
	if err := b.check(ctx); err != nil {
		return "", err
	}
	id, err := b.client.SendPrivate(ctx, qq, text)
	return id, inviteSendError(err)
}

type qqInviteExpectedControl struct {
	*napcat.Client
	expectedQQ string
}

func (c qqInviteExpectedControl) Status(ctx context.Context) (napcat.Status, error) {
	s, err := c.Client.Status(ctx)
	if err == nil && s.Online && s.QQ != c.expectedQQ {
		return napcat.Status{}, napcat.ErrOffline
	}
	return s, err
}

type QQInviteRuntime struct {
	core     *qqinvite.Store
	worker   *qqinvite.Runtime
	mu       sync.Mutex
	current  qqinvite.Config
	client   *napcat.Client
	cancel   context.CancelFunc
	done     chan struct{}
	disabled bool
}

func (r *QQInviteRuntime) clientFor(c qqinvite.Config) (*napcat.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client != nil && r.current.Version == c.Version && r.current.OneBotToken == c.OneBotToken && r.current.WebUIToken == c.WebUIToken && r.current.OneBotURL == c.OneBotURL && r.current.WebUIURL == c.WebUIURL {
		return r.client, nil
	}
	client, err := napcat.NewClient(napcat.Config{OneBotURL: c.OneBotURL, WebUIURL: c.WebUIURL, OneBotToken: c.OneBotToken, WebUIToken: c.WebUIToken, AllowedHosts: inviteAllowedHosts()})
	if err != nil {
		return nil, err
	}
	if r.client != nil {
		r.client.Close()
	}
	r.client = client
	r.current = c
	return client, nil
}
func (r *QQInviteRuntime) control(ctx context.Context) (qqInviteControl, error) {
	cfg, err := r.core.RuntimeConfig(ctx)
	if err != nil {
		return nil, err
	}
	c, err := r.clientFor(cfg)
	if err != nil {
		return nil, err
	}
	return qqInviteExpectedControl{Client: c, expectedQQ: cfg.BotQQ}, nil
}
func (r *QQInviteRuntime) Stop(ctx context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	if r.done != nil {
		select {
		case <-r.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client != nil {
		r.client.Close()
	}
	return nil
}
func ProvideQQInviteRuntime(db *sql.DB, cfg *config.Config) *QQInviteRuntime {
	key, err := qqimage.ParseEncryptionKey(os.Getenv("QQ_INVITE_ENCRYPTION_KEY"))
	if err != nil {
		slog.Warn("NapCat invitation encryption key invalid; configuration disabled")
		key = nil
	}
	r := &QQInviteRuntime{core: qqinvite.NewStore(db, key), disabled: cfg != nil && cfg.RunMode == config.RunModeSimple}
	if r.disabled {
		return r
	}
	// Deployment bootstrap is first-run only and disabled until an administrator
	// verifies the expected account and enables reception. Never overwrite a save.
	bootstrapCtx, stopBootstrap := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopBootstrap()
	stored, loadErr := r.core.GetConfig(bootstrapCtx)
	if loadErr == nil && stored.Version == 0 && strings.TrimSpace(os.Getenv("QQ_INVITE_BOT_QQ")) != "" {
		first := qqinvite.Config{BotQQ: os.Getenv("QQ_INVITE_BOT_QQ"), OneBotURL: os.Getenv("QQ_INVITE_ONEBOT_URL"), WebUIURL: os.Getenv("QQ_INVITE_WEBUI_URL"), OneBotToken: os.Getenv("QQ_INVITE_ONEBOT_TOKEN"), WebUIToken: os.Getenv("QQ_INVITE_WEBUI_TOKEN"), EventSecret: os.Getenv("QQ_INVITE_EVENT_SECRET"), PublicBaseURL: os.Getenv("QQ_INVITE_PUBLIC_BASE_URL")}
		if _, e := r.clientFor(first); e == nil {
			if _, e = r.core.SaveConfig(bootstrapCtx, first, 0); e != nil {
				slog.Warn("NapCat first-run configuration not saved; configure in administrator panel")
			}
		} else {
			slog.Warn("NapCat first-run endpoints invalid; configure in administrator panel")
		}
	}
	if loadErr != nil {
		slog.Warn("NapCat configuration bootstrap unavailable; administrator setup remains available")
	}
	stopBootstrap()
	r.worker = qqinvite.NewRuntime(r.core, func(c qqinvite.Config) (qqinvite.Bot, error) {
		client, err := r.clientFor(c)
		if err != nil {
			return nil, err
		}
		return qqInviteBotAdapter{client: client, expectedQQ: c.BotQQ}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() { defer close(r.done); r.worker.Run(ctx) }()
	return r
}
func ProvideQQInviteHandler(r *QQInviteRuntime, settings *service.SettingService, cfg *config.Config) *QQInviteHandler {
	h := &QQInviteHandler{store: r.core, control: r.control, settings: settings, cfg: cfg, disabled: r.disabled}
	if r.worker != nil {
		h.baseline = r.worker.SyncBaseline
	}
	return h
}
