package repository

import (
	"context"
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"time"
)

type welfareBalanceOutboxRepository struct{ db *sql.DB }

func NewWelfareBalanceOutboxRepository(db *sql.DB) service.WelfareBalanceOutboxRepository {
	return &welfareBalanceOutboxRepository{db: db}
}

func (r *welfareBalanceOutboxRepository) Claim(ctx context.Context, limit int, lease time.Duration) ([]service.WelfareBalanceOutboxEvent, error) {
	token := uuid.NewString()
	rows, err := r.db.QueryContext(ctx, `WITH pending AS (
 SELECT id FROM welfare_balance_outbox WHERE completed_at IS NULL AND available_at<=NOW()
 AND (lease_until IS NULL OR lease_until<NOW()) ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED
 ) UPDATE welfare_balance_outbox o SET lease_until=NOW()+($2*interval '1 second'),
 lease_token=$3,attempts=o.attempts+1 FROM pending WHERE o.id=pending.id
 RETURNING o.id,o.user_id,o.version,o.attempts,o.lease_token`, limit, lease.Seconds(), token)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]service.WelfareBalanceOutboxEvent, 0)
	for rows.Next() {
		var e service.WelfareBalanceOutboxEvent
		if err := rows.Scan(&e.ID, &e.UserID, &e.Version, &e.Attempts, &e.LeaseToken); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func (r *welfareBalanceOutboxRepository) Complete(ctx context.Context, e service.WelfareBalanceOutboxEvent) error {
	_, err := r.db.ExecContext(ctx, `UPDATE welfare_balance_outbox SET completed_at=NOW(),lease_until=NULL,lease_token=NULL,last_error=NULL WHERE id=$1 AND lease_token=$2 AND completed_at IS NULL`, e.ID, e.LeaseToken)
	return err
}
func (r *welfareBalanceOutboxRepository) Retry(ctx context.Context, e service.WelfareBalanceOutboxEvent, reason string, delay time.Duration) error {
	if len(reason) > 2000 {
		reason = reason[:2000]
	}
	_, err := r.db.ExecContext(ctx, `UPDATE welfare_balance_outbox SET available_at=NOW()+($3*interval '1 second'),lease_until=NULL,lease_token=NULL,last_error=$4 WHERE id=$1 AND lease_token=$2 AND completed_at IS NULL`, e.ID, e.LeaseToken, delay.Seconds(), reason)
	return err
}
