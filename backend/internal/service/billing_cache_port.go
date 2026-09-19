package service

import (
	"context"
	"time"
)

// BalanceGenerationCache fences cache refills that started before a credit or
// debit invalidated their database snapshot. It is optional for legacy adapters.
type BalanceGenerationCache interface {
	UserBalanceGeneration(context.Context, int64) (int64, error)
	SetUserBalanceIfGeneration(context.Context, int64, float64, int64) (bool, error)
}

// SubscriptionCacheData represents cached subscription data
type SubscriptionCacheData struct {
	Status       string
	ExpiresAt    time.Time
	DailyUsage   float64
	WeeklyUsage  float64
	MonthlyUsage float64
	Version      int64
}
