package qqimage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type ReplyTemplateSnapshot struct {
	SchemaVersion  int                       `json:"schema_version"`
	CatalogVersion int                       `json:"catalog_version"`
	Revision       int64                     `json:"revision"`
	Overrides      map[string]string         `json:"overrides"`
	Catalog        []ReplyTemplateDefinition `json:"catalog"`
	PublishedAt    *time.Time                `json:"-"`
}
type ReplyTemplateRevisionInfo struct {
	Revision             int64     `json:"revision"`
	CreatedAt            time.Time `json:"created_at"`
	ActorID              int64     `json:"actor_id"`
	RestoredFromRevision *int64    `json:"restored_from_revision,omitempty"`
}
type ReplyTemplateRevision struct {
	ReplyTemplateRevisionInfo
	Overrides map[string]string `json:"overrides"`
}
type PublishedReplyTemplates struct {
	SchemaVersion  int               `json:"schema_version"`
	CatalogVersion int               `json:"catalog_version"`
	Revision       int64             `json:"revision"`
	PublishedAt    *time.Time        `json:"published_at"`
	Templates      map[string]string `json:"templates"`
}

func newReplyTemplateSnapshot(revision int64, overrides map[string]string, at *time.Time) *ReplyTemplateSnapshot {
	return &ReplyTemplateSnapshot{SchemaVersion: 1, CatalogVersion: 1, Revision: revision, Overrides: overrides, Catalog: ReplyTemplateCatalog(), PublishedAt: at}
}
func (s *ReplyTemplateSnapshot) Published() PublishedReplyTemplates {
	out := PublishedReplyTemplates{SchemaVersion: 1, CatalogVersion: 1, Revision: s.Revision, PublishedAt: s.PublishedAt, Templates: map[string]string{}}
	for _, def := range ReplyTemplateCatalog()[:6] {
		out.Templates[def.Key] = def.DefaultText
		if value, ok := s.Overrides[def.Key]; ok {
			if clean, _, errs := ValidateReplyTemplate(def.Key, value); len(errs) == 0 {
				out.Templates[def.Key] = clean
			}
		}
	}
	return out
}

// Read outside ProcessEvent: a template read failure must never poison an
// already-open business transaction. The command caller falls back to defaults.
func (s *Store) GetReplyTemplates(ctx context.Context) (*ReplyTemplateSnapshot, error) {
	var revision int64
	var raw []byte
	var at *time.Time
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(s.active_revision,0),COALESCE(r.overrides,'{}'::jsonb),r.created_at FROM qq_reply_template_state s LEFT JOIN qq_reply_template_revisions r ON r.revision=s.active_revision WHERE s.id=1`).Scan(&revision, &raw, &at)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err = json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return newReplyTemplateSnapshot(revision, values, at), nil
}
func (s *Store) SaveReplyTemplates(ctx context.Context, expected int64, overrides map[string]string, actor int64) (*ReplyTemplateSnapshot, error) {
	clean, err := ValidateReplyTemplateOverrides(overrides)
	if err != nil {
		return nil, err
	}
	return s.writeReplyTemplates(ctx, expected, clean, actor, nil)
}
func (s *Store) RollbackReplyTemplates(ctx context.Context, expected, revision, actor int64) (*ReplyTemplateSnapshot, error) {
	if revision <= 0 {
		return nil, ErrConflict
	}
	return s.writeReplyTemplates(ctx, expected, nil, actor, &revision)
}
func (s *Store) writeReplyTemplates(ctx context.Context, expected int64, overrides map[string]string, actor int64, restore *int64) (*ReplyTemplateSnapshot, error) {
	if expected < 0 || actor <= 0 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(active_revision,0) FROM qq_reply_template_state WHERE id=1 FOR UPDATE`).Scan(&current); err != nil {
		return nil, err
	}
	if current != expected {
		return nil, ErrConflict
	}
	if restore != nil {
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT overrides FROM qq_reply_template_revisions WHERE revision=$1`, *restore).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConflict
		} else if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &overrides); err != nil {
			return nil, err
		}
		overrides, err = ValidateReplyTemplateOverrides(overrides)
		if err != nil {
			return nil, err
		}
	}
	revision := current + 1
	var at time.Time
	if err = tx.QueryRowContext(ctx, `INSERT INTO qq_reply_template_revisions(revision,overrides,actor_id,restored_from_revision) VALUES($1,$2,$3,$4) RETURNING created_at`, revision, jsonBytes(overrides), actor, restore).Scan(&at); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE qq_reply_template_state SET active_revision=$1 WHERE id=1`, revision); err != nil {
		return nil, err
	}
	action, reason := "reply_templates_save", ""
	if restore != nil {
		action = "reply_templates_rollback"
		reason = "revision:" + strconv.FormatInt(*restore, 10)
	}
	if err = audit(ctx, tx, actor, action, strconv.FormatInt(revision, 10), reason); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return newReplyTemplateSnapshot(revision, overrides, &at), nil
}
func (s *Store) ListReplyTemplateRevisions(ctx context.Context) ([]ReplyTemplateRevisionInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT revision,created_at,actor_id,restored_from_revision FROM qq_reply_template_revisions ORDER BY revision DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReplyTemplateRevisionInfo{}
	for rows.Next() {
		var value ReplyTemplateRevisionInfo
		if err = rows.Scan(&value.Revision, &value.CreatedAt, &value.ActorID, &value.RestoredFromRevision); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
func (s *Store) GetReplyTemplateRevision(ctx context.Context, revision int64) (*ReplyTemplateRevision, error) {
	var out ReplyTemplateRevision
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT revision,created_at,actor_id,restored_from_revision,overrides FROM qq_reply_template_revisions WHERE revision=$1`, revision).Scan(&out.Revision, &out.CreatedAt, &out.ActorID, &out.RestoredFromRevision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.Overrides); err != nil {
		return nil, err
	}
	return &out, nil
}
