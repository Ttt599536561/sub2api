package qqimage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"time"
)

// Discovery never exposes the code hash or a QQ member identity. Command is
// populated only by StartDiscovery and is never persisted.
type Discovery struct {
	ID        string              `json:"id"`
	Status    string              `json:"status"`
	AppID     string              `json:"app_id"`
	ExpiresAt time.Time           `json:"expires_at"`
	Command   string              `json:"command,omitempty"`
	Candidate *DiscoveryCandidate `json:"candidate,omitempty"`
	configID  int64
}
type DiscoveryCandidate struct {
	GroupOpenID string    `json:"group_openid"`
	EventID     string    `json:"event_id"`
	ReceivedAt  time.Time `json:"received_at"`
}

const discoveryColumns = `id,status,app_id,expires_at,config_id,group_openid,event_id,received_at`

func scanDiscovery(row interface{ Scan(...any) error }) (*Discovery, error) {
	d := &Discovery{}
	var group, event sql.NullString
	var at sql.NullTime
	if err := row.Scan(&d.ID, &d.Status, &d.AppID, &d.ExpiresAt, &d.configID, &group, &event, &at); err != nil {
		return nil, normalizeError(err)
	}
	if group.Valid && event.Valid && at.Valid {
		d.Candidate = &DiscoveryCandidate{GroupOpenID: group.String, EventID: event.String, ReceivedAt: at.Time}
	}
	return d, nil
}
func refreshDiscoveries(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE qq_image_discoveries SET status=CASE WHEN expires_at<=$1 THEN 'expired' ELSE 'closed' END WHERE status IN ('waiting','found') AND (expires_at<=$1 OR config_id<>(SELECT id FROM qq_image_configs ORDER BY id DESC LIMIT 1))`, now)
	return err
}

// BotRuntimeConfig decrypts only the current QQ secret. Disabled generation and
// missing, revoked or undecryptable upstream API keys cannot block discovery.
func (s *Store) BotRuntimeConfig(ctx context.Context) (*RuntimeConfig, error) {
	return s.botRuntimeConfig(ctx, s.queryer(ctx))
}
func (s *Store) botRuntimeConfig(ctx context.Context, q storeQueryer) (*RuntimeConfig, error) {
	c, bot, _, err := s.configFrom(q.QueryRowContext(ctx, `SELECT id,config,bot_secret,''::bytea FROM qq_image_configs ORDER BY id DESC LIMIT 1`))
	if err != nil {
		return nil, err
	}
	var revoked bool
	if err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_credential_revocations WHERE config_id=$1 AND kind='bot')`, c.ID).Scan(&revoked); err != nil {
		return nil, err
	}
	if revoked || len(bot) == 0 || c.AppID == "" {
		return nil, ErrDisabled
	}
	secret, err := s.decrypt(bot)
	if err != nil {
		return nil, err
	}
	return &RuntimeConfig{Config: *c, BotSecret: secret}, nil
}
func (s *Store) StartDiscovery(ctx context.Context, actorID int64) (*Discovery, error) {
	if actorID <= 0 {
		return nil, ErrForbidden
	}
	var result *Discovery
	err := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		c, err := s.botRuntimeConfig(ctx, tx)
		if err != nil {
			return err
		}
		if err = refreshDiscoveries(ctx, tx, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE qq_image_discoveries SET status='closed' WHERE status IN ('waiting','found')`); err != nil {
			return err
		}
		code := token(24)
		id := token(16)
		result, err = scanDiscovery(tx.QueryRowContext(ctx, `INSERT INTO qq_image_discoveries(id,config_id,app_id,status,code_hash,expires_at) VALUES($1,$2,$3,'waiting',$4,$5) RETURNING `+discoveryColumns, id, c.ID, c.AppID, digest(code), now.Add(5*time.Minute)))
		if err != nil {
			return err
		}
		result.Command = "@机器人 /识别群 " + code
		return audit(ctx, tx, actorID, "discovery.start", id, "")
	})
	return result, err
}
func (s *Store) GetDiscovery(ctx context.Context) (*Discovery, error) {
	var result *Discovery
	err := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		if err := refreshDiscoveries(ctx, tx, now); err != nil {
			return err
		}
		var err error
		result, err = scanDiscovery(tx.QueryRowContext(ctx, `SELECT `+discoveryColumns+` FROM qq_image_discoveries ORDER BY created_at DESC,id DESC LIMIT 1`))
		if err == ErrNotFound {
			return nil
		}
		return err
	})
	return result, err
}
func validDiscoveryID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && len(id) == 32
}
func (s *Store) CloseDiscovery(ctx context.Context, id string, actorID int64) (*Discovery, error) {
	if actorID <= 0 {
		return nil, ErrForbidden
	}
	if !validDiscoveryID(id) {
		return nil, ErrInvalid
	}
	var result *Discovery
	var stateErr error
	err := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		if err := refreshDiscoveries(ctx, tx, now); err != nil {
			return err
		}
		var err error
		result, err = scanDiscovery(tx.QueryRowContext(ctx, `SELECT `+discoveryColumns+` FROM qq_image_discoveries WHERE id=$1`, id))
		if err != nil {
			return err
		}
		if result.Status != "waiting" && result.Status != "found" {
			stateErr = ErrConflict
			return nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE qq_image_discoveries SET status='closed' WHERE id=$1`, id); err != nil {
			return err
		}
		result.Status = "closed"
		return audit(ctx, tx, actorID, "discovery.close", id, "")
	})
	if err == nil {
		err = stateErr
	}
	return result, err
}

