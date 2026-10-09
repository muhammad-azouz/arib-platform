// These mirror the Go API responses.
//
// IMPORTANT (same gotcha as admin/): the domain models (Tenant/Company/Branch/
// BranchDevice) only carry `bson` tags, so encoding/json serializes them with
// their Go field names (PascalCase). The Bundle wrapper has no json tags either,
// so its keys are PascalCase too. Hand-written response maps (sync-token,
// session) DO use explicit lower/snake_case keys. Keep this in sync with
// internal/model/model.go + internal/tenant/service.go + httpapi handlers.

export type TenantStatus = 'active' | 'suspended'
export type BranchStatus = 'active' | 'deactivated'
export type DeviceStatus = 'active' | 'released'
export type Provider = 'email' | 'google' | 'facebook'

// --- domain models (PascalCase keys) ---

export interface Tenant {
  ID: string
  AccountID: string
  Name: string
  Status: TenantStatus
  Plan?: string
  DBName?: string
  CreatedAt: string
  UpdatedAt: string
  SchemaVersion?: number
  RolloutStatus?: string
}

export interface Company {
  ID: string
  TenantID: string
  Name: string
  Phone?: string
  Address?: string
  TaxNumber?: string
  CreatedAt: string
  UpdatedAt: string
}

export type BillStatus = 'paid' | 'void'
export type SubscriptionState = 'none' | 'active' | 'expiring' | 'grace' | 'expired'

export interface Bill {
  ID: string
  TenantID: string
  Amount: number // minor units (e.g. piasters)
  Currency: string
  StartsAt: string
  EndsAt: string
  Status: BillStatus
  VoidReason?: string
  Notes?: string
  CreatedBy: string
  Source: string
  CreatedAt: string
  UpdatedAt: string
}

// SubscriptionSummary carries json tags -> snake_case keys
// (api/internal/billing.Summary).
export interface SubscriptionSummary {
  state: SubscriptionState
  ends_at: string
  grace_until: string
  days_left: number
}

// SubscriptionResponse carries json tags -> snake_case keys
// (GET /v1/tenants/{id}/subscription).
export interface SubscriptionResponse {
  bills: Bill[] | null
  summary: SubscriptionSummary
}

export interface Branch {
  ID: string
  TenantID: string
  CompanyID: string
  Name: string
  Phone1?: string // required on the POS branch; printed on receipts
  Phone2?: string
  Phone3?: string
  Address?: string // required on the POS branch; printed on receipts
  Seats: number // admin-controlled seat limit (merchant cannot change it)
  Status: BranchStatus
  CreatedAt: string
  UpdatedAt: string
  ActiveDevices?: number // live seat usage, computed server-side in GetBundle
}

export interface BranchDevice {
  ID: string
  TenantID: string
  BranchID: string
  MachineID: string
  MachineName?: string
  OS?: string
  Status: DeviceStatus
  BoundAt: string
  LastSeenAt: string
  ReleasedAt: string | null
}

// The requesting member's own role/permissions/branch-allowlist — the
// bundle's `me` block (spec-console-rbac T108, tenant.MeView on the API
// side). Unlike Tenant/Company/Branches above, this field carries an
// explicit `json:"me"` tag in Go, hence the lowercase key here — and unlike
// them it's hand-written JSON, so its own keys are snake_case too. Not to
// be confused with the unrelated account-identity `MeView` below (`GET
// /v1/me`) — that one wraps an Account, this one is per-tenant scope.
export interface TenantMeView {
  role: MemberRole
  role_id?: string
  role_name?: string
  permissions: string[]
  branch_ids: string[]
}

// GET /v1/tenants/{id} — the activation/login bundle. `Company` is null until
// the company is registered; the Setup-Wizard completion gate keys off this.
export interface Bundle {
  Tenant: Tenant
  Company: Company | null
  Branches: Branch[] | null
  me: TenantMeView
}

// --- hand-written response maps (snake_case keys) ---

// POST /v1/tenants/{id}/sync-token
export interface SyncToken {
  token: string
  expires_at: string
  db_name: string
  gateway_url: string
}

// Members (T14): GET/POST/DELETE /v1/tenants/{id}/members[/{memberId}].
// tenant.MemberView on the API side — hand-written JSON tags, unlike the raw
// bson-tagged Tenant/Branch/Bundle types above.
export type MemberRole = 'owner' | 'member'

export interface Member {
  id: string
  account_id: string
  email: string
  first_name?: string
  last_name?: string
  role: MemberRole
  // RBAC role assignment (spec-console-rbac T108/T109). Empty role_id/
  // role_name describes the owner row or a member not yet assigned a role
  // (should not exist post-backfill — T113 renders it as «بدون دور»).
  // branch_ids is always present, never omitted: [] means every branch.
  role_id?: string
  role_name?: string
  branch_ids: string[]
  invited_by?: string
  created_at: string
  // "Pending" (T125, spec-console-rbac D6) is derived client-side from this
  // being absent — set once, server-side, on the member's first
  // authenticated request on this tenant. Always absent for the owner row.
  accepted_at?: string
}

// Roles (spec-console-rbac T107/T112): GET/POST/PUT/DELETE
// /v1/tenants/{id}/roles[/{roleId}]. tenant.RoleView on the API side —
// `permissions` always arrives already normalized (manage implies view,
// perm.Normalize), so the console never re-derives that rule, only reflects
// it (see lib/perm.ts). `assigned_count` lets the roles list warn before a
// delete D8 would refuse anyway.
export interface RoleView {
  id: string
  name: string
  permissions: string[]
  assigned_count: number
  created_at: string
  updated_at: string
}

// --- HQ reads (freshness envelope; hq/service.go + hq_handlers.go) ---

// How fresh branch-derived data is: "synced" while the branch's sync cadence
// is healthy, "offline" once its last completed round goes stale (>30 min),
// "live" reserved for the future SignalR tier.
export type FreshnessSource = 'synced' | 'offline' | 'live'

