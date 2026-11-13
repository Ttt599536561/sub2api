package qqinvite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Runtime struct {
	store   *Store
	factory BotFactory
}

const defaultDeliveryPendingTemplate = "已获得月卡奖励，但私信尚未完成。请添加机器人好友后私信“领取奖励”。"

func NewRuntime(s *Store, factory BotFactory) *Runtime { return &Runtime{store: s, factory: factory} }
func (r *Runtime) Run(ctx context.Context) {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = r.ProcessOnce(ctx)
			_ = r.DeliverOnce(ctx)
		}
	}
}
func (r *Runtime) SyncBaseline(ctx context.Context, id, actor int64) error {
	c, e := r.store.GetCampaign(ctx, id)
	if e != nil {
		return e
	}
	if c.Enabled {
		return ErrConflict
	}
	cfg, e := r.store.RuntimeConfig(ctx)
	if e != nil {
		return e
	}
	bot, e := r.factory(cfg)
	if e != nil {
		return e
	}
	members := map[string][]string{}
	for _, g := range c.GroupIDs {
		list, e := bot.GroupMembers(ctx, g)
		if e != nil {
			return e
		}
		containsSelf := false
		for _, qq := range list {
			if qq == cfg.BotQQ {
				containsSelf = true
				break
			}
		}
		if !containsSelf {
			return ErrInvalid
		}
		members[g] = list
	}
	return r.store.SaveBaseline(ctx, id, members, actor)
}

type workRecord struct {
	id, campaignID                               int64
	self, group, invitee, inviter, status, lease string
	campaign                                     Campaign
}

