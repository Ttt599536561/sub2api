//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWelfareOperationByKeyRecoversOnlyOwnersCommittedOperation(t *testing.T) {
	s, id := welfareFixture(t)
	ctx := context.Background()
	_, err := s.CheckIn(ctx, id)
	require.NoError(t, err)
	quote, err := s.Quote(ctx, id, "all", "")
	require.NoError(t, err)
	result, err := s.Redeem(ctx, id, "lost-response", quote.Amount, quote.WelfareBalanceVersion)
	require.NoError(t, err)
	recovered, err := s.OperationByKey(ctx, id, "redeem", "lost-response")
	require.NoError(t, err)
	require.Equal(t, result.OperationID, recovered.OperationID)
	require.Equal(t, result.Amount, recovered.Amount)
	_, err = s.OperationByKey(ctx, id+1, "redeem", "lost-response")
	require.ErrorIs(t, err, service.ErrWelfareOperationNotFound)
	_, err = s.OperationByKey(ctx, id, "draw", "lost-response")
	require.ErrorIs(t, err, service.ErrWelfareOperationNotFound)
	_, err = s.OperationByKey(ctx, id, "redeem", "")
	require.ErrorIs(t, err, service.ErrWelfareInvalidRequest)
	_, err = s.OperationByKey(ctx, id, "unknown", "lost-response")
	require.ErrorIs(t, err, service.ErrWelfareInvalidRequest)
	var credits int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM welfare_ledger WHERE user_id=$1 AND type='redeem'`, id).Scan(&credits))
	require.Equal(t, 1, credits)
}
