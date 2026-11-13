package qqinvite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *Store) AcceptEvent(ctx context.Context, ev Event) error {
	if !validQQ(ev.SelfID) || !validQQ(ev.UserID) || ev.Time <= 0 || ev.Time > time.Now().Add(5*time.Minute).Unix() {
		return ErrInvalid
	}
	if ev.Kind != "group_increase" && ev.Kind != "private_claim" && ev.Kind != "group_decrease" {
		return ErrInvalid
	}
	if ev.Kind != "private_claim" && !validGroup(ev.GroupID) {
		return ErrInvalid
	}
	if ev.Kind == "group_decrease" && ev.SubType != "leave" && ev.SubType != "kick" && ev.SubType != "kick_me" {
		return ErrInvalid
	}
	tx, e := s.beginWrite(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(81425302)`); e != nil {
		return e
	}
	cfg, e := s.loadConfig(ctx, tx)
	if e != nil {
		return e
	}
	if !cfg.Enabled && ev.Kind != "group_decrease" && ev.Kind != "private_claim" {
		return ErrDisabled
	}
	if cfg.BotQQ != ev.SelfID {
		return ErrInvalid
	}
	b, _ := json.Marshal(ev)
	sum := sha256.Sum256(b)
	key := hex.EncodeToString(sum[:])
	if ev.EventID != "" {
		key = ev.SelfID + ":" + ev.EventID
	}
	res, e := tx.ExecContext(ctx, `INSERT INTO qq_invite_events(event_key,payload) VALUES($1,$2) ON CONFLICT DO NOTHING`, key, b)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return tx.Commit()
	}
	if ev.Kind != "private_claim" && ev.UserID == ev.SelfID {
		return tx.Commit()
	}
	if ev.Kind == "group_decrease" {
		if e = s.acceptDeparture(ctx, tx, ev); e != nil {
			return e
		}
		return tx.Commit()
	}
	if ev.Kind == "private_claim" {
		if e = s.claimRewards(ctx, tx, ev, cfg.Enabled); e != nil {
			return e
		}
		return tx.Commit()
	}

	rows, e := tx.QueryContext(ctx, `SELECT DISTINCT ON(campaign_id) campaign_id,version,payload FROM qq_invite_campaign_versions WHERE created_at < to_timestamp($1)+interval '1 second' ORDER BY campaign_id,version DESC`, ev.Time)
	if e != nil {
		return e
	}
	var candidates []Campaign
	for rows.Next() {
		var c Campaign
		var b []byte
		if e = rows.Scan(&c.ID, &c.Version, &b); e != nil {
			_ = rows.Close()
			return e
		}
		if e = json.Unmarshal(b, &c); e != nil {
			_ = rows.Close()
			return e
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	_ = rows.Close()
	if e != nil {
		return e
	}
	eventTime := time.Unix(ev.Time, 0).UTC()
	for _, c := range candidates {
		if !c.Enabled || eventTime.Before(c.StartsAt) || !eventTime.Before(c.EndsAt) {
			continue
		}
		inGroup := false
		for _, g := range c.GroupIDs {
			if g == ev.GroupID {
				inGroup = true
			}
		}
		if !inGroup {
			continue
		}
		var baseline bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_invite_baseline WHERE campaign_id=$1 AND qq=$2)`, c.ID, ev.UserID).Scan(&baseline); e != nil {
			return e
		}
		status, reason, inviter := "received", "", ""
		if baseline {
			status = "baseline"
			reason = "existing_member"
		}
		if ev.SubType == "invite" && validQQ(ev.OperatorID) && ev.OperatorID != ev.UserID && ev.OperatorID != ev.SelfID {
			inviter = ev.OperatorID
		}
		snapshot, _ := json.Marshal(c)
		var recordID int64
		e = tx.QueryRowContext(ctx, `INSERT INTO qq_invite_records(campaign_id,campaign_version,snapshot,self_id,group_id,invitee_qq,inviter_qq,status,reason,event_time) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(campaign_id,invitee_qq) DO NOTHING RETURNING id`, c.ID, c.Version, snapshot, ev.SelfID, ev.GroupID, ev.UserID, inviter, status, reason, eventTime).Scan(&recordID)
		if errors.Is(e, sql.ErrNoRows) {
			if e = tx.QueryRowContext(ctx, `SELECT id FROM qq_invite_records WHERE campaign_id=$1 AND invitee_qq=$2`, c.ID, ev.UserID).Scan(&recordID); e != nil {
				return e
			}
			if inviter != "" {
				if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_records SET inviter_qq=$2,status='received',next_attempt_at=now() WHERE id=$1 AND status='pending_attribution' AND group_id=$3 AND event_time=$4`, recordID, inviter, ev.GroupID, eventTime); e != nil {
					return e
				}
			}
		} else if e != nil {
			return e
		}

		var departedAt sql.NullTime
		if e = tx.QueryRowContext(ctx, `SELECT max(d.departed_at) FROM qq_invite_departures d JOIN qq_invite_records r ON d.self_id=r.self_id AND d.group_id=r.group_id AND d.invitee_qq=r.invitee_qq AND d.departed_at>=r.event_time WHERE r.id=$1`, recordID).Scan(&departedAt); e != nil {
			return e
		}
		if departedAt.Valid {
			var state string
			if e = tx.QueryRowContext(ctx, `SELECT status FROM qq_invite_records WHERE id=$1 FOR UPDATE`, recordID).Scan(&state); e != nil {
				return e
			}
			if state != "revoked" && state != "baseline" {
				if e = s.revokeRecord(ctx, tx, recordID, departedAt.Time); e != nil {
					return e
				}
			}
			continue
		}
		joined, e := tx.ExecContext(ctx, `INSERT INTO qq_invite_joins(campaign_id,group_id,qq) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, c.ID, ev.GroupID, ev.UserID)
		if e != nil {
			return e
		}
		added, _ := joined.RowsAffected()
		if added == 1 && !baseline && c.WelcomeTemplate != "" {
			if e = enqueue(ctx, tx, recordID, "welcome", ev.GroupID, ev.UserID, c.WelcomeTemplate); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}
func enqueue(ctx context.Context, tx *sql.Tx, id int64, kind, destination, mention, template string) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO qq_invite_outbox(record_id,kind,destination,mention,template) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, id, kind, destination, mention, template)
	return e
}
func (s *Store) ListRecords(ctx context.Context, campaignID int64, status string, page, size int) (RecordList, error) {
	out := RecordList{Items: []Record{}}
	offset, size := pageBounds(page, size)
	if e := s.db.QueryRowContext(ctx, `SELECT count(*) FROM qq_invite_records WHERE ($1=0 OR campaign_id=$1) AND ($2='' OR status=$2)`, campaignID, status).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT r.id,r.campaign_id,r.group_id,r.invitee_qq,r.inviter_qq,r.qq_level,r.status,r.reason,w.id,COALESCE(c.code,''),COALESCE((SELECT status FROM qq_invite_outbox WHERE record_id=r.id AND kind='private' LIMIT 1),''),COALESCE((SELECT status FROM qq_invite_outbox WHERE record_id=r.id AND kind='congratulation' LIMIT 1),''),COALESCE((SELECT status FROM qq_invite_outbox WHERE record_id=r.id AND kind='revoke_private' LIMIT 1),''),COALESCE((SELECT status FROM qq_invite_outbox WHERE record_id=r.id AND kind='revoke_group' LIMIT 1),''),r.created_at FROM qq_invite_records r LEFT JOIN qq_invite_rewards w ON w.record_id=r.id LEFT JOIN qq_invite_code_pool p ON p.id=w.pool_id LEFT JOIN redeem_codes c ON c.id=p.redeem_code_id WHERE ($1=0 OR r.campaign_id=$1) AND ($2='' OR r.status=$2) ORDER BY r.id DESC LIMIT $3 OFFSET $4`, campaignID, status, size, offset)
	if e != nil {
		return out, e
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var r Record
		var code string
		if e = rows.Scan(&r.ID, &r.CampaignID, &r.GroupID, &r.InviteeQQ, &r.InviterQQ, &r.QQLevel, &r.Status, &r.Reason, &r.RewardID, &code, &r.PrivateStatus, &r.GroupStatus, &r.RevokePrivateStatus, &r.RevokeGroupStatus, &r.CreatedAt); e != nil {
			return out, e
		}
		if code != "" {
			r.CodeHint = hint(code)
		}
		out.Items = append(out.Items, r)
	}
	return out, rows.Err()
}
func (s *Store) ReviewRecord(ctx context.Context, id int64, inviter, reason string, actor int64) error {
	if !validQQ(inviter) || len(strings.TrimSpace(reason)) < 3 || len(reason) > 1000 {
		return ErrInvalid
	}
	tx, e := s.beginWrite(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	var status, invitee, self string
	if e = tx.QueryRowContext(ctx, `SELECT status,invitee_qq,self_id FROM qq_invite_records WHERE id=$1 FOR UPDATE`, id).Scan(&status, &invitee, &self); e != nil {
		return mapError(e)
	}
	if status == "awarded" || status == "baseline" || status == "revoked" || inviter == invitee || inviter == self {
		return ErrConflict
	}
	if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_records SET inviter_qq=$2,status='received',reason='admin_verified',lease_token=NULL,lease_until=NULL,next_attempt_at=now(),updated_at=now() WHERE id=$1`, id, inviter); e != nil {
		return e
	}
	if e = audit(ctx, tx, actor, "review_attribution", id, map[string]string{"inviter_qq": inviter, "reason": reason}); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) RecheckRecord(ctx context.Context, id, actor int64) error {
	tx, e := s.beginWrite(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	res, e := tx.ExecContext(ctx, `UPDATE qq_invite_records SET status='received',next_attempt_at=now(),lease_token=NULL,lease_until=NULL WHERE id=$1 AND status NOT IN ('awarded','baseline','revoked')`, id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	if e = audit(ctx, tx, actor, "recheck", id, map[string]string{}); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) RedeliverReward(ctx context.Context, id, actor int64) error {
	tx, e := s.beginWrite(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	var recordID int64
	if e = tx.QueryRowContext(ctx, `SELECT r.id FROM qq_invite_rewards w JOIN qq_invite_records r ON r.id=w.record_id WHERE w.id=$1 AND r.status='awarded' FOR UPDATE OF r`, id).Scan(&recordID); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return ErrConflict
		}
		return e
	}
	res, e := tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status='pending',attempts=0,next_attempt_at=now(),lease_token=NULL,lease_until=NULL WHERE record_id=$1 AND kind='private' AND (status<>'sending' OR lease_until<now())`, recordID)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	if e = audit(ctx, tx, actor, "redeliver_original", id, map[string]string{}); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status='pending',attempts=0,next_attempt_at=now(),lease_token=NULL,lease_until=NULL WHERE record_id=$1 AND kind='congratulation' AND status IN ('unknown','blocked','failed')`, recordID); e != nil {
		return e
	}
	return tx.Commit()
}

