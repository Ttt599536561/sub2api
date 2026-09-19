//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestWelfarePublicAvailabilityMirrorsInjection(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{}, &config.Config{})
	settings, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.WelfareEnabled)
	// Availability is separate from accepting new rewards: a launched but
	// paused program must keep the existing wallet accessible.
	svc.SetWelfareAvailabilityProvider(func(context.Context) (bool, error) { return true, nil })
	settings, err = svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.WelfareEnabled)
	injected, err := svc.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	require.True(t, injected.(*PublicSettingsInjectionPayload).WelfareEnabled)
}

func TestWelfareAvailabilityFailureDoesNotBreakPublicSettings(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{}, &config.Config{})
	svc.SetWelfareAvailabilityProvider(func(context.Context) (bool, error) { return false, errors.New("unavailable") })
	settings, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.WelfareEnabled)
}

func TestWelfarePublicSettingsAndSSRExposeOnlyAvailability(t *testing.T) {
	// Freeze the existing public fields as an allowlist. Welfare adds only its
	// availability flag; even an unprefixed internal field must fail this check.
	// The service view uses some Go field names while SSR uses snake_case, so
	// normalize that naming difference without deriving the list from the DTO.
	approved := make(map[string]struct{})
	for _, field := range strings.Fields(`
		welfare_enabled registration_enabled email_verify_enabled force_email_on_third_party_signup
		registration_email_suffix_whitelist registration_email_domain_quota_enabled promo_code_enabled
		password_reset_enabled invitation_code_enabled totp_enabled passkey_enabled login_agreement_enabled
		login_agreement_mode login_agreement_updated_at login_agreement_revision login_agreement_documents
		turnstile_enabled turnstile_site_key tencent_captcha_enabled tencent_captcha_app_id tencent_captcha_region
		aliyun_captcha_enabled aliyun_captcha_scene_id aliyun_captcha_prefix aliyun_captcha_region
		site_name site_logo site_subtitle api_base_url contact_info doc_url home_content compact_home_enabled
		hide_ccs_import_button purchase_subscription_enabled purchase_subscription_url table_default_page_size
		table_page_size_options custom_menu_items custom_endpoints linuxdo_oauth_enabled dingtalk_oauth_enabled
		wechat_oauth_enabled wechat_oauth_open_enabled wechat_oauth_mp_enabled wechat_oauth_mobile_enabled
		backend_mode_enabled payment_enabled payment_balance_disabled oidc_oauth_enabled oidc_oauth_provider_name
		github_oauth_enabled google_oauth_enabled version server_timezone server_utc_offset balance_low_notify_enabled
		account_quota_notify_enabled balance_low_notify_threshold balance_low_notify_recharge_url channel_monitor_enabled
		channel_monitor_mode channel_monitor_default_interval_seconds channel_monitor_hide_throughput channel_monitor_show_quota
		channel_monitor_hide_user_ranking grok_default_text_model grok_cross_client_model_map_enabled grok_default_base_url_mode
		available_channels_enabled subscription_enabled model_plaza_enabled model_plaza_require_auth plugin_management_enabled
		affiliate_enabled risk_control_enabled allow_user_view_error_requests
	`) {
		approved[strings.ReplaceAll(field, "_", "")] = struct{}{}
	}
	for _, tc := range []struct {
		name      string
		available bool
	}{{"unavailable", false}, {"available", true}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{
				"welfare_rules_version":  "2",
				"welfare_daily_rewards":  `{"min":"0.01","max":"1.00","probabilities":[20,30,50,100]}`,
				"welfare_streak_rewards": `{"7":"0.60","15":"2.00","30":"5.00"}`,
				"welfare_private_audit":  `{"branch_roll":19,"amount_roll":99,"band_roll":49,"low_before":3}`,
			}}, &config.Config{})
			svc.SetWelfareAvailabilityProvider(func(context.Context) (bool, error) { return tc.available, nil })
			public, err := svc.GetPublicSettings(context.Background())
			require.NoError(t, err)
			injected, err := svc.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			for name, payload := range map[string]any{"public_settings": public, "ssr": injected} {
				t.Run(name, func(t *testing.T) {
					object := welfarePublicContractObject(t, payload)
					require.Equal(t, tc.available, object["welfare_enabled"])
					for field := range object {
						normalized := strings.ToLower(strings.ReplaceAll(field, "_", ""))
						require.Contains(t, approved, normalized, "unexpected public field %q", field)
					}
				})
			}
		})
	}
}
