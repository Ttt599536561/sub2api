package qqimage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type Store struct {
	db                   *sql.DB
	key                  []byte
	disableNewAdmissions bool
}

type StoreOptions struct {
	DisableNewAdmissions bool
}

type storeTxKey struct{}
type storeTransaction struct {
	store *Store
	tx    *sql.Tx
}
type storeQueryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) queryer(ctx context.Context) storeQueryer {
	if scoped, ok := ctx.Value(storeTxKey{}).(storeTransaction); ok && scoped.store == s {
		return scoped.tx
	}
	return s.db
}

func NewStore(db *sql.DB, key []byte, options ...StoreOptions) *Store {
	s := &Store{db: db, key: append([]byte(nil), key...)}
	if len(options) > 0 {
		s.disableNewAdmissions = options[0].DisableNewAdmissions
	}
	return s
}
func ParseEncryptionKey(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	b, e := hex.DecodeString(value)
	if e != nil || len(b) != 32 {
		return nil, ErrEncryptionUnavailable
	}
	return b, nil
}
func token(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }
func normalizeError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// A single domain lock keeps quota/dispatch/binding lock order deterministic across instances.
// Wallet updates still take the existing users row lock, coordinating with API charging.
func (s *Store) transaction(ctx context.Context, f func(*sql.Tx, time.Time) error) error {
	if scoped, ok := ctx.Value(storeTxKey{}).(storeTransaction); ok && scoped.store == s {
		var now time.Time
		if e := scoped.tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return e
		}
		return f(scoped.tx, now)
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(716709231248)"); e != nil {
		return e
	}
	var now time.Time
	if e = tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return e
	}
	if e = f(tx, now); e != nil {
		return e
	}
	return tx.Commit()
}
func audit(ctx context.Context, tx *sql.Tx, actor int64, action, id, reason string) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO qq_image_audit(actor_id,action,object_id,reason) VALUES($1,$2,$3,$4)`, actor, action, id, reason)
	return e
}
func eligible(ctx context.Context, tx *sql.Tx, userID int64) error {
	// All authorization mutations lock the canonical user before reading the
	// email proof, so concurrent email/status changes cannot invalidate admission.
	var id int64
	if e := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&id); e != nil {
		return ErrForbidden
	}
	var ok bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN qq_image_email_verifications e ON e.user_id=u.id AND e.email=u.email WHERE u.id=$1 AND u.status='active' AND u.deleted_at IS NULL)`, userID).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func activeOwner(ctx context.Context, tx *sql.Tx, userID int64) error {
	var id int64
	e := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrForbidden
	}
	return e
}
func (s *Store) CurrentEmail(ctx context.Context, userID int64) (string, error) {
	var v string
	e := s.queryer(ctx).QueryRowContext(ctx, `SELECT email FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL`, userID).Scan(&v)
	return v, normalizeError(e)
}
func (s *Store) EmailVerified(ctx context.Context, userID int64) (bool, error) {
	var ok bool
	e := s.queryer(ctx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN qq_image_email_verifications e ON e.user_id=u.id AND e.email=u.email WHERE u.id=$1 AND u.status='active' AND u.deleted_at IS NULL)`, userID).Scan(&ok)
	return ok, e
}
func (s *Store) MarkEmailVerified(ctx context.Context, userID int64, email string) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		r, e := tx.ExecContext(ctx, `INSERT INTO qq_image_email_verifications(user_id,email) SELECT id,email FROM users WHERE id=$1 AND email=$2 AND status='active' AND deleted_at IS NULL ON CONFLICT(user_id,email) DO UPDATE SET verified_at=clock_timestamp()`, userID, email)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrForbidden
		}
		return nil
	})
}
func (s *Store) MarkRecentAuthorization(ctx context.Context, userID int64, sessionID string) error {
	if sessionID == "" {
		return ErrForbidden
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		if e := eligible(ctx, tx, userID); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO qq_image_recent_authorizations(user_id,session_hash,email) SELECT id,$2,email FROM users WHERE id=$1 ON CONFLICT(user_id,session_hash) DO UPDATE SET email=EXCLUDED.email,verified_at=clock_timestamp()`, userID, digest(sessionID))
		return e
	})
}
func (s *Store) RecentAuthorization(ctx context.Context, userID int64, sessionID string) (bool, error) {
	var ok bool
	e := s.queryer(ctx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_recent_authorizations r JOIN users u ON u.id=r.user_id AND u.email=r.email WHERE r.user_id=$1 AND r.session_hash=$2 AND r.verified_at>clock_timestamp()-interval '15 minutes' AND u.status='active' AND u.deleted_at IS NULL)`, userID, digest(sessionID)).Scan(&ok)
	return ok, e
}
func (s *Store) PendingBalanceInvalidations(ctx context.Context, limit int) ([]CacheInvalidation, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, e := s.queryer(ctx).QueryContext(ctx, `SELECT id,user_id FROM qq_image_balance_outbox ORDER BY id LIMIT $1`, limit)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []CacheInvalidation{}
	for rows.Next() {
		var v CacheInvalidation
		if e = rows.Scan(&v.ID, &v.UserID); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) AckBalanceInvalidation(ctx context.Context, id int64) error {
	_, e := s.queryer(ctx).ExecContext(ctx, `DELETE FROM qq_image_balance_outbox WHERE id=$1`, id)
	return e
}
func (s *Store) ListAudit(ctx context.Context, limit int) ([]Audit, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, e := s.queryer(ctx).QueryContext(ctx, `SELECT id,actor_id,action,object_id,reason,created_at FROM qq_image_audit ORDER BY id DESC LIMIT $1`, limit)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []Audit{}
	for rows.Next() {
		var a Audit
		if e = rows.Scan(&a.ID, &a.ActorID, &a.Action, &a.ObjectID, &a.Reason, &a.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
