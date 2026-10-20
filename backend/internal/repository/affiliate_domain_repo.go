package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.AffiliateDomainRepository = (*affiliateRepository)(nil)

func (r *affiliateRepository) ResolveAffiliateDomain(ctx context.Context, hostname string) (*service.AffiliatePromotionDomain, error) {
	return queryAffiliateDomain(ctx, clientFromContext(ctx, r.client), "WHERE hostname=$1", hostname)
}

func (r *affiliateRepository) GetUserAffiliateDomain(ctx context.Context, userID int64) (*service.AffiliatePromotionDomain, error) {
	return queryAffiliateDomain(ctx, clientFromContext(ctx, r.client), "WHERE inviter_id=$1 AND enabled=true", userID)
}

func queryAffiliateDomain(ctx context.Context, client affiliateQueryExecer, suffix string, arg any) (*service.AffiliatePromotionDomain, error) {
	rows, err := client.QueryContext(ctx, `SELECT hostname,COALESCE(inviter_id,0),enabled,revision
FROM affiliate_promotion_hosts `+suffix, arg)
	if err != nil {
		return nil, fmt.Errorf("query affiliate promotion host: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var domain service.AffiliatePromotionDomain
	if err := rows.Scan(&domain.Hostname, &domain.InviterID, &domain.Enabled, &domain.Revision); err != nil {
		return nil, err
	}
	return &domain, rows.Err()
}

func (r *affiliateRepository) SetUserAffiliateDomain(ctx context.Context, userID int64, hostname string) (*service.AffiliatePromotionDomain, error) {
	if userID <= 0 {
		return nil, service.ErrUserNotFound
	}
	if hostname != "" {
		canonical, err := service.NormalizeAffiliateHostname(hostname)
		if err != nil || canonical != hostname {
			return nil, service.ErrAffiliateDomainInvalid
		}
	}
	var result *service.AffiliatePromotionDomain
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		// Admin changes are rare. Serialize mapping mutations across instances so
		// replacing two hosts cannot acquire their tombstone rows in reverse order.
		if _, err := client.ExecContext(txCtx, "SELECT pg_advisory_xact_lock(hashtext('affiliate-promotion-host-admin'))"); err != nil {
			return fmt.Errorf("lock affiliate domain administration: %w", err)
		}
		if err := lockAffiliateDomainUsers(txCtx, client, []int64{userID}, hostname != ""); err != nil {
			return err
		}
		var target *service.AffiliatePromotionDomain
		if hostname != "" {
			var err error
			target, err = queryAffiliateDomain(txCtx, client, "WHERE hostname=$1 FOR UPDATE", hostname)
			if err != nil {
				return err
			}
			if target != nil && target.Enabled && target.InviterID != userID {
				return service.ErrAffiliateDomainTaken
			}
			if target != nil && target.Enabled && target.InviterID == userID {
				result = target
				return nil
			}
		}
		if _, err := client.ExecContext(txCtx, `UPDATE affiliate_promotion_hosts
SET enabled=false,revision=revision+1,updated_at=NOW() WHERE inviter_id=$1 AND enabled=true`, userID); err != nil {
			return fmt.Errorf("disable prior affiliate domain: %w", err)
		}
		if hostname == "" {
			return nil
		}
		if target == nil {
			if _, err := client.ExecContext(txCtx, `INSERT INTO affiliate_promotion_hosts(hostname,inviter_id)
VALUES($1,$2)`, hostname, userID); err != nil {
				return fmt.Errorf("insert affiliate domain: %w", err)
			}
		} else {
			if _, err := client.ExecContext(txCtx, `UPDATE affiliate_promotion_hosts
SET inviter_id=$2,enabled=true,revision=revision+1,updated_at=NOW() WHERE hostname=$1`, hostname, userID); err != nil {
				return fmt.Errorf("enable affiliate domain: %w", err)
			}
		}
		// Domain-only users still need the existing affiliate summary for admin
		// pagination and the standard invitee/rebate functionality.
		if _, err := ensureUserAffiliateWithClient(txCtx, client, userID); err != nil {
			return err
		}
		var err error
		result, err = queryAffiliateDomain(txCtx, client, "WHERE hostname=$1", hostname)
		return err
	})
	return result, err
}

