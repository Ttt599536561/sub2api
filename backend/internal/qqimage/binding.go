package qqimage

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func stringID(id int64) string { return strconv.FormatInt(id, 10) }
func maskSubject(v string) string {
	if len(v) < 5 {
		return "***"
	}
	return "***" + v[len(v)-4:]
}
func (s *Store) rate(ctx context.Context, tx *sql.Tx, key string, limit int, window time.Duration, now time.Time) error {
	var start time.Time
	var count int
	e := tx.QueryRowContext(ctx, `SELECT window_start,count FROM qq_image_rate_limits WHERE key=$1`, key).Scan(&start, &count)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if e == sql.ErrNoRows || now.Sub(start) >= window {
		_, e = tx.ExecContext(ctx, `INSERT INTO qq_image_rate_limits(key,window_start,count) VALUES($1,$2,1) ON CONFLICT(key) DO UPDATE SET window_start=EXCLUDED.window_start,count=1`, key, now)
		return e
	}
	if count >= limit {
		return ErrLimit
	}
	_, e = tx.ExecContext(ctx, `UPDATE qq_image_rate_limits SET count=count+1 WHERE key=$1`, key)
	return e
}
func (s *Store) CreateBindingRequest(ctx context.Context, i Identity) (*BindingRequest, error) {
	if e := i.Validate(); e != nil {
		return nil, e
	}
	var out *BindingRequest
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		if e := s.requireBindingAdmission(ctx, tx, i); e != nil {
			return e
		}
		if e := s.rate(ctx, tx, "bind:"+i.Key(), 5, 5*time.Minute, now); e != nil {
			return e
		}
		var exists bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_grants WHERE identity_key=$1 AND status='active')`, i.Key()).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return ErrConflict
		}
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_binding_requests SET status='cancelled',revision=revision+1,code_hash=NULL WHERE identity_key=$1 AND status IN ('await_website','await_qq')`, i.Key()); e != nil {
			return e
		}
		r := &BindingRequest{ID: strings.ToUpper(token(6)), Status: "await_website", Identity: i, GroupID: i.GroupID, SubjectHint: maskSubject(i.SubjectID), ExpiresAt: now.Add(10 * time.Minute), Revision: 1}
		if _, e := tx.ExecContext(ctx, `INSERT INTO qq_image_binding_requests(id,identity_key,identity,status,expires_at) VALUES($1,$2,$3,$4,$5)`, r.ID, i.Key(), jsonBytes(i), r.Status, r.ExpiresAt); e != nil {
			return e
		}
		out = r
		return nil
	})
	return out, e
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func (s *Store) GetBindingRequest(ctx context.Context, id string) (*BindingRequest, error) {
	var r BindingRequest
	var data []byte
	e := s.queryer(ctx).QueryRowContext(ctx, `SELECT id,status,identity,revision,expires_at FROM qq_image_binding_requests WHERE id=$1`, strings.ToUpper(id)).Scan(&r.ID, &r.Status, &data, &r.Revision, &r.ExpiresAt)
	if e != nil {
		return nil, normalizeError(e)
	}
	if e = json.Unmarshal(data, &r.Identity); e != nil {
		return nil, e
	}
	r.GroupID = r.Identity.GroupID
	r.SubjectHint = maskSubject(r.Identity.SubjectID)
	if !time.Now().Before(r.ExpiresAt) && r.Status != "completed" {
		r.Status = "expired"
	}
	return &r, nil
}
func (s *Store) AuthorizeBinding(ctx context.Context, id string, userID int64, sessionID string) (*BindingAuthorization, error) {
	if sessionID == "" {
		return nil, ErrForbidden
	}
	var out *BindingAuthorization
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		if e := eligible(ctx, tx, userID); e != nil {
			return e
		}
		var recent bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_recent_authorizations r JOIN users u ON u.id=r.user_id AND u.email=r.email WHERE r.user_id=$1 AND r.session_hash=$2 AND r.verified_at>$3)`, userID, digest(sessionID), now.Add(-15*time.Minute)).Scan(&recent); e != nil {
			return e
		}
		if !recent {
			return ErrForbidden
		}
		var status string
		var expires time.Time
		var candidate sql.NullInt64
		var session sql.NullString
		var identityData []byte
		e := tx.QueryRowContext(ctx, `SELECT status,expires_at,candidate_user_id,session_hash,identity FROM qq_image_binding_requests WHERE id=$1 FOR UPDATE`, strings.ToUpper(id)).Scan(&status, &expires, &candidate, &session, &identityData)
		if e != nil {
			return normalizeError(e)
		}
		var identity Identity
		if e = json.Unmarshal(identityData, &identity); e != nil {
			return e
		}
		if e = s.requireBindingAdmission(ctx, tx, identity); e != nil {
			return e
		}
		if !now.Before(expires) {
			return ErrExpired
		}
		if status != "await_website" || candidate.Valid {
			return ErrConflict
		}
		code := strings.ToUpper(token(4))
		end := now.Add(5 * time.Minute)
		if end.After(expires) {
			end = expires
		}
		_, e = tx.ExecContext(ctx, `UPDATE qq_image_binding_requests SET status='await_qq',candidate_user_id=$2,session_hash=$3,revision=revision+1,code_hash=$4,code_expires_at=$5 WHERE id=$1`, strings.ToUpper(id), userID, digest(sessionID), digest(code), end)
		if e != nil {
			return e
		}
		out = &BindingAuthorization{ConfirmationCode: code, ExpiresAt: end}
		return audit(ctx, tx, userID, "binding.authorize", strings.ToUpper(id), "")
	})
	return out, e
}
func (s *Store) ConfirmBinding(ctx context.Context, i Identity, code string) (*Grant, error) {
	if e := i.Validate(); e != nil {
		return nil, e
	}
	var out *Grant
	var resultErr error
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		if e := s.requireBindingAdmission(ctx, tx, i); e != nil {
			return e
		}
		if e := s.rate(ctx, tx, "confirm:"+i.Key(), 5, 5*time.Minute, now); e != nil {
			return e
		}
		if e := s.rate(ctx, tx, "confirm:global", 1000, 5*time.Minute, now); e != nil {
			return e
		}
		var id, key, status string
		var candidate sql.NullInt64
		var expires, codeExpires time.Time
		var data []byte
		e := tx.QueryRowContext(ctx, `SELECT id,identity_key,status,candidate_user_id,expires_at,code_expires_at,identity FROM qq_image_binding_requests WHERE code_hash=$1 FOR UPDATE`, digest(strings.ToUpper(strings.TrimSpace(code)))).Scan(&id, &key, &status, &candidate, &expires, &codeExpires, &data)
		if e == sql.ErrNoRows {
			resultErr = ErrInvalid
			return nil
		}
		if e != nil {
			return e
		}
		if status != "await_qq" || !candidate.Valid {
			resultErr = ErrConflict
			return nil
		}
		if !now.Before(expires) || !now.Before(codeExpires) {
			resultErr = ErrExpired
			return nil
		}
		if key != i.Key() {
			_, e = tx.ExecContext(ctx, `UPDATE qq_image_binding_requests SET status='cancelled',revision=revision+1,code_hash=NULL WHERE id=$1`, id)
			if e != nil {
				return e
			}
			resultErr = ErrForbidden
			return audit(ctx, tx, 0, "binding.compromised", id, "")
		}
		if e = eligible(ctx, tx, candidate.Int64); e != nil {
			return e
		}
		var exists bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_grants WHERE identity_key=$1 AND status='active')`, key).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return ErrConflict
		}
		g := &Grant{ID: token(16), UserID: candidate.Int64, Identity: i, GroupID: i.GroupID, Status: "active", Revision: 1, CreatedAt: now}
		if _, e = tx.ExecContext(ctx, `INSERT INTO qq_image_grants(id,identity_key,identity,user_id,status,created_at) VALUES($1,$2,$3,$4,'active',$5)`, g.ID, key, data, g.UserID, now); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE qq_image_binding_requests SET status='completed',code_hash=NULL WHERE id=$1`, id); e != nil {
			return e
		}
		out = g
		return audit(ctx, tx, g.UserID, "binding.confirm", g.ID, "")
	})
	if e != nil {
		return nil, e
	}
	return out, resultErr
}
func (s *Store) ListGrants(ctx context.Context, userID int64) ([]Grant, error) {
	rows, e := s.queryer(ctx).QueryContext(ctx, `SELECT id,user_id,identity,status,revision,created_at,revoked_at FROM qq_image_grants WHERE ($1::bigint=0 OR user_id=$1) ORDER BY created_at DESC LIMIT 500`, userID)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		var b []byte
		if e = rows.Scan(&g.ID, &g.UserID, &b, &g.Status, &g.Revision, &g.CreatedAt, &g.RevokedAt); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &g.Identity); e != nil {
			return nil, e
		}
		g.GroupID = g.Identity.GroupID
		out = append(out, g)
	}
	return out, rows.Err()
}

// userID=0 is reserved for an authenticated administrator at the handler boundary.
func (s *Store) RevokeGrant(ctx context.Context, id string, userID int64, reason string) error {
	return s.revokeGrant(ctx, id, userID, userID, reason)
}

func (s *Store) RevokeGrantAsAdmin(ctx context.Context, id string, actorID int64, reason string) error {
	if actorID <= 0 || strings.TrimSpace(reason) == "" {
		return ErrInvalid
	}
	return s.revokeGrant(ctx, id, 0, actorID, reason)
}

func (s *Store) revokeGrant(ctx context.Context, id string, userID, actorID int64, reason string) error {
	return s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		var key, status string
		var owner int64
		if e := tx.QueryRowContext(ctx, `SELECT identity_key,user_id,status FROM qq_image_grants WHERE id=$1 FOR UPDATE`, id).Scan(&key, &owner, &status); e != nil {
			return normalizeError(e)
		}
		if userID != 0 && userID != owner {
			return ErrNotFound
		}
		if status == "revoked" {
			return nil
		}
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_grants SET status='revoked',revision=revision+1,revoked_at=$2 WHERE id=$1`, id, now); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_binding_requests SET status='cancelled',revision=revision+1,code_hash=NULL WHERE identity_key=$1 AND status IN ('await_website','await_qq')`, key); e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, `SELECT id FROM qq_image_orders WHERE grant_id=$1 AND status='queued' ORDER BY id`, id)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var orderID string
			if e = rows.Scan(&orderID); e != nil {
				_ = rows.Close()
				return e
			}
			ids = append(ids, orderID)
		}
		_ = rows.Close()
		for _, orderID := range ids {
			if e = s.release(ctx, tx, orderID, "cancelled", "grant_revoked"); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE qq_image_deliveries SET status='blocked' WHERE status='pending' AND order_id IN (SELECT id FROM qq_image_orders WHERE grant_id=$1)`, id); e != nil {
			return e
		}
		return audit(ctx, tx, actorID, "binding.revoke", id, reason)
	})
}
