package service

import (
	"context"
	"errors"
	"time"
)

var ErrAffiliateAttributionMissing = errors.New("affiliate attribution not found")

// AffiliateAttributionStore persists opaque referral tickets, browser records,
// OAuth snapshots, and counters. Serialization and key ownership stay with the
// caller. Get and Take return ErrAffiliateAttributionMissing for absent records.
type AffiliateAttributionStore interface {
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	SetIfAbsent(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	Get(ctx context.Context, key string) (string, error)
	Take(ctx context.Context, key string) (string, error)
	Count(ctx context.Context, key string, ttl time.Duration) (int64, error)
}
