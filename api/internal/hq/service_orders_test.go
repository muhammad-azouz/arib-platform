package hq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Reuses testStore/fakeTokens from service_test.go, same as
// service_suppliers_test.go does — one fake gateway per test, asserting the
// request this package built and the typed result/error it decoded back.

func TestOrders_PassesParamsAndFreshness(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if r.URL.Path != "/hq/orders" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":1,"page":1,"page_size":50,"items":[` +
			`{"id":"o1","ref":"HQ-26-00001","customer_name":"احمد","branch_id":"b1",` +
			`"branch_name":"وسط البلد","total":80,"status":0,"channel":1,"created_at":"2026-08-01T10:00:00Z"}]}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	params := url.Values{"status": {"0"}, "branch_id": {"b1"}, "search": {"HQ-26"}}
	env, err := s.Orders(context.Background(), "acc_owner", "tnt_1", params)
	if err != nil {
		t.Fatalf("orders: %v", err)
	}
	if gotQuery.Get("status") != "0" || gotQuery.Get("branch_id") != "b1" || gotQuery.Get("search") != "HQ-26" {
		t.Fatalf("gateway did not see passed-through params: %v", gotQuery)
	}
	if env.Data.Total != 1 || len(env.Data.Items) != 1 {
		t.Fatalf("orders envelope wrong: %+v", env)
	}
	row := env.Data.Items[0]
	if row.Ref != "HQ-26-00001" || row.BranchName != "وسط البلد" || row.Total != 80 {
		t.Fatalf("order row wrong: %+v", row)
	}
}

func TestOrders_EmptyItemsNeverNil(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":0,"page":1,"page_size":50,"items":null}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	env, err := s.Orders(context.Background(), "acc_owner", "tnt_1", url.Values{})
	if err != nil {
		t.Fatalf("orders: %v", err)
	}
	if env.Data.Items == nil {
		t.Fatalf("expected a non-nil empty slice, got nil")
	}
}

func TestOrderDetail_DecodesHistoryAndNotFound(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hq/orders/o2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"o2","ref":"HQ-26-00002","status":6,"channel":1,` +
				`"branch_id":"b1","branch_name":"وسط البلد","partner_id":"p1","customer_name":"احمد",` +
				`"mode":2,"total":80,"created_at":"2026-08-01T10:00:00Z",` +
				`"lines":[{"product_id":"pr1","product_name":"سكر","unit_id":"u1","unit_name":"كيلو","qty":4,"price":20,"total":80}],` +
				`"history":[` +
				`{"id":"o2","branch_id":"b1","branch_name":"وسط البلد","status":6,"created_at":"2026-08-01T10:00:00Z"},` +
				`{"id":"o3","branch_id":"b2","branch_name":"المعادي","status":0,"previous_order_id":"o2","created_at":"2026-08-01T10:05:00Z"}` +
				`]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)

	env, err := s.OrderDetail(context.Background(), "acc_owner", "tnt_1", "o2")
	if err != nil {
		t.Fatalf("order detail: %v", err)
	}
	if env.Data.Ref != "HQ-26-00002" || len(env.Data.Lines) != 1 || env.Data.Lines[0].Total != 80 {
		t.Fatalf("order detail wrong: %+v", env.Data)
	}
	if len(env.Data.History) != 2 || env.Data.History[1].PreviousOrderID == nil || *env.Data.History[1].PreviousOrderID != "o2" {
		t.Fatalf("transfer chain history wrong: %+v", env.Data.History)
	}

	if _, err := s.OrderDetail(context.Background(), "acc_owner", "tnt_1", "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOrderAvailability_PassesBranchAndProductIDs(t *testing.T) {
	var gotQuery url.Values
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"last_sync_at":"2026-08-06T09:00:00Z","is_fresh":true,` +
			`"lines":[{"product_id":"pr1","product_name":"سكر","on_hand":10,"committed":4,"available":6}]}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	env, err := s.OrderAvailability(context.Background(), "acc_owner", "tnt_1", "b1", []string{"pr1", "pr2"})
	if err != nil {
		t.Fatalf("order availability: %v", err)
	}
	if gotQuery.Get("branch_id") != "b1" || len(gotQuery["product_id"]) != 2 {
		t.Fatalf("gateway did not see branch_id/product_id params: %v", gotQuery)
	}
	if !env.Data.IsFresh || env.Data.LastSyncAt == nil || len(env.Data.Lines) != 1 {
		t.Fatalf("availability envelope wrong: %+v", env.Data)
	}
	if env.Data.Lines[0].Available != 6 {
		t.Fatalf("availability line wrong: %+v", env.Data.Lines[0])
	}
}

func TestCreateOrder_ForwardsAndReturnsResult(t *testing.T) {
	var gotMethod string
	var gotBody NewOrder
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if r.URL.Path != "/hq/orders" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"o1","ref":"HQ-26-00001","written_at":"2026-08-06T10:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	input := NewOrder{
		BranchID: "b1", PartnerID: "p1", CreatedByName: "Operator", Mode: 2,
		Lines: []OrderLineInput{{ProductID: "pr1", UnitID: "u1", Qty: 4, Price: 20}},
	}
	result, err := s.CreateOrder(context.Background(), "acc_owner", "tnt_1", input)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, gateway saw %s", gotMethod)
	}
	if gotBody.BranchID != "b1" || len(gotBody.Lines) != 1 || gotBody.Lines[0].ProductID != "pr1" {
		t.Fatalf("gateway did not receive the forwarded order: %+v", gotBody)
	}
	if result.ID != "o1" || result.Ref != "HQ-26-00001" {
		t.Fatalf("create order result wrong: %+v", result)
	}

	if _, err := s.CreateOrder(context.Background(), "acc_intruder", "tnt_1", input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestCreateOrder_InvalidInputForwardsGatewayMessage(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"branch not found"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateOrder(context.Background(), "acc_owner", "tnt_1", NewOrder{BranchID: "ghost"})
	var badInput *InvalidCustomerInputError
	if !errors.As(err, &badInput) || badInput.Error() != "branch not found" {
		t.Fatalf("expected InvalidCustomerInputError(\"branch not found\"), got %v", err)
	}
}

func TestCreateOrder_UnavailableCarriesShortfalls(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"one or more lines exceed available stock",` +
			`"shortfalls":[{"product_id":"pr1","product_name":"سكر","requested":10,"available":6}]}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateOrder(context.Background(), "acc_owner", "tnt_1", NewOrder{})
	var unavailable *OrderUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("expected OrderUnavailableError, got %v", err)
	}
	if len(unavailable.Shortfalls) != 1 || unavailable.Shortfalls[0].Available != 6 {
		t.Fatalf("shortfalls not carried through: %+v", unavailable.Shortfalls)
	}
}

func TestCreateOrder_MissingAccountOperand(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CreateOrder(context.Background(), "acc_owner", "tnt_1", NewOrder{})
	if !errors.Is(err, ErrMissingAccountOperand) {
		t.Fatalf("expected ErrMissingAccountOperand, got %v", err)
	}
}

func TestCancelOrder_ForwardsReasonAndReturnsResult(t *testing.T) {
	var gotPath string
	var gotBody struct {
		Reason *string `json:"reason"`
	}
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"written_at":"2026-08-06T10:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	reason := "العميل غيّر رأيه"
	result, err := s.CancelOrder(context.Background(), "acc_owner", "tnt_1", "o1", &reason)
	if err != nil {
		t.Fatalf("cancel order: %v", err)
	}
	if gotPath != "/hq/orders/o1/cancel" || gotBody.Reason == nil || *gotBody.Reason != reason {
		t.Fatalf("gateway did not receive the forwarded cancel: path=%s body=%+v", gotPath, gotBody)
	}
	if result.WrittenAt.IsZero() {
		t.Fatalf("cancel order result wrong: %+v", result)
	}
}

// TestCancelOrder_NotCancellable covers criterion 9: cancelling a Preparing
// (or otherwise non-New) order is refused, not silently accepted.
func TestCancelOrder_NotCancellable(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"order can no longer be cancelled"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.CancelOrder(context.Background(), "acc_owner", "tnt_1", "o1", nil)
	var notCancellable *OrderNotCancellableError
	if !errors.As(err, &notCancellable) || notCancellable.Error() != "order can no longer be cancelled" {
		t.Fatalf("expected OrderNotCancellableError, got %v", err)
	}
}

func TestCancelOrder_NotFound(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	if _, err := s.CancelOrder(context.Background(), "acc_owner", "tnt_1", "ghost", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTransferOrder_ForwardsAndReturnsResult(t *testing.T) {
	var gotPath string
	var gotBody struct {
		ToBranchID string `json:"to_branch_id"`
	}
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"o4","ref":"HQ-26-00001","written_at":"2026-08-06T10:00:00Z"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	result, err := s.TransferOrder(context.Background(), "acc_owner", "tnt_1", "o1", "b2")
	if err != nil {
		t.Fatalf("transfer order: %v", err)
	}
	if gotPath != "/hq/orders/o1/transfer" || gotBody.ToBranchID != "b2" {
		t.Fatalf("gateway did not receive the forwarded transfer: path=%s body=%+v", gotPath, gotBody)
	}
	// Same Ref as the origin (D7) — the whole point of the reference.
	if result.ID != "o4" || result.Ref != "HQ-26-00001" {
		t.Fatalf("transfer order result wrong: %+v", result)
	}
}

// TestTransferOrder_NotTransferable covers criterion 9's transfer half:
// transferring a Preparing (or otherwise non-New) order is refused.
func TestTransferOrder_NotTransferable(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"order can no longer be transferred"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.TransferOrder(context.Background(), "acc_owner", "tnt_1", "o1", "b2")
	var notTransferable *OrderNotTransferableError
	if !errors.As(err, &notTransferable) || notTransferable.Error() != "order can no longer be transferred" {
		t.Fatalf("expected OrderNotTransferableError, got %v", err)
	}
}

func TestTransferOrder_InvalidBranch(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"branch not found"}`))
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.TransferOrder(context.Background(), "acc_owner", "tnt_1", "o1", "ghost")
	var badInput *InvalidCustomerInputError
	if !errors.As(err, &badInput) || badInput.Error() != "branch not found" {
		t.Fatalf("expected InvalidCustomerInputError(\"branch not found\"), got %v", err)
	}
}

func TestTransferOrder_MissingAccountOperand(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer gw.Close()

	s := New(testStore(gw.URL), &fakeTokens{}, nil)
	_, err := s.TransferOrder(context.Background(), "acc_owner", "tnt_1", "o1", "b2")
	if !errors.Is(err, ErrMissingAccountOperand) {
		t.Fatalf("expected ErrMissingAccountOperand, got %v", err)
	}
}
