package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type mcpReauditPauseUpdateKey struct{}

// User.Update reads the existing state before Save obtains a write lock. Pause
// exactly at that boundary, then restore authority and mint a valid grant.
// A subsequent disable/demotion must revoke that grant despite the old read.
func TestMCPOAuthReauditConcurrentRestorationCannotSkipRevocation(t *testing.T) {
	for _, admin := range []bool{false, true} {
		name := "disable"
		if admin {
			name = "demote"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			db := upstreamAC1B8F1TestDatabase(t, ctx)
			require.NoError(t, ApplyMigrations(ctx, db))
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			status := service.StatusDisabled
			if admin {
				status = service.StatusActive
			}
			entity, err := client.User.Create().SetEmail("mcp-reaudit@example.com").SetPasswordHash("stable-password").SetStatus(status).SetRole(service.RoleUser).Save(ctx)
			require.NoError(t, err)
			users := NewUserRepository(client, db)
			stale, err := users.GetByID(ctx, entity.ID)
			require.NoError(t, err)
			beforeSave, proceed := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(proceed) }) }
			defer release()
			client.User.Use(func(next dbent.Mutator) dbent.Mutator {
				return dbent.MutateFunc(func(hookCtx context.Context, m dbent.Mutation) (dbent.Value, error) {
					if hookCtx.Value(mcpReauditPauseUpdateKey{}) == true {
						close(beforeSave)
						select {
						case <-proceed:
						case <-hookCtx.Done():
							return nil, hookCtx.Err()
						}
					}
					return next.Mutate(hookCtx, m)
				})
			})
			fields := service.UserUpdateFields{Status: !admin, Role: admin}
			done := make(chan error, 1)
			go func() {
				done <- users.Update(context.WithValue(ctx, mcpReauditPauseUpdateKey{}, true), stale, fields)
			}()
			select {
			case <-beforeSave:
			case <-ctx.Done():
				t.Fatal("update did not reach Save after reading its old state")
			}
			restored, err := users.GetByID(ctx, entity.ID)
			require.NoError(t, err)
			restored.Status = service.StatusActive
			scope := service.MCPScopeUser
			if admin {
				restored.Role = service.RoleAdmin
				scope = service.MCPScopeAdmin
			}
			require.NoError(t, users.Update(ctx, restored, fields))
			repo := NewMCPOAuthRepository(client, db)
			oauth := service.NewMCPOAuthService(repo, users)
			const resource = "https://platform.example/api/v1/mcp"
			const callback = "https://client.example/callback"
			registered, err := oauth.Register(ctx, service.MCPOAuthRegisterInput{RedirectURIs: []string{callback}})
			require.NoError(t, err)
			verifier := strings.Repeat("v", 64)
			challenge := sha256.Sum256([]byte(verifier))
			redirect, err := oauth.Decide(ctx, entity.ID, service.MCPOAuthConsentInput{Approved: true, Scope: scope,
				Authorization: service.MCPOAuthAuthorizeInput{ResponseType: "code", ClientID: registered.ClientID,
					RedirectURI: callback, Scope: "mcp", Resource: resource,
					CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: "S256"}}, resource)
			require.NoError(t, err)
			parsed, err := url.Parse(redirect)
			require.NoError(t, err)
			tokens, err := oauth.Token(ctx, service.MCPOAuthTokenInput{GrantType: "authorization_code", ClientID: registered.ClientID,
				Code: parsed.Query().Get("code"), RedirectURI: callback, Resource: resource, CodeVerifier: verifier}, resource)
			require.NoError(t, err)
			release()
			require.NoError(t, <-done)
			_, err = oauth.Authenticate(ctx, tokens.AccessToken)
			require.ErrorIs(t, err, service.ErrMCPOAuthInvalidToken, "disabled or demoted state must immediately block the grant")
			require.NoError(t, users.Update(ctx, restored, fields))
			_, err = oauth.Authenticate(ctx, tokens.AccessToken)
			require.ErrorIs(t, err, service.ErrMCPOAuthInvalidToken, "restoring authority must not revive a grant created between the old state read and disabling/demoting write")
		})
	}
}

