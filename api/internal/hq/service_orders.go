package hq

// Orders (T19): thin typed proxy for the six /hq/orders* endpoints the
// gateway implements — T16 create, T17 availability, T18 list/detail/
// cancel/transfer. No business logic lives here: every rule (D4, D5, D7,
// D8, D9, D11, D16) is enforced gateway-side in HqApi.cs; this file only
// marshals requests, forwards them with an HQ token, and maps status codes
// to typed errors, the same shape every other slice in this package uses.
// A separate file (unlike Suppliers, which stayed inside service.go)
// because six endpoints plus their request/response types are enough to
// warrant their own home.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// --- read: availability (T17) ---

// OrderAvailabilityLine is one product's on-hand/committed/available figures
// at one branch, already normalized to base units by the gateway.
type OrderAvailabilityLine struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	OnHand      float64 `json:"on_hand"`
	Committed   float64 `json:"committed"`
	Available   float64 `json:"available"`
}

// OrderAvailability is the whole cart's availability read for one branch.
// IsFresh/LastSyncAt describe that specific branch's own sync recency
// (D16.3) — never hidden or zeroed when stale, the gateway always reports
// honest numbers and leaves "block or just warn" to the caller.
type OrderAvailability struct {
	LastSyncAt *time.Time              `json:"last_sync_at,omitempty"`
	IsFresh    bool                    `json:"is_fresh"`
	Lines      []OrderAvailabilityLine `json:"lines"`
}

// OrderAvailabilityEnvelope wraps the availability read in the freshness
// envelope. Its Source/AsOf answer a different question than the payload's
// own IsFresh/LastSyncAt: the envelope is the tenant's overall data
// freshness (same as every other read in this package), the payload fields
// are specifically about the one branch being priced.
type OrderAvailabilityEnvelope struct {
	Data   OrderAvailability `json:"data"`
	Source string            `json:"source"`
	AsOf   *time.Time        `json:"as_of,omitempty"`
}