// Every branch-derived payload arrives wrapped in this envelope.
export interface Envelope<T> {
  data: T
  source: FreshnessSource
  as_of?: string | null
}

// One branch's last completed sync round.
export interface BranchSync {
  branch_id: string
  last_sync_at: string
}

// GET /v1/tenants/{id}/hq/branch-activity
export interface BranchActivityResponse {
  branches: Envelope<BranchSync>[]
}

// Sync-health dot for a branch: ok 🟢 <10 min, lagging 🟡 10–30, stale 🔴
// older, never = no completed round yet.
export type BranchHealth = 'ok' | 'lagging' | 'stale' | 'never'

// An open cashier shift (one per workstation; a branch can have several).
export interface OpenShift {
  num: number
  opened_by: string
  opened_at: string
}

// One branch's day-so-far from the tenant central DB.
export interface BranchSnapshotData {
  branch_id: string
  today_sales_total: number
  today_sales_count: number
  today_refunds_total: number
  open_shift: OpenShift | null
  open_shift_count: number
}

// GET /v1/tenants/{id}/hq/branches — control-plane branch + health + snapshot.
// The snapshot degrades to {data: null, source: "offline"} when the tenant has
// no sync subscription or the gateway is unreachable.
export interface BranchView {
  id: string
  name: string
  status: BranchStatus
  health: BranchHealth
  last_sync_at?: string | null
  // Uploaded rows central set aside (ConflictLog ApplyError, not yet reviewed): they retry
  // every sync but wait on someone fixing the data. 0 / absent from an older API.
  unacked_apply_errors?: number
  snapshot: Envelope<BranchSnapshotData | null>
}

// Company-wide day-so-far summed over the branch snapshots (Overview KPIs).
// Stale branch data is included in the sums; honesty comes from
// offline_branches and as_of (the oldest contributing sync).
export interface HqTotals {
  sales_total: number
  sales_count: number
  refunds_total: number
  open_shift_count: number
  synced_branches: number
  offline_branches: number
  as_of?: string | null
}

export interface HqBranchesResponse {
  branches: BranchView[]
  totals: HqTotals
}

// --- HQ catalog reads (slice 3; hq/service.go's catalog methods) ---
//
// Catalog data is read off the tenant's central DB, which is itself only as
// fresh as the newest completed branch sync — so `as_of` is that sync time
// (absent for a never-synced tenant) and `source` flips to "offline" once it
// ages past 30 minutes. The freshness pill reports sync recency, never "just
// read".
export interface CatalogEnvelope<T> {
  data: T
  source: FreshnessSource
  as_of?: string | null
}

// One product group; the console builds the parent/child tree client-side
// from parent_id (root groups use the all-zero GUID).
export interface CatalogGroup {
  id: string
  parent_id: string
  name: string
  is_active: boolean
  num: number
  product_count: number
}

// One row of the paged product list.
export interface CatalogProduct {
  id: string
  code: number
  name: string
  kind: number
  group_id?: string | null
  group_name?: string | null
  is_active: boolean
  unit?: string | null
  sale: number
  buy: number
  barcodes: string[]
  total_qty: number
}

export interface CatalogProductsPage {
  total: number
  page: number
  page_size: number
  items: CatalogProduct[]
}

// GET /v1/tenants/{id}/hq/catalog/groups
export type CatalogGroupsResponse = CatalogEnvelope<CatalogGroup[]>

// GET /v1/tenants/{id}/hq/catalog/products
export type CatalogProductsResponse = CatalogEnvelope<CatalogProductsPage>

// One unit of measure with its full price ladder and barcodes.
export interface ProductUnit {
  id: string
  name: string
  val_sub: number
  level: number
  buy: number
  sale: number
  prices: number[]
  barcodes: string[]
}

// One branch warehouse's stock of the product, decorated with that branch's
// sync health tier so the console needs no second call to judge trust.
export interface ProductAvailability {
  branch_id: string
  branch_name: string
  health: BranchHealth
  warehouse_id: string
  warehouse_name: string
  total_qty: number
  unit_cost: number
  updated_at?: string | null
  last_sync_at?: string | null
}

export interface ProductDetail {
  id: string
  code: number
  name: string
  kind: number
  group_id?: string | null
  group_name?: string | null
  is_active: boolean
  re_order: number
  is_expire: boolean
  created_at: string
  units: ProductUnit[]
  availability: ProductAvailability[]
}

// GET /v1/tenants/{id}/hq/catalog/products/{productId}
export type ProductDetailResponse = CatalogEnvelope<ProductDetail>

// PUT /v1/tenants/{id}/hq/catalog/products/{pid}/prices — one unit's price
// update; omitted fields are left unchanged by the gateway.
export interface PriceChangeInput {
  unit_id: string
  sale?: number
  buy?: number
}

// The gateway's write receipt: the UTC instant the change committed to
// central. A branch "has" the write once its live `last_sync_at` (already
// streamed via SSE) is at or after this timestamp.
export interface PriceChangeResult {
  written_at: string
}

// POST /v1/tenants/{id}/hq/catalog/products — v1 keeps this minimal: one
// unit, Sale/Buy only (no opening balance, no price tiers), matching
// EditUnitPriceDialog's same scope decision for consistency.
export interface NewProductUnitInput {
  name: string
  val_sub: number
  buy: number
  sale: number
  barcodes?: string[]
}
export interface NewProductInput {
  name: string
  kind: number // 0 = Product (inventory), 1 = SalesService, 2 = PurchaseService
  group_id?: string
  units: NewProductUnitInput[]
}
export interface NewProductResult {
  id: string
  code: number
  written_at: string
}

// --- HQ inventory reads (slice 4; hq/service.go's inventory methods) ---
//
// One dataset (WarehousesProductInventories + InventoryMovements), three
// perspectives. Like catalog, these read the central DB live on every call —
// `source` is always "synced"; the per-branch `health`/`last_sync_at` fields
// are what actually grade trust.

