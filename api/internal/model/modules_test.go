package model

import (
	"reflect"
	"testing"
)

func TestNormalizeModulesRejectsUnknown(t *testing.T) {
	if _, err := NormalizeModules([]string{"customers", "bogus"}); err == nil {
		t.Fatal("expected error for unknown module")
	}
}

func TestNormalizeModules(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty is a bills-only POS", nil, []string{"bills", "users"}},
		{"dedupes, lowercases, catalog order", []string{" Mahger ", "customers", "CUSTOMERS"}, []string{"bills", "customers", "mahger", "users"}},
		{"legacy sales/purchase are dropped", []string{"sales", "purchase", "accounting"}, []string{"bills", "accounting", "users"}},
		{"submodule pulls in its parent", []string{"accounting.banks"}, []string{"bills", "accounting", "accounting.banks", "users"}},
		{"core codes are accepted", []string{"bills", "users", "ariblink"}, []string{"bills", "users", "ariblink"}},
	}
	for _, c := range cases {
		got, err := NormalizeModules(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsLegacyModuleList(t *testing.T) {
	if !IsLegacyModuleList(nil) {
		t.Error("empty list should be legacy")
	}
	if !IsLegacyModuleList([]string{"purchase", "sales", "customers", "accounting"}) {
		t.Error("pre-catalog list should be legacy")
	}
	if IsLegacyModuleList([]string{"bills", "users"}) {
		t.Error("normalized list should not be legacy")
	}
}

func TestCatalogIsConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Catalog {
		if seen[d.Code] {
			t.Fatalf("duplicate code %q", d.Code)
		}
		seen[d.Code] = true
		if d.NameAr == "" || d.NameEn == "" {
			t.Errorf("%q is missing a name", d.Code)
		}
		switch d.Kind {
		case ModuleSub, ModuleAddon:
			p, ok := ModuleByCode(d.Parent)
			if !ok || p.Parent != "" {
				t.Errorf("%q must sit under a top-level parent, got %q", d.Code, d.Parent)
			}
		default:
			if d.Parent != "" {
				t.Errorf("%q is %s but has parent %q", d.Code, d.Kind, d.Parent)
			}
		}
	}
	if !reflect.DeepEqual(AllModules, func() []string {
		out, _ := NormalizeModules(AllModules)
		return out
	}()) {
		t.Errorf("AllModules is not in normalized catalog order: %v", AllModules)
	}
}
