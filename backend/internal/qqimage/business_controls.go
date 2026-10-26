package qqimage

import (
	"context"
	"database/sql"
	"encoding/json"
)

// BusinessControls reads the canonical settings on every admission. Missing keys
// retain existing installations' behavior; database errors never become an allow.
// It deliberately does not use the public settings cache or control the worker.
func (s *Store) BusinessControls(ctx context.Context) (binding, image bool, err error) {
	if s.disableNewAdmissions {
		return false, false, nil
	}
	err = s.queryer(ctx).QueryRowContext(ctx, `SELECT
		COALESCE((SELECT value <> 'false' FROM settings WHERE key='qq_binding_enabled'),true),
		COALESCE((SELECT value <> 'false' FROM settings WHERE key='qq_image_enabled'),true)`).Scan(&binding, &image)
	return binding, image, err
}

func (s *Store) requireBindingAdmission(ctx context.Context, tx *sql.Tx, i Identity) error {
	binding, _, err := s.BusinessControls(context.WithValue(ctx, storeTxKey{}, storeTransaction{s, tx}))
	if err != nil {
		return err
	}
	if !binding {
		return ErrDisabled
	}
	var data []byte
	if err = tx.QueryRowContext(ctx, `SELECT config FROM qq_image_configs ORDER BY id DESC LIMIT 1`).Scan(&data); err != nil {
		if err == sql.ErrNoRows {
			return ErrDisabled
		}
		return err
	}
	var c Config
	if err = json.Unmarshal(data, &c); err != nil {
		return err
	}
	if !c.Enabled || c.AppID != i.AppID || !contains(c.AllowedGroups, i.GroupID) {
		return ErrDisabled
	}
	return nil
}
