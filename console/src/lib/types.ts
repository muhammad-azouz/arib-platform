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

// GET /v1/tenants/{id} — the activation/login bundle. `Company` is null until
// the company is registered; the Setup-Wizard completion gate keys off this.
export interface Bundle {
  Tenant: Tenant
  Company: Company | null
  Branches: Branch[] | null
}

// --- hand-written response maps (snake_case keys) ---

// POST /v1/tenants/{id}/sync-token
export interface SyncToken {
  token: string
  expires_at: string
  db_name: string
  gateway_url: string
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
  CreatedAt: string
  UpdatedAt: string
}

export interface MeView {
  account: Account
}
