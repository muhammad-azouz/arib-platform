package hq

// Promotions (T134): typed proxy for the six /hq/promotions* endpoints the
// gateway implements (T130-T132), following service_orders.go's shape — a
// separate file because six endpoints plus their request/response types are
// enough to warrant their own home.
//
// Unlike Orders, this file is NOT purely a marshaller. It carries two things
// of its own:
//
//  1. The first copy of the validation table. The gateway holds the second
//     (HqApi.ValidatePromotion). Duplicating it is deliberate: this copy
//     fails fast with one Arabic message the console can show verbatim,
//     while the gateway's copy is the one that guarantees a caller who
//     bypasses this layer still cannot store a 150% promotion.
//
//  2. Spec D14's branch-scoping rules, which are sharper here than anywhere
//     else in the package because a promotion's branch is OPTIONAL. A null
//     branch_id means company-wide, so it is a Tier-A write landing at every
//     branch — and a branch allowlist cannot authorize an operation it has
//     no way to check against. See promotionWriteScope below.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aribpos/license-api/internal/model"
	"github.com/aribpos/license-api/internal/perm"
)

// --- wire types ---

// PromotionRow is one promotion in the list. Status is derived server-side
// by the gateway (HqApi.PromotionStatusOf) and passed through untouched —
// the console must never recompute it, or it and the till will disagree
// about what a cashier is currently getting.
type PromotionRow struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Level        int       `json:"level"`
	Scope        int       `json:"scope"`
	DiscountType int       `json:"discount_type"`
	Value        float64   `json:"value"`
	BranchID     *string   `json:"branch_id"`
	BranchName   *string   `json:"branch_name"`
	StartsOn     time.Time `json:"starts_on"`
	EndsOn       time.Time `json:"ends_on"`
	IsActive     bool      `json:"is_active"`
	MinQty       *float64  `json:"min_qty"`
	MinBillTotal *float64  `json:"min_bill_total"`
	CreatedAt    time.Time `json:"created_at"`
	Status       string    `json:"status"`
	TargetCount  int       `json:"target_count"`
}

type PromotionsPage struct {
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Items    []PromotionRow `json:"items"`
}

type PromotionsEnvelope struct {
	Data   PromotionsPage `json:"data"`
	Source string         `json:"source"`
	AsOf   *time.Time     `json:"as_of,omitempty"`
}

// PromotionTargetRow is one product or group an item-level promotion applies
// to. Name is nil when the referenced row no longer exists — the target is
// still returned rather than dropped, so the author can see and remove it.
type PromotionTargetRow struct {
	Kind  int     `json:"kind"`
	RefID string  `json:"ref_id"`
	Name  *string `json:"name"`
}

type PromotionDetail struct {
	PromotionRow
	Targets []PromotionTargetRow `json:"targets"`
}

type PromotionEnvelope struct {
	Data   PromotionDetail `json:"data"`
	Source string          `json:"source"`
	AsOf   *time.Time      `json:"as_of,omitempty"`
}

// PromotionTargetInput is one target in a create/update body.
type PromotionTargetInput struct {
	Kind  int    `json:"kind"`
	RefID string `json:"ref_id"`
}

// PromotionInput is the whole promotion, for both create and update — there
// is no partial-patch variant, because a promotion is a small wholly-authored
// rule and the console always sends the complete form back.
type PromotionInput struct {
	Name         string                 `json:"name"`
	Level        int                    `json:"level"`
	Scope        int                    `json:"scope"`
	DiscountType int                    `json:"discount_type"`
	Value        float64                `json:"value"`
	BranchID     *string                `json:"branch_id"`
	StartsOn     time.Time              `json:"starts_on"`
	EndsOn       time.Time              `json:"ends_on"`
	IsActive     bool                   `json:"is_active"`
	MinQty       *float64               `json:"min_qty"`
	MinBillTotal *float64               `json:"min_bill_total"`
	Targets      []PromotionTargetInput `json:"targets"`
}

// PromotionWriteResult is the gateway's write receipt.
type PromotionWriteResult struct {
	ID        string    `json:"id"`
	WrittenAt time.Time `json:"written_at"`
}

// Promotion level/scope/target-kind values, mirroring the C# enums in
// AribONE.Data (PromotionLevel, PromotionScope, PromotionTargetKind) and the
// existing DiscountType. Named here so the validation table below reads as
// rules rather than as magic numbers.
const (
	promotionLevelItem = 0
	promotionLevelBill = 1

	promotionScopeAllProducts = 0
	promotionScopeProducts    = 1
	promotionScopeGroups      = 2

	promotionTargetProduct = 0
	promotionTargetGroup   = 1

	discountTypePercentage = 0
	discountTypeFixed      = 1
)

