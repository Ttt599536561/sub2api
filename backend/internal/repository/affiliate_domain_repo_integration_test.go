//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

var affiliateDomainUserSequence atomic.Uint64

func affiliateDomainFixtureUser(t *testing.T, client *dbent.Client) *service.User {
	t.Helper()
	return mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("domain-%d-%d@example.com", time.Now().UnixNano(), affiliateDomainUserSequence.Add(1)),
	})
}

func affiliateDomainFixture(t *testing.T) (context.Context, *dbent.Tx, *affiliateRepository, *service.User, *service.User) {
	t.Helper()
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)
	client := tx.Client()
	_, err := client.ExecContext(ctx, `INSERT INTO settings (key,value,updated_at) VALUES ('affiliate_enabled','true',NOW())
ON CONFLICT (key) DO UPDATE SET value='true',updated_at=NOW()`)
	require.NoError(t, err)
	inviter := affiliateDomainFixtureUser(t, client)
	invitee := affiliateDomainFixtureUser(t, client)
	return ctx, tx, &affiliateRepository{client: integrationEntClient}, inviter, invitee
}

func affiliateDomainAttribution(domain *service.AffiliatePromotionDomain) *service.SignupAffiliateAttribution {
	now := time.Now().UTC()
	return &service.SignupAffiliateAttribution{
		InviterID: domain.InviterID, SourceHost: domain.Hostname, HostRevision: domain.Revision,
		CapturedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
}

func TestAffiliateDomainRepository_LifecycleAndDomainOnlyList(t *testing.T) {
	ctx, _, repo, inviter, _ := affiliateDomainFixture(t)
	domain, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, "partner.api.example.com")
	require.NoError(t, err)
	require.Equal(t, int64(1), domain.Revision)
	require.True(t, domain.Enabled)
	same, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, domain.Hostname)
	require.NoError(t, err)
	require.Equal(t, domain, same)
	entries, total, err := repo.ListUsersWithCustomSettings(ctx, service.AffiliateAdminFilter{Search: inviter.Email})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, entries, 1)
	require.Equal(t, domain.Hostname, entries[0].AffiliateDomain)
	require.Equal(t, "https://partner.api.example.com/", entries[0].ReferralURL)

	attr := affiliateDomainAttribution(domain)
	require.NoError(t, repo.ValidateSignupAffiliateAttribution(ctx, attr))
	next, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, "second.api.example.com")
	require.NoError(t, err)
	require.Equal(t, int64(1), next.Revision)
	old, err := repo.ResolveAffiliateDomain(ctx, domain.Hostname)
	require.NoError(t, err)
	require.False(t, old.Enabled)
	require.Equal(t, int64(2), old.Revision)
	require.Error(t, repo.ValidateSignupAffiliateAttribution(ctx, attr))
	reused, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, domain.Hostname)
	require.NoError(t, err)
	require.Equal(t, int64(3), reused.Revision)
	require.Error(t, repo.ValidateSignupAffiliateAttribution(ctx, attr))
	cleared, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, "")
	require.NoError(t, err)
	require.Nil(t, cleared)
	old, err = repo.ResolveAffiliateDomain(ctx, domain.Hostname)
	require.NoError(t, err)
	require.False(t, old.Enabled)
	require.Equal(t, int64(4), old.Revision)
	entries, total, err = repo.ListUsersWithCustomSettings(ctx, service.AffiliateAdminFilter{Search: inviter.Email})
	require.NoError(t, err)
	require.Empty(t, entries)
	require.Zero(t, total)
}

func TestAffiliateDomainRepository_UniqueHostAndReassignmentInvalidatePriorSource(t *testing.T) {
	ctx, _, repo, inviter, other := affiliateDomainFixture(t)
	domain, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, "partner.api.example.com")
	require.NoError(t, err)
	otherDomain, err := repo.SetUserAffiliateDomain(ctx, other.ID, "other.api.example.com")
	require.NoError(t, err)
	_, err = repo.SetUserAffiliateDomain(ctx, other.ID, domain.Hostname)
	require.ErrorIs(t, err, service.ErrAffiliateDomainTaken)
	stillAssigned, err := repo.GetUserAffiliateDomain(ctx, other.ID)
	require.NoError(t, err)
	require.Equal(t, otherDomain, stillAssigned)
	attr := affiliateDomainAttribution(domain)
	_, err = repo.SetUserAffiliateDomain(ctx, inviter.ID, "")
	require.NoError(t, err)
	reassigned, err := repo.SetUserAffiliateDomain(ctx, other.ID, domain.Hostname)
	require.NoError(t, err)
	require.Equal(t, other.ID, reassigned.InviterID)
	require.Equal(t, int64(3), reassigned.Revision)
	require.Error(t, repo.ValidateSignupAffiliateAttribution(ctx, attr))
}

