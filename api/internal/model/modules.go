package model

import (
	"fmt"
	"strings"
)

// Module is a fixed device-license entitlement code, carried inside the
// signed token's features field (v1:<csv>) so an offline client can gate
// feature areas cryptographically.
type Module string

const (
	ModulePurchase   Module = "purchase"
	ModuleSales      Module = "sales"
	ModuleCustomers  Module = "customers"
	ModuleAccounting Module = "accounting"
)

// AllModules is the full catalog, granted to trials and any legacy license
// with no explicit module selection.
var AllModules = []string{
	string(ModulePurchase),
	string(ModuleSales),
	string(ModuleCustomers),
	string(ModuleAccounting),
}

var validModules = map[string]bool{
	string(ModulePurchase):   true,
	string(ModuleSales):      true,
	string(ModuleCustomers):  true,
	string(ModuleAccounting): true,
}

// NormalizeModules trims/lowercases and dedupes the given module codes,
// rejecting anything outside the fixed catalog. It is the single source of
// truth reused by the admin assign handler and (later) billing plan
// validation.
func NormalizeModules(in []string) ([]string, error) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, m := range in {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		if !validModules[m] {
			return nil, fmt.Errorf("unknown module: %s", m)
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out, nil
}