func newMCPReauditRevocationFixture(t *testing.T, ctx context.Context) (*sql.DB, *dbent.Client, *service.User) {
	t.Helper()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, ApplyMigrations(ctx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	entity, err := client.User.Create().SetEmail("mcp-revocation@example.com").SetPasswordHash("stable-password").SetRole(service.RoleAdmin).Save(ctx)
	require.NoError(t, err)
	user, err := NewUserRepository(client, db).GetByID(ctx, entity.ID)
	require.NoError(t, err)
	for _, scope := range []string{service.MCPScopeUser, service.MCPScopeAdmin} {
		_, err := db.ExecContext(ctx, `INSERT INTO mcp_oauth_grants (id,client_id,client_name,user_id,token_version,resource,scope,read_only,created_at,expires_at)
VALUES ($1,'client','Client',$2,1,'https://platform.example/api/v1/mcp',$1,true,NOW(),NOW()+INTERVAL '1 day')`, scope, user.ID)
		require.NoError(t, err)
	}
	return db, client, user
}

func TestMCPOAuthReauditRevocationPreservesScopesAndIdempotence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, client, user := newMCPReauditRevocationFixture(t, ctx)
	users, repo := NewUserRepository(client, db), NewMCPOAuthRepository(client, db)
	assertRevoked := func(scope string, want bool) {
		grant, err := repo.GetGrant(ctx, scope)
		require.NoError(t, err)
		require.Equal(t, want, grant.RevokedAt != nil, scope)
	}
	for range 2 {
		require.NoError(t, users.Update(ctx, user, service.UserUpdateFields{Status: true, Role: true}))
		assertRevoked(service.MCPScopeUser, false)
		assertRevoked(service.MCPScopeAdmin, false)
	}
	user.Role = service.RoleUser
	for range 2 {
		require.NoError(t, users.Update(ctx, user, service.UserUpdateFields{Role: true}))
		assertRevoked(service.MCPScopeUser, false)
		assertRevoked(service.MCPScopeAdmin, true)
	}
	user.Status = service.StatusDisabled
	for range 2 {
		require.NoError(t, users.Update(ctx, user, service.UserUpdateFields{Status: true}))
		assertRevoked(service.MCPScopeUser, true)
		assertRevoked(service.MCPScopeAdmin, true)
	}
	require.NoError(t, repo.RevokeUserGrants(ctx, user.ID+1000, time.Now()), "an unknown owner remains an idempotent no-op")
}

func TestMCPOAuthReauditRevocationRollsBackOwnerChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, client, user := newMCPReauditRevocationFixture(t, ctx)
	users, repo := NewUserRepository(client, db), NewMCPOAuthRepository(client, db)
	_, err := db.ExecContext(ctx, `CREATE FUNCTION reject_mcp_revoke() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced revoke failure'; END $$;
CREATE TRIGGER reject_mcp_revoke BEFORE UPDATE ON mcp_oauth_grants FOR EACH ROW EXECUTE FUNCTION reject_mcp_revoke()`)
	require.NoError(t, err)
	before := *user
	for _, action := range []func() error{
		func() error { return repo.RevokeUserGrants(ctx, user.ID, time.Now()) },
		func() error {
			user.Role = service.RoleUser
			return users.Update(ctx, user, service.UserUpdateFields{Role: true})
		},
		func() error {
			user.Status = service.StatusDisabled
			return users.Update(ctx, user, service.UserUpdateFields{Status: true})
		},
	} {
		require.ErrorContains(t, action(), "forced revoke failure")
		current, err := users.GetByID(ctx, user.ID)
		require.NoError(t, err)
		require.True(t, current.UpdatedAt.Equal(before.UpdatedAt), "failed revoke must roll back the consent fence")
		require.Equal(t, before.Status, current.Status)
		require.Equal(t, before.Role, current.Role)
		for _, scope := range []string{service.MCPScopeUser, service.MCPScopeAdmin} {
			grant, err := repo.GetGrant(ctx, scope)
			require.NoError(t, err)
			require.Nil(t, grant.RevokedAt)
		}
	}
}