func TestAffiliateDomainRepository_BindingIsIdempotentAndConflictsAreRejected(t *testing.T) {
	ctx, _, repo, inviter, invitee := affiliateDomainFixture(t)
	domain, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, "partner.api.example.com")
	require.NoError(t, err)
	attr := affiliateDomainAttribution(domain)
	require.NoError(t, repo.BindSignupAffiliateAttribution(ctx, invitee.ID, attr))
	require.NoError(t, repo.BindSignupAffiliateAttribution(ctx, invitee.ID, attr))
	require.Equal(t, 1, querySingleInt(t, ctx, dbent.TxFromContext(ctx).Client(),
		"SELECT aff_count FROM user_affiliates WHERE user_id=$1", inviter.ID))
	other := affiliateDomainFixtureUser(t, dbent.TxFromContext(ctx).Client())
	otherDomain, err := repo.SetUserAffiliateDomain(ctx, other.ID, "other.api.example.com")
	require.NoError(t, err)
	require.ErrorIs(t, repo.BindSignupAffiliateAttribution(ctx, invitee.ID, affiliateDomainAttribution(otherDomain)), service.ErrAffiliateAlreadyBound)
	require.Equal(t, int(inviter.ID), querySingleInt(t, ctx, dbent.TxFromContext(ctx).Client(),
		"SELECT inviter_id FROM user_affiliates WHERE user_id=$1", invitee.ID))
	require.Error(t, repo.BindSignupAffiliateAttribution(ctx, inviter.ID, attr))
	require.Error(t, repo.BindSignupAffiliateAttribution(context.Background(), invitee.ID, attr))
}

func TestAffiliateDomainRepository_RejectsDisabledDeletedAndExpiredSources(t *testing.T) {
	for _, kind := range []string{"feature disabled", "user disabled", "user soft deleted", "host disabled", "expired", "wrong revision", "wrong owner"} {
		t.Run(kind, func(t *testing.T) {
			ctx, tx, repo, inviter, invitee := affiliateDomainFixture(t)
			domain, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, "partner.api.example.com")
			require.NoError(t, err)
			attr := affiliateDomainAttribution(domain)
			switch kind {
			case "feature disabled":
				_, err = tx.Client().ExecContext(ctx, "UPDATE settings SET value='false' WHERE key='affiliate_enabled'")
			case "user disabled":
				_, err = tx.Client().ExecContext(ctx, "UPDATE users SET status='disabled' WHERE id=$1", inviter.ID)
			case "user soft deleted":
				_, err = tx.Client().ExecContext(ctx, "UPDATE users SET deleted_at=NOW() WHERE id=$1", inviter.ID)
			case "host disabled":
				_, err = repo.SetUserAffiliateDomain(ctx, inviter.ID, "")
			case "expired":
				attr.ExpiresAt = time.Now().Add(-time.Second)
			case "wrong revision":
				attr.HostRevision++
			case "wrong owner":
				attr.InviterID = invitee.ID
			}
			require.NoError(t, err)
			require.Error(t, repo.ValidateSignupAffiliateAttribution(ctx, attr))
			require.Error(t, repo.BindSignupAffiliateAttribution(ctx, invitee.ID, attr))
			require.Zero(t, querySingleInt(t, ctx, tx.Client(), "SELECT count(*) FROM user_affiliates WHERE user_id=$1 AND inviter_id IS NOT NULL", invitee.ID))
		})
	}
}

func TestAffiliateDomainRepository_HardDeleteKeepsTombstone(t *testing.T) {
	ctx, tx, repo, inviter, _ := affiliateDomainFixture(t)
	domain, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, "partner.api.example.com")
	require.NoError(t, err)
	_, err = tx.Client().ExecContext(ctx, "DELETE FROM users WHERE id=$1", inviter.ID)
	require.NoError(t, err)
	tombstone, err := repo.ResolveAffiliateDomain(ctx, domain.Hostname)
	require.NoError(t, err)
	require.NotNil(t, tombstone)
	require.Zero(t, tombstone.InviterID)
	require.False(t, tombstone.Enabled)
	require.Greater(t, tombstone.Revision, domain.Revision)
	require.Error(t, repo.ValidateSignupAffiliateAttribution(ctx, affiliateDomainAttribution(domain)))
}