// lockAffiliateDomainUsers follows an ascending user order before acquiring
// mapping and affiliate-summary locks. FOR SHARE excludes deletion/status edits.
func lockAffiliateDomainUsers(ctx context.Context, client affiliateQueryExecer, ids []int64, requireActive bool) error {
	rows, err := client.QueryContext(ctx, `SELECT id,status,deleted_at FROM users
WHERE id=ANY($1) ORDER BY id FOR SHARE`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("lock affiliate users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var id int64
		var status string
		var deleted sql.NullTime
		if err := rows.Scan(&id, &status, &deleted); err != nil {
			return err
		}
		if deleted.Valid || (requireActive && status != service.StatusActive) {
			return service.ErrAffiliateAttributionInvalid
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != len(ids) {
		return service.ErrAffiliateAttributionInvalid
	}
	return nil
}

func (r *affiliateRepository) ValidateSignupAffiliateAttribution(ctx context.Context, attr *service.SignupAffiliateAttribution) error {
	return r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		return validateAffiliateDomainAuthority(txCtx, client, attr, 0)
	})
}

func validateAffiliateDomainAuthority(ctx context.Context, client *dbent.Client, attr *service.SignupAffiliateAttribution, userID int64) error {
	if err := service.ValidateSignupAffiliateAttributionValue(attr, time.Now()); err != nil {
		return err
	}
	// The setting service's contract is exact "true", with missing/error default
	// disabled. Lock the actual row so a disable cannot race the signup commit.
	rows, err := client.QueryContext(ctx, "SELECT value FROM settings WHERE key=$1 FOR SHARE", service.SettingKeyAffiliateEnabled)
	if err != nil {
		return fmt.Errorf("lock affiliate feature setting: %w", err)
	}
	var value string
	if rows.Next() {
		err = rows.Scan(&value)
	}
	rowErr := rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if rowErr != nil {
		return rowErr
	}
	if value != "true" {
		return service.ErrAffiliateDisabled
	}
	ids := []int64{attr.InviterID}
	if userID > 0 {
		if userID == attr.InviterID {
			return service.ErrAffiliateAttributionInvalid
		}
		ids = append(ids, userID)
	}
	if err := lockAffiliateDomainUsers(ctx, client, ids, true); err != nil {
		return err
	}
	domain, err := queryAffiliateDomain(ctx, client, "WHERE hostname=$1 FOR SHARE", attr.SourceHost)
	if err != nil {
		return err
	}
	if domain == nil || !domain.Enabled || domain.InviterID != attr.InviterID || domain.Revision != attr.HostRevision {
		return service.ErrAffiliateAttributionInvalid
	}
	// Waiting for any of the above locks must not extend an expired attribution.
	return service.ValidateSignupAffiliateAttributionValue(attr, time.Now())
}

func (r *affiliateRepository) BindSignupAffiliateAttribution(ctx context.Context, userID int64, attr *service.SignupAffiliateAttribution) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return fmt.Errorf("trusted affiliate signup binding requires the account creation transaction")
	}
	if userID <= 0 {
		return service.ErrAffiliateAttributionInvalid
	}
	client := tx.Client()
	if err := validateAffiliateDomainAuthority(ctx, client, attr, userID); err != nil {
		return err
	}
	ids := []int64{userID, attr.InviterID}
	if ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	for _, id := range ids {
		if _, err := ensureUserAffiliateWithClient(ctx, client, id); err != nil {
			return err
		}
	}
	rows, err := client.QueryContext(ctx, `SELECT user_id FROM user_affiliates
WHERE user_id=ANY($1) ORDER BY user_id FOR UPDATE`, pq.Array(ids))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if err := service.ValidateSignupAffiliateAttributionValue(attr, time.Now()); err != nil {
		return err
	}
	summary, err := queryAffiliateByUserID(ctx, client, userID)
	if err != nil {
		return err
	}
	if summary.InviterID != nil {
		if *summary.InviterID == attr.InviterID {
			return nil
		}
		return service.ErrAffiliateAlreadyBound
	}
	bound, err := r.BindInviter(ctx, userID, attr.InviterID)
	if err != nil {
		return err
	}
	if !bound {
		return service.ErrAffiliateAlreadyBound
	}
	return nil
}
