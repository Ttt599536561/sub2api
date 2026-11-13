//go:build unit

package service

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestRedeemCancelledCodeReturnsSpecificReasonBeforeTransaction(t *testing.T) {
	repo := &redeemRejectRepo{code: RedeemCode{ID: 1, Code: "CANCELLED-REWARD", Type: RedeemTypeSubscription, Status: "cancelled"}}
	svc := NewRedeemService(repo, nil, nil, nil, nil, nil, nil, nil)
	got, err := svc.Redeem(context.Background(), 2, repo.code.Code)
	require.Nil(t, got)
	require.Equal(t, "REDEEM_CODE_CANCELLED", infraerrors.Reason(err))
	require.False(t, repo.useCalled)
}

type concurrentlyCancelledRedeemRepo struct {
	*redeemRejectRepo
	readInTransaction bool
}

func (r *concurrentlyCancelledRedeemRepo) Use(context.Context, int64, int64) error {
	r.code.Status = "cancelled"
	return ErrRedeemCodeUsed
}

func (r *concurrentlyCancelledRedeemRepo) GetByID(ctx context.Context, id int64) (*RedeemCode, error) {
	r.readInTransaction = dbent.TxFromContext(ctx) != nil
	return r.redeemRejectRepo.GetByID(ctx, id)
}

func TestRedeemConcurrentCancellationReturnsSpecificReasonAfterUseConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	mock.ExpectBegin()
	mock.ExpectRollback()
	groupID := int64(13)
	repo := &concurrentlyCancelledRedeemRepo{redeemRejectRepo: &redeemRejectRepo{code: RedeemCode{ID: 1,
		Code: "RACING-CANCELLED-REWARD", Type: RedeemTypeSubscription, Status: StatusUnused, GroupID: &groupID, ValidityDays: 30}}}
	svc := NewRedeemService(repo, &mockUserRepo{getByIDUser: &User{ID: 11}}, nil, nil, nil, client, nil, nil)
	got, err := svc.Redeem(context.Background(), 11, repo.code.Code)
	require.Nil(t, got)
	require.Equal(t, "REDEEM_CODE_CANCELLED", infraerrors.Reason(err))
	require.True(t, repo.readInTransaction, "conflict status must be read through the redemption transaction")
	require.NoError(t, mock.ExpectationsWereMet())
}