func TestAffiliateDomainRepository_SoftDeleteRevokesDomainAndAllowsReassignment(t *testing.T) {
	ctx, tx, repo, inviter, other := affiliateDomainFixture(t)
	domain, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, "deleted.api.example.com")
	require.NoError(t, err)
	_, err = tx.Client().ExecContext(ctx, "UPDATE users SET deleted_at=NOW() WHERE id=$1", inviter.ID)
	require.NoError(t, err)
	tombstone, err := repo.ResolveAffiliateDomain(ctx, domain.Hostname)
	require.NoError(t, err)
	require.False(t, tombstone.Enabled)
	require.Greater(t, tombstone.Revision, domain.Revision)
	reassigned, err := repo.SetUserAffiliateDomain(ctx, other.ID, domain.Hostname)
	require.NoError(t, err)
	require.Equal(t, other.ID, reassigned.InviterID)
	require.Greater(t, reassigned.Revision, tombstone.Revision)
	require.Error(t, repo.ValidateSignupAffiliateAttribution(ctx, affiliateDomainAttribution(domain)))
}

func TestAffiliateDomainRepository_OuterRollbackRemovesUserAndInviteCount(t *testing.T) {
	ctx, tx, repo, inviter, invitee := affiliateDomainFixture(t)
	domain, err := repo.SetUserAffiliateDomain(ctx, inviter.ID, "rollback.api.example.com")
	require.NoError(t, err)
	require.NoError(t, repo.BindSignupAffiliateAttribution(ctx, invitee.ID, affiliateDomainAttribution(domain)))
	require.NoError(t, tx.Rollback())
	require.Zero(t, querySingleInt(t, context.Background(), integrationEntClient, "SELECT count(*) FROM users WHERE id IN ($1,$2)", inviter.ID, invitee.ID))
	require.Zero(t, querySingleInt(t, context.Background(), integrationEntClient, "SELECT count(*) FROM user_affiliates WHERE user_id IN ($1,$2)", inviter.ID, invitee.ID))
}

func TestAffiliateDomainRepository_BindLocksOutConcurrentRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var priorSetting string
	priorErr := integrationDB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='affiliate_enabled'").Scan(&priorSetting)
	require.True(t, priorErr == nil || errors.Is(priorErr, sql.ErrNoRows))
	setup, err := integrationEntClient.Tx(ctx)
	require.NoError(t, err)
	setupCtx := dbent.NewTxContext(ctx, setup)
	inviter := affiliateDomainFixtureUser(t, setup.Client())
	invitee := affiliateDomainFixtureUser(t, setup.Client())
	host := fmt.Sprintf("race-%d.api.example.com", inviter.ID)
	_, err = setup.Client().ExecContext(setupCtx, `INSERT INTO settings(key,value,updated_at) VALUES('affiliate_enabled','true',NOW())
ON CONFLICT (key) DO UPDATE SET value='true'`)
	require.NoError(t, err)
	repo := &affiliateRepository{client: integrationEntClient}
	domain, err := repo.SetUserAffiliateDomain(setupCtx, inviter.ID, host)
	require.NoError(t, err)
	require.NoError(t, setup.Commit())
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM affiliate_promotion_hosts WHERE hostname=$1", host)
		_, _ = integrationDB.Exec("DELETE FROM users WHERE id IN ($1,$2)", inviter.ID, invitee.ID)
		if errors.Is(priorErr, sql.ErrNoRows) {
			_, _ = integrationDB.Exec("DELETE FROM settings WHERE key='affiliate_enabled'")
		} else {
			_, _ = integrationDB.Exec("UPDATE settings SET value=$1 WHERE key='affiliate_enabled'", priorSetting)
		}
	})
	bindTx, err := integrationEntClient.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = bindTx.Rollback() }()
	require.NoError(t, repo.BindSignupAffiliateAttribution(dbent.NewTxContext(ctx, bindTx), invitee.ID, affiliateDomainAttribution(domain)))
	revokeTx, err := integrationEntClient.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = revokeTx.Rollback() }()
	revokeCtx := dbent.NewTxContext(ctx, revokeTx)
	revokePID := querySingleInt(t, revokeCtx, revokeTx.Client(), "SELECT pg_backend_pid()")
	done := make(chan error, 1)
	go func() {
		_, err := repo.SetUserAffiliateDomain(revokeCtx, inviter.ID, "")
		done <- err
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := integrationDB.QueryRowContext(ctx, "SELECT cardinality(pg_blocking_pids($1))>0", revokePID).Scan(&blocked)
		return err == nil && blocked
	}, 2*time.Second, 10*time.Millisecond, "revocation must wait for the binding transaction")
	require.NoError(t, bindTx.Rollback())
	require.NoError(t, <-done)
	require.NoError(t, revokeTx.Commit())
	require.Error(t, repo.ValidateSignupAffiliateAttribution(ctx, affiliateDomainAttribution(domain)))
}

// This crosses the real AuthService -> UserRepository -> affiliate repository
// boundary and lets the registration service commit its own PostgreSQL tx.
func TestAffiliateDomainRepository_AuthRegistrationCommitsTrustedSource(t *testing.T) {
	testAffiliateAuthRegistration(t, false)
}

