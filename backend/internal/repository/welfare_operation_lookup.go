package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *welfareRepository) OperationByKey(ctx context.Context, userID int64, kind, key string) (*service.WelfareOperation, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT result FROM welfare_operations WHERE user_id=$1 AND type=$2 AND idempotency_key=$3`, userID, kind, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrWelfareOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	var result service.WelfareOperation
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