// A WPI row's stock condition, mirroring the desktop's InventoryStockRule.
// `ok` means none of the other three apply (including every inactive
// product, which never gets flagged).
export type InventoryStatus = 'negative' | 'out' | 'low' | 'ok'

// Query-param status filter — 'attention' additionally matches any row
// failing the desktop rule (negative, out, or under reorder), same set the
// needs-attention view lists.
export type InventoryStatusFilter = InventoryStatus | 'attention'

// One warehouse's slice of a branch's stock summary.
export interface WarehouseStock {
  warehouse_id: string
  warehouse_name: string
  is_active: boolean
  sku_count: number
  stock_value: number
  negative_count: number
  out_count: number
  low_count: number
}

// One branch's stock summary, decorated with sync health (zeroed if the
// gateway has no stock rows for it — still a real branch, just no stock yet).
export interface InventoryBranchView {
  branch_id: string
  branch_name: string
  health: BranchHealth
  last_sync_at?: string | null
  sku_count: number
  stock_value: number
  negative_count: number
  out_count: number
  low_count: number
  warehouses: WarehouseStock[]
}

// Company-wide roll-up over every InventoryBranchView (no sku_count — a
// product stocked at two branches would double-count).
export interface InventoryTotals {
  stock_value: number
  negative_count: number
  out_count: number
  low_count: number
}

export interface InventoryBranchesData {
  branches: InventoryBranchView[]
  totals: InventoryTotals
}

// GET /v1/tenants/{id}/hq/inventory/branches
export type InventoryBranchesResponse = CatalogEnvelope<InventoryBranchesData>

// One row of the "by product" inventory view. Qty/value are company-wide, or
// scoped to one branch when the branch_id param is set.
export interface InventoryProduct {
  id: string
  code: number
  name: string
  group_id?: string | null
  group_name?: string | null
  is_active: boolean
  unit?: string | null
  re_order: number
  total_qty: number
  stock_value: number
  branches_with_stock: number
  last_activity_at?: string | null
  status: InventoryStatus
}

export interface InventoryProductsPage {
  total: number
  page: number
  page_size: number
  items: InventoryProduct[]
}

// GET /v1/tenants/{id}/hq/inventory/products
export type InventoryProductsResponse = CatalogEnvelope<InventoryProductsPage>

export interface AttentionCounts {
  negative: number
  out: number
  low: number
}

// One WPI row needing attention, decorated with its branch's name and
// current health tier.
export interface AttentionItem {
  status: InventoryStatus
  product_id: string
  product_code: number
  product_name: string
  unit?: string | null
  re_order: number
  branch_id: string
  branch_name: string
  health: BranchHealth
  warehouse_id: string
  warehouse_name: string
  total_qty: number
  unit_cost: number
  last_in_date?: string | null
  last_out_date?: string | null
}

// A branch whose data is too old to trust — a separate list from the paged
// stock items so it never disturbs paging math. Never-synced branches are
// excluded (Overview's alerts already own "never connected").
export interface StaleBranch {
  branch_id: string
  branch_name: string
  last_sync_at?: string | null
}

export interface AttentionData {
  stale_branches: StaleBranch[]
  counts: AttentionCounts
  total: number
  page: number
  page_size: number
  items: AttentionItem[]
}

// GET /v1/tenants/{id}/hq/inventory/attention
export type AttentionResponse = CatalogEnvelope<AttentionData>

// One inventory movement, decorated with its branch's name.
export interface MovementRow {
  id: string
  issue_date: string
  dealing: number
  branch_id: string
  branch_name: string
  warehouse_id: string
  warehouse_name: string
  customer_name?: string | null
  in_qty: number
  in_price: number
  out_qty: number
  out_price: number
  cost: number
  unit: string
  reg_num: string
  running_qty: number
}

export interface MovementsPage {
  opening_qty: number
  total: number
  page: number
  page_size: number
  items: MovementRow[]
}

// GET /v1/tenants/{id}/hq/catalog/products/{productId}/movements
export type MovementsResponse = CatalogEnvelope<MovementsPage>

// --- HQ conflicts read (slice 5; hq/service.go's Conflicts/AckConflicts) ---
//
// ServerWins (D12) already resolved these at sync time; this is the review
// trail. `local_row` is the central row that was kept, `remote_row` is the
// branch's losing write (null when the branch had deleted the row) — both
// JSON-encoded entity snapshots, diffed client-side on the review page.

export interface ConflictItem {
  id: number
  occurred_at: string
  branch_id?: string | null
  branch_name?: string
  table_name: string
  row_pk?: string | null
  conflict_type: string
  resolution: string
  local_row?: string | null
  remote_row?: string | null
  acknowledged_at?: string | null
  product_id?: string | null
  product_name?: string | null
}

export interface ConflictsData {
  unacked: number
  total: number
  page: number
  page_size: number
  items: ConflictItem[]
  // The filters the server actually applied (branch_id/type). Absent from an older server that
  // ignores them — the page must not then claim the list is filtered.
  applied_filters?: { branch_id?: string | null; type?: string | null } | null
}

// GET /v1/tenants/{id}/hq/conflicts
export type ConflictsResponse = CatalogEnvelope<ConflictsData>

// POST /v1/tenants/{id}/hq/conflicts/ack — at least one of ids/up_to_id is
// required (handler-enforced); up_to_id acks everything with a lower-or-equal
// id, ids acks an explicit set. Both are inclusive and idempotent.
export interface AckConflictsInput {
  ids?: number[]
  up_to_id?: number
}

export interface AckConflictsResult {
  acked: number
}

// --- HQ reports (slice 6; hq/service.go's Report* methods) ---
//
// Question-organized period aggregates, computed live off the central DB —
// like catalog, `source` is always "synced" and `as_of` is the read time.
// All money figures follow the desktop's own report semantics (Sale/ReSale
// bills, tender fields, Σ(Total − ItemCost) profit).

