package adminaccess

import "testing"

func TestScopedPermissionCatalogRequiresExplicitGrant(t *testing.T) {
	for _, id := range []string{"accounts.identity.read", "accounts.usage.read", "accounts.options.write", "accounts.rate.write", "groups.list.read", "groups.capacity.read", "openai.tokens.refresh"} {
		found := false
		for _, entry := range PermissionCatalog() {
			if entry.ID == id {
				found = true
			}
		}
		if !found {
			t.Errorf("权限未登记: %s", id)
		}
	}
	if RouteAllowed(Policy{}, "POST", "/api/v1/admin/openai/refresh-token") {
		t.Fatal("旧策略不得继承新增权限")
	}
}

func TestWelfareAdjustmentRequiresDedicatedPermission(t *testing.T) {
	const route = "/api/v1/admin/subscriptions/:id/adjust-welfare"
	for _, permissions := range [][]string{nil, {"subscriptions.read"}, {"subscriptions.extend"}, {"subscriptions.reset"}} {
		if RouteAllowed(Policy{GlobalPermissions: permissions}, "POST", route) {
			t.Fatalf("unrelated permissions authorize welfare adjustment: %v", permissions)
		}
	}
	if !RouteAllowed(Policy{GlobalPermissions: []string{"subscriptions.welfare.adjust"}}, "POST", route) {
		t.Fatal("dedicated permission must authorize welfare adjustment")
	}
}
