package handler

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func ProvideQQImageRuntime(db *sql.DB, balance *service.BillingCacheService, keys *service.APIKeyService, cfg *config.Config) *QQImageRuntime {
	key, err := qqimage.ParseEncryptionKey(os.Getenv("QQ_IMAGE_ENCRYPTION_KEY"))
	if err != nil {
		slog.Warn("QQ image encryption key invalid; credential operations disabled")
		key = nil
	}
	externalHelpValue := os.Getenv("QQ_IMAGE_EXTERNAL_HELP")
	externalHelp, parseErr := strconv.ParseBool(externalHelpValue)
	if externalHelpValue != "" && parseErr != nil {
		slog.Warn("QQ image external-help flag invalid; standalone help retained")
	}
	// The admission policy is immutable and installed before any receiver starts.
	store := qqimage.NewStore(db, key, qqimage.StoreOptions{DisableNewAdmissions: cfg != nil && cfg.RunMode == config.RunModeSimple})
	client := qqimage.NewQQClient()
	runtime := &QQImageRuntime{core: store, commands: qqimage.NewQQCommandHandlerWithOptions(store, client, qqimage.QQCommandOptions{ExternalGeneralHelp: externalHelp}), worker: qqimage.NewWorker(store, qqimage.NewImageProvider(), client), outbox: store, balance: balance, auth: keys}
	if cfg == nil || cfg.RunMode != config.RunModeSimple {
		runtime.gateway = qqimage.NewGatewaySupervisor(store, client, runtime.commands)
	}
	runtime.Start()
	return runtime
}
func ProvideQQImageHandler(runtime *QQImageRuntime, auth *service.AuthService, settings *service.SettingService, cfg *config.Config) *QQImageHandler {
	h := &QQImageHandler{store: runtime.core, email: auth, settings: settings, cfg: cfg}
	h.connection = func() qqimage.GatewayStatus {
		if runtime.gateway == nil {
			return qqimage.GatewayStatus{State: "disabled"}
		}
		return runtime.gateway.Status()
	}
	h.webhook = func(ctx context.Context, headers http.Header, raw []byte) (any, error) {
		challenge, err := runtime.commands.HandleWebhook(ctx, headers, raw)
		if errors.Is(err, qqimage.ErrQQUnsupportedEvent) {
			return map[string]int{"op": 12}, nil
		}
		if err != nil {
			return nil, err
		}
		if challenge != nil {
			return challenge, nil
		}
		return map[string]int{"op": 12}, nil
	}
	return h
}