// How the period's sales were paid: cash in drawer, bank/card, e-wallet, and
// credit = the on-account remainder.
export interface TenderSplit {
  cash: number
  bank: number
  wallet: number
  credit: number
}

// One local calendar day of the sales series. `day` is a plain YYYY-MM-DD
// string in the tenant's day-scope, not an instant — render it as a date.
export interface SalesDay {
  day: string
  sales_total: number
  sales_count: number
  refunds_total: number
}

// Period totals + tender split + gap-filled day series. `from`/`to` echo the
// gateway's resolved period (it owns defaulting and clamping).
export interface SalesReport {
  from: string
  to: string
  sales_total: number
  sales_count: number
  refunds_total: number
  refunds_count: number
  tender: TenderSplit
  days: SalesDay[]
}

// GET /v1/tenants/{id}/hq/reports/sales
export type SalesReportResponse = CatalogEnvelope<SalesReport>

// Products report sort order (gateway-side, descending).
export type ReportSort = 'revenue' | 'qty' | 'profit'

// One product's period performance. qty_sold is in base units, labeled with
// the master-unit name (same convention as the inventory views).
export interface ProductReportRow {
  id: string
  code: number
  name: string
  group_name?: string | null
  unit?: string | null
  qty_sold: number
  revenue: number
  profit: number
}

export interface ProductsReportPage {
  total: number
  page: number
  page_size: number
  items: ProductReportRow[]
}

// GET /v1/tenants/{id}/hq/reports/products
export type ProductsReportResponse = CatalogEnvelope<ProductsReportPage>

// One branch's period performance, registry-decorated (every branch renders,
// zeroed when it has no rows in the period).
export interface BranchReportRow {
  branch_id: string
  branch_name: string
  health: BranchHealth
  last_sync_at?: string | null
  sales_total: number
  sales_count: number
  refunds_total: number
  refunds_count: number
  profit: number
}

export interface BranchesReportData {
  branches: BranchReportRow[]
}

// GET /v1/tenants/{id}/hq/reports/branches
export type BranchesReportResponse = CatalogEnvelope<BranchesReportData>

// One user's period performance. user_name comes from the tenant DB's Tier-A
// Users table, not the control plane.
export interface StaffReportRow {
  user_id: string
  user_name: string
  sales_total: number
  sales_count: number
  refunds_total: number
  refunds_count: number
}

export interface StaffReportData {
  staff: StaffReportRow[]
}

// GET /v1/tenants/{id}/hq/reports/staff
export type StaffReportResponse = CatalogEnvelope<StaffReportData>

// One cashier shift opened in the period. A closed shift carries its stored
// count (actual_cash/difference: positive = over, negative = short); an open
// one has none yet (null) and expected_cash is live.
export interface ShiftReportRow {
  id: string
  num: number
  branch_id: string
  workstation_id: string
  is_open: boolean
  is_force_closed: boolean
  opened_by_user_id: string
  opened_by: string
  opened_at: string
  closed_by: string | null
  closed_at: string | null
  opening_cash: number
  sales_total: number
  sales_count: number
  refunds_total: number
  refunds_count: number
  expected_cash: number
  actual_cash: number | null
  difference: number | null
}

// Totals over every shift matching the filters (not just the page). Over/short
// figures cover closed shifts only.
export interface ShiftsReportSummary {
  shift_count: number
  open_count: number
  force_closed_count: number
  sales_total: number
  refunds_total: number
  net_difference: number
  short_count: number
  short_total: number
  over_count: number
  over_total: number
}

export interface ShiftCashier {
  user_id: string
  name: string
}

export interface ShiftsReportPage {
  summary: ShiftsReportSummary
  cashiers: ShiftCashier[]
  total: number
  page: number
  page_size: number
  items: ShiftReportRow[]
}

export type ShiftStatusFilter = 'open' | 'closed'

// GET /v1/tenants/{id}/hq/reports/shifts
export type ShiftsReportResponse = CatalogEnvelope<ShiftsReportPage>

// The online Z report (X while still open) for one shift.
export interface ShiftDetail {
  id: string
  num: number
  branch_id: string
  branch_name: string
  workstation_id: string
  is_open: boolean
  is_force_closed: boolean
  opened_by: string
  opened_at: string
  open_note: string | null
  closed_by: string | null
  closed_at: string | null
  close_note: string | null
  sales_count: number
  sales_total: number
  refunds_count: number
  refunds_total: number
  tender: { cash: number; bank: number; wallet: number; credit: number }
  cash_in: number
  cash_out: number
  expenses: number
  revenue: number
  opening_cash: number
  expected_cash: number
  actual_cash: number | null
  difference: number | null
  customers_served: number
  inventory_adjustments: number
}

// GET /v1/tenants/{id}/hq/reports/shifts/{shiftId}
export type ShiftDetailResponse = CatalogEnvelope<ShiftDetail>

export type ShiftTransactionKind = 'sale' | 'return' | 'expense' | 'revenue'

/** One bill, return or expense/income voucher a shift owns. Amounts are
 *  positive; kind says which way they move. customer is the account name for
 *  vouchers. */
export interface ShiftTransaction {
  kind: ShiftTransactionKind
  id: string
  at: string
  num: string
  daily_num: number | null
  customer: string | null
  user: string
  item_count: number
  cash: number
  bank: number
  wallet: number
  credit: number
  total: number
  original_num: string | null
  note: string | null
}

export interface ShiftTransactions {
  shift_id: string
  branch_id: string
  items: ShiftTransaction[]
}

// GET /v1/tenants/{id}/hq/reports/shifts/{shiftId}/transactions
export type ShiftTransactionsResponse = CatalogEnvelope<ShiftTransactions>

export interface ShiftInvoiceLine {
  product: string
  qty: number
  unit: string
  price: number
  discount: number
  total: number
}

export interface ShiftInvoice {
  id: string
  branch_id: string
  kind: 'sale' | 'return'
  num: string
  daily_num: number | null
  at: string
  customer: string | null
  user: string
  item_total: number
  total_discount: number
  bill_tax: number
  total_extra: number
  total: number
  cash: number
  bank: number
  bank_name: string | null
  wallet: number
  wallet_name: string | null
  credit: number
  original_num: string | null
  note: string | null
  lines: ShiftInvoiceLine[]
}

