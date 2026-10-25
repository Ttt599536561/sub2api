package qqimage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// RecoverExpired always releases hard-deadline holds before trying any saved result.
func (s *Store) RecoverExpired(ctx context.Context) error {
	if e := s.recoverExpired(ctx); e != nil {
		return e
	}
	rows, e := s.queryer(ctx).QueryContext(ctx, `SELECT order_id FROM qq_image_refund_intents WHERE status='pending' ORDER BY created_at LIMIT 100`)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			_ = rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	if e = rows.Err(); e != nil {
		_ = rows.Close()
		return e
	}
	_ = rows.Close()
	var firstError error
	for _, id := range ids {
		if _, e = s.applyRefund(ctx, id); e != nil && firstError == nil {
			firstError = e
		}
	}
	rows, e = s.queryer(ctx).QueryContext(ctx, `SELECT o.id,o.lease_token FROM qq_image_orders o JOIN users u ON u.id=o.user_id WHERE o.artifact IS NOT NULL AND o.website_status='pending' AND o.funds_status IN ('held','released') AND u.status='active' AND u.deleted_at IS NULL ORDER BY o.created_at,o.id LIMIT 100`)
	if e != nil {
		return e
	}
	type saved struct{ id, lease string }
	items := []saved{}
	for rows.Next() {
		var a saved
		if e = rows.Scan(&a.id, &a.lease); e != nil {
			_ = rows.Close()
			return e
		}
		items = append(items, a)
	}
	if e = rows.Err(); e != nil {
		_ = rows.Close()
		return e
	}
	_ = rows.Close()
	for _, a := range items {
		_, e = s.captureArtifact(ctx, a.id, a.lease)
		if e == nil {
			continue
		}
		if errors.Is(e, ErrConflict) {
			e = s.publishLateArtifact(ctx, a.id)
		}
		if e != nil && !errors.Is(e, ErrForbidden) && !errors.Is(e, ErrExpired) && !errors.Is(e, ErrConflict) && firstError == nil {
			firstError = e
		}
	}
	return firstError
}

// A safe persisted late result belongs to the original website owner. Publishing
// it does not reverse release/refund and does not create a QQ delivery intention.
func (s *Store) publishLateArtifact(ctx context.Context, id string) error {
	return s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		o, e := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1 FOR UPDATE`, id))
		if e != nil {
			return e
		}
		if o.FundsStatus != "released" {
			return ErrConflict
		}
		if e = activeOwner(ctx, tx, o.UserID); e != nil {
			return e
		}
		var b []byte
		if e = tx.QueryRowContext(ctx, `SELECT config FROM qq_image_configs WHERE id=$1`, o.ConfigID).Scan(&b); e != nil {
			return e
		}
		var c Config
		if e = json.Unmarshal(b, &c); e != nil {
			return e
		}
		r, e := tx.ExecContext(ctx, `UPDATE qq_image_orders SET website_status='available',artifact_expires_at=$2 WHERE id=$1 AND artifact IS NOT NULL AND website_status='pending'`, id, now.Add(time.Duration(c.RetentionDays)*24*time.Hour))
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n == 1 {
			return audit(ctx, tx, 0, "order.late_result_free", id, "")
		}
		return nil
	})
}
