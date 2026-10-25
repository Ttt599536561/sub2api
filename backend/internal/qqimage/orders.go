package qqimage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type storedOrder struct {
	Prompt             string
	Model              string
	Size               string
	Identity           Identity
	EventID            string
	ReplyMessageID     string
	ReplyCreatedAt     time.Time
	Currency           string  `json:"currency"`
	ProviderCost       *string `json:"provider_cost"`
	ProviderCurrency   *string `json:"provider_currency"`
	ProviderCostStatus string  `json:"provider_cost_status"`
}

const orderColumns = `id,user_id,grant_id,grant_revision,config_id,data,price::text,status,funds_status,delivery_status,website_status,created_at,deadline,queue_deadline,lease_token,lease_until,error,artifact_expires_at`

func scanOrder(row interface{ Scan(...any) error }) (*Order, error) {
	var o Order
	var b []byte
	if e := row.Scan(&o.ID, &o.UserID, &o.GrantID, &o.GrantRevision, &o.ConfigID, &b, &o.Price, &o.Status, &o.FundsStatus, &o.DeliveryStatus, &o.WebsiteStatus, &o.CreatedAt, &o.Deadline, &o.QueueDeadline, &o.LeaseToken, &o.LeaseUntil, &o.Error, &o.ArtifactExpiresAt); e != nil {
		return nil, normalizeError(e)
	}
	var d storedOrder
	if e := json.Unmarshal(b, &d); e != nil {
		return nil, e
	}
	o.Prompt = d.Prompt
	o.Model = d.Model
	o.Size = d.Size
	o.Identity = d.Identity
	o.EventID = d.EventID
	o.ReplyMessageID = d.ReplyMessageID
	o.Currency = d.Currency
	if o.Currency == "" {
		o.Currency = "USD"
	}
	o.ProviderCost = d.ProviderCost
	o.ProviderCurrency = d.ProviderCurrency
	o.ProviderCostStatus = d.ProviderCostStatus
	if o.ProviderCostStatus == "" {
		o.ProviderCostStatus = "unknown"
	}
	return &o, nil
}
func (s *Store) GetOrder(ctx context.Context, id string, userID int64) (*Order, error) {
	return scanOrder(s.queryer(ctx).QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1 AND ($2::bigint=0 OR user_id=$2)`, id, userID))
}
func (s *Store) ListOrders(ctx context.Context, userID int64, limit int) ([]Order, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, e := s.queryer(ctx).QueryContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE ($1::bigint=0 OR user_id=$1) ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []Order{}
	for rows.Next() {
		o, e := scanOrder(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}
func (s *Store) GetOrderForIdentity(ctx context.Context, i Identity, id string) (*Order, error) {
	if e := i.Validate(); e != nil {
		return nil, e
	}
	o, e := s.GetOrder(ctx, id, 0)
	if e != nil {
		return nil, e
	}
	var ok bool
	e = s.queryer(ctx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_grants WHERE id=$1 AND revision=$2 AND status='active' AND identity_key=$3)`, o.GrantID, o.GrantRevision, i.Key()).Scan(&ok)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, ErrForbidden
	}
	return o, nil
}
func (s *Store) CreateOrder(ctx context.Context, i Identity, eventID, prompt string) (*Order, error) {
	return s.CreateOrderFromEvent(ctx, i, eventID, eventID, time.Now(), prompt)
}
func (s *Store) CreateOrderFromEvent(ctx context.Context, i Identity, eventID, messageID string, createdAt time.Time, prompt string) (*Order, error) {
	if e := i.Validate(); e != nil {
		return nil, e
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || eventID == "" || messageID == "" || len(eventID) > 512 || len(messageID) > 512 {
		return nil, ErrInvalid
	}
	eventKey := digest(i.Key() + ":" + eventID)
	fingerprint := digest(string(jsonBytes(struct {
		Identity  Identity
		Prompt    string
		MessageID string
	}{i, prompt, messageID})))
	var out *Order
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		var oldID, oldHash string
		e := tx.QueryRowContext(ctx, `SELECT id,request_hash FROM qq_image_orders WHERE event_key=$1`, eventKey).Scan(&oldID, &oldHash)
		if e == nil {
			if oldHash != fingerprint {
				return ErrConflict
			}
			out, e = scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1`, oldID))
			return e
		}
		if e != sql.ErrNoRows {
			return e
		}
		var configID int64
		var b, botCredential, apiCredential []byte
		if e = tx.QueryRowContext(ctx, `SELECT id,config,bot_secret,api_key FROM qq_image_configs ORDER BY id DESC LIMIT 1`).Scan(&configID, &b, &botCredential, &apiCredential); e != nil {
			return ErrDisabled
		}
		var c Config
		if e = json.Unmarshal(b, &c); e != nil {
			return e
		}
		if !c.Enabled || c.AppID != i.AppID || !contains(c.AllowedGroups, i.GroupID) {
			return ErrDisabled
		}
		if len(s.key) != 32 {
			return ErrEncryptionUnavailable
		}
		if _, e = s.decrypt(botCredential); e != nil {
			return e
		}
		if _, e = s.decrypt(apiCredential); e != nil {
			return e
		}
		if utf8.RuneCountInString(prompt) > c.PromptMaxChars {
			return ErrInvalid
		}
		var userID, revision int64
		var grantID string
		if e = tx.QueryRowContext(ctx, `SELECT id,user_id,revision FROM qq_image_grants WHERE identity_key=$1 AND status='active' FOR UPDATE`, i.Key()).Scan(&grantID, &userID, &revision); e != nil {
			return ErrForbidden
		}
		if e = eligible(ctx, tx, userID); e != nil {
			return e
		}
		var inflight, queued, daily int
		var dailyOK bool
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM qq_image_orders WHERE user_id=$1 AND funds_status='held'`, userID).Scan(&inflight); e != nil {
			return e
		}
		if inflight > 0 {
			return ErrLimit
		}
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM qq_image_orders WHERE status='queued'`).Scan(&queued); e != nil {
			return e
		}
		if queued >= c.QueueLimit {
			return ErrLimit
		}
		if e = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(price),0)+$2::numeric<=$3::numeric FROM qq_image_orders WHERE user_id=$1 AND business_day=($4::timestamptz AT TIME ZONE 'Asia/Shanghai')::date AND funds_status IN ('held','captured','refunded')`, userID, c.Price, c.UserDailyAmount, now).Scan(&daily, &dailyOK); e != nil {
			return e
		}
		if daily >= c.UserDailyLimit || !dailyOK {
			return ErrLimit
		}
		if e = s.rate(ctx, tx, "order:"+stringID(userID), 1, 10*time.Second, now); e != nil {
			return e
		}
		r, e := tx.ExecContext(ctx, `UPDATE users SET balance=balance-$2::numeric,frozen_balance=frozen_balance+$2::numeric,updated_at=clock_timestamp() WHERE id=$1 AND balance>=$2::numeric AND status='active' AND deleted_at IS NULL`, userID, c.Price)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrInsufficientBalance
		}
		id := token(16)
		// Images usage does not establish a monetary provider cost. Persist unknown
		// explicitly, independently of the fixed USD user price; never assume zero.
		data := storedOrder{Prompt: prompt, Model: c.Model, Size: c.Size, Identity: i, EventID: eventID, ReplyMessageID: messageID, ReplyCreatedAt: createdAt, Currency: "USD", ProviderCostStatus: "unknown"}
		deadline := now.Add(time.Duration(c.DeadlineSeconds) * time.Second)
		queueDeadline := now.Add(time.Duration(c.QueueTimeoutSeconds) * time.Second)
		if queueDeadline.After(deadline) {
			queueDeadline = deadline
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO qq_image_orders(id,user_id,grant_id,grant_revision,identity_key,event_key,request_hash,config_id,data,price,business_day,status,funds_status,created_at,deadline,queue_deadline) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,($11::timestamptz AT TIME ZONE 'Asia/Shanghai')::date,'queued','held',$11,$12,$13)`, id, userID, grantID, revision, i.Key(), eventKey, fingerprint, configID, jsonBytes(data), c.Price, now, deadline, queueDeadline)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO qq_image_holds(order_id,user_id,amount,status) VALUES($1,$2,$3,'held')`, id, userID, c.Price); e != nil {
			return e
		}
		if e = recordMoney(ctx, tx, id, userID, c.Price, "hold"); e != nil {
			return e
		}
		if e = audit(ctx, tx, userID, "order.queued", id, ""); e != nil {
			return e
		}
		out, e = scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1`, id))
		return e
	})
	return out, e
}
func recordMoney(ctx context.Context, tx *sql.Tx, id string, user int64, amount, op string) error {
	if _, e := tx.ExecContext(ctx, `INSERT INTO qq_image_ledger(order_id,user_id,amount,operation) VALUES($1,$2,$3,$4)`, id, user, amount, op); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO qq_image_balance_outbox(user_id) VALUES($1)`, user)
	return e
}
func (s *Store) release(ctx context.Context, tx *sql.Tx, id, status, reason string) error {
	var user int64
	var amount, hold string
	if e := tx.QueryRowContext(ctx, `SELECT user_id,amount::text,status FROM qq_image_holds WHERE order_id=$1 FOR UPDATE`, id).Scan(&user, &amount, &hold); e != nil {
		return normalizeError(e)
	}
	if hold == "released" {
		return nil
	}
	if hold != "held" {
		return ErrConflict
	}
	r, e := tx.ExecContext(ctx, `UPDATE users SET balance=balance+$2::numeric,frozen_balance=frozen_balance-$2::numeric,updated_at=clock_timestamp() WHERE id=$1 AND frozen_balance>=$2::numeric`, user, amount)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	if _, e = tx.ExecContext(ctx, `UPDATE qq_image_holds SET status='released' WHERE order_id=$1`, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE qq_image_orders SET funds_status='released',status=$2,error=$3,lease_token=CASE WHEN status='queued' THEN '' ELSE lease_token END,lease_until=NULL WHERE id=$1`, id, status, reason); e != nil {
		return e
	}
	if e = recordMoney(ctx, tx, id, user, amount, "release"); e != nil {
		return e
	}
	return audit(ctx, tx, 0, "order.release", id, reason)
}
func (s *Store) CancelOrder(ctx context.Context, id string, userID int64) (*Order, error) {
	e := s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		o, e := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, userID))
		if e != nil {
			return e
		}
		if o.Status == "cancelled" && o.FundsStatus == "released" {
			return nil
		}
		if o.Status != "queued" {
			return ErrConflict
		}
		return s.release(ctx, tx, id, "cancelled", "user_cancelled")
	})
	if e != nil {
		return nil, e
	}
	return s.GetOrder(ctx, id, userID)
}
func (s *Store) ClaimOrder(ctx context.Context, owner string, lease time.Duration) (*Order, error) {
	if lease <= 0 || lease > 5*time.Minute {
		lease = 3 * time.Minute
	}
	var out *Order
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		// Walk past saturated groups without making the first batch a queue barrier.
		var afterCreated *time.Time
		var afterID string
		for {
			rows, e := tx.QueryContext(ctx, `SELECT id,created_at FROM qq_image_orders WHERE status='queued' AND ($1::timestamptz IS NULL OR (created_at,id)>($1,$2)) ORDER BY created_at,id LIMIT 100`, afterCreated, afterID)
			if e != nil {
				return e
			}
			type candidate struct {
				id      string
				created time.Time
			}
			items := []candidate{}
			for rows.Next() {
				var item candidate
				if e = rows.Scan(&item.id, &item.created); e != nil {
					_ = rows.Close()
					return e
				}
				items = append(items, item)
			}
			if e = rows.Err(); e != nil {
				_ = rows.Close()
				return e
			}
			_ = rows.Close()
			for _, item := range items {
				id := item.id
				afterCreated, afterID = &item.created, item.id
				o, e := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1 FOR UPDATE`, id))
				if e != nil {
					return e
				}
				if !now.Before(o.QueueDeadline) || !now.Before(o.Deadline) {
					if e = s.release(ctx, tx, id, "cancelled", "queue_expired"); e != nil {
						return e
					}
					continue
				}
				var grantOK bool
				if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_grants WHERE id=$1 AND revision=$2 AND status='active')`, o.GrantID, o.GrantRevision).Scan(&grantOK); e != nil {
					return e
				}
				if !grantOK || eligible(ctx, tx, o.UserID) != nil {
					if e = s.release(ctx, tx, id, "cancelled", "authorization_revoked"); e != nil {
						return e
					}
					continue
				}
				if e = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
					return e
				}
				if !now.Before(o.QueueDeadline) || !now.Before(o.Deadline) {
					if e = s.release(ctx, tx, id, "cancelled", "queue_expired"); e != nil {
						return e
					}
					continue
				}
				var b []byte
				if e = tx.QueryRowContext(ctx, `SELECT config FROM qq_image_configs WHERE id=$1`, o.ConfigID).Scan(&b); e != nil {
					return e
				}
				var c Config
				if e = json.Unmarshal(b, &c); e != nil {
					return e
				}
				var global, group int
				if e = tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE data->'Identity'->>'group_id'=$1 AND data->'Identity'->>'app_id'=$2) FROM qq_image_orders WHERE status IN ('dispatching','generating','reconciling') AND funds_status='held'`, o.Identity.GroupID, o.Identity.AppID).Scan(&global, &group); e != nil {
					return e
				}
				if global >= c.GlobalConcurrency || group >= c.GroupConcurrency {
					continue
				}
				lt := token(16)
				if _, e = tx.ExecContext(ctx, `UPDATE qq_image_orders SET status='dispatching',lease_token=$2,lease_until=$3 WHERE id=$1`, id, lt, now.Add(lease)); e != nil {
					return e
				}
				if e = audit(ctx, tx, 0, "order.dispatch", id, ""); e != nil {
					return e
				}
				out, e = scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1`, id))
				return e
			}
			if len(items) < 100 {
				return nil
			}
		}
	})
	return out, e
}
func (s *Store) CompleteGeneration(ctx context.Context, id, leaseToken string, a Artifact) (*Order, error) {
	if len(a.Data) == 0 || len(a.Data) > 20<<20 || a.Width <= 0 || a.Height <= 0 || int64(a.Width)*int64(a.Height) > 20000000 || !strings.HasPrefix(a.MIME, "image/") {
		return nil, ErrInvalid
	}
	// Persist the validated result privately first. A crash or temporary account
	// restriction before capture can recover this artifact without another POST.
	if e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		var status, storedToken, website string
		var configID int64
		if e := tx.QueryRowContext(ctx, `SELECT funds_status,lease_token,website_status,config_id FROM qq_image_orders WHERE id=$1 FOR UPDATE`, id).Scan(&status, &storedToken, &website, &configID); e != nil {
			return normalizeError(e)
		}
		if status == "captured" || status == "refunded" {
			return nil
		}
		if leaseToken == "" || storedToken != leaseToken {
			return ErrConflict
		}
		if website == "expired" {
			return ErrExpired
		}
		var raw []byte
		if e := tx.QueryRowContext(ctx, `SELECT config FROM qq_image_configs WHERE id=$1`, configID).Scan(&raw); e != nil {
			return e
		}
		var config Config
		if e := json.Unmarshal(raw, &config); e != nil {
			return e
		}
		// Private staging is bounded even if its owner never regains eligibility.
		// Publishing below starts the owner's full promised retention afresh.
		expires := now.Add(time.Duration(config.RetentionDays) * 24 * time.Hour)
		_, e := tx.ExecContext(ctx, `UPDATE qq_image_orders SET artifact=$2,artifact_mime=$3,artifact_sha256=$4,artifact_width=$5,artifact_height=$6,artifact_expires_at=$7 WHERE id=$1 AND artifact IS NULL`, id, a.Data, a.MIME, digest(string(a.Data)), a.Width, a.Height, expires)
		return e
	}); e != nil {
		return nil, e
	}
	o, e := s.captureArtifact(ctx, id, leaseToken)
	if errors.Is(e, ErrConflict) {
		if pe := s.publishLateArtifact(ctx, id); pe == nil {
			return s.GetOrder(ctx, id, 0)
		}
	}
	return o, e
}

func (s *Store) captureArtifact(ctx context.Context, id, leaseToken string) (*Order, error) {
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		o, e := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1 FOR UPDATE`, id))
		if e != nil {
			return e
		}
		if o.FundsStatus == "captured" && o.Status == "succeeded" {
			return nil
		}
		if o.FundsStatus != "held" {
			return ErrConflict
		}
		if !now.Before(o.Deadline) {
			return ErrExpired
		}
		if o.LeaseToken != leaseToken || leaseToken == "" || o.Status == "queued" {
			return ErrConflict
		}
		// API charging may hold the same wallet row. Re-read authoritative time and
		// eligibility after acquiring that lock; a pre-wait timestamp is insufficient.
		var lockedUser int64
		if e = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, o.UserID).Scan(&lockedUser); e != nil {
			return normalizeError(e)
		}
		if e = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return e
		}
		if !now.Before(o.Deadline) {
			return ErrExpired
		}
		if e = activeOwner(ctx, tx, o.UserID); e != nil {
			return e
		}
		var amount, hold string
		var user int64
		if e = tx.QueryRowContext(ctx, `SELECT user_id,amount::text,status FROM qq_image_holds WHERE order_id=$1 FOR UPDATE`, id).Scan(&user, &amount, &hold); e != nil {
			return e
		}
		if hold != "held" || user != o.UserID || amount != o.Price {
			return ErrConflict
		}
		r, e := tx.ExecContext(ctx, `UPDATE users SET frozen_balance=frozen_balance-$2::numeric,updated_at=clock_timestamp() WHERE id=$1 AND frozen_balance>=$2::numeric AND status='active' AND deleted_at IS NULL`, user, amount)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrForbidden
		}
		var b []byte
		if e = tx.QueryRowContext(ctx, `SELECT config FROM qq_image_configs WHERE id=$1`, o.ConfigID).Scan(&b); e != nil {
			return e
		}
		var c Config
		if e = json.Unmarshal(b, &c); e != nil {
			return e
		}
		var exists bool
		if e = tx.QueryRowContext(ctx, `SELECT artifact IS NOT NULL FROM qq_image_orders WHERE id=$1`, id).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return ErrInvalid
		}
		expires := now.Add(time.Duration(c.RetentionDays) * 24 * time.Hour)
		if _, e = tx.ExecContext(ctx, `UPDATE qq_image_holds SET status='captured' WHERE order_id=$1`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE qq_image_orders SET funds_status='captured',status='succeeded',website_status='available',artifact_expires_at=$2,error='',lease_until=NULL WHERE id=$1`, id, expires); e != nil {
			return e
		}
		if e = recordMoney(ctx, tx, id, user, amount, "capture"); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO qq_image_deliveries(id,order_id,reply_message_id) VALUES($1,$2,$3)`, token(16), id, o.ReplyMessageID); e != nil {
			return e
		}
		return audit(ctx, tx, user, "order.capture", id, "")
	})
	if e != nil {
		return nil, e
	}
	return s.GetOrder(ctx, id, 0)
}
func (s *Store) FailGeneration(ctx context.Context, id, leaseToken, reason string) (*Order, error) {
	e := s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		o, e := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1 FOR UPDATE`, id))
		if e != nil {
			return e
		}
		if o.FundsStatus == "released" {
			return nil
		}
		if o.LeaseToken != leaseToken || leaseToken == "" {
			return ErrConflict
		}
		return s.release(ctx, tx, id, "failed", reason)
	})
	if e != nil {
		return nil, e
	}
	return s.GetOrder(ctx, id, 0)
}
func (s *Store) MarkReconciling(ctx context.Context, id, leaseToken, reason string) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		r, e := tx.ExecContext(ctx, `UPDATE qq_image_orders SET status='reconciling',error=$3 WHERE id=$1 AND lease_token=$2 AND funds_status='held' AND status IN ('dispatching','generating','reconciling')`, id, leaseToken, reason)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrConflict
		}
		return audit(ctx, tx, 0, "order.reconciling", id, reason)
	})
}
func (s *Store) RefundOrder(ctx context.Context, id string, actorID int64, reason string) (*Order, error) {
	if strings.TrimSpace(reason) == "" || len(reason) > 500 {
		return nil, ErrInvalid
	}
	if e := s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		var status string
		if e := tx.QueryRowContext(ctx, `SELECT status FROM qq_image_holds WHERE order_id=$1 FOR UPDATE`, id).Scan(&status); e != nil {
			return normalizeError(e)
		}
		if status != "captured" && status != "refunded" {
			return ErrConflict
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO qq_image_refund_intents(order_id,actor_id,reason) VALUES($1,$2,$3) ON CONFLICT(order_id) DO NOTHING`, id, actorID, reason)
		return e
	}); e != nil {
		return nil, e
	}
	return s.applyRefund(ctx, id)
}

