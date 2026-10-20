package handler

import (
	"context"
	"encoding/json"
	"net/http"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const signupAffiliatePendingStateKey = "signup_affiliate_attribution"

func (h *AuthHandler) completeOAuthProviderRegistration(
	ctx context.Context,
	session *dbent.PendingAuthSession,
	decision *dbent.IdentityAdoptionDecision,
	email, username, invitationCode, affiliateCode, provider string,
) (*service.TokenPair, *service.User, error) {
	ctx = pendingSignupAttributionContext(ctx, session)
	create := func(createCtx context.Context) (*service.TokenPair, *service.User, error) {
		return h.authService.LoginOrRegisterOAuthWithTokenPairAndPromoCode(createCtx, email, username,
			invitationCode, affiliateCode, pendingOAuthPromoCode(session), provider)
	}
	if _, protected := service.SignupAffiliateAttributionFromContext(ctx); protected {
		return h.completeTrustedOAuthSignup(ctx, session, decision, create)
	}
	pair, user, err := create(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := applyPendingOAuthAdoptionAndConsumeSession(ctx, h.entClient(), h.authService, h.userService, session, decision, user.ID); err != nil {
		if code := infraerrors.Code(err); code < http.StatusBadRequest || code >= http.StatusInternalServerError {
			err = infraerrors.InternalServer("PENDING_AUTH_BIND_APPLY_FAILED", "failed to bind pending oauth identity").WithCause(err)
		}
		return nil, nil, err
	}
	return pair, user, nil
}

func pendingSignupAttributionContext(ctx context.Context, session *dbent.PendingAuthSession) context.Context {
	if session == nil {
		return ctx
	}
	raw, exists := session.LocalFlowState[signupAffiliatePendingStateKey]
	if !exists {
		return ctx
	}
	// A malformed protected snapshot remains protected and must fail validation
	// at creation. It must never silently fall back to a new cookie or aff_code.
	var attr *service.SignupAffiliateAttribution
	encoded, err := json.Marshal(raw)
	if err != nil {
		attr = &service.SignupAffiliateAttribution{}
	} else if err := json.Unmarshal(encoded, &attr); err != nil {
		attr = &service.SignupAffiliateAttribution{}
	}
	// JSON null (including an interface holding a typed nil pointer) explicitly
	// clears a later cookie; an empty/invalid object stays a protected failure.
	return service.WithSignupAffiliateAttribution(ctx, attr)
}

// completeTrustedOAuthSignup is shared by provider auto signup (synthetic
// session ID zero) and pending completion. Tokens are issued only after the
// user, admission code, identity, referral and pending consume have committed.
func (h *AuthHandler) completeTrustedOAuthSignup(
	ctx context.Context,
	session *dbent.PendingAuthSession,
	decision *dbent.IdentityAdoptionDecision,
	create func(context.Context) (*service.TokenPair, *service.User, error),
) (*service.TokenPair, *service.User, error) {
	ctx = pendingSignupAttributionContext(ctx, session)
	var user *service.User
	err := h.authService.RunSignupTransaction(ctx, func(txCtx context.Context) error {
		tx := dbent.TxFromContext(txCtx)
		if session != nil && session.ID > 0 {
			// Claim the pending row before creating a user. A later failure rolls
			// this update back, and concurrent completions cannot both proceed.
			if err := consumePendingOAuthBrowserSessionTx(txCtx, tx, session); err != nil {
				return err
			}
		}
		var err error
		_, user, err = create(txCtx)
		if err != nil {
			return err
		}
		if user == nil {
			return service.ErrServiceUnavailable
		}
		if err := applyPendingOAuthBinding(txCtx, h.entClient(), h.authService, h.userService, session, decision, &user.ID, true, false); err != nil {
			return err
		}
		if session != nil && session.ID > 0 && pendingOAuthCreateAccountPreCommitHook != nil {
			return pendingOAuthCreateAccountPreCommitHook(txCtx, session)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	pair, err := h.authService.GenerateTokenPair(ctx, user, "")
	if err != nil {
		return nil, nil, err
	}
	return pair, user, nil
}
