package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/lib/pq"
)

type redeemCodeRepository struct {
	client *dbent.Client
}

func NewRedeemCodeRepository(client *dbent.Client) service.RedeemCodeRepository {
	return &redeemCodeRepository{client: client}
}

func (r *redeemCodeRepository) Create(ctx context.Context, code *service.RedeemCode) error {
	created, err := clientFromContext(ctx, r.client).RedeemCode.Create().
		SetCode(code.Code).
		SetType(code.Type).
		SetValue(code.Value).
		SetStatus(code.Status).
		SetNotes(code.Notes).
		SetValidityDays(code.ValidityDays).
		SetNillableExpiresAt(code.ExpiresAt).
		SetNillableUsedBy(code.UsedBy).
		SetNillableUsedAt(code.UsedAt).
		SetNillableGroupID(code.GroupID).
		SetSubscriptionEffect(code.SubscriptionEffect).
		SetNillableWelfareCampaignID(code.WelfareCampaignID).
		SetNillableWelfareCampaignVersion(code.WelfareCampaignVersion).
		SetWelfareCampaignName(code.WelfareCampaignName).
		SetNillableQuotaAmountUsd(code.QuotaAmountUSD).
		Save(ctx)
	if err == nil {
		code.ID = created.ID
		code.CreatedAt = created.CreatedAt
	}
	return err
}

func (r *redeemCodeRepository) CreateBatch(ctx context.Context, codes []service.RedeemCode) error {
	if len(codes) == 0 {
		return nil
	}

	builders := make([]*dbent.RedeemCodeCreate, 0, len(codes))
	for i := range codes {
		c := &codes[i]
		b := clientFromContext(ctx, r.client).RedeemCode.Create().
			SetCode(c.Code).
			SetType(c.Type).
			SetValue(c.Value).
			SetStatus(c.Status).
			SetNotes(c.Notes).
			SetValidityDays(c.ValidityDays).
			SetNillableExpiresAt(c.ExpiresAt).
			SetNillableUsedBy(c.UsedBy).
			SetNillableUsedAt(c.UsedAt).
			SetNillableGroupID(c.GroupID).
			SetSubscriptionEffect(c.SubscriptionEffect).
			SetNillableWelfareCampaignID(c.WelfareCampaignID).
			SetNillableWelfareCampaignVersion(c.WelfareCampaignVersion).
			SetWelfareCampaignName(c.WelfareCampaignName).
			SetNillableQuotaAmountUsd(c.QuotaAmountUSD)
		builders = append(builders, b)
	}

	return clientFromContext(ctx, r.client).RedeemCode.CreateBulk(builders...).Exec(ctx)
}

func (r *redeemCodeRepository) GetByID(ctx context.Context, id int64) (*service.RedeemCode, error) {
	m, err := clientFromContext(ctx, r.client).RedeemCode.Query().
		Where(redeemcode.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrRedeemCodeNotFound
		}
		return nil, err
	}
	out := redeemCodeEntityToService(m)
	if err := r.loadWelfareSupplementResults(ctx, []*service.RedeemCode{out}); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *redeemCodeRepository) GetByCode(ctx context.Context, code string) (*service.RedeemCode, error) {
	m, err := clientFromContext(ctx, r.client).RedeemCode.Query().
		Where(redeemcode.CodeEQ(code)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrRedeemCodeNotFound
		}
		return nil, err
	}
	out := redeemCodeEntityToService(m)
	if err := r.loadWelfareSupplementResults(ctx, []*service.RedeemCode{out}); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *redeemCodeRepository) Delete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	deleted, err := client.RedeemCode.Delete().Where(
		redeemcode.IDEQ(id), redeemcode.StatusNEQ(service.StatusUsed),
		redeemcode.UsedByIsNil(), redeemcode.UsedAtIsNil(),
	).Exec(ctx)
	if err != nil {
		var foreignKey *pq.Error
		if errors.As(err, &foreignKey) && (foreignKey.Code == "23001" || foreignKey.Code == "23503") {
			switch foreignKey.Constraint {
			case "user_welfare_subscription_grants_redeem_code_id_fkey",
				"user_welfare_subscription_supplements_redeem_code_id_fkey",
				"user_subscription_supplements_redeem_code_id_fkey":
				return service.ErrRedeemCodeDeleteUsed
			}
		}
		return err
	}
	if deleted == 0 {
		exists, err := client.RedeemCode.Query().Where(redeemcode.IDEQ(id)).Exist(ctx)
		if err != nil {
			return err
		}
		if exists {
			return service.ErrRedeemCodeDeleteUsed
		}
	}
	return nil
}

