//go:build unit

package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

type reservedPaymentLookupRepo struct {
	RedeemCodeRepository
	lookups int
}

func (r *reservedPaymentLookupRepo) GetByCode(context.Context, string) (*RedeemCode, error) {
	r.lookups++
	return nil, ErrRedeemCodeNotFound
}

func TestGenericRedeemRejectsReservedPaymentCodeBeforeLookup(t *testing.T) {
	for _, entry := range []string{"public", "admin"} {
		for _, code := range []string{"PAY-REVIEW-ONLY", " PAY-REVIEW-ONLY "} {
			t.Run(entry+code, func(t *testing.T) {
				repo := &reservedPaymentLookupRepo{}
				svc := NewRedeemService(repo, nil, nil, nil, nil, nil, nil, nil)
				call := svc.Redeem
				if entry == "admin" {
					call = svc.RedeemForAdminFulfillment
				}
				result, err := call(context.Background(), 42, code)
				require.Nil(t, result)
				require.Equal(t, "PAYMENT_REDEEM_CODE_RESERVED", infraerrors.Reason(err))
				require.Zero(t, repo.lookups)
			})
		}
	}
}

func TestReservedPaymentCodeStillReachesTrustedFulfillmentValidation(t *testing.T) {
	repo := &redeemRejectRepo{code: RedeemCode{ID: 1, Code: "PAY-REVIEW-EXPIRED", Type: RedeemTypeBalance, Value: 10, Status: StatusExpired}}
	cache := &paymentFulfillmentRedeemCacheStub{count: redeemMaxFailedAttempts}
	svc := &RedeemService{redeemRepo: repo, cache: cache}
	result, err := svc.redeemForPaymentFulfillment(context.Background(), 42, repo.code.Code)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrRedeemCodeExpired)
	require.Zero(t, cache.getCalls)
	require.False(t, repo.useCalled)
}