func TestMCPOAuthReauditRevokeAllWaitsForConsentAndUsesFreshSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, client, user := newMCPReauditRevocationFixture(t, ctx)
	repo := NewMCPOAuthRepository(client, db)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, user.ID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO mcp_oauth_grants (id,client_id,client_name,user_id,token_version,resource,scope,read_only,created_at,expires_at)
VALUES ('racing','client','Client',$1,1,'https://platform.example/api/v1/mcp','user',true,NOW(),NOW()+INTERVAL '1 day')`, user.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- repo.RevokeUserGrants(ctx, user.ID, user.UpdatedAt.Add(-time.Hour)) }()
	require.Eventually(t, func() bool {
		var waiting int
		err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%UPDATE users SET updated_at=GREATEST%'`).Scan(&waiting)
		return err == nil && waiting > 0
	}, 10*time.Second, 10*time.Millisecond, "revoke must wait on the same owner lock as consent")
	require.NoError(t, tx.Commit())
	require.NoError(t, <-done)
	for _, id := range []string{service.MCPScopeUser, service.MCPScopeAdmin, "racing"} {
		grant, err := repo.GetGrant(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, grant.RevokedAt, "revocation must see the grant committed while waiting for the owner lock")
	}
	current, err := NewUserRepository(client, db).GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, current.UpdatedAt.After(user.UpdatedAt), "consent fence must advance even when the requested timestamp is older")
}

func TestMCPOAuthReauditRevokeAllUsesReadCommittedSnapshot(t *testing.T) {
	for _, isolation := range []string{"read committed", "repeatable read"} {
		t.Run(isolation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			db, client, user := newMCPReauditRevocationFixture(t, ctx)
			var database string
			require.NoError(t, db.QueryRowContext(ctx, "SELECT current_database()").Scan(&database))
			// Only this fixture's randomly named database is changed. Closing idle
			// connections ensures every subsequent transaction inherits its default.
			_, err := db.ExecContext(ctx, "ALTER DATABASE "+pq.QuoteIdentifier(database)+" SET default_transaction_isolation TO "+pq.QuoteLiteral(isolation))
			require.NoError(t, err)
			db.SetMaxIdleConns(0)
			var actual string
			require.NoError(t, db.QueryRowContext(ctx, "SHOW transaction_isolation").Scan(&actual))
			require.Equal(t, isolation, actual)

			repo := NewMCPOAuthRepository(client, db)
			oauth := service.NewMCPOAuthService(repo, NewUserRepository(client, db))
			const resource = "https://platform.example/api/v1/mcp"
			const callback = "https://client.example/callback"
			registered, err := oauth.Register(ctx, service.MCPOAuthRegisterInput{RedirectURIs: []string{callback}})
			require.NoError(t, err)
			verifier := strings.Repeat("v", 64)
			challenge := sha256.Sum256([]byte(verifier))

			// Pause the real consent transaction after it locks the owner and
			// inserts the grant, but before its code insert and commit complete.
			_, err = db.ExecContext(ctx, `CREATE FUNCTION pause_mcp_consent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_advisory_xact_lock(5005431); RETURN NEW; END $$;
CREATE TRIGGER pause_mcp_consent BEFORE INSERT ON mcp_oauth_codes FOR EACH ROW EXECUTE FUNCTION pause_mcp_consent()`)
			require.NoError(t, err)
			blocker, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback() }()
			_, err = blocker.ExecContext(ctx, "SELECT pg_advisory_xact_lock(5005431)")
			require.NoError(t, err)
			type decision struct {
				redirect string
				err      error
			}
			decided := make(chan decision, 1)
			go func() {
				redirect, err := oauth.Decide(ctx, user.ID, service.MCPOAuthConsentInput{Approved: true, Scope: service.MCPScopeUser,
					Authorization: service.MCPOAuthAuthorizeInput{ResponseType: "code", ClientID: registered.ClientID,
						RedirectURI: callback, Scope: "mcp", Resource: resource,
						CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: "S256"}}, resource)
				decided <- decision{redirect, err}
			}()
			require.Eventually(t, func() bool {
				var waiting int
				err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'INSERT INTO mcp_oauth_codes%'`).Scan(&waiting)
				return err == nil && waiting > 0
			}, 10*time.Second, 10*time.Millisecond, "consent must hold the owner lock before revocation starts")

			revoked := make(chan error, 1)
			go func() { revoked <- oauth.RevokeAllGrants(ctx, user.ID) }()
			require.Eventually(t, func() bool {
				var waiting int
				err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%UPDATE users SET updated_at=GREATEST%'`).Scan(&waiting)
				return err == nil && waiting > 0
			}, 10*time.Second, 10*time.Millisecond, "revocation must wait on the consent's owner lock")
			require.NoError(t, blocker.Rollback())
			result := <-decided
			require.NoError(t, result.err)
			require.NoError(t, <-revoked)

			parsed, err := url.Parse(result.redirect)
			require.NoError(t, err)
			tokens, err := oauth.Token(ctx, service.MCPOAuthTokenInput{GrantType: "authorization_code", ClientID: registered.ClientID,
				Code: parsed.Query().Get("code"), RedirectURI: callback, Resource: resource, CodeVerifier: verifier}, resource)
			var oauthErr *service.MCPOAuthError
			require.ErrorAs(t, err, &oauthErr, "a grant committed while revoke waited must not issue usable credentials")
			require.Equal(t, "invalid_grant", oauthErr.Code)
			require.Nil(t, tokens)
			grants, err := repo.ListGrants(ctx, user.ID)
			require.NoError(t, err)
			require.Len(t, grants, 3)
			for _, grant := range grants {
				require.NotNil(t, grant.RevokedAt, "revocation must include the newly committed authorization")
			}
		})
	}
}

