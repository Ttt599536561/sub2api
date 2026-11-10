package qqinvite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) PreviewCodes(ctx context.Context, id int64, codes []string) (CodePreview, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return CodePreview{}, e
	}
	defer func() { _ = tx.Rollback() }()
	return previewCodes(ctx, tx, id, codes, false)
}
func previewCodes(ctx context.Context, tx *sql.Tx, id int64, codes []string, lock bool) (CodePreview, error) {
	out := CodePreview{Items: []CodePreviewItem{}}
	if len(codes) == 0 || len(codes) > 500 {
		return out, ErrInvalid
	}
	c, e := scanCampaign(tx.QueryRowContext(ctx, `SELECT id,version,payload,baseline_at,created_at FROM qq_invite_campaigns WHERE id=$1`, id))
	if e != nil {
		return out, e
	}
	if _, e = subscriptionName(ctx, tx, c.SubscriptionGroupID); e != nil {
		return out, e
	}
	seen := map[string]bool{}
	for i, raw := range codes {
		code := strings.TrimSpace(raw)
		it := CodePreviewItem{Index: i, CodeHint: hint(code)}
		var typ, status string
		var group sql.NullInt64
		var days int
		var expiry sql.NullTime
		var used sql.NullInt64
		var pool bool
		query := `SELECT type,status,group_id,validity_days,expires_at,used_by,EXISTS(SELECT 1 FROM qq_invite_code_pool p WHERE p.redeem_code_id=r.id) FROM redeem_codes r WHERE code=$1`
		if lock {
			query += " FOR UPDATE"
		}
		e := tx.QueryRowContext(ctx, query, code).Scan(&typ, &status, &group, &days, &expiry, &used, &pool)
		switch {
		case seen[code]:
			it.Reason = "duplicate"
		case errors.Is(e, sql.ErrNoRows):
			it.Reason = "not_found"
		case e != nil:
			return out, e
		case typ != "subscription" || !group.Valid || group.Int64 != c.SubscriptionGroupID || days != 30:
			it.Reason = "not_monthly_subscription"
		case status != "unused" || used.Valid:
			it.Reason = "not_unused"
		case expiry.Valid && !expiry.Time.After(time.Now()):
			it.Reason = "expired"
		case pool:
			it.Reason = "already_in_pool"
		default:
			it.Valid = true
		}
		seen[code] = true
		if it.Valid {
			out.ValidCount++
		} else {
			out.InvalidCount++
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}
func (s *Store) ImportCodes(ctx context.Context, id int64, codes []string, actor int64) (CodePreview, error) {
	tx, e := s.beginWrite(ctx)
	if e != nil {
		return CodePreview{}, e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(81425303)`); e != nil {
		return CodePreview{}, e
	}
	out, e := previewCodes(ctx, tx, id, codes, true)
	if e != nil {
		return out, e
	}
	if out.InvalidCount > 0 {
		return out, ErrInvalid
	}
	for _, code := range codes {
		if _, e = tx.ExecContext(ctx, `INSERT INTO qq_invite_code_pool(campaign_id,redeem_code_id) SELECT $1,id FROM redeem_codes WHERE code=$2`, id, strings.TrimSpace(code)); e != nil {
			return out, e
		}
	}
	if e = audit(ctx, tx, actor, "import_codes", id, map[string]int{"count": len(codes)}); e != nil {
		return out, e
	}
	if e = wakeInventory(ctx, tx, id); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s *Store) GenerateCodes(ctx context.Context, id int64, count int, actor int64) (CodePreview, error) {
	out := CodePreview{Items: []CodePreviewItem{}}
	if count < 1 || count > 500 {
		return out, ErrInvalid
	}
	tx, e := s.beginWrite(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback() }()
	c, e := scanCampaign(tx.QueryRowContext(ctx, `SELECT id,version,payload,baseline_at,created_at FROM qq_invite_campaigns WHERE id=$1 FOR UPDATE`, id))
	if e != nil {
		return out, e
	}
	if _, e = subscriptionName(ctx, tx, c.SubscriptionGroupID); e != nil {
		return out, e
	}
	for i := 0; i < count; i++ {
		code := strings.ToUpper(token())
		var codeID int64
		if e = tx.QueryRowContext(ctx, `INSERT INTO redeem_codes(code,type,value,status,group_id,validity_days,notes) VALUES($1,'subscription',0,'unused',$2,30,'QQ invitation monthly reward') RETURNING id`, code, c.SubscriptionGroupID).Scan(&codeID); e != nil {
			return out, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO qq_invite_code_pool(campaign_id,redeem_code_id) VALUES($1,$2)`, id, codeID); e != nil {
			return out, e
		}
		out.Items = append(out.Items, CodePreviewItem{Index: i, Valid: true, CodeHint: hint(code)})
		out.ValidCount++
	}
	if e = audit(ctx, tx, actor, "generate_codes", id, map[string]int{"count": count}); e != nil {
		return out, e
	}
	if e = wakeInventory(ctx, tx, id); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func wakeInventory(ctx context.Context, tx *sql.Tx, id int64) error {
	_, e := tx.ExecContext(ctx, `UPDATE qq_invite_records SET next_attempt_at=now() WHERE campaign_id=$1 AND status='queued_inventory'`, id)
	return e
}
func (s *Store) ListCodes(ctx context.Context, id int64, page, size int) (CodeList, error) {
	out := CodeList{Items: []PoolCode{}}
	offset, size := pageBounds(page, size)
	if e := s.db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE p.record_id IS NULL AND r.status='unused' AND (r.expires_at IS NULL OR r.expires_at>now())),count(*) FILTER(WHERE p.record_id IS NOT NULL) FROM qq_invite_code_pool p JOIN redeem_codes r ON r.id=p.redeem_code_id WHERE ($1=0 OR p.campaign_id=$1)`, id).Scan(&out.Total, &out.Available, &out.Assigned); e != nil {
		return out, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT p.id,p.campaign_id,r.code,p.record_id,p.created_at,r.status,r.expires_at FROM qq_invite_code_pool p JOIN redeem_codes r ON r.id=p.redeem_code_id WHERE ($1=0 OR p.campaign_id=$1) ORDER BY p.id DESC LIMIT $2 OFFSET $3`, id, size, offset)
	if e != nil {
		return out, e
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p PoolCode
		var code, status string
		var expiry sql.NullTime
		if e = rows.Scan(&p.ID, &p.CampaignID, &code, &p.RecordID, &p.CreatedAt, &status, &expiry); e != nil {
			return out, e
		}
		p.CodeHint = hint(code)
		p.State = "available"
		if p.RecordID != nil {
			p.State = "assigned"
		}
		if status == "used" {
			p.State = "used"
		} else if expiry.Valid && !expiry.Time.After(time.Now()) {
			p.State = "expired"
		}
		out.Items = append(out.Items, p)
	}
	return out, rows.Err()
}
