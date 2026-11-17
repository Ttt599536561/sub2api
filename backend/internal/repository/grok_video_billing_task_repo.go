package repository

import (
	"context"
	"fmt"
)

// A task's billing identity is immutable; replaying exactly the same snapshot
// is safe, but a reused upstream task ID cannot replace its original owner/term.
func (r *usageLogRepository) StoreGrokVideoBillingTask(ctx context.Context, taskID string, userID, apiKeyID int64, payload []byte) error {
	result, err := r.sql.ExecContext(ctx, `INSERT INTO grok_video_billing_tasks(task_id,user_id,api_key_id,snapshot)
 VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(user_id,api_key_id,task_id) DO UPDATE SET task_id=EXCLUDED.task_id
 WHERE grok_video_billing_tasks.snapshot=EXCLUDED.snapshot`, taskID, userID, apiKeyID, string(payload))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("video task billing snapshot conflict")
	}
	return nil
}

func (r *usageLogRepository) LoadGrokVideoBillingTask(ctx context.Context, taskID string, userID, apiKeyID int64) ([]byte, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT snapshot FROM grok_video_billing_tasks WHERE task_id=$1 AND user_id=$2 AND api_key_id=$3`, taskID, userID, apiKeyID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var payload []byte
	if err := rows.Scan(&payload); err != nil {
		return nil, err
	}
	return payload, rows.Err()
}
