package service

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// RunSignupTransaction lets an OAuth completion include the new user, identity,
// referral and pending-session consumption in the same database transaction.
// A caller-owned transaction is reused and is never committed here.
// Protected signup methods invoked inside apply return the user without tokens;
// the transaction owner must issue tokens only after its commit succeeds.
func (s *AuthService) RunSignupTransaction(ctx context.Context, apply func(context.Context) error) error {
	if dbent.TxFromContext(ctx) != nil {
		return apply(ctx)
	}
	if s == nil || s.entClient == nil {
		return ErrServiceUnavailable
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := apply(dbent.NewTxContext(ctx, tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func signupTokenDeferred(ctx context.Context) bool {
	_, protected := SignupAffiliateAttributionFromContext(ctx)
	return protected && dbent.TxFromContext(ctx) != nil
}

// afterSignupCommit keeps non-transactional bootstrap/cache effects out of a
// protected signup that may still fail at identity binding or pending consume.
func afterSignupCommit(ctx context.Context, apply func(context.Context)) {
	if signupTokenDeferred(ctx) {
		tx := dbent.TxFromContext(ctx)
		afterCtx := dbent.NewTxContext(ctx, nil)
		tx.OnCommit(func(next dbent.Committer) dbent.Committer {
			return dbent.CommitFunc(func(commitCtx context.Context, tx *dbent.Tx) error {
				if err := next.Commit(commitCtx, tx); err != nil {
					return err
				}
				apply(afterCtx)
				return nil
			})
		})
		return
	}
	apply(ctx)
}

func (s *AuthService) signupEntClient(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return s.entClient
}

func (s *AuthService) createAttributedSignupUser(ctx context.Context, user *User, invitation *RedeemCode, guardEmail bool) error {
	attr, protected := SignupAffiliateAttributionFromContext(ctx)
	if !protected || s == nil || s.affiliateService == nil {
		return ErrAffiliateAttributionInvalid
	}
	// Validate before any creation; Bind performs the authoritative locked check
	// again inside the transaction to cover revocation races.
	if err := s.affiliateService.ValidateSignupAffiliateAttribution(ctx, attr); err != nil {
		return err
	}
	return s.RunSignupTransaction(ctx, func(txCtx context.Context) error {
		var err error
		if guardEmail {
			err = s.createUserWithRegistrationEmailGuard(txCtx, user)
		} else {
			err = s.userRepo.Create(txCtx, user)
		}
		if err != nil {
			return err
		}
		if invitation != nil {
			if err := s.redeemRepo.Use(txCtx, invitation.ID, user.ID); err != nil {
				return ErrInvitationCodeInvalid
			}
		}
		return s.affiliateService.BindSignupAffiliateAttribution(txCtx, user.ID, attr)
	})
}

func (s *AuthService) bootstrapAttributedSignup(ctx context.Context, user *User, source string, plan signupGrantPlan, touchLogin bool) {
	afterSignupCommit(ctx, func(afterCtx context.Context) {
		s.postAuthUserBootstrap(afterCtx, user, source, touchLogin)
		s.assignSubscriptions(afterCtx, user.ID, plan.Subscriptions, "auto assigned by signup defaults")
		_ = s.snapshotPlatformQuotaDefaults(afterCtx, user.ID, &plan)
	})
}

func (s *AuthService) attributedSignupPromo(ctx context.Context, user *User, promoCode string) {
	afterSignupCommit(ctx, func(afterCtx context.Context) {
		if updated := s.applyOAuthSignupPromoCode(afterCtx, user, promoCode); updated != nil {
			*user = *updated
		}
	})
}

// completeOAuthUsername retains the legacy best-effort update for ordinary
// login. During a protected transaction, defer the repository's own write
// transaction and do not overwrite a profile choice made before commit.
func (s *AuthService) completeOAuthUsername(ctx context.Context, user *User, username, provider string) {
	user.Username = username
	deferred := signupTokenDeferred(ctx)
	afterSignupCommit(ctx, func(writeCtx context.Context) {
		target := user
		if deferred {
			current, err := s.userRepo.GetByID(writeCtx, user.ID)
			if err != nil {
				logger.LegacyPrintf("service.auth", "[Auth] Failed to inspect username after %s oauth login: %v", provider, err)
				return
			}
			if current.Username != "" {
				user.Username = current.Username
				return
			}
			current.Username = username
			target = current
		}
		if err := s.userRepo.Update(writeCtx, target, UserUpdateFields{Username: true}); err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to update username after %s oauth login: %v", provider, err)
			return
		}
		user.UpdatedAt = target.UpdatedAt
	})
}
