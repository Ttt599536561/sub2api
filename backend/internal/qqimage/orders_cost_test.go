package qqimage

import (
	"context"
	"encoding/json"
	"testing"
)

func assertUnknownProviderCost(t *testing.T, order *Order) {
	t.Helper()
	b, err := json.Marshal(order)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["currency"] != "USD" || fields["price"] != "1.00000000" {
		t.Fatalf("fixed user charge changed or currency missing: %v %v", fields["currency"], fields["price"])
	}
	for _, name := range []string{"provider_cost", "provider_currency"} {
		value, exists := fields[name]
		if !exists || value != nil {
			t.Fatalf("%s must be explicit JSON null, got exists=%v value=%v", name, exists, value)
		}
	}
	if fields["provider_cost_status"] != "unknown" {
		t.Fatalf("unknown cost was not explicit: %v", fields["provider_cost_status"])
	}
}

func TestOrderProviderCostUnknownIsPersistedSeparately(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "cost@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	order, err := s.CreateOrder(ctx, i, "event", "cat")
	if err != nil {
		t.Fatal(err)
	}
	assertUnknownProviderCost(t, order)
	var currency, status string
	var costNull, currencyNull bool
	if err = s.db.QueryRow(`SELECT data->>'currency',data->>'provider_cost_status',data->'provider_cost'='null'::jsonb,data->'provider_currency'='null'::jsonb FROM qq_image_orders WHERE id=$1`, order.ID).Scan(&currency, &status, &costNull, &currencyNull); err != nil {
		t.Fatal(err)
	}
	if currency != "USD" || status != "unknown" || !costNull || !currencyNull {
		t.Fatalf("snapshot cost metadata is not explicit: %s %s %v %v", currency, status, costNull, currencyNull)
	}
	loaded, err := s.GetOrder(ctx, order.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	assertUnknownProviderCost(t, loaded)
	checkWallet(t, s, u, "1.00000000", "1.00000000")
}

func TestOrderProviderCostLegacySnapshotDefaultsUnknown(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := fixtureUser(t, s, "cost@example.com", "2")
	i := fixtureIdentity("one", "group")
	fixtureGrant(t, s, u, i)
	order, err := s.CreateOrder(ctx, i, "event", "cat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE qq_image_orders SET data=data-'currency'-'provider_cost'-'provider_currency'-'provider_cost_status' WHERE id=$1`, order.ID); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.GetOrder(ctx, order.ID, u)
	if err != nil {
		t.Fatal(err)
	}
	assertUnknownProviderCost(t, legacy)
}