// GET /v1/tenants/{id}/hq/reports/shifts/{shiftId}/invoices/{invoiceId}
export type ShiftInvoiceResponse = CatalogEnvelope<ShiftInvoice>

// --- HQ customers (slice 7; hq/service.go's Customer* methods) ---
//
// Read-mostly, branch-specific (Customers is a Tier-B, own-BranchId table —
// no cross-branch customer identity). Every row is decorated with its owning
// branch's registry name/health, same "no second call needed" pattern as
// ProductAvailability. Balance is always the gateway's D10-recomputed ledger
// sum, never a stored column.

// One customer group; mirrors CatalogGroup minus product_count.
export interface CustomerGroup {
  id: string
  parent_id: string
  name: string
  is_active: boolean
  num: number
}

// GET /v1/tenants/{id}/hq/customer-groups
export type CustomerGroupsResponse = CatalogEnvelope<CustomerGroup[]>

// Debt/credit filter for the customer list, profile insights, and export.
export type CustomerDebtFilter = 'has_debt' | 'credit' | 'exceeding'

// One row of the paged customer list.
export interface CustomerRow {
  id: string
  num: number
  name: string
  branch_id: string
  branch_name: string
  health: BranchHealth
  group_id?: string | null
  group_name?: string | null
  phone1: string
  is_active: boolean
  balance: number
  credit_limit: number
  is_credit: boolean
  last_purchase_at?: string | null
}

export interface CustomersPage {
  total: number
  page: number
  page_size: number
  items: CustomerRow[]
}

// GET /v1/tenants/{id}/hq/customers
export type CustomersResponse = CatalogEnvelope<CustomersPage>

// One customer's purchase performance, straight off the gateway's Bills
// aggregate — no client-side arithmetic.
export interface CustomerStats {
  number_of_orders: number
  total_spent: number
  average_order_value: number
  last_purchase_date?: string | null
}

export interface CustomerDetail {
  id: string
  num: number
  name: string
  branch_id: string
  branch_name: string
  health: BranchHealth
  group_id?: string | null
  group_name?: string | null
  phone1: string
  phone2?: string | null
  phone3?: string | null
  address?: string | null
  note?: string | null
  credit_limit: number
  is_credit: boolean
  is_active: boolean
  balance: number
  stats: CustomerStats
}

// GET /v1/tenants/{id}/hq/customers/{customerId}
export type CustomerDetailResponse = CatalogEnvelope<CustomerDetail>

// One purchase (Bill), newest first.
export interface CustomerPurchaseRow {
  id: string
  num: string
  issued_at: string
  total: number
  item_count: number
  is_paid: boolean
  type: number
}

export interface CustomerPurchasesPage {
  total: number
  page: number
  page_size: number
  items: CustomerPurchaseRow[]
}

// GET /v1/tenants/{id}/hq/customers/{customerId}/purchases
export type CustomerPurchasesResponse = CatalogEnvelope<CustomerPurchasesPage>

// One ledger (CustomerTransaction) row, running balance already computed
// server-side (T29-style self-contained pages).
export interface CustomerLedgerRow {
  id: string
  created_at: string
  dealing: number
  total: number
  debit: number
  credit: number
  running_balance: number
  note?: string | null
  user_id: string
}

export interface CustomerLedgerPage {
  total: number
  page: number
  page_size: number
  items: CustomerLedgerRow[]
}

// GET /v1/tenants/{id}/hq/customers/{customerId}/ledger
export type CustomerLedgerResponse = CatalogEnvelope<CustomerLedgerPage>

// One customer ranked by a spend figure (period or lifetime, depending on
// which insights block it appears in).
export interface CustomerInsightRow {
  id: string
  num: number
  name: string
  branch_id: string
  amount: number
}

// One customer with no ranking figure attached (new-this-month / inactive).
export interface CustomerRef {
  id: string
  num: number
  name: string
  branch_id: string
}

// A count plus a capped preview list — count can exceed items.length.
export interface CustomerRefList {
  count: number
  items: CustomerRef[]
}

// One customer approaching (>=80% of limit) or exceeding (>=100%) its credit
// limit.
export interface CreditWarningRow {
  id: string
  num: number
  name: string
  branch_id: string
  balance: number
  credit_limit: number
  level: 'approaching' | 'exceeding'
}

// One local calendar day of the new-customer series — a date string, not an
// instant, same convention as SalesDay.
export interface CustomerGrowthDay {
  day: string
  new_customers: number
}

export interface CustomerInsights {
  top_customers: CustomerInsightRow[]
  new_this_month: CustomerRefList
  inactive: CustomerRefList
  credit_limit_warnings: CreditWarningRow[]
  highest_spenders: CustomerInsightRow[]
  growth_over_time: CustomerGrowthDay[]
}

// GET /v1/tenants/{id}/hq/customers/insights
export type CustomerInsightsResponse = CatalogEnvelope<CustomerInsights>

// POST /v1/tenants/{id}/hq/customers — bounded create, no opening balance in
// v1 (mirrors NewProductInput's "no opening balance from HQ" decision).
export interface NewCustomerInput {
  name: string
  phone1: string
  phone2?: string
  phone3?: string
  address?: string
  note?: string
  group_id?: string
  credit_limit?: number
  branch_id: string
}

export interface NewCustomerResult {
  id: string
  num: number
  written_at: string
}

// PUT /v1/tenants/{id}/hq/customers/{customerId} — flat partial update; every
// field optional, only provided fields are changed. "Deactivate" is just
// is_active:false through this same call.
export interface CustomerEditInput {
  name?: string
  phone1?: string
  phone2?: string
  phone3?: string
  address?: string
  note?: string
  group_id?: string
  credit_limit?: number
  is_active?: boolean
}

