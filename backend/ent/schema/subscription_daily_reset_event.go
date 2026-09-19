package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SubscriptionDailyResetEvent struct{ ent.Schema }

func (SubscriptionDailyResetEvent) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscription_daily_reset_events"}}
}

func (SubscriptionDailyResetEvent) Fields() []ent.Field {
	money := map[string]string{dialect.Postgres: "numeric(20,10)"}
	timestamp := map[string]string{dialect.Postgres: "timestamptz"}
	return []ent.Field{
		field.Int64("subscription_id"), field.Int64("user_id"), field.Int64("group_id"),
		field.String("source").MaxLen(16),
		field.String("operation_id").MaxLen(128),
		field.String("request_fingerprint").MaxLen(128),
		field.Int64("before_version").NonNegative(), field.Int64("after_version").NonNegative(),
		field.String("timezone").MaxLen(64),
		field.Time("count_date").SchemaType(map[string]string{dialect.Postgres: "date"}),
		field.Int("day_sequence").Min(1).Max(100),
		field.Float("before_daily_usage_usd").SchemaType(money),
		field.Float("after_daily_usage_usd").Default(0).SchemaType(money),
		field.Float("daily_limit_usd").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Time("before_expires_at").SchemaType(timestamp),
		field.Time("after_expires_at").SchemaType(timestamp),
		field.Int("deducted_seconds").Default(86400),
		field.Time("decided_at").SchemaType(timestamp),
	}
}

func (SubscriptionDailyResetEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("subscription_id", "operation_id").Unique(),
		index.Fields("subscription_id", "before_version").Unique(),
		index.Fields("subscription_id", "count_date", "day_sequence").Unique(),
	}
}
