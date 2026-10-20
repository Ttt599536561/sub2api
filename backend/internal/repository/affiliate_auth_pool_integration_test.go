//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAffiliateAuthReadMethodsReuseOuterTransaction(t *testing.T) {
	for _, method := range []string{"user_id", "user_id_deleted", "user_email", "user_exists", "allowed_groups", "redeem_id", "redeem_code"} {
		t.Run(method, func(t *testing.T) {
			previousMax := integrationDB.Stats().MaxOpenConnections
			integrationDB.SetMaxOpenConns(1)
			t.Cleanup(func() { integrationDB.SetMaxOpenConns(previousMax) })
			tx := testEntTx(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			txCtx := dbent.NewTxContext(ctx, tx)
			u := affiliateDomainFixtureUser(t, tx.Client())
			group := mustCreateGroup(t, tx.Client(), &service.Group{Name: fmt.Sprintf("aff-pool-%d", u.ID)})
			_, err := tx.Client().ExecContext(txCtx, "INSERT INTO user_allowed_groups(user_id,group_id) VALUES($1,$2)", u.ID, group.ID)
			require.NoError(t, err)
			code, err := tx.Client().RedeemCode.Create().SetCode(fmt.Sprintf("POOL-%d", u.ID)).
				SetType(service.RedeemTypeInvitation).SetStatus(service.StatusUnused).SetValue(0).Save(txCtx)
			require.NoError(t, err)
			users := NewUserRepository(integrationEntClient, integrationDB).(*userRepository)
			redeems := NewRedeemCodeRepository(integrationEntClient)
			switch method {
			case "user_id", "user_id_deleted", "user_email":
				var loaded *service.User
				switch method {
				case "user_id":
					loaded, err = users.GetByID(txCtx, u.ID)
				case "user_id_deleted":
					loaded, err = users.GetByIDIncludeDeleted(txCtx, u.ID)
				case "user_email":
					loaded, err = users.GetByEmail(txCtx, u.Email)
				}
				require.NoError(t, err, "read must use the leased transaction connection")
				require.Equal(t, u.ID, loaded.ID)
				require.Equal(t, []int64{group.ID}, loaded.AllowedGroups)
			case "user_exists":
				var exists bool
				exists, err = users.ExistsByEmail(txCtx, u.Email)
				require.NoError(t, err)
				require.True(t, exists, "read must see the transaction's uncommitted user")
			case "allowed_groups":
				var groups map[int64][]int64
				groups, err = users.loadAllowedGroups(txCtx, []int64{u.ID})
				require.NoError(t, err)
				require.Equal(t, []int64{group.ID}, groups[u.ID])
			case "redeem_id", "redeem_code":
				var loaded *service.RedeemCode
				if method == "redeem_id" {
					loaded, err = redeems.GetByID(txCtx, code.ID)
				} else {
					loaded, err = redeems.GetByCode(txCtx, code.Code)
				}
				require.NoError(t, err)
				require.Equal(t, code.ID, loaded.ID)
			}
		})
	}
}

