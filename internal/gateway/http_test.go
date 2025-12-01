package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/nikita/yadro/internal/engine"
)

// testEngine creates a test engine with a temporary WAL file.
func testEngine(t *testing.T) (*engine.Engine, func()) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "test_wal_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	eng.Start(ctx)

	cleanup := func() {
		cancel()
		eng.Stop()
		os.Remove(tmpFile.Name())
	}

	return eng, cleanup
}

func TestHandleHealth(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Body.String() != "OK" {
		t.Errorf("expected body 'OK', got '%s'", rec.Body.String())
	}
}

func TestHandleOrderPost(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	tests := []struct {
		name       string
		request    OrderRequest
		wantStatus int
		wantError  bool
	}{
		{
			name: "valid limit bid order",
			request: OrderRequest{
				UserID: 1,
				Side:   "bid",
				Type:   "limit",
				Price:  10000,
				Size:   100,
			},
			wantStatus: http.StatusOK,
			wantError:  false,
		},
		{
			name: "valid limit ask order",
			request: OrderRequest{
				UserID: 2,
				Side:   "ask",
				Type:   "limit",
				Price:  10100,
				Size:   50,
			},
			wantStatus: http.StatusOK,
			wantError:  false,
		},
		{
			name: "valid market order",
			request: OrderRequest{
				UserID: 3,
				Side:   "buy",
				Type:   "market",
				Size:   25,
			},
			wantStatus: http.StatusOK,
			wantError:  false,
		},
		{
			name: "invalid side",
			request: OrderRequest{
				UserID: 1,
				Side:   "invalid",
				Type:   "limit",
				Price:  10000,
				Size:   100,
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name: "invalid type",
			request: OrderRequest{
				UserID: 1,
				Side:   "bid",
				Type:   "invalid",
				Price:  10000,
				Size:   100,
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name: "zero size",
			request: OrderRequest{
				UserID: 1,
				Side:   "bid",
				Type:   "limit",
				Price:  10000,
				Size:   0,
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name: "negative size",
			request: OrderRequest{
				UserID: 1,
				Side:   "bid",
				Type:   "limit",
				Price:  10000,
				Size:   -10,
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
		{
			name: "limit order without price",
			request: OrderRequest{
				UserID: 1,
				Side:   "bid",
				Type:   "limit",
				Price:  0,
				Size:   100,
			},
			wantStatus: http.StatusBadRequest,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/order", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			server.handleOrder(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}

			var resp OrderResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if tt.wantError && resp.Success {
				t.Errorf("expected error but got success")
			}
			if !tt.wantError && !resp.Success {
				t.Errorf("expected success but got error: %s", resp.Error)
			}
		})
	}
}

func TestHandleOrderMethodNotAllowed(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/order", nil)
			rec := httptest.NewRecorder()

			server.handleOrder(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
			}
		})
	}
}

func TestHandleOrderInvalidJSON(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	req := httptest.NewRequest(http.MethodPost, "/order", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleOrder(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var resp OrderResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Success {
		t.Errorf("expected error but got success")
	}
}

func TestHandleOrderByIDCancel(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	// First, place an order
	orderReq := OrderRequest{
		UserID: 1,
		Side:   "bid",
		Type:   "limit",
		Price:  10000,
		Size:   100,
	}
	body, _ := json.Marshal(orderReq)
	req := httptest.NewRequest(http.MethodPost, "/order", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleOrder(rec, req)

	var createResp OrderResponse
	if err := json.NewDecoder(rec.Body).Decode(&createResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !createResp.Success {
		t.Fatalf("failed to create order: %s", createResp.Error)
	}

	// Now cancel the order
	cancelReq := httptest.NewRequest(http.MethodDelete, "/order/"+itoa(createResp.OrderID), nil)
	cancelRec := httptest.NewRecorder()

	server.handleOrderByID(cancelRec, cancelReq)

	if cancelRec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, cancelRec.Code)
	}

	var cancelResp OrderResponse
	if err := json.NewDecoder(cancelRec.Body).Decode(&cancelResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !cancelResp.Success {
		t.Errorf("expected cancel success, got error: %s", cancelResp.Error)
	}
}

func TestHandleOrderByIDNotFound(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	req := httptest.NewRequest(http.MethodDelete, "/order/999999", nil)
	rec := httptest.NewRecorder()

	server.handleOrderByID(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestHandleOrderByIDInvalidID(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	req := httptest.NewRequest(http.MethodDelete, "/order/invalid", nil)
	rec := httptest.NewRecorder()

	server.handleOrderByID(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleBook(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	// Place some orders first
	orders := []OrderRequest{
		{UserID: 1, Side: "bid", Type: "limit", Price: 10000, Size: 100},
		{UserID: 2, Side: "bid", Type: "limit", Price: 9900, Size: 50},
		{UserID: 3, Side: "ask", Type: "limit", Price: 10100, Size: 75},
		{UserID: 4, Side: "ask", Type: "limit", Price: 10200, Size: 25},
	}

	for _, order := range orders {
		body, _ := json.Marshal(order)
		req := httptest.NewRequest(http.MethodPost, "/order", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		server.handleOrder(rec, req)
	}

	// Get the book
	req := httptest.NewRequest(http.MethodGet, "/book?depth=10", nil)
	rec := httptest.NewRecorder()

	server.handleBook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var snapshot engine.BookSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&snapshot); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(snapshot.Bids) != 2 {
		t.Errorf("expected 2 bid levels, got %d", len(snapshot.Bids))
	}

	if len(snapshot.Asks) != 2 {
		t.Errorf("expected 2 ask levels, got %d", len(snapshot.Asks))
	}

	// Verify bid ordering (highest first)
	if len(snapshot.Bids) >= 2 {
		if snapshot.Bids[0].Price <= snapshot.Bids[1].Price {
			t.Errorf("bids not sorted correctly: %d <= %d", snapshot.Bids[0].Price, snapshot.Bids[1].Price)
		}
	}

	// Verify ask ordering (lowest first)
	if len(snapshot.Asks) >= 2 {
		if snapshot.Asks[0].Price >= snapshot.Asks[1].Price {
			t.Errorf("asks not sorted correctly: %d >= %d", snapshot.Asks[0].Price, snapshot.Asks[1].Price)
		}
	}
}

func TestHandleBookDepthParameter(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	// Place many orders
	for i := 0; i < 10; i++ {
		order := OrderRequest{
			UserID: uint64(i),
			Side:   "bid",
			Type:   "limit",
			Price:  int64(10000 - i*100),
			Size:   100,
		}
		body, _ := json.Marshal(order)
		req := httptest.NewRequest(http.MethodPost, "/order", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		server.handleOrder(rec, req)
	}

	// Get book with depth=3
	req := httptest.NewRequest(http.MethodGet, "/book?depth=3", nil)
	rec := httptest.NewRecorder()

	server.handleBook(rec, req)

	var snapshot engine.BookSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&snapshot); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(snapshot.Bids) != 3 {
		t.Errorf("expected 3 bid levels, got %d", len(snapshot.Bids))
	}
}

func TestHandleStats(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	// Place some orders
	orders := []OrderRequest{
		{UserID: 1, Side: "bid", Type: "limit", Price: 10000, Size: 100},
		{UserID: 2, Side: "ask", Type: "limit", Price: 10100, Size: 50},
	}

	for _, order := range orders {
		body, _ := json.Marshal(order)
		req := httptest.NewRequest(http.MethodPost, "/order", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		server.handleOrder(rec, req)
	}

	// Get stats
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()

	server.handleStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var stats engine.EngineStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if stats.TotalOrders != 2 {
		t.Errorf("expected 2 total orders, got %d", stats.TotalOrders)
	}

	if stats.BidLevels != 1 {
		t.Errorf("expected 1 bid level, got %d", stats.BidLevels)
	}

	if stats.AskLevels != 1 {
		t.Errorf("expected 1 ask level, got %d", stats.AskLevels)
	}

	if stats.Spread != 100 {
		t.Errorf("expected spread of 100, got %d", stats.Spread)
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("adds CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("missing or incorrect Access-Control-Allow-Origin header")
		}
	})

	t.Run("handles OPTIONS preflight", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d for OPTIONS, got %d", http.StatusOK, rec.Code)
		}
	})
}

func TestOrderMatching(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	// Place a sell order
	askOrder := OrderRequest{
		UserID: 1,
		Side:   "ask",
		Type:   "limit",
		Price:  10000,
		Size:   100,
	}
	body, _ := json.Marshal(askOrder)
	req := httptest.NewRequest(http.MethodPost, "/order", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleOrder(rec, req)

	// Place a matching buy order
	bidOrder := OrderRequest{
		UserID: 2,
		Side:   "bid",
		Type:   "limit",
		Price:  10000,
		Size:   50,
	}
	body, _ = json.Marshal(bidOrder)
	req = httptest.NewRequest(http.MethodPost, "/order", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	server.handleOrder(rec, req)

	var resp OrderResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	if len(resp.Trades) != 1 {
		t.Errorf("expected 1 trade, got %d", len(resp.Trades))
	}

	if len(resp.Trades) > 0 {
		trade := resp.Trades[0]
		if trade.Size != 50 {
			t.Errorf("expected trade size 50, got %d", trade.Size)
		}
		if trade.Price != 10000 {
			t.Errorf("expected trade price 10000, got %d", trade.Price)
		}
	}

	// Verify remaining book state
	bookReq := httptest.NewRequest(http.MethodGet, "/book", nil)
	bookRec := httptest.NewRecorder()
	server.handleBook(bookRec, bookReq)

	var snapshot engine.BookSnapshot
	if err := json.NewDecoder(bookRec.Body).Decode(&snapshot); err != nil {
		t.Fatalf("failed to decode book: %v", err)
	}

	// Should have remaining ask of 50
	if len(snapshot.Asks) != 1 {
		t.Errorf("expected 1 ask level, got %d", len(snapshot.Asks))
	}
	if len(snapshot.Asks) > 0 && snapshot.Asks[0].Size != 50 {
		t.Errorf("expected remaining ask size 50, got %d", snapshot.Asks[0].Size)
	}

	// Should have no bids
	if len(snapshot.Bids) != 0 {
		t.Errorf("expected 0 bid levels, got %d", len(snapshot.Bids))
	}
}

func TestServerStartStop(t *testing.T) {
	eng, cleanup := testEngine(t)
	defer cleanup()

	server := NewServer(eng, ":0")

	ctx, cancel := context.WithCancel(context.Background())

	// Start in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Stop the server
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()

	if err := server.Stop(shutdownCtx); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// itoa converts uint64 to string (avoiding fmt import for simple conversion).
func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
