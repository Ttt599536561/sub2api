//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQQControlsSettingsPreserveOmittedAndRoundTrip(t *testing.T) {
	for _, binding := range []bool{false, true} {
		for _, image := range []bool{false, true} {
			t.Run(strconv.FormatBool(binding)+"/"+strconv.FormatBool(image), func(t *testing.T) {
				h, repo := newStepUpSwitchTestHandler(t, map[string]string{"qq_binding_enabled": strconv.FormatBool(binding), "qq_image_enabled": strconv.FormatBool(image)})
				for _, body := range []map[string]any{{"site_name": "test"}, {"qq_binding_enabled": binding, "qq_image_enabled": image}} {
					rec := doUpdateSettings(t, h, body, nil)
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
					require.Equal(t, strconv.FormatBool(binding), repo.values["qq_binding_enabled"])
					require.Equal(t, strconv.FormatBool(image), repo.values["qq_image_enabled"])
					var response struct {
						Data map[string]any `json:"data"`
					}
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
					require.Equal(t, binding, response.Data["qq_binding_enabled"])
					require.Equal(t, image, response.Data["qq_image_enabled"])
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
				h.GetSettings(c)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var response struct {
					Data map[string]any `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
				require.Equal(t, binding, response.Data["qq_binding_enabled"])
				require.Equal(t, image, response.Data["qq_image_enabled"])
			})
		}
	}
}

func TestQQControlsAuditOnlyChangedFields(t *testing.T) {
	before := &service.SystemSettings{QQBindingEnabled: true, QQImageEnabled: true}
	after := &service.SystemSettings{QQBindingEnabled: false, QQImageEnabled: true}
	require.Equal(t, []string{"qq_binding_enabled"}, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}))
	after.QQImageEnabled = false
	require.Equal(t, []string{"qq_binding_enabled", "qq_image_enabled"}, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}))
	require.Empty(t, diffSettings(after, after, nil, nil, UpdateSettingsRequest{}))
}

func TestQQControlsLegacyPUTDoesNotWriteOmittedSwitches(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	for _, body := range []map[string]any{{"site_name": "test"}, {"qq_binding_enabled": nil, "qq_image_enabled": nil}} {
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NotContains(t, repo.values, "qq_binding_enabled", "omitted field must not overwrite a concurrent close")
		require.NotContains(t, repo.values, "qq_image_enabled", "omitted field must not overwrite a concurrent close")
	}
}

func TestQQControlsSettingsExplicitChangesAreWritable(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{"qq_binding_enabled": "true", "qq_image_enabled": "true"})
	for _, value := range []bool{false, true} {
		rec := doUpdateSettings(t, h, map[string]any{"qq_binding_enabled": value, "qq_image_enabled": value}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, strconv.FormatBool(value), repo.values["qq_binding_enabled"])
		require.Equal(t, strconv.FormatBool(value), repo.values["qq_image_enabled"])
	}
}