// RevokeAllGrants is called by /auth/revoke-all-sessions. An approval that
// already read its user must not publish a new, usable family after revocation.
func TestMCPOAuthReauditRevokeAllRejectsInFlightConsent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db := upstreamAC1B8F1TestDatabase(t, ctx)
	require.NoError(t, ApplyMigrations(ctx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	entity, err := client.User.Create().SetEmail("mcp-revoke-in-flight@example.com").SetPasswordHash("stable-password").SetRole(service.RoleUser).Save(ctx)
	require.NoError(t, err)
	users := NewUserRepository(client, db)
	paused := &pausedMCPOAuthUserReader{UserRepository: users, read: make(chan struct{}), proceed: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(paused.proceed) }) }
	defer release()
	repo := NewMCPOAuthRepository(client, db)
	oauth := service.NewMCPOAuthService(repo, paused)
	const resource = "https://platform.example/api/v1/mcp"
	const callback = "https://client.example/callback"
	registered, err := oauth.Register(ctx, service.MCPOAuthRegisterInput{RedirectURIs: []string{callback}})
	require.NoError(t, err)
	verifier := strings.Repeat("v", 64)
	challenge := sha256.Sum256([]byte(verifier))
	type decision struct {
		redirect string
		err      error
	}
	done := make(chan decision, 1)
	go func() {
		redirect, err := oauth.Decide(ctx, entity.ID, service.MCPOAuthConsentInput{Approved: true, Scope: service.MCPScopeUser,
			Authorization: service.MCPOAuthAuthorizeInput{ResponseType: "code", ClientID: registered.ClientID,
				RedirectURI: callback, Scope: "mcp", Resource: resource,
				CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: "S256"}}, resource)
		done <- decision{redirect, err}
	}()
	select {
	case <-paused.read:
	case <-ctx.Done():
		t.Fatal("consent did not reach its account read")
	}
	require.NoError(t, oauth.RevokeAllGrants(ctx, entity.ID))
	release()
	result := <-done
	if result.err == nil {
		parsed, err := url.Parse(result.redirect)
		require.NoError(t, err)
		tokens, err := oauth.Token(ctx, service.MCPOAuthTokenInput{GrantType: "authorization_code", ClientID: registered.ClientID,
			Code: parsed.Query().Get("code"), RedirectURI: callback, Resource: resource, CodeVerifier: verifier}, resource)
		require.NoError(t, err)
		_, err = oauth.Authenticate(ctx, tokens.AccessToken)
		require.NoError(t, err)
		t.Fatal("in-flight consent minted a usable MCP grant after revoke-all-sessions completed")
	}
	require.ErrorIs(t, result.err, service.ErrMCPOAuthConsentChanged)
}