// RecordDiscovery is called only for an authenticated original platform event.
// It runs before group admission, performs no network calls, and commits only a
// minimal first candidate plus hashed replay keys under the shared domain lock.
func (s *Store) RecordDiscovery(ctx context.Context, configID int64, event *QQEvent, code string) error {
	decoded, err := hex.DecodeString(code)
	if err != nil || len(decoded) != 24 || len(code) != 48 {
		return nil
	}
	if event == nil || event.Identity.Validate() != nil || event.EventID == "" || len(event.EventID) > 256 || event.MessageID == "" || len(event.MessageID) > 256 {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		guarded := context.WithValue(ctx, storeTxKey{}, storeTransaction{store: s, tx: tx})
		if err := checkGatewayGuard(guarded); err != nil {
			return err
		}

		if age := now.Sub(event.CreatedAt); age < -time.Minute || age >= 5*time.Minute {
			return ErrExpired
		}
		if err := refreshDiscoveries(ctx, tx, now); err != nil {
			return err
		}
		var currentID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM qq_image_configs ORDER BY id DESC LIMIT 1`).Scan(&currentID); err != nil {
			return err
		}
		if currentID != configID {
			return ErrConflict
		}
		d, err := scanDiscovery(tx.QueryRowContext(ctx, `SELECT `+discoveryColumns+` FROM qq_image_discoveries WHERE status='waiting' AND config_id=$1 AND app_id=$2 AND code_hash=$3`, configID, event.Identity.AppID, digest(code)))
		if err == ErrNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		eventHash := digest(event.Identity.AppID + "\x00event\x00" + event.EventID)
		messageHash := digest(event.Identity.AppID + "\x00message\x00" + event.MessageID)
		var replay bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM qq_image_discovery_events WHERE event_hash=$1 OR message_hash=$2)`, eventHash, messageHash).Scan(&replay); err != nil {
			return err
		}
		if replay {
			return nil
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO qq_image_discovery_events(event_hash,message_hash,discovery_id) VALUES($1,$2,$3)`, eventHash, messageHash, d.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE qq_image_discoveries SET status='found',group_openid=$2,event_id=$3,received_at=$4 WHERE id=$1`, d.ID, event.Identity.GroupID, event.EventID, now); err != nil {
			return err
		}
		return audit(ctx, tx, 0, "discovery.found", d.ID, "")
	})
}
func (s *Store) ConfirmDiscovery(ctx context.Context, id string, actorID int64) (*Discovery, error) {
	if actorID <= 0 {
		return nil, ErrForbidden
	}
	if !validDiscoveryID(id) {
		return nil, ErrInvalid
	}
	var result *Discovery
	var stateErr error
	err := s.transaction(ctx, func(tx *sql.Tx, now time.Time) error {
		if err := refreshDiscoveries(ctx, tx, now); err != nil {
			return err
		}
		var err error
		result, err = scanDiscovery(tx.QueryRowContext(ctx, `SELECT `+discoveryColumns+` FROM qq_image_discoveries WHERE id=$1`, id))
		if err != nil {
			return err
		}
		if result.Status != "found" || result.Candidate == nil {
			stateErr = ErrConflict
			return nil
		}
		c, bot, key, err := s.configFrom(tx.QueryRowContext(ctx, `SELECT id,config,bot_secret,api_key FROM qq_image_configs ORDER BY id DESC LIMIT 1`))
		if err != nil {
			return err
		}
		if c.ID != result.configID || c.AppID != result.AppID {
			stateErr = ErrConflict
			return nil
		}
		if !contains(c.AllowedGroups, result.Candidate.GroupOpenID) {
			c.AllowedGroups = append(c.AllowedGroups, result.Candidate.GroupOpenID)
		}
		c.ID = 0
		c.BotSecretConfigured = false
		c.APIKeyConfigured = false
		c.EncryptionAvailable = false
		if _, err = tx.ExecContext(ctx, `INSERT INTO qq_image_configs(config,bot_secret,api_key) VALUES($1,$2,$3)`, jsonBytes(c), bot, key); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE qq_image_discoveries SET status='confirmed' WHERE id=$1`, id); err != nil {
			return err
		}
		result.Status = "confirmed"
		return audit(ctx, tx, actorID, "discovery.confirm", id, "")
	})
	if err == nil {
		err = stateErr
	}
	return result, err
}
