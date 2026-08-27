package perm

import (
	"errors"
	"reflect"
	"testing"
)

// The catalog is exactly the codes in spec D3 (r2) — a test asserts the
// full set, so adding or dropping one is a deliberate diff.
func TestAll_ExactCatalog(t *testing.T) {
	want := []string{
		"branches.view", "branches.manage",
		"catalog.view", "catalog.manage",
		"inventory.view",
		"customers.view", "customers.manage",
		"suppliers.view", "suppliers.manage",
		"orders.view", "orders.manage",
		"reports.view",
		"conflicts.view", "conflicts.manage",
		"company.manage",
	}
	if !reflect.DeepEqual(All, want) {
		t.Fatalf("All = %v, want %v", All, want)
	}
}

func TestCan(t *testing.T) {
	tests := []struct {
		name  string
		perms []string
		code  string
		want  bool
	}{
		{"exact code held", []string{CatalogView}, CatalogView, true},
		{"manage implies view", []string{CatalogManage}, CatalogView, true},
		{"unknown code", []string{CatalogManage}, "catalog.price.write", false},
		{"empty set", nil, CatalogView, false},
		{"view-only set asked for manage", []string{CatalogView}, CatalogManage, false},
		{"view-only section has no manage implication", []string{InventoryView}, "inventory.manage", false},
		{"company has no view code to imply", []string{CompanyManage}, "company.view", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Can(tt.perms, tt.code); got != tt.want {
				t.Errorf("Can(%v, %q) = %v, want %v", tt.perms, tt.code, got, tt.want)
			}
		})
	}
}

func TestNormalize_RejectsUnknownCode(t *testing.T) {
	_, err := Normalize([]string{CatalogView, "catalog.price.write"})
	if !errors.Is(err, ErrUnknownCode) {
		t.Fatalf("err = %v, want ErrUnknownCode", err)
	}
}

func TestNormalize_RejectsEmpty(t *testing.T) {
	_, err := Normalize(nil)
	if !errors.Is(err, ErrEmptyPermissions) {
		t.Fatalf("err = %v, want ErrEmptyPermissions", err)
	}
	if _, err := Normalize([]string{}); !errors.Is(err, ErrEmptyPermissions) {
		t.Fatalf("err = %v, want ErrEmptyPermissions", err)
	}
}

func TestNormalize_DedupesAndExpandsManageToView(t *testing.T) {
	got, err := Normalize([]string{CatalogManage, CatalogManage, ReportsView})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{CatalogManage, CatalogView, ReportsView}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Normalize = %v, want %v", got, want)
	}
}

func TestNormalize_CompanyManageHasNoViewToExpand(t *testing.T) {
	got, err := Normalize([]string{CompanyManage})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{CompanyManage}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Normalize = %v, want %v", got, want)
	}
}

func TestScope_Owner(t *testing.T) {
	owner := Scope{Role: "owner", Permissions: All}
	for _, code := range All {
		if !owner.Has(code) {
			t.Errorf("owner scope missing %q", code)
		}
	}
	if !owner.IsUnscoped() {
		t.Fatal("owner scope must be unscoped")
	}
	if !owner.AllowsBranch("br_anything") {
		t.Fatal("owner scope must allow every branch")
	}
}

func TestScope_ScopedMember(t *testing.T) {
	member := Scope{
		Role:        "member",
		Permissions: []string{OrdersView, OrdersManage},
		BranchIDs:   []string{"br_2", "br_7"},
	}
	if member.IsUnscoped() {
		t.Fatal("member with a non-empty allowlist must not be unscoped")
	}
	if !member.AllowsBranch("br_2") || !member.AllowsBranch("br_7") {
		t.Fatal("member must be allowed on listed branches")
	}
	if member.AllowsBranch("br_3") {
		t.Fatal("member must not be allowed on an unlisted branch")
	}
	if !member.Has(OrdersManage) || !member.Has(OrdersView) {
		t.Fatal("member should hold its granted permissions")
	}
	if member.Has(CatalogView) {
		t.Fatal("member should not hold an ungranted permission")
	}
}

func TestScope_EmptyAllowlistAllowsEveryBranch(t *testing.T) {
	member := Scope{Role: "member", Permissions: []string{ReportsView}}
	if !member.IsUnscoped() {
		t.Fatal("empty BranchIDs means unscoped")
	}
	if !member.AllowsBranch("br_1") || !member.AllowsBranch("br_99") {
		t.Fatal("empty allowlist must allow every branch")
	}
}
