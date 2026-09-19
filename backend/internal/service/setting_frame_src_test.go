package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type frameSrcSettingsRepo struct {
	SettingRepository
	values map[string]string
	err    error
}

func (r frameSrcSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func TestGetFrameSrcOriginsIndependentOfWelfareAvailability(t *testing.T) {
	for _, purchaseEnabled := range []bool{true, false} {
		name := "purchase_disabled"
		if purchaseEnabled {
			name = "purchase_enabled"
		}
		t.Run(name, func(t *testing.T) {
			values := map[string]string{
				SettingKeyHomeContent:             "https://home.example.test:8443/welcome",
				SettingKeyPurchaseSubscriptionURL: " https://purchase.example.test/checkout ",
				SettingKeyCustomMenuItems: `[
					{"url":"https://home.example.test:8443/duplicate"},
					{"url":"https://admin.example.test/dashboard","visibility":"admin"},
					{"url":"javascript:alert(1)"},
					{"url":"/local-page"}
				]`,
			}
			if purchaseEnabled {
				values[SettingKeyPurchaseSubscriptionEnabled] = "true"
			}
			svc := NewSettingService(frameSrcSettingsRepo{values: values}, &config.Config{})
			welfareErr := errors.New("welfare temporarily unavailable")
			svc.SetWelfareAvailabilityProvider(func(context.Context) (bool, error) { return false, welfareErr })

			origins, err := svc.GetFrameSrcOrigins(context.Background())
			require.NoError(t, err, "healthy iframe configuration must not depend on the welfare query")
			want := []string{"https://home.example.test:8443", "https://admin.example.test"}
			if purchaseEnabled {
				want = append(want, "https://purchase.example.test")
			}
			require.ElementsMatch(t, want, origins)

			// Public configuration must still propagate unknown welfare availability
			// so HTML cannot cache the feature as disabled.
			_, err = svc.GetPublicSettings(context.Background())
			require.ErrorIs(t, err, welfareErr)
		})
	}
}

func TestGetFrameSrcOriginsPropagatesSettingsReadFailure(t *testing.T) {
	readErr := errors.New("settings database unavailable")
	svc := NewSettingService(frameSrcSettingsRepo{err: readErr}, &config.Config{})
	origins, err := svc.GetFrameSrcOrigins(context.Background())
	require.ErrorIs(t, err, readErr)
	require.Nil(t, origins)
}