func TestAffiliateDomainRepository_AuthRegistrationSingleConnection(t *testing.T) {
	testAffiliateAuthRegistration(t, true)
}

func testAffiliateAuthRegistration(t *testing.T, singleConnection bool) {
	t.Helper()
	ctx := context.Background()
	priorSettings := map[string]*string{}
	for _, key := range []string{service.SettingKeyAffiliateEnabled, service.SettingKeyRegistrationEnabled} {
		var value string
		err := integrationDB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=$1", key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			priorSettings[key] = nil
		} else {
			require.NoError(t, err)
			priorSettings[key] = &value
		}
	}
	setup, err := integrationEntClient.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = setup.Rollback() }()
	setupCtx := dbent.NewTxContext(ctx, setup)
	inviter := affiliateDomainFixtureUser(t, setup.Client())
	other := affiliateDomainFixtureUser(t, setup.Client())
	repo := &affiliateRepository{client: integrationEntClient}
	host := fmt.Sprintf("auth-%d.api.example.com", inviter.ID)
	domain, err := repo.SetUserAffiliateDomain(setupCtx, inviter.ID, host)
	require.NoError(t, err)
	otherSummary, err := repo.EnsureUserAffiliate(setupCtx, other.ID)
	require.NoError(t, err)
	for key := range priorSettings {
		_, err := setup.Client().ExecContext(setupCtx, `INSERT INTO settings(key,value,updated_at) VALUES($1,'true',NOW())
ON CONFLICT(key) DO UPDATE SET value='true',updated_at=NOW()`, key)
		require.NoError(t, err)
	}
	require.NoError(t, setup.Commit())
	email := fmt.Sprintf("trusted-auth-%d@example.com", inviter.ID)
	rejectedEmail := fmt.Sprintf("revoked-auth-%d@example.com", inviter.ID)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM affiliate_promotion_hosts WHERE hostname=$1", host)
		_, _ = integrationDB.Exec("DELETE FROM users WHERE id IN ($1,$2) OR email IN ($3,$4)", inviter.ID, other.ID, email, rejectedEmail)
		for key, value := range priorSettings {
			if value == nil {
				_, _ = integrationDB.Exec("DELETE FROM settings WHERE key=$1", key)
			} else {
				_, _ = integrationDB.Exec("UPDATE settings SET value=$2 WHERE key=$1", key, *value)
			}
		}
	})
	if singleConnection {
		previousMax := integrationDB.Stats().MaxOpenConnections
		integrationDB.SetMaxOpenConns(1)
		t.Cleanup(func() { integrationDB.SetMaxOpenConns(previousMax) })
	}
	cfg := &config.Config{
		JWT:     config.JWTConfig{Secret: "affiliate-domain-integration-test-signing-key", ExpireHour: 1},
		Default: config.DefaultConfig{UserConcurrency: 2},
	}
	cfg.Server.FrontendURL = "https://api.example.com"
	settings := service.NewSettingService(NewSettingRepository(integrationEntClient), cfg)
	affiliates := service.NewAffiliateService(repo, settings, nil, nil)
	auth := service.NewAuthService(integrationEntClient, NewUserRepository(integrationEntClient, integrationDB),
		nil, nil, cfg, settings, nil, nil, nil, nil, nil, affiliates, nil)
	attr := affiliateDomainAttribution(domain)
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	signupCtx := service.WithSignupAffiliateAttribution(requestCtx, attr)
	token, registered, err := auth.RegisterWithVerification(signupCtx, email, "StrongPass2026!", "", "", "", otherSummary.AffCode)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.NotNil(t, registered)
	require.Equal(t, int(inviter.ID), querySingleInt(t, ctx, integrationEntClient,
		"SELECT inviter_id FROM user_affiliates WHERE user_id=$1", registered.ID))
	require.Equal(t, 1, querySingleInt(t, ctx, integrationEntClient,
		"SELECT aff_count FROM user_affiliates WHERE user_id=$1", inviter.ID))
	require.Zero(t, querySingleInt(t, ctx, integrationEntClient,
		"SELECT aff_count FROM user_affiliates WHERE user_id=$1", other.ID))
	_, err = affiliates.SetUserAffiliateDomain(ctx, inviter.ID, "")
	require.NoError(t, err)
	token, rejected, err := auth.RegisterWithVerification(signupCtx, rejectedEmail, "StrongPass2026!", "", "", "", otherSummary.AffCode)
	require.Error(t, err)
	require.Empty(t, token)
	require.Nil(t, rejected)
	require.Zero(t, querySingleInt(t, ctx, integrationEntClient, "SELECT count(*) FROM users WHERE email=$1", rejectedEmail))
	require.Equal(t, 1, querySingleInt(t, ctx, integrationEntClient,
		"SELECT aff_count FROM user_affiliates WHERE user_id=$1", inviter.ID))
}