func TestAffiliateAuthOAuthFlowsSingleConnection(t *testing.T) {
	for _, flow := range []string{"new_token_pair", "new_github", "new_google", "new_verified_pending", "new_email_pending", "existing_token_pair", "existing_github", "existing_google"} {
		t.Run(flow, func(t *testing.T) {
			baseCtx := context.Background()
			settingsToSet := map[string]string{
				service.SettingKeyAffiliateEnabled: "true", service.SettingKeyRegistrationEnabled: "true",
				service.SettingKeyInvitationCodeEnabled: "true", service.SettingKeyRegistrationEmailSuffixWhitelist: "[]",
			}
			priorSettings := map[string]*string{}
			for key := range settingsToSet {
				var value string
				err := integrationDB.QueryRowContext(baseCtx, "SELECT value FROM settings WHERE key=$1", key).Scan(&value)
				if errors.Is(err, sql.ErrNoRows) {
					priorSettings[key] = nil
				} else {
					require.NoError(t, err)
					priorSettings[key] = &value
				}
			}
			setup, err := integrationEntClient.Tx(baseCtx)
			require.NoError(t, err)
			defer func() { _ = setup.Rollback() }()
			setupCtx := dbent.NewTxContext(baseCtx, setup)
			inviter := affiliateDomainFixtureUser(t, setup.Client())
			email := fmt.Sprintf("pool-oauth-%d@example.com", inviter.ID)
			var existingID int64
			if strings.HasPrefix(flow, "existing_") {
				existing := mustCreateUser(t, setup.Client(), &service.User{Email: email, Username: ""})
				existingID = existing.ID
			}
			affiliateRepo := &affiliateRepository{client: integrationEntClient}
			host := fmt.Sprintf("pool-%d.api.example.com", inviter.ID)
			domain, err := affiliateRepo.SetUserAffiliateDomain(setupCtx, inviter.ID, host)
			require.NoError(t, err)
			_, err = affiliateRepo.EnsureUserAffiliate(setupCtx, inviter.ID)
			require.NoError(t, err)
			invitation, err := setup.Client().RedeemCode.Create().SetCode(fmt.Sprintf("POOL-OAUTH-%d", inviter.ID)).
				SetType(service.RedeemTypeInvitation).SetStatus(service.StatusUnused).SetValue(0).Save(setupCtx)
			require.NoError(t, err)
			for key, value := range settingsToSet {
				_, err := setup.Client().ExecContext(setupCtx, `INSERT INTO settings(key,value,updated_at) VALUES($1,$2,NOW())
ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()`, key, value)
				require.NoError(t, err)
			}
			require.NoError(t, setup.Commit())
			t.Cleanup(func() {
				_, _ = integrationDB.Exec("DELETE FROM affiliate_promotion_hosts WHERE hostname=$1", host)
				_, _ = integrationDB.Exec("DELETE FROM redeem_codes WHERE id=$1", invitation.ID)
				_, _ = integrationDB.Exec("DELETE FROM users WHERE id=$1 OR email=$2", inviter.ID, email)
				for key, value := range priorSettings {
					if value == nil {
						_, _ = integrationDB.Exec("DELETE FROM settings WHERE key=$1", key)
					} else {
						_, _ = integrationDB.Exec("UPDATE settings SET value=$2 WHERE key=$1", key, *value)
					}
				}
			})
			previousMax := integrationDB.Stats().MaxOpenConnections
			integrationDB.SetMaxOpenConns(1)
			t.Cleanup(func() { integrationDB.SetMaxOpenConns(previousMax) })
			cfg := &config.Config{
				JWT: config.JWTConfig{Secret: "affiliate-pool-integration-signing-key", ExpireHour: 1,
					AccessTokenExpireMinutes: 60, RefreshTokenExpireDays: 1},
				Default: config.DefaultConfig{UserConcurrency: 2},
			}
			cfg.Server.FrontendURL = "https://api.example.com"
			settingsRepo := NewSettingRepository(integrationEntClient)
			settings := service.NewSettingService(settingsRepo, cfg)
			rdb := testRedis(t)
			emailCache := NewEmailCache(rdb)
			require.NoError(t, emailCache.SetVerificationCode(baseCtx, email, &service.VerificationCodeData{
				Code: "246810", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
			}, time.Hour))
			auth := service.NewAuthService(integrationEntClient, NewUserRepository(integrationEntClient, integrationDB),
				NewRedeemCodeRepository(integrationEntClient), NewRefreshTokenCache(rdb), cfg, settings,
				service.NewEmailService(settingsRepo, emailCache), nil, nil, nil, nil,
				service.NewAffiliateService(affiliateRepo, settings, nil, nil), nil)
			requestCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
			defer cancel()
			signupCtx := service.WithSignupAffiliateAttribution(requestCtx, affiliateDomainAttribution(domain))
			var pair *service.TokenPair
			var result *service.User
			provider := "github"
			if strings.Contains(flow, "google") {
				provider = "google"
			}
			signup := func(callCtx context.Context) error {
				switch {
				case strings.Contains(flow, "token_pair"):
					pair, result, err = auth.LoginOrRegisterOAuthWithTokenPair(callCtx, email, "Filled User", invitation.Code, "IGNORED", "linuxdo")
				case flow == "new_verified_pending":
					pair, result, err = auth.RegisterVerifiedOAuthEmailAccount(callCtx, email, "StrongPass2026!", invitation.Code, "github")
				case flow == "new_email_pending":
					pair, result, err = auth.RegisterOAuthEmailAccount(callCtx, email, "StrongPass2026!", "246810", invitation.Code, "oidc")
				default:
					pair, result, err = auth.LoginOrRegisterVerifiedEmailOAuthWithSignupCodes(callCtx, service.EmailOAuthIdentityInput{
						ProviderType: provider, ProviderKey: provider, ProviderSubject: fmt.Sprintf("pool-%d", inviter.ID),
						Email: email, EmailVerified: true, Username: "Filled User",
					}, invitation.Code, "IGNORED", "")
				}
				if err != nil {
					return err
				}
				if strings.HasSuffix(flow, "_pending") {
					return auth.FinalizeOAuthEmailAccount(callCtx, result, invitation.Code, result.SignupSource, "")
				}
				return nil
			}
			if flow == "new_github" || flow == "new_google" {
				err = signup(signupCtx) // Exercise the automatic flow's own transaction.
			} else {
				err = auth.RunSignupTransaction(signupCtx, signup)
				if err == nil {
					pair, err = auth.GenerateTokenPair(signupCtx, result, "")
				}
			}
			require.NoError(t, err, "OAuth must complete without renting a second SQL connection")
			require.NotNil(t, pair)
			require.NotEmpty(t, pair.AccessToken)
			require.NotNil(t, result)
			stored, err := integrationEntClient.User.Get(baseCtx, result.ID)
			require.NoError(t, err)
			storedInvitation, err := integrationEntClient.RedeemCode.Get(baseCtx, invitation.ID)
			require.NoError(t, err)
			if existingID != 0 {
				require.Equal(t, existingID, result.ID)
				require.Equal(t, "Filled User", stored.Username)
				require.Equal(t, service.StatusUnused, storedInvitation.Status)
				require.Zero(t, querySingleInt(t, baseCtx, integrationEntClient, "SELECT aff_count FROM user_affiliates WHERE user_id=$1", inviter.ID))
				require.Zero(t, querySingleInt(t, baseCtx, integrationEntClient, "SELECT count(*) FROM user_affiliates WHERE user_id=$1 AND inviter_id IS NOT NULL", result.ID))
			} else {
				require.Equal(t, service.StatusUsed, storedInvitation.Status)
				require.Equal(t, int(inviter.ID), querySingleInt(t, baseCtx, integrationEntClient, "SELECT inviter_id FROM user_affiliates WHERE user_id=$1", result.ID))
				require.Equal(t, 1, querySingleInt(t, baseCtx, integrationEntClient, "SELECT aff_count FROM user_affiliates WHERE user_id=$1", inviter.ID))
			}
		})
	}
}