// Claim and sending share the record -> outbox lock order. A revoked record can
// only replay its cancellation explanation, never its original reward.
func (s *Store) claimRewards(ctx context.Context, tx *sql.Tx, ev Event, enabled bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,status FROM qq_invite_records WHERE inviter_qq=$1 AND self_id=$2 AND status IN ('awarded','revoked') AND ($3 OR status='revoked') AND (last_claim_at IS NULL OR last_claim_at<now()-interval '1 minute') ORDER BY id FOR UPDATE`, ev.UserID, ev.SelfID, enabled)
	if err != nil {
		return err
	}
	type claim struct {
		id     int64
		status string
	}
	var claims []claim
	for rows.Next() {
		var c claim
		if err = rows.Scan(&c.id, &c.status); err != nil {
			_ = rows.Close()
			return err
		}
		claims = append(claims, c)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, c := range claims {
		kind := "private"
		if c.status == "revoked" {
			kind = "revoke_private"
		}
		result, err := tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status='pending',attempts=0,next_attempt_at=now(),lease_token=NULL,lease_until=NULL WHERE record_id=$1 AND kind=$2 AND status NOT IN ('sending','pending')`, c.id, kind)
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		if n > 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE qq_invite_records SET last_claim_at=now() WHERE id=$1`, c.id); err != nil {
				return err
			}
		}
	}
	return nil
}
