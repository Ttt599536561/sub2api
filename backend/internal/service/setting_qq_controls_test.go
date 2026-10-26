//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestQQControlsPublicSettingsAndInjection(t *testing.T) {
	for _, tc := range []struct {
		name           string
		values         map[string]string
		binding, image bool
	}{
		{"missing", nil, true, true},
		{"both", map[string]string{"qq_binding_enabled": "true", "qq_image_enabled": "true"}, true, true},
		{"binding", map[string]string{"qq_binding_enabled": "true", "qq_image_enabled": "false"}, true, false},
		{"image", map[string]string{"qq_binding_enabled": "false", "qq_image_enabled": "true"}, false, true},
		{"neither", map[string]string{"qq_binding_enabled": "false", "qq_image_enabled": "false"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewSettingService(&settingPublicRepoStub{values: tc.values}, &config.Config{})
			public, err := svc.GetPublicSettings(context.Background())
			require.NoError(t, err)
			injection, err := svc.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			for _, payload := range []any{public, injection} {
				b, err := json.Marshal(payload)
				require.NoError(t, err)
				var object map[string]any
				require.NoError(t, json.Unmarshal(b, &object))
				require.Equal(t, tc.binding, object["qq_binding_enabled"])
				require.Equal(t, tc.image, object["qq_image_enabled"])
			}
			parsed := svc.parseSettings(tc.values)
			repo := &settingUpdateRepoStub{}
			writer := NewSettingService(repo, &config.Config{})
			require.NoError(t, writer.UpdateSettings(context.Background(), parsed))
			require.Equal(t, strconv.FormatBool(tc.binding), repo.updates["qq_binding_enabled"])
			require.Equal(t, strconv.FormatBool(tc.image), repo.updates["qq_image_enabled"])
		})
	}
}

func TestQQControlsPublicSettingsReadFailure(t *testing.T) {
	unavailable := errors.New("settings unavailable")
	svc := NewSettingService(&settingPublicRepoStub{err: unavailable}, &config.Config{})
	value, err := svc.GetPublicSettingsForInjection(context.Background())
	require.ErrorIs(t, err, unavailable)
	require.Nil(t, value)
}

func TestQQControlsDefaultSettingsInitialization(t *testing.T) {
	repo := &forwardedIPMigrationRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.InitializeDefaultSettings(context.Background()))
	require.Equal(t, "true", repo.values[SettingKeyQQBindingEnabled])
	require.Equal(t, "true", repo.values[SettingKeyQQImageEnabled])
}
