//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/stretchr/testify/require"
)

type attributedSignupUserRepo struct {
	UserRepository
	client *dbent.Client
}

func (r *attributedSignupUserRepo) db(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return r.client
}
func (r *attributedSignupUserRepo) Create(ctx context.Context, u *User) error {
	entity, err := r.db(ctx).User.Create().SetEmail(u.Email).SetPasswordHash(u.PasswordHash).
		SetUsername(u.Username).SetRole(u.Role).SetStatus(u.Status).SetBalance(u.Balance).
		SetConcurrency(u.Concurrency).SetSignupSource(u.SignupSource).Save(ctx)
	if err != nil {
		return err
	}
	u.ID = entity.ID
	return nil
}
func (r *attributedSignupUserRepo) CreateWithEmailAliasGuard(ctx context.Context, u *User) error {
	return r.Create(ctx, u)
}
func (r *attributedSignupUserRepo) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	return r.db(ctx).User.Query().Where(user.EmailEQ(email)).Exist(ctx)
}
func (r *attributedSignupUserRepo) ExistsByEmailAlias(ctx context.Context, email string) (bool, error) {
	return r.ExistsByEmail(ctx, email)
}
func (r *attributedSignupUserRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	entity, err := r.db(ctx).User.Query().Where(user.EmailEQ(email)).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &User{ID: entity.ID, Email: entity.Email, Username: entity.Username, Role: entity.Role, Status: entity.Status}, nil
}
func (r *attributedSignupUserRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	entity, err := r.db(ctx).User.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &User{ID: entity.ID, Email: entity.Email, Username: entity.Username, Role: entity.Role, Status: entity.Status}, nil
}
func (r *attributedSignupUserRepo) Update(ctx context.Context, u *User, _ UserUpdateFields) error {
	return r.db(ctx).User.UpdateOneID(u.ID).SetUsername(u.Username).Exec(ctx)
}
func (r *attributedSignupUserRepo) Delete(ctx context.Context, id int64) error {
	return r.db(ctx).User.DeleteOneID(id).Exec(ctx)
}

type attributedSignupFailingTokenCache struct{ refreshTokenCacheStub }

func (*attributedSignupFailingTokenCache) StoreRefreshToken(context.Context, string, *RefreshTokenData, time.Duration) error {
	return errors.New("injected token cache failure")
}

type attributedSignupDomainRepo struct {
	AffiliateRepository
	failBind bool
}

func (*attributedSignupDomainRepo) ResolveAffiliateDomain(context.Context, string) (*AffiliatePromotionDomain, error) {
	return nil, nil
}
func (*attributedSignupDomainRepo) GetUserAffiliateDomain(context.Context, int64) (*AffiliatePromotionDomain, error) {
	return nil, nil
}
func (*attributedSignupDomainRepo) SetUserAffiliateDomain(context.Context, int64, string) (*AffiliatePromotionDomain, error) {
	return nil, nil
}
func (*attributedSignupDomainRepo) ValidateSignupAffiliateAttribution(context.Context, *SignupAffiliateAttribution) error {
	return nil
}
func (r *attributedSignupDomainRepo) BindSignupAffiliateAttribution(ctx context.Context, userID int64, attr *SignupAffiliateAttribution) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return errors.New("attribution binding must share the user transaction")
	}
	if _, err := tx.Client().ExecContext(ctx, "INSERT INTO test_signup_bindings (user_id, inviter_id) VALUES (?, ?)", userID, attr.InviterID); err != nil {
		return err
	}
	if r.failBind {
		return errors.New("injected attribution failure after write")
	}
	return nil
}

func newAttributedSignupService(t *testing.T, failBind bool) (*AuthService, *dbent.Client, context.Context) {
	t.Helper()
	_, client := newAuthPendingIdentityServiceTestClient(t)
	_, err := client.ExecContext(context.Background(), "CREATE TABLE test_signup_bindings (user_id INTEGER PRIMARY KEY, inviter_id INTEGER NOT NULL)")
	require.NoError(t, err)
	settings := map[string]string{SettingKeyRegistrationEnabled: "true", SettingKeyAffiliateEnabled: "true", SettingKeyRegistrationEmailSuffixWhitelist: "[]"}
	svc := newAuthService(&userRepoStub{}, settings, nil, nil)
	svc.entClient = client
	svc.userRepo = &attributedSignupUserRepo{client: client}
	svc.refreshTokenCache = &refreshTokenCacheStub{}
	svc.affiliateService = NewAffiliateService(&attributedSignupDomainRepo{failBind: failBind}, svc.settingService, nil, nil)
	now := time.Now().UTC()
	ctx := WithSignupAffiliateAttribution(context.Background(), &SignupAffiliateAttribution{
		InviterID: 7, SourceHost: "partner.example.test", HostRevision: 1, CapturedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	return svc, client, ctx
}

func TestTrustedSignupBindingFailureRollsBackCreatedUser(t *testing.T) {
	for _, mode := range []string{"email", "legacy_oauth", "oauth_pair", "verified_email", "email_auto"} {
		t.Run(mode, func(t *testing.T) {
			svc, client, ctx := newAttributedSignupService(t, true)
			var err error
			switch mode {
			case "email":
				_, _, err = svc.RegisterWithVerification(ctx, "new@example.com", "password", "", "", "", "OTHER")
			case "legacy_oauth":
				_, _, err = svc.LoginOrRegisterOAuth(ctx, "new@example.com", "new")
			case "oauth_pair":
				_, _, err = svc.LoginOrRegisterOAuthWithTokenPair(ctx, "new@example.com", "new", "", "OTHER", "linuxdo")
			case "verified_email":
				_, _, err = svc.RegisterVerifiedOAuthEmailAccount(ctx, "new@example.com", "password", "", "github")
			case "email_auto":
				_, err = svc.createEmailOAuthUser(ctx, "new@example.com", "new", "github", "", "OTHER")
			}
			require.Error(t, err)
			count, countErr := client.User.Query().Count(context.Background())
			require.NoError(t, countErr)
			require.Zero(t, count, "failed binding must not leave a usable unbound account")
			rows, queryErr := client.QueryContext(context.Background(), "SELECT COUNT(*) FROM test_signup_bindings")
			require.NoError(t, queryErr)
			defer rows.Close()
			require.True(t, rows.Next())
			var bindings int
			require.NoError(t, rows.Scan(&bindings))
			require.Zero(t, bindings)
		})
	}
}

func TestTrustedEmailOAuthIdentityFailureRollsBackUserAndAttribution(t *testing.T) {
	svc, client, ctx := newAttributedSignupService(t, false)
	client.AuthIdentity.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(context.Context, dbent.Mutation) (dbent.Value, error) {
			return nil, errors.New("injected identity failure")
		})
	})
	_, _, err := svc.LoginOrRegisterVerifiedEmailOAuthWithSignupCodes(ctx, EmailOAuthIdentityInput{
		ProviderType: "github", ProviderKey: "github", ProviderSubject: "subject", Email: "identity@example.com", EmailVerified: true,
	}, "", "OTHER", "")
	require.ErrorContains(t, err, "injected identity failure")
	count, err := client.User.Query().Count(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
	rows, err := client.QueryContext(context.Background(), "SELECT COUNT(*) FROM test_signup_bindings")
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var bindings int
	require.NoError(t, rows.Scan(&bindings))
	require.Zero(t, bindings)
}

