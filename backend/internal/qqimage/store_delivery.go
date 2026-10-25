package qqimage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

func (s *Store) ClaimEvent(ctx context.Context, key string) (bool, error) {
	return s.ClaimEventPayload(ctx, key, "")
}
func (s *Store) ClaimEventPayload(ctx context.Context, key, payloadHash string) (bool, error) {
	if key == "" || len(key) > 2048 {
		return false, ErrInvalid
	}
	var claimed bool
	e := s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		var prior string
		e := tx.QueryRowContext(ctx, `SELECT request_hash FROM qq_image_events WHERE event_key=$1`, digest(key)).Scan(&prior)
		if e == nil {
			if prior != payloadHash {
				return ErrConflict
			}
			return nil
		}
		if e != sql.ErrNoRows {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO qq_image_events(event_key,request_hash) VALUES($1,$2)`, digest(key), payloadHash); e != nil {
			return e
		}
		claimed = true
		return nil
	})
	return claimed, e
}
func deliveryAllowed(ctx context.Context, tx *sql.Tx, o *Order) error {
	var ok bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_grants g JOIN users u ON u.id=g.user_id WHERE g.id=$1 AND g.revision=$2 AND g.status='active' AND u.status='active' AND u.deleted_at IS NULL)`, o.GrantID, o.GrantRevision).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok || o.FundsStatus != "captured" || o.ArtifactExpiresAt == nil {
		return ErrForbidden
	}
	var revoked bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_credential_revocations WHERE config_id=$1 AND kind='bot')`, o.ConfigID).Scan(&revoked); e != nil {
		return e
	}
	if revoked {
		return ErrForbidden
	}
	var data []byte
	if e = tx.QueryRowContext(ctx, `SELECT config FROM qq_image_configs ORDER BY id DESC LIMIT 1`).Scan(&data); e != nil {
		return e
	}
	var current Config
	if e = json.Unmarshal(data, &current); e != nil {
		return e
	}
	// Disabling new orders preserves old deliveries; removing the group or app
	// explicitly revokes platform delivery permission, including outstanding URLs.
	if current.AppID != o.Identity.AppID || !contains(current.AllowedGroups, o.Identity.GroupID) {
		return ErrForbidden
	}
	return nil
}
func (s *Store) RequestRedelivery(ctx context.Context, i Identity, id, replyMessageID string) (*Order, error) {
	if e := i.Validate(); e != nil {
		return nil, e
	}
	if replyMessageID == "" || len(replyMessageID) > 512 {
		return nil, ErrInvalid
	}
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		o, e := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1 FOR UPDATE`, id))
		if e != nil {
			return e
		}
		if o.Identity.Key() != i.Key() {
			return ErrForbidden
		}
		if e = deliveryAllowed(ctx, tx, o); e != nil {
			return e
		}
		if !now.Before(*o.ArtifactExpiresAt) {
			return ErrExpired
		}
		if e = s.rate(ctx, tx, "redelivery:"+id, 1, time.Minute, now); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO qq_image_deliveries(id,order_id,reply_message_id,created_at) VALUES($1,$2,$3,$4)`, token(16), id, replyMessageID, now); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE qq_image_orders SET delivery_status='pending' WHERE id=$1`, id)
		return e
	})
	if e != nil {
		return nil, e
	}
	return s.GetOrderForIdentity(ctx, i, id)
}
func (s *Store) ClaimDelivery(ctx context.Context, owner string, lease time.Duration) (*Delivery, error) {
	if lease <= 0 || lease > 5*time.Minute {
		lease = time.Minute
	}
	var out *Delivery
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		rows, e := tx.QueryContext(ctx, `SELECT id FROM qq_image_deliveries WHERE status='pending' ORDER BY created_at,id LIMIT 100`)
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				_ = rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		_ = rows.Close()
		for _, id := range ids {
			var oid, msg string
			var created time.Time
			if e = tx.QueryRowContext(ctx, `SELECT order_id,reply_message_id,created_at FROM qq_image_deliveries WHERE id=$1`, id).Scan(&oid, &msg, &created); e != nil {
				return e
			}
			o, e := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1 FOR UPDATE`, oid))
			if e != nil {
				return e
			}
			status := ""
			if e = deliveryAllowed(ctx, tx, o); e != nil {
				status = "blocked"
			} else if !now.Before(*o.ArtifactExpiresAt) {
				status = "expired"
			}
			var data []byte
			if e = tx.QueryRowContext(ctx, `SELECT data FROM qq_image_orders WHERE id=$1`, oid).Scan(&data); e != nil {
				return e
			}
			var d storedOrder
			if e = json.Unmarshal(data, &d); e != nil {
				return e
			}
			seq := 2
			if msg == d.ReplyMessageID {
				created = d.ReplyCreatedAt
				seq = 2
			}
			if now.Sub(created) >= 5*time.Minute {
				status = "expired"
			}
			if status != "" {
				if _, e = tx.ExecContext(ctx, `UPDATE qq_image_deliveries SET status=$2 WHERE id=$1`, id, status); e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, `UPDATE qq_image_orders SET delivery_status=$2 WHERE id=$1`, oid, status); e != nil {
					return e
				}
				continue
			}
			lt := token(16)
			if _, e = tx.ExecContext(ctx, `UPDATE qq_image_deliveries SET status='sending',lease_token=$2,lease_until=$3 WHERE id=$1`, id, lt, now.Add(lease)); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE qq_image_orders SET delivery_status='sending' WHERE id=$1`, oid); e != nil {
				return e
			}
			var artifactBytes int64
			if e = tx.QueryRowContext(ctx, `SELECT octet_length(artifact) FROM qq_image_orders WHERE id=$1`, oid).Scan(&artifactBytes); e != nil {
				return e
			}
			out = &Delivery{ID: id, Order: o, LeaseToken: lt, ReplyMessageID: msg, ReplySequence: seq, ReplyCreatedAt: created, ArtifactBytes: artifactBytes}
			return nil
		}
		return nil
	})
	return out, e
}
func (s *Store) CompleteDelivery(ctx context.Context, id, leaseToken, status, reason string) error {
	if !contains([]string{"sent", "site_only", "retryable_failed", "unknown", "blocked", "expired"}, status) {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx *sql.Tx, _ time.Time) error {
		var oid, current, lt string
		if e := tx.QueryRowContext(ctx, `SELECT order_id,status,lease_token FROM qq_image_deliveries WHERE id=$1 FOR UPDATE`, id).Scan(&oid, &current, &lt); e != nil {
			return normalizeError(e)
		}
		if lt != leaseToken || leaseToken == "" {
			return ErrConflict
		}
		if current == status {
			return nil
		}
		if current != "sending" {
			return ErrConflict
		}
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_deliveries SET status=$2,error=$3,lease_until=NULL WHERE id=$1`, id, status, reason); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE qq_image_orders SET delivery_status=$2 WHERE id=$1`, oid, status); e != nil {
			return e
		}
		return audit(ctx, tx, 0, "delivery."+status, oid, reason)
	})
}
func (s *Store) CreateMediaGrant(ctx context.Context, deliveryID, leaseToken string) (string, error) {
	var plain string
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		var oid string
		if e := tx.QueryRowContext(ctx, `SELECT order_id FROM qq_image_deliveries WHERE id=$1 AND lease_token=$2 AND status='sending' AND lease_until>clock_timestamp()`, deliveryID, leaseToken).Scan(&oid); e != nil {
			return ErrForbidden
		}
		o, e := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1`, oid))
		if e != nil {
			return e
		}
		if e = deliveryAllowed(ctx, tx, o); e != nil {
			return e
		}
		if !now.Before(*o.ArtifactExpiresAt) {
			return ErrExpired
		}
		plain = token(32)
		var artifactBytes int64
		if e = tx.QueryRowContext(ctx, `SELECT octet_length(artifact) FROM qq_image_orders WHERE id=$1`, oid).Scan(&artifactBytes); e != nil {
			return e
		}
		if artifactBytes > 5*1024*1024 {
			return ErrInvalid
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO qq_image_media_grants(token_hash,order_id,grant_id,grant_revision,expires_at) VALUES($1,$2,$3,$4,$5)`, digest(plain), oid, o.GrantID, o.GrantRevision, now.Add(120*time.Second))
		return e
	})
	return plain, e
}
func (s *Store) ReadMedia(ctx context.Context, plain string) (*Artifact, error) {
	if len(plain) != 64 {
		return nil, ErrNotFound
	}
	var out *Artifact
	e := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		var oid, gid string
		var revision int64
		var expires time.Time
		var fetches int
		if e := tx.QueryRowContext(ctx, `SELECT order_id,grant_id,grant_revision,expires_at,fetches FROM qq_image_media_grants WHERE token_hash=$1 FOR UPDATE`, digest(plain)).Scan(&oid, &gid, &revision, &expires, &fetches); e != nil {
			return normalizeError(e)
		}
		if !now.Before(expires) || fetches >= 3 {
			return ErrExpired
		}
		o, e := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM qq_image_orders WHERE id=$1`, oid))
		if e != nil {
			return e
		}
		if o.GrantID != gid || o.GrantRevision != revision {
			return ErrForbidden
		}
		if e = deliveryAllowed(ctx, tx, o); e != nil {
			return e
		}
		if !now.Before(*o.ArtifactExpiresAt) {
			return ErrExpired
		}
		var a Artifact
		if e = tx.QueryRowContext(ctx, `SELECT artifact,artifact_mime,artifact_width,artifact_height,artifact_sha256 FROM qq_image_orders WHERE id=$1 AND artifact IS NOT NULL`, oid).Scan(&a.Data, &a.MIME, &a.Width, &a.Height, &a.SHA256); e != nil {
			return normalizeError(e)
		}
		if _, e = tx.ExecContext(ctx, `UPDATE qq_image_media_grants SET fetches=fetches+1 WHERE token_hash=$1`, digest(plain)); e != nil {
			return e
		}
		out = &a
		return nil
	})
	return out, e
}