func (s *Store) applyRefund(ctx context.Context, id string) (*Order, error) {
	e := s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		var actorID int64
		var reason string
		if e := tx.QueryRowContext(ctx, `SELECT actor_id,reason FROM qq_image_refund_intents WHERE order_id=$1`, id).Scan(&actorID, &reason); e != nil {
			return normalizeError(e)
		}
		var user int64
		var amount, status string
		if e := tx.QueryRowContext(ctx, `SELECT user_id,amount::text,status FROM qq_image_holds WHERE order_id=$1 FOR UPDATE`, id).Scan(&user, &amount, &status); e != nil {
			return normalizeError(e)
		}
		if status == "refunded" {
			_, e := tx.ExecContext(ctx, `UPDATE qq_image_refund_intents SET status='completed',completed_at=clock_timestamp() WHERE order_id=$1`, id)
			return e
		}
		if status != "captured" {
			return ErrConflict
		}
		if _, e := tx.ExecContext(ctx, `UPDATE users SET balance=balance+$2::numeric,updated_at=clock_timestamp() WHERE id=$1`, user, amount); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_holds SET status='refunded' WHERE order_id=$1`, id); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_orders SET funds_status='refunded',delivery_status='blocked' WHERE id=$1`, id); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_deliveries SET status='blocked' WHERE order_id=$1 AND status='pending'`, id); e != nil {
			return e
		}
		if e := recordMoney(ctx, tx, id, user, amount, "refund"); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_refund_intents SET status='completed',completed_at=clock_timestamp() WHERE order_id=$1`, id); e != nil {
			return e
		}
		return audit(ctx, tx, actorID, "order.refund", id, reason)
	})
	if e != nil {
		return nil, e
	}
	return s.GetOrder(ctx, id, 0)
}
func (s *Store) recoverExpired(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_orders SET status='reconciling',error='dispatch_result_unknown' WHERE status IN ('dispatching','generating') AND lease_until<$1 AND funds_status='held'`, now); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO qq_image_audit(actor_id,action,object_id,reason) SELECT 0,'order.reconcile_attention',o.id,'unknown_result_over_30_minutes' FROM qq_image_orders o WHERE o.status='reconciling' AND o.funds_status='held' AND EXISTS(SELECT 1 FROM qq_image_audit d WHERE d.object_id=o.id AND d.action='order.dispatch' AND d.created_at<=$1::timestamptz-interval '30 minutes') AND NOT EXISTS(SELECT 1 FROM qq_image_audit a WHERE a.object_id=o.id AND a.action='order.reconcile_attention')`, now); e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, `SELECT id,status FROM qq_image_orders WHERE funds_status='held' AND (deadline<=$1 OR (status='queued' AND queue_deadline<=$1)) ORDER BY id`, now)
		if e != nil {
			return e
		}
		type candidate struct{ id, status string }
		items := []candidate{}
		for rows.Next() {
			var v candidate
			if e = rows.Scan(&v.id, &v.status); e != nil {
				_ = rows.Close()
				return e
			}
			items = append(items, v)
		}
		_ = rows.Close()
		for _, v := range items {
			status := "closed_unknown"
			if v.status == "queued" {
				status = "cancelled"
			}
			if e = s.release(ctx, tx, v.id, status, "deadline_expired"); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE qq_image_deliveries SET status='unknown',error='delivery_result_unknown' WHERE status='sending' AND lease_until<$1`, now); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE qq_image_orders o SET delivery_status='unknown' WHERE EXISTS(SELECT 1 FROM qq_image_deliveries d WHERE d.order_id=o.id AND d.status='unknown') AND o.delivery_status='sending'`); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE qq_image_orders o SET website_status='expired',artifact=NULL WHERE artifact IS NOT NULL AND (artifact_expires_at<=$1 OR (website_status='pending' AND artifact_expires_at IS NULL AND deadline + (SELECT (c.config->>'retention_days')::int * interval '1 day' FROM qq_image_configs c WHERE c.id=o.config_id)<=$1))`, now)
		return e
	})
}
func (s *Store) GetArtifact(ctx context.Context, id string, userID int64) (*Artifact, error) {
	var a Artifact
	e := s.queryer(ctx).QueryRowContext(ctx, `SELECT o.artifact,o.artifact_mime,o.artifact_width,o.artifact_height,o.artifact_sha256 FROM qq_image_orders o JOIN users u ON u.id=o.user_id WHERE o.id=$1 AND o.user_id=$2 AND u.status='active' AND u.deleted_at IS NULL AND o.website_status='available' AND o.artifact_expires_at>clock_timestamp() AND o.artifact IS NOT NULL`, id, userID).Scan(&a.Data, &a.MIME, &a.Width, &a.Height, &a.SHA256)
	return &a, normalizeError(e)
}
func (s *Store) RequestReconcile(ctx context.Context, id string, actorID int64) error {
	return s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		var status string
		if e := tx.QueryRowContext(ctx, `SELECT status FROM qq_image_orders WHERE id=$1`, id).Scan(&status); e != nil {
			return normalizeError(e)
		}
		if status != "reconciling" && status != "dispatching" && status != "generating" {
			return ErrConflict
		}
		return audit(ctx, tx, actorID, "order.reconcile_requested", id, "provider_has_no_query_capability")
	})
}