func (r *redeemCodeRepository) List(ctx context.Context, params pagination.PaginationParams) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	return r.ListWithFilters(ctx, params, "", "", "")
}

func (r *redeemCodeRepository) ListWithFilters(ctx context.Context, params pagination.PaginationParams, codeType, status, search string) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	q := r.client.RedeemCode.Query()

	if codeType != "" {
		q = q.Where(redeemcode.TypeEQ(codeType))
	}
	if status != "" {
		now := time.Now()
		switch status {
		case service.StatusExpired:
			q = q.Where(redeemcode.Or(
				redeemcode.StatusEQ(service.StatusExpired),
				redeemcode.And(
					redeemcode.StatusEQ(service.StatusUnused),
					redeemcode.ExpiresAtNotNil(),
					redeemcode.ExpiresAtLTE(now),
				),
			))
		case service.StatusUnused:
			q = q.Where(
				redeemcode.StatusEQ(service.StatusUnused),
				redeemcode.Or(
					redeemcode.ExpiresAtIsNil(),
					redeemcode.ExpiresAtGT(now),
				),
			)
		default:
			q = q.Where(redeemcode.StatusEQ(status))
		}
	}
	if search != "" {
		q = q.Where(
			redeemcode.Or(
				redeemcode.CodeContainsFold(search),
				redeemcode.HasUserWith(user.EmailContainsFold(search)),
			),
		)
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	codesQuery := q.
		WithUser().
		WithGroup().
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range redeemCodeListOrder(params) {
		codesQuery = codesQuery.Order(order)
	}

	codes, err := codesQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	outCodes := redeemCodeEntitiesToService(codes)
	if err := r.loadWelfareSupplementSlice(ctx, outCodes); err != nil {
		return nil, nil, err
	}

	return outCodes, paginationResultFromTotal(int64(total), params), nil
}

func redeemCodeListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)

	var field string
	switch sortBy {
	case "type":
		field = redeemcode.FieldType
	case "value":
		field = redeemcode.FieldValue
	case "status":
		field = redeemcode.FieldStatus
	case "used_at":
		field = redeemcode.FieldUsedAt
	case "created_at":
		field = redeemcode.FieldCreatedAt
	case "expires_at":
		field = redeemcode.FieldExpiresAt
	case "code":
		field = redeemcode.FieldCode
	default:
		field = redeemcode.FieldID
	}

	if sortOrder == pagination.SortOrderAsc {
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(redeemcode.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(redeemcode.FieldID)}
}

func (r *redeemCodeRepository) Update(ctx context.Context, code *service.RedeemCode) error {
	up := r.client.RedeemCode.UpdateOneID(code.ID).
		SetCode(code.Code).
		SetType(code.Type).
		SetValue(code.Value).
		SetStatus(code.Status).
		SetNotes(code.Notes).
		SetValidityDays(code.ValidityDays)

	if code.UsedBy != nil {
		up.SetUsedBy(*code.UsedBy)
	} else {
		up.ClearUsedBy()
	}
	if code.UsedAt != nil {
		up.SetUsedAt(*code.UsedAt)
	} else {
		up.ClearUsedAt()
	}
	if code.GroupID != nil {
		up.SetGroupID(*code.GroupID)
	} else {
		up.ClearGroupID()
	}
	if code.ExpiresAt != nil {
		up.SetExpiresAt(*code.ExpiresAt)
	} else {
		up.ClearExpiresAt()
	}

	updated, err := up.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrRedeemCodeNotFound
		}
		return err
	}
	code.CreatedAt = updated.CreatedAt
	return nil
}

func (r *redeemCodeRepository) BatchUpdate(ctx context.Context, ids []int64, fields service.RedeemCodeBatchUpdateFields) (int64, error) {
	uniqueIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return 0, nil
	}

	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.batchUpdate(ctx, tx.Client(), uniqueIDs, fields)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	txCtx := dbent.NewTxContext(ctx, tx)
	defer func() { _ = tx.Rollback() }()

	updated, err := r.batchUpdate(txCtx, tx.Client(), uniqueIDs, fields)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return updated, nil
}