// OrderAvailability reads the whole cart's on-hand/committed/available
// figures for one branch in a single gateway call (T17) — never one call
// per line, however large the cart.
func (s *Service) OrderAvailability(ctx context.Context, accountID, tenantID, branchID string, productIDs []string) (*OrderAvailabilityEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	q := url.Values{"branch_id": {branchID}}
	for _, id := range productIDs {
		q.Add("product_id", id)
	}
	u := shard.GatewayURL + "/hq/orders/availability?" + q.Encode()

	var resp OrderAvailability
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	if resp.Lines == nil {
		resp.Lines = []OrderAvailabilityLine{}
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &OrderAvailabilityEnvelope{Data: resp, Source: source, AsOf: asOf}, nil
}

// --- read: delivery fee preview (T3b, plan OQ1) ---

// DeliveryFeeResolution is the fee the gateway's three-layer rule (customer
// override > zone tariff > branch default) resolves to for one branch/
// customer pair, and which layer produced it — surfaced to the console as a
// hint next to the prefilled number, same idea as the desktop's own
// DeliveryFeeSource.
type DeliveryFeeResolution struct {
	Fee    float64 `json:"fee"`
	Source string  `json:"source"`
}

// DeliveryFeeEnvelope wraps the resolution in the freshness envelope, same
// as every other read in this package.
type DeliveryFeeEnvelope struct {
	Data   DeliveryFeeResolution `json:"data"`
	Source string                `json:"source"`
	AsOf   *time.Time            `json:"as_of,omitempty"`
}

// DeliveryFee previews the fee a POST /hq/orders for this branch/customer
// would land on, without creating anything — a read-only preview so the
// console can show the resolved number before the operator saves. Returns
// ErrNotFound when the branch or the customer doesn't exist.
func (s *Service) DeliveryFee(ctx context.Context, accountID, tenantID, branchID, partnerID string) (*DeliveryFeeEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	q := url.Values{"branch_id": {branchID}, "partner_id": {partnerID}}
	u := shard.GatewayURL + "/hq/orders/delivery-fee?" + q.Encode()

	var resp DeliveryFeeResolution
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &DeliveryFeeEnvelope{Data: resp, Source: source, AsOf: asOf}, nil
}

// --- read: list (T18) ---

// OrderRow is one row of the order list.
type OrderRow struct {
	ID           string    `json:"id"`
	Ref          string    `json:"ref"`
	CustomerName string    `json:"customer_name"`
	Phone        *string   `json:"phone,omitempty"`
	BranchID     string    `json:"branch_id"`
	BranchName   string    `json:"branch_name"`
	Total        float64   `json:"total"`
	DeliveryFee  *float64  `json:"delivery_fee,omitempty"`
	Status       int       `json:"status"`
	Channel      int       `json:"channel"`
	CreatedAt    time.Time `json:"created_at"`
}

// OrdersPage is the paged order-list payload.
type OrdersPage struct {
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
	Items    []OrderRow `json:"items"`
}

// OrdersEnvelope wraps a page of the order list in the freshness envelope.
type OrdersEnvelope struct {
	Data   OrdersPage `json:"data"`
	Source string     `json:"source"`
	AsOf   *time.Time `json:"as_of,omitempty"`
}

// Orders returns one page of the order list. params carries
// status/branch_id/search/page/page_size straight through to the gateway.
func (s *Service) Orders(ctx context.Context, accountID, tenantID string, params url.Values) (*OrdersEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	u := shard.GatewayURL + "/hq/orders"
	if enc := params.Encode(); enc != "" {
		u += "?" + enc
	}
	var resp OrdersPage
	if err := s.getJSON(ctx, u, t.DBName, &resp); err != nil {
		return nil, err
	}
	if resp.Items == nil {
		resp.Items = []OrderRow{}
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &OrdersEnvelope{Data: resp, Source: source, AsOf: asOf}, nil
}

// --- read: detail (T18) ---

// OrderLine is one line of an order's detail.
type OrderLine struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	UnitID      string  `json:"unit_id"`
	UnitName    string  `json:"unit_name"`
	Qty         float64 `json:"qty"`
	Price       float64 `json:"price"`
	Total       float64 `json:"total"`
}

// OrderChainEntry is one link in the D7 transfer chain, including the
// requested order itself — the gateway walks the whole chain by matching
// Ref, not by this layer recursing PreviousOrderID.
type OrderChainEntry struct {
	ID              string     `json:"id"`
	BranchID        string     `json:"branch_id"`
	BranchName      string     `json:"branch_name"`
	Status          int        `json:"status"`
	PreviousOrderID *string    `json:"previous_order_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	StatusChangedAt *time.Time `json:"status_changed_at,omitempty"`
}

// OrderDetail is one order's full detail, decorated with its branch's name
// and the D7 transfer chain (History).
type OrderDetail struct {
	ID              string            `json:"id"`
	Ref             string            `json:"ref"`
	Status          int               `json:"status"`
	Channel         int               `json:"channel"`
	BranchID        string            `json:"branch_id"`
	BranchName      string            `json:"branch_name"`
	PartnerID       string            `json:"partner_id"`
	CustomerName    string            `json:"customer_name"`
	Phone           *string           `json:"phone,omitempty"`
	Address         *string           `json:"address,omitempty"`
	Mode            int               `json:"mode"`
	Total           float64           `json:"total"`
	DeliveryFee     *float64          `json:"delivery_fee,omitempty"`
	CreatedByName   *string           `json:"created_by_name,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	DueAt           *time.Time        `json:"due_at,omitempty"`
	StatusChangedAt *time.Time        `json:"status_changed_at,omitempty"`
	Note            *string           `json:"note,omitempty"`
	CancelReason    *string           `json:"cancel_reason,omitempty"`
	SaleID          *string           `json:"sale_id,omitempty"`
	Lines           []OrderLine       `json:"lines"`
	History         []OrderChainEntry `json:"history"`
}

// OrderDetailEnvelope wraps one order's detail in the freshness envelope.
type OrderDetailEnvelope struct {
	Data   *OrderDetail `json:"data"`
	Source string       `json:"source"`
	AsOf   *time.Time   `json:"as_of,omitempty"`
}

// OrderDetail fetches one order's full detail, including its transfer
// history. Returns ErrNotFound for an unknown id.
func (s *Service) OrderDetail(ctx context.Context, accountID, tenantID, orderID string) (*OrderDetailEnvelope, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	var detail OrderDetail
	if err := s.getJSON(ctx, shard.GatewayURL+"/hq/orders/"+orderID, t.DBName, &detail); err != nil {
		return nil, err
	}
	source, asOf := s.tenantFreshness(ctx, tenantID)
	return &OrderDetailEnvelope{Data: &detail, Source: source, AsOf: asOf}, nil
}

// --- write: create (T16) ---

// OrderLineInput is one line of a new order.
type OrderLineInput struct {
	ProductID string  `json:"product_id"`
	UnitID    string  `json:"unit_id"`
	Qty       float64 `json:"qty"`
	Price     float64 `json:"price"`
}

// NewOrder is an order to create at a branch on the console operator's
// behalf. Channel is always CallCenter — the gateway sets it, never the
// console.
type NewOrder struct {
	BranchID       string           `json:"branch_id"`
	PartnerID      string           `json:"partner_id"`
	CreatedByName  string           `json:"created_by_name"`
	Mode           int              `json:"mode"`
	ContactAddress *string          `json:"contact_address,omitempty"`
	DeliveryFee    *float64         `json:"delivery_fee,omitempty"`
	DueAt          *time.Time       `json:"due_at,omitempty"`
	Note           *string          `json:"note,omitempty"`
	Lines          []OrderLineInput `json:"lines"`
}

// NewOrderResult is the gateway's create receipt.
type NewOrderResult struct {
	ID        string    `json:"id"`
	Ref       string    `json:"ref"`
	WrittenAt time.Time `json:"written_at"`
}

// OrderShortfall is one line whose requested qty exceeds what's available
// once other open orders are netted off (D16).
type OrderShortfall struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Requested   float64 `json:"requested"`
	Available   float64 `json:"available"`
}