export interface UpdateCustomerResult {
  written_at: string
}

// PUT /v1/tenants/{id}/hq/customers/bulk — at least one of group_id/price_tier
// is required.
export interface BulkUpdateCustomersInput {
  ids: string[]
  group_id?: string
  price_tier?: number
}

export interface BulkUpdateCustomersResult {
  updated: number
  written_at: string
}

// POST /v1/tenants/{id}/hq/customers/import — one bad row never aborts the
// batch; each failure is reported here instead.
export interface ImportCustomersError {
  row: number
  message: string
}

export interface ImportCustomersResult {
  created: number
  errors: ImportCustomersError[]
}

// --- HQ suppliers (slice 8; hq/service.go's Supplier* methods) ---
//
// Mirrors the Customer types above field-for-field — same underlying
// Customer table/entity on the gateway (Type == Supplier instead of
// Customer). CustomerGroup/CustomerGroupsResponse above are reused as-is
// for suppliers: groups aren't type-scoped in the schema, no
// SupplierGroup type exists.

// Debt/credit filter for the supplier list, profile insights, and export.
export type SupplierDebtFilter = 'has_debt' | 'credit' | 'exceeding'

// One row of the paged supplier list.
export interface SupplierRow {
  id: string
  num: number
  name: string
  branch_id: string
  branch_name: string
  health: BranchHealth
  group_id?: string | null
  group_name?: string | null
  phone1: string
  is_active: boolean
  balance: number
  credit_limit: number
  is_credit: boolean
  last_purchase_at?: string | null
}

export interface SuppliersPage {
  total: number
  page: number
  page_size: number
  items: SupplierRow[]
}

// GET /v1/tenants/{id}/hq/suppliers
export type SuppliersResponse = CatalogEnvelope<SuppliersPage>

// One supplier's purchase performance (bills the business received from the
// supplier), straight off the gateway's Bills aggregate.
export interface SupplierStats {
  number_of_orders: number
  total_spent: number
  average_order_value: number
  last_purchase_date?: string | null
}

export interface SupplierDetail {
  id: string
  num: number
  name: string
  branch_id: string
  branch_name: string
  health: BranchHealth
  group_id?: string | null
  group_name?: string | null
  phone1: string
  phone2?: string | null
  phone3?: string | null
  address?: string | null
  note?: string | null
  credit_limit: number
  is_credit: boolean
  is_active: boolean
  balance: number
  stats: SupplierStats
}

// GET /v1/tenants/{id}/hq/suppliers/{supplierId}
export type SupplierDetailResponse = CatalogEnvelope<SupplierDetail>

// One purchase (a Bill the business received from the supplier), newest
// first.
export interface SupplierPurchaseRow {
  id: string
  num: string
  issued_at: string
  total: number
  item_count: number
  is_paid: boolean
  type: number
}

// --- HQ orders (T19; hq/service_orders.go + Program.cs's /hq/orders*
// routes) ---
//
// Status/channel travel as the same numeric values as the desktop's
// OrderStatus/OrderChannel enums (AribONE.Data/Models) — neither side
// applies a string enum converter.

export const ORDER_STATUS = {
  New: 0,
  Preparing: 1,
  Ready: 2,
  OutForDelivery: 3,
  Delivered: 4,
  Cancelled: 5,
  Transferred: 6,
} as const
export type OrderStatusValue = (typeof ORDER_STATUS)[keyof typeof ORDER_STATUS]

export const ORDER_CHANNEL = {
  CallCenter: 1,
  Branch: 2,
  Sales: 3,
} as const
export type OrderChannelValue = (typeof ORDER_CHANNEL)[keyof typeof ORDER_CHANNEL]

// One row of the paged order list.
export interface OrderRow {
  id: string
  ref: string
  customer_name: string
  phone: string
  branch_id: string
  branch_name: string
  total: number
  delivery_fee: number
  status: OrderStatusValue
  channel: OrderChannelValue
  created_at: string
}

export interface OrdersPage {
  total: number
  page: number
  page_size: number
  items: OrderRow[]
}

// GET /v1/tenants/{id}/hq/orders
export type OrdersResponse = CatalogEnvelope<OrdersPage>

// --- Promotions (spec-promotions.md, T137) --------------------------------

// Mirrors AribONE.Data's PromotionLevel / PromotionScope / PromotionTargetKind
// and the existing DiscountType. Persisted as ints, so the console must not
// reorder them.
export const PROMOTION_LEVEL = {
  Item: 0,
  Bill: 1,
} as const
export type PromotionLevelValue = (typeof PROMOTION_LEVEL)[keyof typeof PROMOTION_LEVEL]

export const PROMOTION_SCOPE = {
  AllProducts: 0,
  Products: 1,
  Groups: 2,
} as const
export type PromotionScopeValue = (typeof PROMOTION_SCOPE)[keyof typeof PROMOTION_SCOPE]

export const PROMOTION_TARGET_KIND = {
  Product: 0,
  Group: 1,
} as const
export type PromotionTargetKindValue =
  (typeof PROMOTION_TARGET_KIND)[keyof typeof PROMOTION_TARGET_KIND]

export const DISCOUNT_TYPE = {
  Percentage: 0,
  Fixed: 1,
} as const
export type DiscountTypeValue = (typeof DISCOUNT_TYPE)[keyof typeof DISCOUNT_TYPE]

/**
 * A promotion's lifecycle state, **derived server-side** by the gateway
 * (HqApi.PromotionStatusOf) and passed through untouched. The console must
 * never recompute it from the dates: the till reads the same function, and a
 * second client-side definition of "active" is a second thing that can
 * disagree with what a cashier is actually getting (spec D8).
 *
 * The four are mutually exclusive and exhaustive, so filter tab counts add up
 * to the unfiltered total. `paused` beats the dates — a switched-off future
 * campaign is off, not scheduled.
 */
export type PromotionStatus = 'active' | 'scheduled' | 'expired' | 'paused'