func (r *redeemCodeRepository) batchUpdate(ctx context.Context, client *dbent.Client, ids []int64, fields service.RedeemCodeBatchUpdateFields) (int64, error) {
	// Acquire locks in code ID order before reading status or mutable fields.
	// Redemption and QQ inventory writes coordinate on these same rows.
	existing, err := client.RedeemCode.Query().
		Where(redeemcode.IDIn(ids...), lockRedeemCodeRow).
		Unique(false).Order(dbent.Asc(redeemcode.FieldID)).
		All(ctx)
	if err != nil {
		return 0, err
	}
	if len(existing) != len(ids) {
		return 0, service.ErrRedeemCodeNotFound
	}
	if fields.GroupID.Set {
		for _, code := range existing {
			if code.SubscriptionEffect != "" {
				return 0, service.ErrWelfareSupplementInvalid
			}
		}
	}
	if fields.TouchesUsedSensitiveFields() {
		subscriptionIDs := make([]int64, 0, len(existing))
		for _, code := range existing {
			if code.Status == service.StatusUsed || code.UsedAt != nil || code.UsedBy != nil {
				return 0, service.ErrRedeemCodeUsed
			}
			if code.Type == service.RedeemTypeSubscription {
				subscriptionIDs = append(subscriptionIDs, code.ID)
			}
		}
		if len(subscriptionIDs) > 0 {
			// An earlier stale administrative write may have cleared the
			// status and use fields. The immutable receipts still prove use.
			recorded, err := client.RedeemCode.Query().
				Where(redeemcode.IDIn(subscriptionIDs...), hasSubscriptionRedeemReceipt).
				Exist(ctx)
			if err != nil {
				return 0, err
			}
			if recorded {
				return 0, service.ErrRedeemCodeUsed
			}
		}
	}

	up := client.RedeemCode.Update().Where(redeemcode.IDIn(ids...))
	if fields.Status != nil {
		up.SetStatus(*fields.Status)
	}
	if fields.Notes != nil {
		up.SetNotes(*fields.Notes)
	}
	if fields.ExpiresAt.Set {
		if fields.ExpiresAt.Value != nil {
			up.SetExpiresAt(*fields.ExpiresAt.Value)
		} else {
			up.ClearExpiresAt()
		}
	}
	if fields.GroupID.Set {
		if fields.GroupID.Value != nil {
			up.SetGroupID(*fields.GroupID.Value)
		} else {
			up.ClearGroupID()
		}
	}

	affected, err := up.Save(ctx)
	if err != nil {
		return 0, err
	}
	if affected != len(ids) {
		return 0, service.ErrRedeemCodeNotFound
	}
	return int64(affected), nil
}

func lockRedeemCodeRow(selector *entsql.Selector) {
	if selector.Dialect() == dialect.Postgres {
		selector.ForUpdate()
	}
}

func hasSubscriptionRedeemReceipt(selector *entsql.Selector) {
	if selector.Dialect() != dialect.Postgres {
		selector.Where(entsql.False())
		return
	}
	builder := entsql.Dialect(dialect.Postgres)
	receipts := []string{
		"user_welfare_subscription_grants",
		"user_welfare_subscription_supplements",
		"user_subscription_supplements",
	}
	predicates := make([]*entsql.Predicate, 0, len(receipts))
	for _, name := range receipts {
		table := builder.Table(name)
		query := builder.Select(table.C("redeem_code_id")).From(table).
			Where(entsql.ColumnsEQ(table.C("redeem_code_id"), selector.C(redeemcode.FieldID)))
		predicates = append(predicates, entsql.Exists(query))
	}
	selector.Where(entsql.Or(predicates...))
}

func (r *redeemCodeRepository) Use(ctx context.Context, id, userID int64) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.use(ctx, tx.Client(), id, userID)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.use(dbent.NewTxContext(ctx, tx), tx.Client(), id, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *redeemCodeRepository) use(ctx context.Context, client *dbent.Client, id, userID int64) error {
	code, err := client.RedeemCode.Query().Where(redeemcode.IDEQ(id), lockRedeemCodeRow).Unique(false).Only(ctx)
	if dbent.IsNotFound(err) {
		return service.ErrRedeemCodeUsed
	}
	if err != nil {
		return err
	}
	if code.UsedAt != nil || code.UsedBy != nil || code.Status == service.StatusUsed {
		return service.ErrRedeemCodeUsed
	}
	if code.Status == "cancelled" {
		return service.ErrRedeemCodeCancelled
	}
	// Read the clock after the locked read. A waited-on code may have expired
	// while the administrator's transaction held the row.
	now := time.Now()
	if code.Status == service.StatusExpired || (code.Status == service.StatusUnused && code.ExpiresAt != nil && !code.ExpiresAt.After(now)) {
		return service.ErrRedeemCodeExpired
	}
	if code.Status != service.StatusUnused {
		return service.ErrRedeemCodeUsed
	}
	affected, err := client.RedeemCode.Update().
		Where(redeemcode.IDEQ(id), redeemcode.StatusEQ(service.StatusUnused),
			redeemcode.UsedByIsNil(), redeemcode.UsedAtIsNil()).
		SetStatus(service.StatusUsed).
		SetUsedBy(userID).
		SetUsedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrRedeemCodeUsed
	}
	return nil
}