// OrderUnavailableError is CreateOrder's D16 stock-gate refusal. Carries the
// short lines so the console can render them without a second round trip.
type OrderUnavailableError struct{ Shortfalls []OrderShortfall }

func (e *OrderUnavailableError) Error() string { return "one or more lines exceed available stock" }

// CreateOrder forwards an order-create request to the gateway (T16, D9's
// only write path into a tenant's orders). Returns *InvalidCustomerInputError
// (the gateway's own message, forwarded verbatim — the same generic 400
// shape every HQ write in this package already uses, reused here rather
// than a new order-specific type) for bad input or an unknown branch/
// partner/product, *OrderUnavailableError for a D16 stock refusal, or
// ErrMissingAccountOperand for a missing OQ1 posting-account mapping.
func (s *Service) CreateOrder(ctx context.Context, accountID, tenantID string, input NewOrder) (*NewOrderResult, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	tok, err := s.tokens.IssueHQToken(t.DBName)
	if err != nil {
		return nil, fmt.Errorf("mint hq token: %w", err)
	}
	buf, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, shard.GatewayURL+"/hq/orders", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGatewayUnreachable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusBadRequest:
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return nil, &InvalidCustomerInputError{Message: body.Error}
	case http.StatusConflict:
		var body struct {
			Shortfalls []OrderShortfall `json:"shortfalls"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return nil, &OrderUnavailableError{Shortfalls: body.Shortfalls}
	case http.StatusInternalServerError:
		return nil, ErrMissingAccountOperand
	case http.StatusOK, http.StatusCreated:
		var result NewOrderResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		return &result, nil
	default:
		return nil, fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
}

// --- write: cancel / transfer (T18) ---

// OrderNotCancellableError and OrderNotTransferableError carry the
// gateway's own reason verbatim (already delivered vs. Status no longer
// New) — D4 refuses a console cancel/transfer for either reason, and this
// layer doesn't need to branch on which one to decide the write failed.
type OrderNotCancellableError struct{ Message string }

func (e *OrderNotCancellableError) Error() string { return e.Message }

type OrderNotTransferableError struct{ Message string }

func (e *OrderNotTransferableError) Error() string { return e.Message }

// CancelOrderResult is the gateway's cancel receipt.
type CancelOrderResult struct {
	WrittenAt time.Time `json:"written_at"`
}

// CancelOrder cancels a still-New order (D4: HQ's cancel authority is
// New-only, stricter than the assigned branch's own — see
// CancelOrderAsync's doc comment in HqApi.cs). Returns ErrNotFound for an
// unknown id or *OrderNotCancellableError once the order has moved past
// New or was already delivered.
func (s *Service) CancelOrder(ctx context.Context, accountID, tenantID, orderID string, reason *string) (*CancelOrderResult, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	tok, err := s.tokens.IssueHQToken(t.DBName)
	if err != nil {
		return nil, fmt.Errorf("mint hq token: %w", err)
	}
	buf, err := json.Marshal(struct {
		Reason *string `json:"reason,omitempty"`
	}{Reason: reason})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, shard.GatewayURL+"/hq/orders/"+orderID+"/cancel", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGatewayUnreachable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusConflict:
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return nil, &OrderNotCancellableError{Message: body.Error}
	case http.StatusOK:
		var result CancelOrderResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		return &result, nil
	default:
		return nil, fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
}

// TransferOrderResult is the gateway's transfer receipt: the reissued
// order's own id, plus the Ref both rows share (D7).
type TransferOrderResult struct {
	ID        string    `json:"id"`
	Ref       string    `json:"ref"`
	WrittenAt time.Time `json:"written_at"`
}

// TransferOrder closes the order at its current branch and reissues it at
// toBranchID under the same Ref (D7), ensuring the customer exists there
// (D8/OQ1). Same D4 New-only gate as CancelOrder. Returns ErrNotFound,
// *OrderNotTransferableError, *InvalidCustomerInputError (unknown
// toBranchID), or ErrMissingAccountOperand.
func (s *Service) TransferOrder(ctx context.Context, accountID, tenantID, orderID, toBranchID string) (*TransferOrderResult, error) {
	t, shard, err := s.resolveGateway(ctx, accountID, tenantID)
	if err != nil {
		return nil, err
	}
	tok, err := s.tokens.IssueHQToken(t.DBName)
	if err != nil {
		return nil, fmt.Errorf("mint hq token: %w", err)
	}
	buf, err := json.Marshal(struct {
		ToBranchID string `json:"to_branch_id"`
	}{ToBranchID: toBranchID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, shard.GatewayURL+"/hq/orders/"+orderID+"/transfer", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGatewayUnreachable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusBadRequest:
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return nil, &InvalidCustomerInputError{Message: body.Error}
	case http.StatusConflict:
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return nil, &OrderNotTransferableError{Message: body.Error}
	case http.StatusInternalServerError:
		return nil, ErrMissingAccountOperand
	case http.StatusOK:
		var result TransferOrderResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		return &result, nil
	default:
		return nil, fmt.Errorf("%w: gateway status %d", ErrGatewayUnreachable, resp.StatusCode)
	}
}