/** The `status` query filter, which additionally accepts 'all'. */
export type PromotionStatusFilter = PromotionStatus | 'all'

/** One product or group an item-level promotion applies to. */
export interface PromotionTarget {
  kind: PromotionTargetKindValue
  ref_id: string
  // null when the referenced row no longer resolves — the target is still
  // listed rather than dropped, so the author can see and remove it.
  name?: string | null
}

/** One row of the paged promotion list. */
export interface Promotion {
  id: string
  name: string
  level: PromotionLevelValue
  scope: PromotionScopeValue
  discount_type: DiscountTypeValue
  value: number
  // null = company-wide: applies at every branch (spec D2).
  branch_id?: string | null
  branch_name?: string | null
  // Calendar dates, both bounds inclusive, evaluated in branch-local time
  // (spec D8). Serialized as midnight UTC — the date is the payload, the
  // time is not, so never render these through a timezone conversion.
  starts_on: string
  ends_on: string
  is_active: boolean
  min_qty?: number | null
  min_bill_total?: number | null
  created_at: string
  status: PromotionStatus
  target_count: number
}

/** A promotion plus its resolved target list. */
export interface PromotionDetail extends Promotion {
  targets: PromotionTarget[]
}

export interface PromotionsPage {
  total: number
  page: number
  page_size: number
  items: Promotion[]
}

// GET /v1/tenants/{id}/hq/promotions
export type PromotionsResponse = CatalogEnvelope<PromotionsPage>

// GET /v1/tenants/{id}/hq/promotions/{promotionId}
export type PromotionResponse = CatalogEnvelope<PromotionDetail>

/**
 * The body of both POST and PUT — there is no partial-patch variant, because
 * a promotion is a small wholly-authored rule and the form always sends the
 * complete thing back.
 */
export interface PromotionInput {
  name: string
  level: PromotionLevelValue
  scope: PromotionScopeValue
  discount_type: DiscountTypeValue
  value: number
  branch_id: string | null
  starts_on: string
  ends_on: string
  is_active: boolean
  min_qty: number | null
  min_bill_total: number | null
  targets: { kind: PromotionTargetKindValue; ref_id: string }[]
}

export interface PromotionWriteResult {
  id: string
  written_at: string
}

// --- Promotion performance (spec-promotions.md, T151-T153) -----------------
//
// GET /v1/tenants/{id}/hq/promotions/{promotionId}/performance. Same
// bills/items/amount triple at three grains (overall, per branch, per day) —
// one shape, three slices.

/** One branch's slice of a promotion's period. `branch_id` is never null
 * here, unlike `Promotion.branch_id`: a `PromotionApplication` always names
 * the real branch where the sale happened, so there is no company-wide row
 * at this grain the way there is in the promotion list itself. */
export interface PromotionPerformanceBranch {
  branch_id: string
  branch_name?: string | null
  bills_count: number
  items_count: number
  total_amount: number
}

/** One local calendar day of the series — a date string, not an instant,
 * same convention as `SalesDay`. */
export interface PromotionPerformanceDay {
  day: string
  bills_count: number
  items_count: number
  total_amount: number
}

/**
 * One promotion's applied history over a period. `bills_count` is distinct
 * invoices touched, never row count — an item promotion hitting three lines
 * on one bill is one bill and three items (`items_count`). `total_amount`
 * sums both levels: it equals Σ PromotionApplications.Amount for the
 * promotion over the same window (spec success criterion 7).
 *
 * `bills_discount_total` is Σ Invoice.ItemDiscount/BillDiscount — the WHOLE
 * column, not this promotion's own slice of it — over exactly the invoices
 * `bills_count` counts. `bills_discount_total - total_amount` is therefore
 * how much OTHER discounting (a manual entry, or a different promotion)
 * happened on those same bills: the reconciliation invariant (spec D6), read
 * here rather than stored as a separate figure. Compute the split where it
 * is displayed, not in a shared helper — it is one subtraction, and a helper
 * would only hide that it is exactly the invariant and nothing more.
 */
export interface PromotionPerformance {
  from: string
  to: string
  bills_count: number
  items_count: number
  total_amount: number
  bills_discount_total: number
  by_branch: PromotionPerformanceBranch[]
  by_day: PromotionPerformanceDay[]
}

// GET /v1/tenants/{id}/hq/promotions/{promotionId}/performance
export type PromotionPerformanceResponse = CatalogEnvelope<PromotionPerformance>

// T21: new-order workspace (pages/console/NewOrder.tsx).

export const ORDER_MODE = {
  Pickup: 1,
  Delivery: 2,
} as const
export type OrderModeValue = (typeof ORDER_MODE)[keyof typeof ORDER_MODE]

// GET /v1/tenants/{id}/hq/orders/availability (T17) — one branch's on-hand/
// committed/available read for a whole cart in a single call. Never hides
// or zeroes numbers when the branch's sync is stale — is_fresh/last_sync_at
// say so instead, and it's on the caller (this page) to warn, not block.
export interface OrderAvailabilityLine {
  product_id: string
  product_name: string
  on_hand: number
  committed: number
  available: number
}

export interface OrderAvailability {
  last_sync_at?: string | null
  is_fresh: boolean
  lines: OrderAvailabilityLine[]
}

export type OrderAvailabilityResponse = CatalogEnvelope<OrderAvailability>

// GET /v1/tenants/{id}/hq/orders/delivery-fee (T3b, plan OQ1) — a preview of
// the fee POST /hq/orders would resolve for this branch/customer, so the
// screen can show the number before the operator saves rather than only
// after. "Customer" | "Zone" | "BranchDefault" | "None" mirror the desktop's
// own DeliveryFeeSource one-to-one — surfaced as the hint next to the field.
export type DeliveryFeeSource = 'Customer' | 'Zone' | 'BranchDefault' | 'None'

export interface DeliveryFeeResolution {
  fee: number
  source: DeliveryFeeSource
}

export type DeliveryFeeResponse = CatalogEnvelope<DeliveryFeeResolution>

