//go:build integration || unit

package repository

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type lockedReductionReadRepo struct {
	service.UserSubscriptionRepository
	afterRead func(*service.UserSubscription)
	lockReads atomic.Int32
	codes     *reductionUseRepo
}

// Use completes its real SQL update (including the users FK KEY SHARE) before
// this hook runs. The service's user-row lock is taken only after Use returns.
type reductionUseRepo struct {
	service.RedeemCodeRepository
	afterUse func(context.Context) error
}

func (r *reductionUseRepo) Use(ctx context.Context, id, userID int64) error {
	if err := r.RedeemCodeRepository.Use(ctx, id, userID); err != nil {
		return err
	}
	if r.afterUse != nil {
		return r.afterUse(ctx)
	}
	return nil
}

func (r *lockedReductionReadRepo) GetByUserIDAndGroupID(ctx context.Context, u, g int64) (*service.UserSubscription, error) {
	sub, err := r.UserSubscriptionRepository.GetByUserIDAndGroupID(ctx, u, g)
	if err == nil && r.afterRead != nil {
		r.afterRead(sub)
	}
	return sub, err
}
func (r *lockedReductionReadRepo) GetByIDForUpdate(ctx context.Context, id int64) (*service.UserSubscription, error) {
	r.lockReads.Add(1)
	return r.UserSubscriptionRepository.GetByIDForUpdate(ctx, id)
}
func newReductionLockFixture(t *testing.T, name string) (*service.RedeemService, *lockedReductionReadRepo, *service.UserSubscription, int64, []service.RedeemCode) {
	t.Helper()
	ctx := context.Background()
	client, db := reductionLockFixtureDatabase(t)
	user, err := client.User.Create().SetEmail(name + "@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName(name).Save(ctx)
	require.NoError(t, err)
	// Redemption commits its own transaction; remove fixtures explicitly so
	// repository list/count tests cannot observe this test's subscriptions.
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(), `DELETE FROM redeem_codes WHERE group_id=$1`, group.ID)
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), `DELETE FROM user_subscriptions WHERE user_id=$1`, user.ID)
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), `DELETE FROM groups WHERE id=$1`, group.ID)
		require.NoError(t, err)
		_, err = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
		require.NoError(t, err)
	})
	now := time.Now().UTC().Truncate(time.Microsecond)
	sub, err := client.UserSubscription.Create().SetUserID(user.ID).SetGroupID(group.ID).SetStartsAt(now.Add(-time.Hour)).SetExpiresAt(now.AddDate(0, 0, 10)).SetStatus(service.SubscriptionStatusActive).SetNotes("initial").Save(ctx)
	require.NoError(t, err)
	repo := &lockedReductionReadRepo{UserSubscriptionRepository: NewUserSubscriptionRepository(client)}
	codes := &reductionUseRepo{RedeemCodeRepository: NewRedeemCodeRepository(client)}
	repo.codes = codes
	values := []service.RedeemCode{
		{Code: name + "-first", Type: service.RedeemTypeSubscription, Status: service.StatusUnused, ValidityDays: -1, GroupID: &group.ID},
		{Code: name + "-second", Type: service.RedeemTypeSubscription, Status: service.StatusUnused, ValidityDays: -1, GroupID: &group.ID},
	}
	for i := range values {
		require.NoError(t, codes.Create(ctx, &values[i]))
		require.Positive(t, values[i].ID)
	}
	subscriptions := service.NewSubscriptionService(nil, repo, nil, client, nil)
	redeem := service.NewRedeemService(codes, NewUserRepository(client, db), subscriptions, nil, nil, client, nil, nil)
	return redeem, repo, userSubscriptionEntityToService(sub), user.ID, values
}
func TestRedeemReductionPreservesRenewal(t *testing.T) {
	redeem, repo, sub, userID, codes := newReductionLockFixture(t, "reduction-renewal")
	renewed := sub.ExpiresAt.AddDate(0, 0, 10)
	repo.afterRead = func(stale *service.UserSubscription) {
		// Commit renewal on another connection after the deduction's initial read.
		require.NoError(t, repo.ExtendExpiry(context.Background(), stale.ID, renewed))
		require.NoError(t, repo.UpdateNotes(context.Background(), stale.ID, "renewal"))
	}
	result, err := redeem.RedeemForAdminFulfillment(context.Background(), userID, codes[0].Code)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, codes[0].ID, result.ID)
	got, err := repo.GetByID(context.Background(), sub.ID)
	require.NoError(t, err)
	require.True(t, renewed.AddDate(0, 0, -1).Equal(got.ExpiresAt))
	require.Contains(t, got.Notes, "renewal")
	require.Contains(t, got.Notes, fmt.Sprintf("通过兑换码 #%d 退款扣减 1 天", codes[0].ID))
	require.NotContains(t, got.Notes, codes[0].Code)
	assertReductionCodeUsed(t, repo.codes, codes[0], userID)
	require.EqualValues(t, 1, repo.lockReads.Load())
}
func TestRedeemConcurrentReductionsBothApply(t *testing.T) {
	redeem, repo, sub, userID, codes := newReductionLockFixture(t, "reduction-concurrent")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	repo.codes.afterUse = func(ctx context.Context) error {
		select {
		case ready <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	type redemptionResult struct {
		code *service.RedeemCode
		err  error
	}
	results := make(chan redemptionResult, 2)
	for _, code := range codes {
		go func(code service.RedeemCode) {
			result, err := redeem.RedeemForAdminFulfillment(ctx, userID, code.Code)
			results <- redemptionResult{code: result, err: err}
		}(code)
	}
	for range 2 {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("both deductions did not finish Use before the user-lock barrier")
		}
	}
	close(release)
	returnedIDs := make([]int64, 0, 2)
	for range 2 {
		select {
		case result := <-results:
			require.NoError(t, result.err)
			require.NotNil(t, result.code)
			returnedIDs = append(returnedIDs, result.code.ID)
		case <-ctx.Done():
			t.Fatal("deductions did not finish")
		}
	}
	require.ElementsMatch(t, []int64{codes[0].ID, codes[1].ID}, returnedIDs)
	got, err := repo.GetByID(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, sub.ExpiresAt.AddDate(0, 0, -2).Equal(got.ExpiresAt))
	for _, code := range codes {
		require.Contains(t, got.Notes, fmt.Sprintf("通过兑换码 #%d 退款扣减 1 天", code.ID))
		require.NotContains(t, got.Notes, code.Code)
		assertReductionCodeUsed(t, repo.codes, code, userID)
	}
	require.EqualValues(t, 2, repo.lockReads.Load())
}

func assertReductionCodeUsed(t *testing.T, repo service.RedeemCodeRepository, original service.RedeemCode, userID int64) {
	t.Helper()
	stored, err := repo.GetByCode(context.Background(), original.Code)
	require.NoError(t, err)
	require.Equal(t, original.ID, stored.ID)
	require.Equal(t, service.StatusUsed, stored.Status)
	require.NotNil(t, stored.UsedBy)
	require.Equal(t, userID, *stored.UsedBy)
	require.NotNil(t, stored.UsedAt)
}