// PromotionInvalidError is a validation refusal. Message is Arabic and
// console-ready; Field names the offending input so the form can highlight it
// rather than showing a bare banner.
type PromotionInvalidError struct {
	Field   string
	Message string
}

func (e *PromotionInvalidError) Error() string { return e.Message }

func promotionInvalid(field, message string) *PromotionInvalidError {
	return &PromotionInvalidError{Field: field, Message: message}
}

// validatePromotion is the first of the feature's two validation copies; the
// gateway's HqApi.ValidatePromotion is the second and the authoritative one.
// Kept in the same order as its twin so the two can be diffed by eye.
//
// The level cross-checks are the substantive half: Scope, MinQty and the
// target list are meaningless at Bill level and MinBillTotal is meaningless at
// Item level (spec D4). They are refused rather than silently dropped — a
// caller sending MinQty on a bill promotion has misunderstood something, and
// quietly discarding the field would ship that misunderstanding into a live
// campaign that then does not behave as its author believes.
func validatePromotion(in PromotionInput) *PromotionInvalidError {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return promotionInvalid("name", "اسم العرض مطلوب")
	}
	if len([]rune(name)) > 100 {
		return promotionInvalid("name", "اسم العرض لا يتجاوز ١٠٠ حرف")
	}

	if in.Level != promotionLevelItem && in.Level != promotionLevelBill {
		return promotionInvalid("level", "مستوى الخصم غير صحيح")
	}
	if in.Scope != promotionScopeAllProducts && in.Scope != promotionScopeProducts && in.Scope != promotionScopeGroups {
		return promotionInvalid("scope", "نطاق العرض غير صحيح")
	}
	if in.DiscountType != discountTypePercentage && in.DiscountType != discountTypeFixed {
		return promotionInvalid("discount_type", "نوع الخصم غير صحيح")
	}

	if in.Value <= 0 {
		return promotionInvalid("value", "قيمة الخصم يجب أن تكون أكبر من صفر")
	}
	// A fixed amount has no ceiling — it is clamped to the line or the bill at
	// billing time (D5). A percentage over 100 would hand money back.
	if in.DiscountType == discountTypePercentage && in.Value > 100 {
		return promotionInvalid("value", "نسبة الخصم لا تتجاوز ١٠٠٪")
	}

	if in.EndsOn.Before(in.StartsOn) {
		return promotionInvalid("ends_on", "تاريخ الانتهاء قبل تاريخ البداية")
	}

	if in.Level == promotionLevelBill {
		if in.Scope != promotionScopeAllProducts {
			return promotionInvalid("scope", "خصم الفاتورة يطبق على الفاتورة كاملة")
		}
		if len(in.Targets) > 0 {
			return promotionInvalid("targets", "خصم الفاتورة لا يحدد أصنافاً")
		}
		if in.MinQty != nil {
			return promotionInvalid("min_qty", "الحد الأدنى للكمية يخص خصم الصنف فقط")
		}
	} else {
		if in.MinBillTotal != nil {
			return promotionInvalid("min_bill_total", "الحد الأدنى للفاتورة يخص خصم الفاتورة فقط")
		}
		// Storewide is the ABSENCE of targets, never a magic row (D3) — so an
		// accidentally populated list can never silently mean something else,
		// and an accidentally empty one can never silently become "everything".
		if in.Scope == promotionScopeAllProducts && len(in.Targets) > 0 {
			return promotionInvalid("targets", "عرض كل الأصناف لا يحدد أصنافاً بعينها")
		}
		if in.Scope != promotionScopeAllProducts && len(in.Targets) == 0 {
			return promotionInvalid("targets", "اختر صنفاً واحداً على الأقل")
		}
		wanted := promotionTargetProduct
		if in.Scope == promotionScopeGroups {
			wanted = promotionTargetGroup
		}
		for _, t := range in.Targets {
			if t.Kind != wanted {
				return promotionInvalid("targets", "نوع الأصناف المحددة لا يطابق نطاق العرض")
			}
			if strings.TrimSpace(t.RefID) == "" {
				return promotionInvalid("targets", "أحد الأصناف المحددة غير صالح")
			}
		}
	}

	if in.MinQty != nil && *in.MinQty <= 0 {
		return promotionInvalid("min_qty", "الحد الأدنى للكمية يجب أن يكون أكبر من صفر")
	}
	if in.MinBillTotal != nil && *in.MinBillTotal <= 0 {
		return promotionInvalid("min_bill_total", "الحد الأدنى للفاتورة يجب أن يكون أكبر من صفر")
	}
	return nil
}

