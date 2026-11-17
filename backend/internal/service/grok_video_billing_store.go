package service

import "context"

// GrokVideoBillingTaskStore stores new task identity durably. Redis snapshots
// with no binding remain readable for tasks created by older application builds.
type GrokVideoBillingTaskStore interface {
	StoreGrokVideoBillingTask(context.Context, string, int64, int64, []byte) error
	LoadGrokVideoBillingTask(context.Context, string, int64, int64) ([]byte, error)
}
