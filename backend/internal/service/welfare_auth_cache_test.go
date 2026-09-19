//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

type welfareFailingAuthCache struct {
	authCacheStub
	deleteErr, publishErr error
}

func (c *welfareFailingAuthCache) DeleteAuthCache(ctx context.Context, key string) error {
	return c.deleteErr
}
func (c *welfareFailingAuthCache) PublishAuthCacheInvalidation(ctx context.Context, key string) error {
	return c.publishErr
}

func TestWelfareAuthInvalidationReportsEveryFailure(t *testing.T) {
	failure := errors.New("offline")
	for _, stage := range []string{"list", "delete", "publish", "ok"} {
		t.Run(stage, func(t *testing.T) {
			repo := &authRepoStub{listKeysByUserID: func(context.Context, int64) ([]string, error) {
				if stage == "list" {
					return nil, failure
				}
				return []string{"key"}, nil
			}}
			cache := &welfareFailingAuthCache{}
			if stage == "delete" {
				cache.deleteErr = failure
			}
			if stage == "publish" {
				cache.publishErr = failure
			}
			svc := &APIKeyService{apiKeyRepo: repo, cache: cache}
			err := svc.InvalidateAuthCacheByUserIDReliable(context.Background(), 7)
			if stage == "ok" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, failure)
			}
		})
	}
}

type welfareAuthUserRepo struct {
	UserRepository
	balance float64
	err     error
}

func (r *welfareAuthUserRepo) GetByID(context.Context, int64) (*User, error) {
	return &User{Balance: r.balance}, r.err
}
func TestWelfareAuthRefreshSeesCreditAndPropagatesFailure(t *testing.T) {
	repo := &welfareAuthUserRepo{balance: 20}
	svc := &APIKeyService{userRepo: repo}
	balance, err := svc.RefreshUserBalanceForAuth(context.Background(), &User{ID: 7, Balance: 0})
	require.NoError(t, err)
	require.Equal(t, 20.0, balance)
	repo.err = errors.New("database unavailable")
	_, err = svc.RefreshUserBalanceForAuth(context.Background(), &User{ID: 7, Balance: 0})
	require.ErrorIs(t, err, repo.err)
}