func TestTrustedCommittedSignupTokenFailureKeepsAttributedAccount(t *testing.T) {
	svc, client, ctx := newAttributedSignupService(t, false)
	svc.refreshTokenCache = &attributedSignupFailingTokenCache{}
	_, _, err := svc.RegisterVerifiedOAuthEmailAccount(ctx, "committed@example.com", "password", "", "github")
	require.ErrorContains(t, err, "injected token cache failure")
	users, err := client.User.Query().Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, users, "token issuance after commit must not delete a user while leaving committed referral counters")
}

func TestTrustedOAuthUsernameUpdateWaitsForCommit(t *testing.T) {
	for _, flow := range []string{"legacy", "token_pair", "github", "google"} {
		for _, rollback := range []bool{false, true} {
			name := flow + "/commit"
			if rollback {
				name = flow + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				svc, client, ctx := newAttributedSignupService(t, false)
				existing, err := client.User.Create().SetEmail("existing-name@example.com").SetPasswordHash("hash").
					SetRole(RoleUser).SetStatus(StatusActive).SetUsername("").Save(context.Background())
				require.NoError(t, err)
				var duringTransaction string
				rollbackErr := errors.New("rollback after login")
				err = svc.RunSignupTransaction(ctx, func(txCtx context.Context) error {
					switch flow {
					case "legacy":
						_, _, err = svc.LoginOrRegisterOAuth(txCtx, existing.Email, "Filled User")
					case "token_pair":
						_, _, err = svc.LoginOrRegisterOAuthWithTokenPair(txCtx, existing.Email, "Filled User", "", "", "linuxdo")
					default:
						_, _, err = svc.LoginOrRegisterVerifiedEmailOAuthWithSignupCodes(txCtx, EmailOAuthIdentityInput{
							ProviderType: flow, ProviderKey: flow, ProviderSubject: "existing-name",
							Email: existing.Email, EmailVerified: true, Username: "Filled User",
						}, "", "", "")
					}
					if err != nil {
						return err
					}
					current, err := dbent.TxFromContext(txCtx).Client().User.Get(txCtx, existing.ID)
					if err != nil {
						return err
					}
					duringTransaction = current.Username
					if rollback {
						return rollbackErr
					}
					return nil
				})
				if rollback {
					require.ErrorIs(t, err, rollbackErr)
				} else {
					require.NoError(t, err)
				}
				require.Empty(t, duringTransaction, "best-effort username completion must not start a second repository transaction inside signup")
				stored, err := client.User.Get(context.Background(), existing.ID)
				require.NoError(t, err)
				if rollback {
					require.Empty(t, stored.Username)
				} else {
					require.Equal(t, "Filled User", stored.Username)
				}
			})
		}
	}
}

func TestTrustedOAuthUsernameCompletionKeepsAdoptedProfile(t *testing.T) {
	svc, client, ctx := newAttributedSignupService(t, false)
	existing, err := client.User.Create().SetEmail("adopted-name@example.com").SetPasswordHash("hash").
		SetRole(RoleUser).SetStatus(StatusActive).SetUsername("").Save(context.Background())
	require.NoError(t, err)
	require.NoError(t, svc.RunSignupTransaction(ctx, func(txCtx context.Context) error {
		if _, _, err := svc.LoginOrRegisterOAuthWithTokenPair(txCtx, existing.Email, "Suggested User", "", "", "linuxdo"); err != nil {
			return err
		}
		// The handler applies the user's profile choice after service login and
		// before the same transaction commits.
		return dbent.TxFromContext(txCtx).Client().User.UpdateOneID(existing.ID).SetUsername("Chosen Display Name").Exec(txCtx)
	}))
	stored, err := client.User.Get(context.Background(), existing.ID)
	require.NoError(t, err)
	require.Equal(t, "Chosen Display Name", stored.Username)
}