func (r *Runtime) ProcessOnce(ctx context.Context) error {
	cfg, e := r.store.RuntimeConfig(ctx)
	if e != nil {
		return e
	}
	if !cfg.Enabled {
		return nil
	}
	lease := token()
	var w workRecord
	var snapshot []byte
	// Drain legacy pending_level records under the current level-free policy.
	e = r.store.scanWrite(ctx, `UPDATE qq_invite_records SET lease_token=$1,lease_until=now()+interval '60 seconds' WHERE id=(SELECT id FROM qq_invite_records WHERE status IN ('received','pending_level','queued_inventory') AND next_attempt_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id,campaign_id,self_id,group_id,invitee_qq,inviter_qq,status,snapshot`, []any{lease}, &w.id, &w.campaignID, &w.self, &w.group, &w.invitee, &w.inviter, &w.status, &snapshot)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	w.lease = lease
	if e = json.Unmarshal(snapshot, &w.campaign); e != nil {
		return e
	}
	if cfg.BotQQ != w.self {
		return r.finishPending(ctx, w, "received", "bot_account_mismatch")
	}
	if w.inviter == "" {
		return r.finishPending(ctx, w, "pending_attribution", "inviter_not_verified")
	}
	bot, e := r.factory(cfg)
	if e != nil {
		return r.finishPending(ctx, w, "received", "bot_unavailable")
	}
	// Account identity remains required; the invitee's QQ level is never queried.
	if e = bot.CheckAccount(ctx); e != nil {
		return r.finishPending(ctx, w, "received", "bot_unavailable")
	}
	return r.allocate(ctx, w)
}
func (r *Runtime) finishPending(ctx context.Context, w workRecord, status, reason string) error {
	tx, e := r.store.beginWrite(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	res, e := tx.ExecContext(ctx, `UPDATE qq_invite_records SET status=$3,reason=$4,lease_token=NULL,lease_until=NULL,next_attempt_at=now()+interval '60 seconds',updated_at=now() WHERE id=$1 AND lease_token=$2`, w.id, w.lease, status, reason)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return tx.Commit()
	}
	if (status == "pending_attribution" || status == "received" || status == "queued_inventory") && w.campaign.PendingTemplate != "" {
		if e = enqueue(ctx, tx, w.id, "pending_notice", w.group, w.invitee, w.campaign.PendingTemplate); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (r *Runtime) allocate(ctx context.Context, w workRecord) error {
	tx, e := r.store.beginWrite(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	var lease sql.NullString
	if e = tx.QueryRowContext(ctx, `SELECT lease_token FROM qq_invite_records WHERE id=$1 FOR UPDATE`, w.id).Scan(&lease); e != nil {
		return e
	}
	if !lease.Valid || lease.String != w.lease {
		return nil
	}
	if _, e = subscriptionName(ctx, tx, w.campaign.SubscriptionGroupID); e != nil {
		if errors.Is(e, ErrInvalid) {
			_ = tx.Rollback()
			return r.finishPending(ctx, w, "queued_inventory", "subscription_unavailable")
		}
		return e
	}
	var poolID int64
	e = tx.QueryRowContext(ctx, `SELECT p.id FROM qq_invite_code_pool p JOIN redeem_codes c ON c.id=p.redeem_code_id WHERE p.campaign_id=$1 AND p.record_id IS NULL AND c.status='unused' AND c.type='subscription' AND c.group_id=$2 AND c.validity_days=30 AND (c.expires_at IS NULL OR c.expires_at>now()) ORDER BY p.id FOR UPDATE OF p,c SKIP LOCKED LIMIT 1`, w.campaignID, w.campaign.SubscriptionGroupID).Scan(&poolID)
	if errors.Is(e, sql.ErrNoRows) {
		_ = tx.Rollback()
		return r.finishPending(ctx, w, "queued_inventory", "monthly_card_stock_empty")
	}
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_code_pool SET record_id=$2 WHERE id=$1`, poolID, w.id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO qq_invite_rewards(record_id,pool_id) VALUES($1,$2)`, w.id, poolID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_records SET status='awarded',reason='',lease_token=NULL,lease_until=NULL,updated_at=now() WHERE id=$1`, w.id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status='cancelled' WHERE record_id=$1 AND kind='pending_notice' AND status IN ('pending','failed','unknown')`, w.id); e != nil {
		return e
	}
	if e = enqueue(ctx, tx, w.id, "private", w.inviter, "", w.campaign.PrivateTemplate); e != nil {
		return e
	}
	if w.campaign.CongratulationTemplate != "" {
		if e = enqueue(ctx, tx, w.id, "congratulation", w.group, w.inviter, w.campaign.CongratulationTemplate); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func renderTemplate(template string, w workRecord, level int, code, baseURL string) string {
	// Keep old templates and recorded levels readable without inventing a level.
	levelText := "未采集"
	if level > 0 {
		levelText = fmt.Sprint(level)
	}
	return strings.NewReplacer("{invitee}", w.invitee, "{inviter}", w.inviter, "{level}", levelText, "{code}", code, "{url}", strings.TrimRight(baseURL, "/")+"/redeem", "{plan_name}", w.campaign.PlanName).Replace(template)
}
func (r *Runtime) DeliverOnce(ctx context.Context) error {
	if e := r.store.invalidateOnce(ctx); e != nil {
		return e
	}
	cfg, e := r.store.RuntimeConfig(ctx)
	if e != nil {
		return e
	}
	if e = r.recoverExpiredSends(ctx); e != nil {
		return e
	}
	lease := token()
	var id, recordID int64
	var kind, destination, mention, template string
	var attempts int
	e = r.store.scanWrite(ctx, `UPDATE qq_invite_outbox SET status='sending',lease_token=$1,lease_until=now()+interval '60 seconds',attempts=attempts+1 WHERE id=(SELECT o.id FROM qq_invite_outbox o WHERE o.status IN ('pending','failed') AND o.next_attempt_at<=now() AND ($2 OR o.kind IN ('revoke_group','revoke_private')) AND (o.kind<>'congratulation' OR EXISTS(SELECT 1 FROM qq_invite_outbox p WHERE p.record_id=o.record_id AND p.kind='private' AND p.status='sent')) ORDER BY o.id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id,record_id,kind,destination,mention,template,attempts`, []any{lease, cfg.Enabled}, &id, &recordID, &kind, &destination, &mention, &template, &attempts)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	// Serialize the final eligibility check, the send, and its result with
	// departure handling. A send already in flight finishes before revocation;
	// unsent work is cancelled, and the compensating notice follows it.
	tx, e := r.store.beginWrite(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	var recordStatus string
	if e = tx.QueryRowContext(ctx, `SELECT status FROM qq_invite_records WHERE id=$1 FOR UPDATE`, recordID).Scan(&recordStatus); e != nil {
		return e
	}
	var validLease bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_invite_outbox WHERE id=$1 AND lease_token=$2 AND status='sending')`, id, lease).Scan(&validLease); e != nil {
		return e
	}
	revokeNotice := kind == "revoke_group" || kind == "revoke_private"
	if !validLease {
		return tx.Commit()
	}
	if recordStatus == "revoked" && !revokeNotice {
		if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status='cancelled',lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2`, id, lease); e != nil {
			return e
		}
		return tx.Commit()
	}
	var w workRecord
	var level sql.NullInt64
	var snapshot []byte
	var code string
	var expiry sql.NullTime
	e = tx.QueryRowContext(ctx, `SELECT r.self_id,r.group_id,r.invitee_qq,r.inviter_qq,r.qq_level,r.snapshot,COALESCE(c.code,''),c.expires_at FROM qq_invite_records r LEFT JOIN qq_invite_rewards w ON w.record_id=r.id LEFT JOIN qq_invite_code_pool p ON p.id=w.pool_id LEFT JOIN redeem_codes c ON c.id=p.redeem_code_id WHERE r.id=$1`, recordID).Scan(&w.self, &w.group, &w.invitee, &w.inviter, &level, &snapshot, &code, &expiry)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(snapshot, &w.campaign); e != nil {
		return e
	}
	if kind == "private" && expiry.Valid && !expiry.Time.After(time.Now()) {
		_, e = tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status=CASE WHEN attempts>=8 THEN 'blocked' ELSE 'failed' END,last_error='code_expired',lease_token=NULL,lease_until=NULL,next_attempt_at=now()+interval '1 day' WHERE id=$1 AND lease_token=$2`, id, lease)
		if e != nil {
			return e
		}
		return tx.Commit()
	}
	if kind != "private" && kind != "revoke_private" {
		code = ""
	}
	message := renderTemplate(template, w, int(level.Int64), code, cfg.PublicBaseURL)
	var msgID string
	sendErr := ErrInvalid
	if w.self == cfg.BotQQ {
		bot, err := r.factory(cfg)
		sendErr = err
		if err == nil {
			if kind == "private" || kind == "revoke_private" {
				msgID, sendErr = bot.SendPrivate(ctx, destination, message)
			} else {
				msgID, sendErr = bot.SendGroup(ctx, destination, message, mention)
			}
		}
	}
	status, reason := "sent", ""
	if sendErr != nil {
		status = "failed"
		reason = "delivery_failed"
		if errors.Is(sendErr, ErrDeliveryUnknown) {
			status = "unknown"
			reason = "delivery_unknown"
		}
		if attempts >= 8 && status != "unknown" {
			status = "blocked"
		}
	}
	delay := time.Duration(1<<min(attempts, 8)) * time.Second
	result, e := tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status=$3,message_id=$4,last_error=$5,lease_token=NULL,lease_until=NULL,next_attempt_at=$6 WHERE id=$1 AND lease_token=$2`, id, lease, status, msgID, reason, time.Now().Add(delay))
	if e != nil {
		return e
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return tx.Commit()
	}
	if kind == "private" {
		if status == "sent" {
			if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status='cancelled' WHERE record_id=$1 AND kind='delivery_pending' AND status IN ('pending','failed','unknown','blocked')`, recordID); e != nil {
				return e
			}
		} else {
			template := w.campaign.DeliveryPendingTemplate
			if template == "" {
				template = defaultDeliveryPendingTemplate
			}
			if e = enqueue(ctx, tx, recordID, "delivery_pending", w.group, w.inviter, template); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}

// A process crash cannot tell us whether QQ accepted an in-flight message.
// Preserve that uncertainty until its owner explicitly requests the original reward.
func (r *Runtime) recoverExpiredSends(ctx context.Context) error {
	tx, e := r.store.beginWrite(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	// Lock records before their outbox rows, including FK checks when inserting
	// delivery_pending. SKIP LOCKED leaves active sends for the next sweep.
	rows, e := tx.QueryContext(ctx, `SELECT r.id,r.status,r.group_id,r.inviter_qq,r.snapshot FROM qq_invite_records r WHERE EXISTS(SELECT 1 FROM qq_invite_outbox o WHERE o.record_id=r.id AND o.status='sending' AND o.lease_until<now()) ORDER BY r.id FOR UPDATE OF r SKIP LOCKED LIMIT 50`)
	if e != nil {
		return e
	}
	type recovery struct {
		id                     int64
		status, group, inviter string
		snapshot               []byte
	}
	var items []recovery
	for rows.Next() {
		var it recovery
		if e = rows.Scan(&it.id, &it.status, &it.group, &it.inviter, &it.snapshot); e != nil {
			_ = rows.Close()
			return e
		}
		items = append(items, it)
	}
	e = rows.Err()
	_ = rows.Close()
	if e != nil {
		return e
	}
	for _, it := range items {
		var privateExpired bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_invite_outbox WHERE record_id=$1 AND kind='private' AND status='sending' AND lease_until<now())`, it.id).Scan(&privateExpired); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_outbox SET status=CASE WHEN $2='revoked' AND kind NOT IN ('revoke_private','revoke_group') THEN 'cancelled' ELSE 'unknown' END,last_error='delivery_unknown',lease_token=NULL,lease_until=NULL WHERE record_id=$1 AND status='sending' AND lease_until<now()`, it.id, it.status); e != nil {
			return e
		}
		if !privateExpired || it.status == "revoked" {
			continue
		}
		var c Campaign
		if e = json.Unmarshal(it.snapshot, &c); e != nil {
			return e
		}
		template := c.DeliveryPendingTemplate
		if template == "" {
			template = defaultDeliveryPendingTemplate
		}
		if e = enqueue(ctx, tx, it.id, "delivery_pending", it.group, it.inviter, template); e != nil {
			return e
		}
	}
	return tx.Commit()
}
