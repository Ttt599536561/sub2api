package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/stretchr/testify/require"
)

type qqStatusStoreStub struct {
	qqHTTPStoreStub
	binding, image bool
	controlErr     error
	disabled       bool
}

func (s *qqStatusStoreStub) BusinessControls(context.Context) (bool, bool, error) {
	return s.binding, s.image, s.controlErr
}
func (s *qqStatusStoreStub) GetConfig(context.Context) (*qqimage.Config, error) {
	return &qqimage.Config{Enabled: !s.disabled}, nil
}
func (s *qqStatusStoreStub) EmailVerified(context.Context, int64) (bool, error) { return true, nil }
func (s *qqStatusStoreStub) ListGrants(context.Context, int64) ([]qqimage.Grant, error) {
	return []qqimage.Grant{}, nil
}

func TestQQControlsStatusEffectiveFlags(t *testing.T) {
	for _, binding := range []bool{false, true} {
		for _, image := range []bool{false, true} {
			for _, mode := range []string{"normal", "simple", "disabled"} {
				t.Run(strconv.FormatBool(binding)+"/"+strconv.FormatBool(image)+"/"+mode, func(t *testing.T) {
					s := &qqStatusStoreStub{binding: binding, image: image, disabled: mode == "disabled"}
					h := &QQImageHandler{store: s, cfg: &config.Config{RunMode: mode}}
					w := qqHTTPRequest(h, "GET", "/item/status", "", 42, "user", h.Status)
					require.Equal(t, 200, w.Code, w.Body.String())
					var response struct {
						Data map[string]any `json:"data"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
					enabled := mode == "normal"
					require.Equal(t, enabled, response.Data["enabled"])
					require.Equal(t, enabled && binding, response.Data["binding_enabled"])
					require.Equal(t, enabled && image, response.Data["image_enabled"])
				})
			}
		}
	}
}
func TestQQControlsStatusSettingsFailure(t *testing.T) {
	s := &qqStatusStoreStub{binding: true, image: true, controlErr: errors.New("settings unavailable")}
	h := &QQImageHandler{store: s}
	w := qqHTTPRequest(h, "GET", "/item/status", "", 42, "user", h.Status)
	require.Equal(t, 500, w.Code)
}
