package model

import (
	"fmt"
	"sort"
	"strings"
)

// ModuleKind classifies a catalog entry.
type ModuleKind string

const (
	// ModuleCore is always granted and never sold: a POS cannot work without
	// it. Core codes are stored on the license but never written to the token.
	ModuleCore ModuleKind = "core"
	// ModuleTop is a sellable top-level module (one welcome-screen tile group).
	ModuleTop ModuleKind = "module"
	// ModuleSub is sold on top of its Parent and requires it.
	ModuleSub ModuleKind = "sub"
	// ModuleAddon is sold on top of its Parent and carries a value (Valued).
	ModuleAddon ModuleKind = "addon"
)

// ModuleDef is one device-license entitlement. Code is carried inside the
// signed token's features field (v1:<csv>) so an offline client can gate
// feature areas cryptographically; it must never be renamed once shipped.
type ModuleDef struct {
	Code   string     `json:"code"`
	Parent string     `json:"parent,omitempty"`
	Kind   ModuleKind `json:"kind"`
	// Valued entries carry an integer in the token as "code=N" (a seat
	// count); the value lives on License.Seats.
	Valued bool   `json:"valued,omitempty"`
	NameAr string `json:"name_ar"`
	NameEn string `json:"name_en"`
	Order  int    `json:"order"`
}

const (
	ModuleBills              = "bills"
	ModuleCustomers          = "customers"
	ModuleInventory          = "inventory"
	ModuleAccounting         = "accounting"
	ModuleAccountingEWallets = "accounting.ewallets"
	ModuleAccountingBanks    = "accounting.banks"
	ModuleMahger             = "mahger"
	ModuleUsers              = "users"
	ModuleAribLink           = "ariblink"
)

// Catalog is the single source of truth for license modules: the admin panel
// renders it from GET /admin/modules and the desktop mirrors it. Top-level
// entries map one-to-one onto the desktop welcome-screen tile groups; depth
// is at most two.
var Catalog = []ModuleDef{
	{Code: ModuleBills, Kind: ModuleCore, NameAr: "الفواتير", NameEn: "Bills", Order: 10},
	{Code: ModuleCustomers, Kind: ModuleTop, NameAr: "العملاء والموردين", NameEn: "Customers & vendors", Order: 20},
	{Code: ModuleInventory, Kind: ModuleTop, NameAr: "المخازن والأصناف", NameEn: "Warehouses & products", Order: 30},
	{Code: ModuleAccounting, Kind: ModuleTop, NameAr: "الحسابات", NameEn: "Accounting", Order: 40},
	{Code: ModuleAccountingEWallets, Parent: ModuleAccounting, Kind: ModuleSub, NameAr: "المحافظ الإلكترونية", NameEn: "E-wallets", Order: 41},
	{Code: ModuleAccountingBanks, Parent: ModuleAccounting, Kind: ModuleSub, NameAr: "البنوك", NameEn: "Banks", Order: 42},
	{Code: ModuleMahger, Kind: ModuleTop, NameAr: "الحجز", NameEn: "Reservations", Order: 50},
	{Code: ModuleUsers, Kind: ModuleCore, NameAr: "المستخدمين", NameEn: "Users", Order: 60},
	{Code: ModuleAribLink, Parent: ModuleUsers, Kind: ModuleAddon, Valued: true, NameAr: "الأجهزة الطرفية", NameEn: "Terminals", Order: 61},
}

// LegacyTokenCodes are always written to the token so desktops still on an
// app version that gates on them keep selling and buying (Bills is always
// on). Delete once every device runs a catalog-aware build.
var LegacyTokenCodes = []string{"sales", "purchase"}

// legacyInputCodes are accepted from older admin clients and stored
// licenses but carry no meaning of their own: Bills (core) replaced them.
var legacyInputCodes = map[string]bool{"sales": true, "purchase": true}

var catalogByCode = func() map[string]ModuleDef {
	m := make(map[string]ModuleDef, len(Catalog))
	for _, d := range Catalog {
		m[d.Code] = d
	}
	return m
}()

// ModuleByCode looks a catalog entry up.
func ModuleByCode(code string) (ModuleDef, bool) {
	d, ok := catalogByCode[code]
	return d, ok
}

// AllModules is every catalog code, granted to trials and to licenses that
// predate the catalog (their owners always had every feature).
var AllModules = func() []string {
	out := make([]string, 0, len(Catalog))
	for _, d := range Catalog {
		out = append(out, d.Code)
	}
	return out
}()

// NormalizeModules trims/lowercases and dedupes the given module codes,
// rejecting anything outside the catalog. It always adds the core modules
// and a submodule's parent, drops legacy codes, and returns the result in
// catalog order. It is the single source of truth reused by the admin
// assign/edit handlers and (later) billing plan validation.
func NormalizeModules(in []string) ([]string, error) {
	seen := make(map[string]bool, len(Catalog))
	for _, d := range Catalog {
		if d.Kind == ModuleCore {
			seen[d.Code] = true
		}
	}
	for _, m := range in {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" || legacyInputCodes[m] {
			continue
		}
		d, ok := catalogByCode[m]
		if !ok {
			return nil, fmt.Errorf("unknown module: %s", m)
		}
		seen[m] = true
		if d.Parent != "" {
			seen[d.Parent] = true
		}
	}
	out := make([]string, 0, len(seen))
	for code := range seen {
		out = append(out, code)
	}
	sort.Slice(out, func(i, j int) bool { return catalogByCode[out[i]].Order < catalogByCode[out[j]].Order })
	return out, nil
}

// IsLegacyModuleList reports whether a stored license's modules predate the
// catalog. NormalizeModules always stores the core Bills code, so a list
// without it (empty, or the old purchase/sales/customers/accounting set) was
// written before the catalog existed.
func IsLegacyModuleList(modules []string) bool {
	for _, m := range modules {
		if m == ModuleBills {
			return false
		}
	}
	return true
}