func (r *redeemCodeRepository) ListByUser(ctx context.Context, userID int64, limit int) ([]service.RedeemCode, error) {
	if limit <= 0 {
		limit = 10
	}

	codes, err := r.client.RedeemCode.Query().
		Where(redeemcode.UsedByEQ(userID)).
		WithGroup().
		Order(dbent.Desc(redeemcode.FieldUsedAt), dbent.Desc(redeemcode.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}

	out := redeemCodeEntitiesToService(codes)
	if err := r.loadWelfareSupplementSlice(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListByUserPaginated returns paginated balance/concurrency history for a user.
// Supports optional type filter (e.g. "balance", "admin_balance", "concurrency", "admin_concurrency", "subscription").
func (r *redeemCodeRepository) ListByUserPaginated(ctx context.Context, userID int64, params pagination.PaginationParams, codeType string) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	q := r.client.RedeemCode.Query().
		Where(redeemcode.UsedByEQ(userID))

	// Optional type filter
	if codeType != "" {
		q = q.Where(redeemcode.TypeEQ(codeType))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	codes, err := q.
		WithGroup().
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(dbent.Desc(redeemcode.FieldUsedAt), dbent.Desc(redeemcode.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	out := redeemCodeEntitiesToService(codes)
	if err := r.loadWelfareSupplementSlice(ctx, out); err != nil {
		return nil, nil, err
	}
	return out, paginationResultFromTotal(int64(total), params), nil
}

// SumPositiveBalanceByUser returns total recharged amount (sum of value > 0 where type is balance/admin_balance).
func (r *redeemCodeRepository) SumPositiveBalanceByUser(ctx context.Context, userID int64) (float64, error) {
	var result []struct {
		Sum float64 `json:"sum"`
	}
	err := r.client.RedeemCode.Query().
		Where(
			redeemcode.UsedByEQ(userID),
			redeemcode.ValueGT(0),
			redeemcode.TypeIn("balance", "admin_balance"),
		).
		Aggregate(dbent.As(dbent.Sum(redeemcode.FieldValue), "sum")).
		Scan(ctx, &result)
	if err != nil {
		return 0, err
	}
	if len(result) == 0 {
		return 0, nil
	}
	return result[0].Sum, nil
}

func redeemCodeEntityToService(m *dbent.RedeemCode) *service.RedeemCode {
	if m == nil {
		return nil
	}
	out := &service.RedeemCode{
		ID:                 m.ID,
		Code:               m.Code,
		Type:               m.Type,
		Value:              m.Value,
		Status:             m.Status,
		UsedBy:             m.UsedBy,
		UsedAt:             m.UsedAt,
		Notes:              derefString(m.Notes),
		CreatedAt:          m.CreatedAt,
		ExpiresAt:          m.ExpiresAt,
		GroupID:            m.GroupID,
		ValidityDays:       m.ValidityDays,
		SubscriptionEffect: m.SubscriptionEffect, WelfareCampaignID: m.WelfareCampaignID, WelfareCampaignVersion: m.WelfareCampaignVersion,
		WelfareCampaignName: m.WelfareCampaignName, QuotaAmountUSD: m.QuotaAmountUsd,
	}
	if m.Edges.User != nil {
		out.User = userEntityToService(m.Edges.User)
	}
	if m.Edges.Group != nil {
		out.Group = groupEntityToService(m.Edges.Group)
	}
	return out
}

func redeemCodeEntitiesToService(models []*dbent.RedeemCode) []service.RedeemCode {
	out := make([]service.RedeemCode, 0, len(models))
	for i := range models {
		if s := redeemCodeEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}
