package repository

import (
	"context"
	"crypto/sha256"
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
	"github.com/stretchr/testify/require"
)

// Suspend consent after its user read but before the repository transaction.
// This reproduces a status/role change between service validation and row lock
// without timing-dependent sleeps or fake database behavior.
type pausedMCPOAuthUserReader struct {
	service.UserRepository
	once    sync.Once
	read    chan struct{}
	proceed chan struct{}
}

func (r *pausedMCPOAuthUserReader) GetByID(ctx context.Context, id int64) (*service.User, error) {
	user, err := r.UserRepository.GetByID(ctx, id)
	if err == nil {
		r.once.Do(func() {
			close(r.read)
			select {
			case <-r.proceed:
			case <-ctx.Done():
			}
		})
	}
	return user, err
}

func TestMCPOAuthMigrationConsentRejectsConcurrentAccountChanges(t *testing.T) {
	for _, tc := range []struct {
		name          string
		admin         bool
		restoreBefore bool
		unchanged     bool
	}{
		{"disabled", false, false, false},
		{"demoted", true, false, false},
		{"disabled then restored", false, true, false},
		{"demoted then restored", true, true, false},
		{"unchanged user", false, false, true},
		{"unchanged admin", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			db := upstreamAC1B8F1TestDatabase(t, ctx)
			require.NoError(t, ApplyMigrations(ctx, db))
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			role, scope := service.RoleUser, service.MCPScopeUser
			if tc.admin {
				role, scope = service.RoleAdmin, service.MCPScopeAdmin
			}
			entity, err := client.User.Create().SetEmail("mcp-race@example.com").SetPasswordHash("unchanged-password-hash").SetRole(role).Save(ctx)
			require.NoError(t, err)
			users := NewUserRepository(client, db)
			paused := &pausedMCPOAuthUserReader{UserRepository: users, read: make(chan struct{}), proceed: make(chan struct{})}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(paused.proceed) }) }
			defer release()
			repo := NewMCPOAuthRepository(client, db)
			oauth := service.NewMCPOAuthService(repo, paused)
			const resource = "https://platform.example/api/v1/mcp"
			const redirectURI = "https://client.example/callback"
			registered, err := oauth.Register(ctx, service.MCPOAuthRegisterInput{RedirectURIs: []string{redirectURI}})
			require.NoError(t, err)
			verifier := strings.Repeat("v", 64)
			challenge := sha256.Sum256([]byte(verifier))
			authorization := service.MCPOAuthAuthorizeInput{ResponseType: "code", ClientID: registered.ClientID,
				RedirectURI: redirectURI, Scope: "mcp", Resource: resource,
				CodeChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]), CodeChallengeMethod: "S256"}
			type decision struct {
				redirect string
				err      error
			}
			done := make(chan decision, 1)
			go func() {
				redirect, err := oauth.Decide(ctx, entity.ID, service.MCPOAuthConsentInput{Authorization: authorization, Approved: true, Scope: scope}, resource)
				done <- decision{redirect, err}
			}()
			select {
			case <-paused.read:
			case <-ctx.Done():
				t.Fatal("consent did not reach its user read")
			}
			user, err := users.GetByID(ctx, entity.ID)
			require.NoError(t, err)
			fields := service.UserUpdateFields{Status: !tc.admin, Role: tc.admin}
			if !tc.unchanged {
				if tc.admin {
					user.Role = service.RoleUser
				} else {
					user.Status = service.StatusDisabled
				}
				require.NoError(t, users.Update(ctx, user, fields))
			}
			restore := func() {
				user.Role, user.Status = role, service.StatusActive
				require.NoError(t, users.Update(ctx, user, fields))
			}
			if tc.restoreBefore {
				restore()
			}
			release()
			result := <-done
			if tc.unchanged {
				require.NoError(t, result.err, "ordinary consent must preserve PostgreSQL timestamp precision")
			}
			if result.err == nil {
				// On the vulnerable implementation, prove the grant is actually
				// usable after restoration, rather than only checking a row exists.
				if !tc.restoreBefore && !tc.unchanged {
					restore()
				}
				callback, err := url.Parse(result.redirect)
				require.NoError(t, err)
				tokens, err := oauth.Token(ctx, service.MCPOAuthTokenInput{GrantType: "authorization_code", ClientID: registered.ClientID,
					Code: callback.Query().Get("code"), CodeVerifier: verifier, RedirectURI: redirectURI, Resource: resource}, resource)
				require.NoError(t, err)
				principal, err := oauth.Authenticate(ctx, tokens.AccessToken)
				require.NoError(t, err)
				require.Equal(t, scope, principal.Scope)
				if tc.unchanged {
					user.Username = "profile edited after consent"
					require.NoError(t, users.Update(ctx, user, service.UserUpdateFields{Username: true}))
					_, err = oauth.Authenticate(ctx, tokens.AccessToken)
					require.NoError(t, err, "profile changes must not invalidate already-issued grants")
					return
				}
				t.Fatal("stale consent minted a usable OAuth grant after account authority was revoked and restored")
			}
			require.ErrorIs(t, result.err, service.ErrMCPOAuthConsentChanged)
			grants, err := repo.ListGrants(ctx, entity.ID)
			require.NoError(t, err)
			require.Empty(t, grants, "reject stale consent before persisting its grant or code")
		})
	}
}
