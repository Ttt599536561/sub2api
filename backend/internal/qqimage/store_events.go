package qqimage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// ProcessEvent commits business mutations and the canonical event response in one
// transaction. Network replies belong outside this callback. A committed replay
// returns fresh=false so an unknown QQ receipt is never resent automatically.
func (s *Store) ProcessEvent(ctx context.Context, key, payloadHash string, execute func(context.Context) (string, string, error)) (reply, manual string, fresh bool, err error) {
	if key == "" || len(key) > 2048 || payloadHash == "" || execute == nil {
		return "", "", false, ErrInvalid
	}
	err = s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		var prior, result string
		e := tx.QueryRowContext(ctx, `SELECT request_hash,result FROM qq_image_events WHERE event_key=$1`, digest(key)).Scan(&prior, &result)
		if e == nil {
			if prior != payloadHash {
				return ErrConflict
			}
			if result != "" {
				var stored struct {
					Reply  string
					Manual string
				}
				if e = json.Unmarshal([]byte(result), &stored); e != nil {
					return e
				}
				reply, manual = stored.Reply, stored.Manual
			}
			return nil
		}
		if e != sql.ErrNoRows {
			return e
		}
		inner := context.WithValue(ctx, storeTxKey{}, storeTransaction{store: s, tx: tx})
		reply, manual, e = execute(inner)
		if e != nil {
			return e
		}
		result = string(jsonBytes(struct {
			Reply  string
			Manual string
		}{reply, manual}))
		if _, e = tx.ExecContext(ctx, `INSERT INTO qq_image_events(event_key,request_hash,result) VALUES($1,$2,$3)`, digest(key), payloadHash, result); e != nil {
			return e
		}
		fresh = true
		return nil
	})
	if err != nil {
		return "", "", false, err
	}
	return
}