// --- D14 scoping ---

// promotionWriteScope authorizes a write against its TARGET branch (spec D14).
//
// The null case is the one that matters and the reason this cannot reuse
// requireBranchInScope alone: a promotion with no branch_id is company-wide,
// so writing it is a Tier-A operation landing at every branch — exactly like
// POST /hq/catalog/products. A branch allowlist has nothing to check such an
// operation against, so it takes an unscoped member (D5c).
func promotionWriteScope(scope *perm.Scope, branchID *string) error {
	if branchID == nil {
		return requireUnscoped(scope)
	}
	return requireBranchInScope(scope, *branchID)
}

// promotionExistingScope authorizes a write against the row as it stands
// TODAY, before the new values are considered. Update and delete both call it,
// and update then also calls promotionWriteScope on the incoming branch — so
// re-scoping a promotion from branch A to branch B requires both ends, the
// same way an order transfer checks its destination as well as its origin.
//
// The two refusals are deliberately different HTTP outcomes:
//
//   - An out-of-allowlist BRANCH promotion is ErrNotFound (404), never 403.
//     A scoped member must not be able to tell "no such promotion" apart from
//     "exists at a branch you cannot see" by probing ids.
//   - An existing COMPANY-WIDE promotion is ErrForbiddenUnscoped (403). There
//     is nothing to hide — a scoped member can already see company-wide
//     promotions in the list, because they apply at their branch too. What
//     they cannot do is edit one, and 404 would be a lie about a row that is
//     visibly right there.
func promotionExistingScope(scope *perm.Scope, branchID *string) error {
	if scope == nil || scope.IsUnscoped() {
		return nil
	}
	if branchID == nil {
		return ErrForbiddenUnscoped
	}
	return hideOutOfScopeRow(scope, *branchID)
}

// promotionBranchID fetches just the branch_id of an existing promotion, so a
// scoped caller's write can be checked against the row as it stands. Distinct
// from rowBranchID because a promotion's branch is nullable and the null is
// meaningful — decoding it into a plain string would flatten "company-wide"
// into "", which AllowsBranch would then simply refuse, turning a 403 into a
// 404 and losing the distinction promotionExistingScope exists to draw.
//
// The write paths (Update, Delete) call this only for a scoped caller, as a
// pure optimization — an unscoped one never pays the extra gateway round
// trip. PromotionPerformance below is the one caller that invokes it
// unconditionally, because here it also doubles as the endpoint's existence
// check; see that function's comment.
func (s *Service) promotionBranchID(ctx context.Context, shard *model.Shard, dbName, promotionID string) (*string, error) {
	var raw struct {
		BranchID *string `json:"branch_id"`
	}
	if err := s.getJSON(ctx, shard.GatewayURL+"/hq/promotions/"+promotionID, dbName, &raw); err != nil {
		return nil, err
	}
	return raw.BranchID, nil
}

// --- reads ---

