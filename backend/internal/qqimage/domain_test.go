package qqimage

import "testing"

func TestAmountExactAndInvalid(t *testing.T) {
	for _, v := range []string{"0.00000001", "1.23000000", "999999999999.99999999"} {
		if _, err := ParseAmount(v); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	for _, v := range []string{"", "0", "-1", "NaN", "Inf", "1e2", "0.000000001", "1000000000000"} {
		if _, err := ParseAmount(v); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
}

func TestBindingIdentityNamespace(t *testing.T) {
	a := Identity{Platform: "qq", AppID: "app", SubjectType: "group_member", SubjectID: "member", ScopeType: "group", ScopeID: "group", GroupID: "group"}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Identity){func(x *Identity) { x.AppID = "other" }, func(x *Identity) { x.ScopeID = "other"; x.GroupID = "other" }, func(x *Identity) { x.SubjectID = "other" }} {
		b := a
		mutate(&b)
		if a.Key() == b.Key() {
			t.Fatal("identity namespace collapsed")
		}
	}
	a.GroupID = "other"
	if a.Validate() == nil {
		t.Fatal("mismatched group accepted")
	}
}
