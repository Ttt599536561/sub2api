package qqimage

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"
)

// ResolveOrderFailure records an authenticated administrator's failure decision
// for an unknown provider result. The caller must enforce administrator access.
// It never invents a successful result or reverses a captured/refunded payment.
func (s *Store) ResolveOrderFailure(ctx context.Context, id string, actorID int64, reason string) (*Order, error) {
	reason = strings.TrimSpace(reason)
	if actorID <= 0 || reason == "" || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > 500 {
		return nil, ErrInvalid
	}
	err := s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		order, err := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if order.FundsStatus == "released" && order.Status == "failed" && order.Error == reason {
			var resolved bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_audit WHERE object_id=$1 AND action='order.resolve_failure' AND reason=$2)`, id, reason).Scan(&resolved); err != nil {
				return err
			}
			if resolved {
				return nil
			}
		}
		if order.FundsStatus != "held" || order.Status != "reconciling" {
			return ErrConflict
		}
		if err = s.release(ctx, tx, id, "failed", reason); err != nil {
			return err
		}
		return audit(ctx, tx, actorID, "order.resolve_failure", id, reason)
	})
	if err != nil {
		return nil, err
	}
	return s.GetOrder(ctx, id, 0)
}