// POST /v1/tenants/{id}/hq/orders (T16) — HQ's only write path into a
// tenant's orders (D9). No discount field anywhere: D2's Order schema
// carries none, unlike Invoice's ~30 money columns.
export interface NewOrderLineInput {
  product_id: string
  unit_id: string
  qty: number
  price: number
}

export interface NewOrderInput {
  branch_id: string
  partner_id: string
  created_by_name: string
  mode: OrderModeValue
  contact_address?: string
  delivery_fee?: number
  due_at?: string
  note?: string
  lines: NewOrderLineInput[]
}

export interface NewOrderResult {
  id: string
  ref: string
  written_at: string
}

// One line whose requested qty exceeds what D16's gate allows at the chosen
// branch — the gateway's 409 refusal body (api.ts's OrderUnavailableError
// carries this verbatim rather than just a message, so the cart can
// highlight the exact short lines with no second round trip).
export interface OrderShortfall {
  product_id: string
  product_name: string
  requested: number
  available: number
}

// T22: order detail (pages/console/OrderDetail.tsx).

export interface OrderLine {
  product_id: string
  product_name: string
  unit_id: string
  unit_name: string
  qty: number
  price: number
  total: number
}

// One link in the D7 transfer chain, including the requested order itself —
// the gateway walks the whole chain by matching Ref, not PreviousOrderId, so
// this list is already in chain order and needs no client-side sorting.
export interface OrderChainEntry {
  id: string
  branch_id: string
  branch_name: string
  status: OrderStatusValue
  previous_order_id?: string | null
  created_at: string
  status_changed_at?: string | null
}

export interface OrderDetail {
  id: string
  ref: string
  status: OrderStatusValue
  channel: OrderChannelValue
  branch_id: string
  branch_name: string
  partner_id: string
  customer_name: string
  phone?: string | null
  address?: string | null
  mode: OrderModeValue
  total: number
  delivery_fee?: number | null
  created_by_name?: string | null
  created_at: string
  due_at?: string | null
  status_changed_at?: string | null
  note?: string | null
  cancel_reason?: string | null
  sale_id?: string | null
  lines: OrderLine[]
  history: OrderChainEntry[]
}

export type OrderDetailResponse = CatalogEnvelope<OrderDetail>

export interface CancelOrderResult {
  written_at: string
}

export interface TransferOrderResult {
  id: string
  ref: string
  written_at: string
}

export interface SupplierPurchasesPage {
  total: number
  page: number
  page_size: number
  items: SupplierPurchaseRow[]
}

// GET /v1/tenants/{id}/hq/suppliers/{supplierId}/purchases
export type SupplierPurchasesResponse = CatalogEnvelope<SupplierPurchasesPage>

// One ledger (CustomerTransaction) row, running balance already computed
// server-side.
export interface SupplierLedgerRow {
  id: string
  created_at: string
  dealing: number
  total: number
  debit: number
  credit: number
  running_balance: number
  note?: string | null
  user_id: string
}

export interface SupplierLedgerPage {
  total: number
  page: number
  page_size: number
  items: SupplierLedgerRow[]
}

// GET /v1/tenants/{id}/hq/suppliers/{supplierId}/ledger
export type SupplierLedgerResponse = CatalogEnvelope<SupplierLedgerPage>

export interface SupplierInsights {
  top_customers: CustomerInsightRow[]
  new_this_month: CustomerRefList
  inactive: CustomerRefList
  credit_limit_warnings: CreditWarningRow[]
  highest_spenders: CustomerInsightRow[]
  growth_over_time: CustomerGrowthDay[]
}

// GET /v1/tenants/{id}/hq/suppliers/insights
export type SupplierInsightsResponse = CatalogEnvelope<SupplierInsights>

// POST /v1/tenants/{id}/hq/suppliers — bounded create, no opening balance in
// v1 (mirrors NewCustomerInput).
export interface NewSupplierInput {
  name: string
  phone1: string
  phone2?: string
  phone3?: string
  address?: string
  note?: string
  group_id?: string
  credit_limit?: number
  branch_id: string
}

export interface NewSupplierResult {
  id: string
  num: number
  written_at: string
}

// PUT /v1/tenants/{id}/hq/suppliers/{supplierId} — flat partial update;
// every field optional, only provided fields are changed. "Deactivate" is
// just is_active:false through this same call.
export interface SupplierEditInput {
  name?: string
  phone1?: string
  phone2?: string
  phone3?: string
  address?: string
  note?: string
  group_id?: string
  credit_limit?: number
  is_active?: boolean
}

export interface UpdateSupplierResult {
  written_at: string
}

// PUT /v1/tenants/{id}/hq/suppliers/bulk — at least one of group_id/price_tier
// is required.
export interface BulkUpdateSuppliersInput {
  ids: string[]
  group_id?: string
  price_tier?: number
}

export interface BulkUpdateSuppliersResult {
  updated: number
  written_at: string
}

// POST /v1/tenants/{id}/hq/suppliers/import — one bad row never aborts the
// batch; each failure is reported here instead.
export interface ImportSuppliersError {
  row: number
  message: string
}

export interface ImportSuppliersResult {
  created: number
  errors: ImportSuppliersError[]
}

// auth session (sessionResponse map in auth_handlers.go)
export interface Session {
  access_token: string
  refresh_token: string
  expires_in: number
  account_id: string
  email: string
}

// GET /v1/me returns a ClientView (account + licenses + devices); the console
// only needs the account identity here.
export interface Account {
  ID: string
  Email: string
  FirstName: string
  LastName: string
  Providers: Provider[] | null
  // Was this account ever invited as a non-owner member of some tenant?
  // Set once, server-side, and never cleared (survives revocation) — used
  // to keep a revoked (or still-active) member from reaching the
  // self-serve "create a tenant" flow, which is an owner-signup path.
  HasBeenMember?: boolean
  CreatedAt: string
  UpdatedAt: string
}

export interface MeView {
  account: Account
}