// Promotions returns one page of the promotion list.
//
// Note what applyScope does and does not do here. It injects a scoped
// member's allowlist as repeated branch_id params, which narrows the
// BRANCH-scoped rows — and the gateway's own query always admits company-wide
// rows regardless of that filter (`BranchId IS NULL OR BranchId IN (...)`).
// So a scoped member sees company-wide promotions plus their own branches',
// which is D14's read rule, without this layer doing anything special. A
// caller-supplied branch_id outside the allowlist is still ErrForbiddenScope,
// never silently narrowed.
func (s *Service) Promotions(ctx context.Context, accountID, tenantID string, params url.Values) (*PromotionsEnvelope, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	params, err = applyScope(params, scope)
	if err != nil {
		return nil, err
	}

	u := shard.GatewayURL + "/hq/promotions"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var page PromotionsPage
	if err := s.getJSON(ctx, u, t.DBName, &page); err != nil {
		return nil, err
	}
	if page.Items == nil {
		page.Items = []PromotionRow{}
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &PromotionsEnvelope{Data: page, Source: source, AsOf: asOf}, nil
}

// PromotionDetail returns one promotion with its targets. A promotion at a
// branch outside a scoped caller's allowlist is ErrNotFound, not
// ErrForbiddenScope — see promotionExistingScope. A company-wide promotion is
// readable by everyone, because it applies at their branch too.
func (s *Service) PromotionDetail(ctx context.Context, accountID, tenantID, promotionID string) (*PromotionEnvelope, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	var detail PromotionDetail
	if err := s.getJSON(ctx, shard.GatewayURL+"/hq/promotions/"+promotionID, t.DBName, &detail); err != nil {
		return nil, err
	}
	if detail.BranchID != nil {
		if err := hideOutOfScopeRow(scope, *detail.BranchID); err != nil {
			return nil, err
		}
	}
	if detail.Targets == nil {
		detail.Targets = []PromotionTargetRow{}
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &PromotionEnvelope{Data: detail, Source: source, AsOf: asOf}, nil
}

// --- performance (T151/T152) ---

// PromotionPerformanceBranch is one branch's slice of a promotion's period —
// bills/items/amount at branch grain. BranchID is never nil here, unlike
// PromotionRow.BranchID: a PromotionApplication always names the real branch
// where the sale happened (see the gateway entity's own doc comment), so
// there is no company-wide row at this grain the way there is in the
// promotion list itself.
type PromotionPerformanceBranch struct {
	BranchID    string  `json:"branch_id"`
	BranchName  *string `json:"branch_name"`
	BillsCount  int     `json:"bills_count"`
	ItemsCount  int     `json:"items_count"`
	TotalAmount float64 `json:"total_amount"`
}

// PromotionPerformanceDay is one local calendar day of the series — a plain
// YYYY-MM-DD string in the tenant's day-scope, matching SalesDay's own
// reasoning: it is a date, not an instant.
type PromotionPerformanceDay struct {
	Day         string  `json:"day"`
	BillsCount  int     `json:"bills_count"`
	ItemsCount  int     `json:"items_count"`
	TotalAmount float64 `json:"total_amount"`
}

// PromotionPerformance is one promotion's applied history over a period.
// BillsCount is distinct invoices touched, never row count — an item
// promotion hitting three lines on one bill is one bill and three items
// (ItemsCount). TotalAmount sums BOTH levels: spec success criterion 7 needs
// it to equal Σ PromotionApplications.Amount for the promotion over the same
// window, and restricting to one level would break that the first time a
// promotion fires at both. From/To echo the gateway's resolved period, same
// convention as SalesReport.
//
// BillsDiscountTotal (T153) is Σ Invoice.ItemDiscount/BillDiscount — the
// WHOLE column, picked per touched invoice by that invoice's own recorded
// level — over exactly the invoices BillsCount counts. It is deliberately
// not restricted to this promotion's own rows: spec D10 (no cashier
// override) plus D11 (manual and promotional never share a line or bill)
// already guarantee this promotion's own footprint can never carry a manual
// component, so a figure computed only from its own rows would be a
// constant 100%/0% split. Summing the whole column is what lets the console
// derive BillsDiscountTotal − TotalAmount = how much OTHER discounting
// (manual entries, or another promotion) happened on the same bills — the
// reconciliation invariant (D6), read at report time rather than stored.
type PromotionPerformance struct {
	From               string                       `json:"from"`
	To                 string                       `json:"to"`
	BillsCount         int                          `json:"bills_count"`
	ItemsCount         int                          `json:"items_count"`
	TotalAmount        float64                      `json:"total_amount"`
	BillsDiscountTotal float64                      `json:"bills_discount_total"`
	ByBranch           []PromotionPerformanceBranch `json:"by_branch"`
	ByDay              []PromotionPerformanceDay    `json:"by_day"`
}

// PromotionPerformanceEnvelope wraps the performance report in the freshness
// envelope, same as every other tenant-DB-backed read.
type PromotionPerformanceEnvelope struct {
	Data   PromotionPerformance `json:"data"`
	Source string               `json:"source"`
	AsOf   *time.Time           `json:"as_of,omitempty"`
}

// PromotionPerformance returns one promotion's performance over params'
// from/to window, gated and branch-scoped identically to PromotionDetail —
// deliberately, per spec: an out-of-allowlist branch promotion is
// ErrNotFound here too, not ErrForbiddenScope, so a scoped member cannot
// distinguish "no such promotion" from "exists somewhere you can't see" by
// probing this endpoint either.
//
// The promotionBranchID call below is NOT the write paths' scoped-only
// optimization — it runs for every caller, scoped or not, because it is what
// turns a bogus promotion id into 404 here. The gateway's aggregate (T151)
// deliberately does not distinguish an unknown/deleted id from "never
// applied" — duplicating that existence check gateway-side would be a second
// source of truth for it — so this is the layer where 404 belongs.
func (s *Service) PromotionPerformance(ctx context.Context, accountID, tenantID, promotionID string, params url.Values) (*PromotionPerformanceEnvelope, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	branchID, err := s.promotionBranchID(ctx, shard, t.DBName, promotionID)
	if err != nil {
		return nil, err
	}
	if branchID != nil {
		if err := hideOutOfScopeRow(scope, *branchID); err != nil {
			return nil, err
		}
	}
	params, err = applyScope(params, scope)
	if err != nil {
		return nil, err
	}

	u := shard.GatewayURL + "/hq/promotions/" + promotionID + "/performance"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var resp PromotionPerformance
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	if resp.ByBranch == nil {
		resp.ByBranch = []PromotionPerformanceBranch{}
	}
	if resp.ByDay == nil {
		resp.ByDay = []PromotionPerformanceDay{}
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &PromotionPerformanceEnvelope{Data: resp, Source: source, AsOf: asOf}, nil
}

// --- writes ---

// CreatePromotion validates, authorizes against the target branch (D14), then
// forwards. A company-wide promotion (branch_id null) requires an unscoped
// member.
func (s *Service) CreatePromotion(ctx context.Context, accountID, tenantID string, in PromotionInput) (*PromotionWriteResult, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if verr := validatePromotion(in); verr != nil {
		return nil, verr
	}
	if err := promotionWriteScope(scope, in.BranchID); err != nil {
		return nil, err
	}
	return s.writePromotion(ctx, http.MethodPost, shard.GatewayURL+"/hq/promotions", t.DBName, in)
}

// UpdatePromotion authorizes BOTH ends of a possible re-scope: the row as it
// stands (promotionExistingScope) and the branch it is being moved to
// (promotionWriteScope). Moving a promotion from branch A to branch B
// therefore needs both in the allowlist, and moving one to company-wide needs
// an unscoped member.
func (s *Service) UpdatePromotion(ctx context.Context, accountID, tenantID, promotionID string, in PromotionInput) (*PromotionWriteResult, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if verr := validatePromotion(in); verr != nil {
		return nil, verr
	}
	if scope != nil && !scope.IsUnscoped() {
		current, err := s.promotionBranchID(ctx, shard, t.DBName, promotionID)
		if err != nil {
			return nil, err
		}
		if err := promotionExistingScope(scope, current); err != nil {
			return nil, err
		}
	}
	if err := promotionWriteScope(scope, in.BranchID); err != nil {
		return nil, err
	}
	return s.writePromotion(ctx, http.MethodPut, shard.GatewayURL+"/hq/promotions/"+promotionID, t.DBName, in)
}

// DeletePromotion soft-deletes (D9): the row survives so the
// PromotionApplications rows written against it stay explicable.
func (s *Service) DeletePromotion(ctx context.Context, accountID, tenantID, promotionID string) (*PromotionWriteResult, error) {
	t, shard, scope, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	if scope != nil && !scope.IsUnscoped() {
		current, err := s.promotionBranchID(ctx, shard, t.DBName, promotionID)
		if err != nil {
			return nil, err
		}
		if err := promotionExistingScope(scope, current); err != nil {
			return nil, err
		}
	}
	return s.writePromotion(ctx, http.MethodDelete, shard.GatewayURL+"/hq/promotions/"+promotionID, t.DBName, nil)
}

// writePromotion performs one HQ-token-authed gateway write and maps its
// statuses. The gateway's 400 carries a sibling "field" naming what failed;
// it is surfaced as a PromotionInvalidError so a rule this layer's own table
// happens to miss still reaches the console as a field-level error rather
// than a generic 500.
func (s *Service) writePromotion(ctx context.Context, method, url, dbName string, in any) (*PromotionWriteResult, error) {
	tok, err := s.tokens.IssueHQToken(dbName)
	if err != nil {
		return nil, fmt.Errorf("mint hq token: %w", err)
	}
	var body []byte
	if in != nil {
		if body, err = json.Marshal(in); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGatewayUnreachable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusBadRequest:
		var b struct {
			Error string `json:"error"`
			Field string `json:"field"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&b)
		return nil, &PromotionInvalidError{Field: b.Field, Message: b.Error}
	case http.StatusServiceUnavailable:
		return nil, ErrTenantNotProvisioned
	case http.StatusOK, http.StatusCreated:
		var result PromotionWriteResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		return &result, nil
	default:
		return nil, fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
}
