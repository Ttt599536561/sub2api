package qqinvite

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"unicode/utf16"
)

type Store struct {
	db                     *sql.DB
	key                    []byte
	invalidatorMu          sync.RWMutex
	invalidateSubscription func(userID, groupID int64) error
}

func NewStore(db *sql.DB, key []byte) *Store { return &Store{db: db, key: append([]byte(nil), key...)} }

// Waiting on advisory locks or row leases must observe fresh committed rows,
// independently of the database's configured default transaction isolation.
func (s *Store) beginWrite(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

// Even a standalone lease UPDATE can encounter a stale RR snapshot while
// another worker commits. Commit the explicit RC claim before making Bot calls.
func (s *Store) scanWrite(ctx context.Context, query string, args []any, dest ...any) error {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = tx.QueryRowContext(ctx, query, args...).Scan(dest...); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) execWrite(ctx context.Context, query string, args ...any) error {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return tx.Commit()
}
func token() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func mapError(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	return e
}

var qqPattern = regexp.MustCompile(`^[1-9][0-9]{4,19}$`)

func validQQ(v string) bool    { return qqPattern.MatchString(v) }
func validGroup(v string) bool { return regexp.MustCompile(`^[1-9][0-9]{2,19}$`).MatchString(v) }
func (s *Store) crypt() (cipher.AEAD, error) {
	if len(s.key) != 32 {
		return nil, ErrEncryptionUnavailable
	}
	b, e := aes.NewCipher(s.key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}
func (s *Store) seal(v []byte) ([]byte, error) {
	a, e := s.crypt()
	if e != nil {
		return nil, e
	}
	n := make([]byte, a.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return nil, e
	}
	return a.Seal(n, n, v, []byte("qqinvite-config-v1")), nil
}
func (s *Store) unseal(v []byte) ([]byte, error) {
	a, e := s.crypt()
	if e != nil {
		return nil, e
	}
	if len(v) < a.NonceSize() {
		return nil, ErrEncryptionUnavailable
	}
	return a.Open(nil, v[:a.NonceSize()], v[a.NonceSize():], []byte("qqinvite-config-v1"))
}
func (s *Store) GetConfig(ctx context.Context) (Config, error) {
	c, e := s.RuntimeConfig(ctx)
	c = s.publicConfig(c)
	return c, e
}
func (s *Store) publicConfig(c Config) Config {
	c.OneBotTokenConfigured = c.OneBotToken != ""
	c.WebUITokenConfigured = c.WebUIToken != ""
	c.EventSecretConfigured = c.EventSecret != ""
	c.OneBotToken = ""
	c.WebUIToken = ""
	c.EventSecret = ""
	c.EncryptionAvailable = len(s.key) == 32
	return c
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) loadConfig(ctx context.Context, q queryRower) (Config, error) {
	var c Config
	var b, secret []byte
	var version int64
	e := q.QueryRowContext(ctx, `SELECT id,payload,secrets FROM qq_invite_config_versions ORDER BY id DESC LIMIT 1`).Scan(&version, &b, &secret)
	if errors.Is(e, sql.ErrNoRows) {
		c.EncryptionAvailable = len(s.key) == 32
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	plain, e := s.unseal(secret)
	if e != nil {
		return c, e
	}
	var secrets map[string]string
	if e = json.Unmarshal(plain, &secrets); e != nil {
		return c, e
	}
	c.Version = version
	c.OneBotToken = secrets["onebot"]
	c.WebUIToken = secrets["webui"]
	c.EventSecret = secrets["event"]
	return c, nil
}
func (s *Store) RuntimeConfig(ctx context.Context) (Config, error) { return s.loadConfig(ctx, s.db) }
func validURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func (s *Store) SaveConfig(ctx context.Context, c Config, actor int64) (Config, error) {
	tx, e := s.beginWrite(ctx)
	if e != nil {
		return Config{}, e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(81425301)`); e != nil {
		return Config{}, e
	}
	old, e := s.loadConfig(ctx, tx)
	if e != nil {
		return Config{}, e
	}
	if c.Version != old.Version {
		return Config{}, ErrConflict
	}
	if c.OneBotToken == "" {
		c.OneBotToken = old.OneBotToken
	}
	if c.WebUIToken == "" {
		c.WebUIToken = old.WebUIToken
	}
	if c.EventSecret == "" {
		c.EventSecret = old.EventSecret
	}
	if !validURL(c.OneBotURL) || !validURL(c.WebUIURL) || !validQQ(c.BotQQ) || len(c.OneBotToken) > 4096 || len(c.WebUIToken) > 4096 || len(c.EventSecret) > 4096 {
		return Config{}, ErrInvalid
	}
	if c.PublicBaseURL != "" && !validOrigin(c.PublicBaseURL) {
		return Config{}, ErrInvalid
	}
	if c.Enabled && (c.OneBotToken == "" || c.WebUIToken == "" || c.EventSecret == "" || c.PublicBaseURL == "") {
		return Config{}, ErrInvalid
	}
	if old.BotQQ != "" && old.BotQQ != c.BotQQ {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM qq_invite_records`).Scan(&n); e != nil {
			return Config{}, e
		}
		if n > 0 {
			return Config{}, ErrConflict
		}
	}
	plain, _ := json.Marshal(map[string]string{"onebot": c.OneBotToken, "webui": c.WebUIToken, "event": c.EventSecret})
	secret, e := s.seal(plain)
	if e != nil {
		return Config{}, e
	}
	pub := s.publicConfig(c)
	b, _ := json.Marshal(pub)
	if e = tx.QueryRowContext(ctx, `INSERT INTO qq_invite_config_versions(payload,secrets,actor_id) VALUES($1,$2,$3) RETURNING id`, b, secret, actor).Scan(&pub.Version); e != nil {
		return Config{}, e
	}
	if e = tx.Commit(); e != nil {
		return Config{}, e
	}
	return pub, nil
}
func audit(ctx context.Context, tx *sql.Tx, actor int64, action string, id int64, detail any) error {
	b, _ := json.Marshal(detail)
	_, e := tx.ExecContext(ctx, `INSERT INTO qq_invite_audit(actor_id,action,entity_id,detail) VALUES($1,$2,$3,$4)`, actor, action, id, b)
	return e
}
func pageBounds(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return (page - 1) * size, size
}
func hint(v string) string {
	if len(v) < 8 {
		return "****"
	}
	return v[:4] + "…" + v[len(v)-4:]
}
func checkTemplate(v string, private bool) bool {
	if strings.TrimSpace(v) == "" || len(utf16.Encode([]rune(v))) > 2000 {
		return false
	}
	if private {
		if !strings.Contains(v, "{code}") || !strings.Contains(v, "{url}") {
			return false
		}
	} else if strings.Contains(v, "{code}") {
		return false
	}
	remaining := strings.NewReplacer("{invitee}", "", "{inviter}", "", "{level}", "", "{code}", "", "{url}", "", "{plan_name}", "").Replace(v)
	return !strings.ContainsAny(remaining, "{}")
}

func validOrigin(raw string) bool {
	if !validURL(raw) {
		return false
	}
	u, e := url.Parse(raw)
	return e == nil && (u.Path == "" || u.Path == "/") && u.RawPath == "" && !u.ForceQuery
}
