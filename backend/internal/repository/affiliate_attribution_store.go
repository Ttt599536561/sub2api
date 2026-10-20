package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

var (
	errAffiliateAttributionStoreUnavailable = errors.New("affiliate attribution storage unavailable")
	errAffiliateAttributionCounterTTL       = errors.New("affiliate attribution counter TTL must be positive")
	affiliateAttributionCountScript         = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)
)

type affiliateAttributionStore struct {
	client *redis.Client
}

// NewAffiliateAttributionStore keeps Redis behind the service-owned contract.
// A missing client remains an explicit storage error on every operation.
func NewAffiliateAttributionStore(client *redis.Client) service.AffiliateAttributionStore {
	return &affiliateAttributionStore{client: client}
}

func affiliateAttributionStoreError(err error) error {
	if errors.Is(err, redis.Nil) {
		return service.ErrAffiliateAttributionMissing
	}
	return err
}

func (s *affiliateAttributionStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return errAffiliateAttributionStoreUnavailable
	}
	return affiliateAttributionStoreError(s.client.Set(ctx, key, value, ttl).Err())
}

func (s *affiliateAttributionStore) SetIfAbsent(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if s == nil || s.client == nil {
		return false, errAffiliateAttributionStoreUnavailable
	}
	stored, err := s.client.SetNX(ctx, key, value, ttl).Result()
	return stored, affiliateAttributionStoreError(err)
}

func (s *affiliateAttributionStore) Get(ctx context.Context, key string) (string, error) {
	if s == nil || s.client == nil {
		return "", errAffiliateAttributionStoreUnavailable
	}
	value, err := s.client.Get(ctx, key).Result()
	return value, affiliateAttributionStoreError(err)
}

func (s *affiliateAttributionStore) Take(ctx context.Context, key string) (string, error) {
	if s == nil || s.client == nil {
		return "", errAffiliateAttributionStoreUnavailable
	}
	value, err := s.client.GetDel(ctx, key).Result()
	return value, affiliateAttributionStoreError(err)
}

func (s *affiliateAttributionStore) Count(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if s == nil || s.client == nil {
		return 0, errAffiliateAttributionStoreUnavailable
	}
	if ttl <= 0 {
		return 0, errAffiliateAttributionCounterTTL
	}
	// EXPIRE uses whole seconds. Round a fractional duration up rather than
	// creating an immediately expired counter; the normal window is 120s.
	seconds := int64(ttl / time.Second)
	if ttl%time.Second != 0 {
		seconds++
	}
	count, err := affiliateAttributionCountScript.Run(ctx, s.client, []string{key}, seconds).Int64()
	return count, affiliateAttributionStoreError(err)
}
