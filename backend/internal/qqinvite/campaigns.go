package qqinvite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

func scanCampaign(row interface{ Scan(...any) error }) (Campaign, error) {
	var c Campaign
	var b []byte
	var baseline sql.NullTime
	var id, version int64
	var created time.Time
	e := row.Scan(&id, &version, &b, &baseline, &created)
	if e != nil {
		return c, mapError(e)
	}
	e = json.Unmarshal(b, &c)
	c.ID = id
	c.Version = version
	c.BaselineReady = baseline.Valid
	c.CreatedAt = created
	return c, e
}
func (s *Store) GetCampaign(ctx context.Context, id int64) (Campaign, error) {
	return scanCampaign(s.db.QueryRowContext(ctx, `SELECT id,version,payload,baseline_at,created_at FROM qq_invite_campaigns WHERE id=$1`, id))
}
func (s *Store) ListCampaigns(ctx context.Context) (CampaignList, error) {
	out := CampaignList{Items: []Campaign{}}
	rows, e := s.db.QueryContext(ctx, `SELECT id,version,payload,baseline_at,created_at FROM qq_invite_campaigns ORDER BY id DESC`)
	if e != nil {
		return out, e
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		c, e := scanCampaign(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, c)
	}
	out.Total = len(out.Items)
	return out, rows.Err()
}
func validateCampaign(c Campaign) error {
	if !checkTemplate(c.DeliveryPendingTemplate, false) {
		return ErrInvalid
	}
	if strings.TrimSpace(c.Name) == "" || len(c.Name) > 120 || len(c.GroupIDs) == 0 || len(c.GroupIDs) > 100 || c.SubscriptionGroupID < 1 || c.StartsAt.IsZero() || !c.EndsAt.After(c.StartsAt) || !checkTemplate(c.WelcomeTemplate, false) || !checkTemplate(c.CongratulationTemplate, false) || !checkTemplate(c.PendingTemplate, false) || !checkTemplate(c.PrivateTemplate, true) || !strings.Contains(c.PrivateTemplate, "{code}") {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, g := range c.GroupIDs {
		if !validGroup(g) || seen[g] {
			return ErrInvalid
		}
		seen[g] = true
	}
	return nil
}
func (s *Store) SaveCampaign(ctx context.Context, c Campaign, actor int64) (Campaign, error) {
	if c.DeliveryPendingTemplate == "" {
		c.DeliveryPendingTemplate = defaultDeliveryPendingTemplate
	}
	if e := validateCampaign(c); e != nil {
		return Campaign{}, e
	}
	tx, e := s.beginWrite(ctx)
	if e != nil {
		return Campaign{}, e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(81425302)`); e != nil {
		return Campaign{}, e
	}
	if c.PlanName, e = subscriptionName(ctx, tx, c.SubscriptionGroupID); e != nil {
		return Campaign{}, e
	}
	var baseline *time.Time
	if c.ID != 0 {
		old, e := scanCampaign(tx.QueryRowContext(ctx, `SELECT id,version,payload,baseline_at,created_at FROM qq_invite_campaigns WHERE id=$1 FOR UPDATE`, c.ID))
		if e != nil {
			return Campaign{}, e
		}
		if old.Version != c.Version {
			return Campaign{}, ErrConflict
		}
		if old.SubscriptionGroupID != c.SubscriptionGroupID {
			return Campaign{}, ErrConflict
		}
		if e = tx.QueryRowContext(ctx, `SELECT baseline_at FROM qq_invite_campaigns WHERE id=$1`, c.ID).Scan(&baseline); e != nil {
			return Campaign{}, e
		}
		a, _ := json.Marshal(old.GroupIDs)
		b, _ := json.Marshal(c.GroupIDs)
		if string(a) != string(b) {
			baseline = nil
		}
		c.Version++
		c.CreatedAt = old.CreatedAt
	} else {
		if c.Enabled {
			return Campaign{}, ErrInvalid
		}
		c.Version = 1
		c.CreatedAt = time.Now().UTC()
	}
	c.BaselineReady = baseline != nil
	if c.Enabled && !c.BaselineReady {
		return Campaign{}, ErrInvalid
	}
	if c.Enabled {
		rows, e := tx.QueryContext(ctx, `SELECT payload FROM qq_invite_campaigns WHERE id<>$1 AND (payload->>'enabled')::boolean`, c.ID)
		if e != nil {
			return Campaign{}, e
		}
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				_ = rows.Close()
				return Campaign{}, e
			}
			var other Campaign
			if e = json.Unmarshal(b, &other); e != nil {
				_ = rows.Close()
				return Campaign{}, e
			}
			if c.StartsAt.Before(other.EndsAt) && c.EndsAt.After(other.StartsAt) {
				for _, g := range c.GroupIDs {
					for _, og := range other.GroupIDs {
						if g == og {
							_ = rows.Close()
							return Campaign{}, ErrConflict
						}
					}
				}
			}
		}
		if e = rows.Err(); e != nil {
			_ = rows.Close()
			return Campaign{}, e
		}
		_ = rows.Close()
	}
	b, _ := json.Marshal(c)
	if c.ID == 0 {
		e = tx.QueryRowContext(ctx, `INSERT INTO qq_invite_campaigns(version,payload) VALUES($1,$2) RETURNING id`, c.Version, b).Scan(&c.ID)
	} else {
		_, e = tx.ExecContext(ctx, `UPDATE qq_invite_campaigns SET version=$2,payload=$3,baseline_at=$4 WHERE id=$1`, c.ID, c.Version, b, baseline)
	}
	if e != nil {
		return Campaign{}, e
	}
	b, _ = json.Marshal(c)
	if _, e = tx.ExecContext(ctx, `INSERT INTO qq_invite_campaign_versions(campaign_id,version,payload,actor_id) VALUES($1,$2,$3,$4)`, c.ID, c.Version, b, actor); e != nil {
		return Campaign{}, e
	}
	if e = tx.Commit(); e != nil {
		return Campaign{}, e
	}
	return c, nil
}

func subscriptionName(ctx context.Context, q queryRower, id int64) (string, error) {
	var name string
	e := q.QueryRowContext(ctx, `SELECT name FROM groups WHERE id=$1 AND subscription_type='subscription' AND status='active' AND deleted_at IS NULL`, id).Scan(&name)
	if e == sql.ErrNoRows {
		return "", ErrInvalid
	}
	return name, e
}
func (s *Store) SaveBaseline(ctx context.Context, id int64, members map[string][]string, actor int64) error {
	tx, e := s.beginWrite(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	c, e := scanCampaign(tx.QueryRowContext(ctx, `SELECT id,version,payload,baseline_at,created_at FROM qq_invite_campaigns WHERE id=$1 FOR UPDATE`, id))
	if e != nil {
		return e
	}
	if c.Enabled {
		return ErrConflict
	}
	if len(members) != len(c.GroupIDs) {
		return ErrInvalid
	}
	for _, g := range c.GroupIDs {
		list, ok := members[g]
		if !ok {
			return ErrInvalid
		}
		for _, qq := range list {
			if !validQQ(qq) {
				return ErrInvalid
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO qq_invite_baseline(campaign_id,group_id,qq) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, g, qq); e != nil {
				return e
			}
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE qq_invite_campaigns SET baseline_at=now() WHERE id=$1`, id); e != nil {
		return e
	}
	if e = audit(ctx, tx, actor, "sync_baseline", id, map[string]int{"groups": len(members)}); e != nil {
		return e
	}
	return tx.Commit()
}
