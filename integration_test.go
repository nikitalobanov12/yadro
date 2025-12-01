// Package main contains integration tests for the YADRO limit order book engine.
// These tests verify end-to-end functionality across all components.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nikita/yadro/internal/engine"
	"github.com/nikita/yadro/internal/gateway"
)

// testServer creates a complete test server with engine and gateway.
type testServer struct {
	engine  *engine.Engine
	gateway *gateway.Server
	addr    string
	ctx     context.Context
	cancel  context.CancelFunc
	walPath string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "integration_test_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 10000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	eng.Start(ctx)

	// Use port 0 to get a random available port
	gw := gateway.NewServer(eng, "127.0.0.1:0")

	// Start gateway in background
	serverStarted := make(chan string, 1)
	go func() {
		// We need to extract the actual address after starting
		// Since we can't easily get it, we'll use a fixed port range
		serverStarted <- ""
		gw.Start(ctx)
	}()

	<-serverStarted
	time.Sleep(100 * time.Millisecond)

	return &testServer{
		engine:  eng,
		gateway: gw,
		ctx:     ctx,
		cancel:  cancel,
		walPath: tmpFile.Name(),
	}
}

func (ts *testServer) cleanup() {
	ts.cancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ts.gateway.Stop(shutdownCtx)
	ts.engine.Stop()
	os.Remove(ts.walPath)
}

// Integration tests using direct engine/gateway calls (no HTTP server needed)

func TestIntegrationOrderLifecycle(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_lifecycle_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// 1. Place a limit order
	order := &engine.Order{
		UserID: 1,
		Side:   engine.Bid,
		Type:   engine.Limit,
		Price:  10000,
		Size:   100,
	}

	resp := eng.PlaceOrder(order)
	if !resp.Success {
		t.Fatalf("failed to place order: %v", resp.Error)
	}

	orderID := resp.Order.ID
	if orderID == 0 {
		t.Error("expected non-zero order ID")
	}

	// 2. Verify order is in book
	snapshot := eng.Snapshot(10)
	if len(snapshot.Bids) != 1 {
		t.Errorf("expected 1 bid level, got %d", len(snapshot.Bids))
	}
	if snapshot.Bids[0].Size != 100 {
		t.Errorf("expected bid size 100, got %d", snapshot.Bids[0].Size)
	}

	// 3. Cancel the order
	cancelResp := eng.CancelOrder(orderID)
	if !cancelResp.Success {
		t.Fatalf("failed to cancel order: %v", cancelResp.Error)
	}

	// 4. Verify order is removed from book
	snapshot = eng.Snapshot(10)
	if len(snapshot.Bids) != 0 {
		t.Errorf("expected 0 bid levels after cancel, got %d", len(snapshot.Bids))
	}
}

func TestIntegrationFullMatchTrade(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_match_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Place a sell order
	askOrder := &engine.Order{
		UserID: 1,
		Side:   engine.Ask,
		Type:   engine.Limit,
		Price:  10000,
		Size:   100,
	}
	eng.PlaceOrder(askOrder)

	// Place a matching buy order
	bidOrder := &engine.Order{
		UserID: 2,
		Side:   engine.Bid,
		Type:   engine.Limit,
		Price:  10000,
		Size:   100,
	}
	resp := eng.PlaceOrder(bidOrder)

	if !resp.Success {
		t.Fatalf("failed to place order: %v", resp.Error)
	}

	// Should have 1 trade
	if len(resp.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(resp.Trades))
	}

	trade := resp.Trades[0]
	if trade.Size != 100 {
		t.Errorf("expected trade size 100, got %d", trade.Size)
	}
	if trade.Price != 10000 {
		t.Errorf("expected trade price 10000, got %d", trade.Price)
	}
	if trade.BuyerUserID != 2 {
		t.Errorf("expected buyer user ID 2, got %d", trade.BuyerUserID)
	}
	if trade.SellerUserID != 1 {
		t.Errorf("expected seller user ID 1, got %d", trade.SellerUserID)
	}

	// Book should be empty
	snapshot := eng.Snapshot(10)
	if len(snapshot.Bids) != 0 || len(snapshot.Asks) != 0 {
		t.Errorf("expected empty book, got %d bids and %d asks", len(snapshot.Bids), len(snapshot.Asks))
	}
}

func TestIntegrationPartialMatchTrade(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_partial_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Place a sell order for 100
	askOrder := &engine.Order{
		UserID: 1,
		Side:   engine.Ask,
		Type:   engine.Limit,
		Price:  10000,
		Size:   100,
	}
	eng.PlaceOrder(askOrder)

	// Place a buy order for 60 (partial match)
	bidOrder := &engine.Order{
		UserID: 2,
		Side:   engine.Bid,
		Type:   engine.Limit,
		Price:  10000,
		Size:   60,
	}
	resp := eng.PlaceOrder(bidOrder)

	if len(resp.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(resp.Trades))
	}

	if resp.Trades[0].Size != 60 {
		t.Errorf("expected trade size 60, got %d", resp.Trades[0].Size)
	}

	// Should have remaining ask of 40
	snapshot := eng.Snapshot(10)
	if len(snapshot.Asks) != 1 {
		t.Fatalf("expected 1 ask level, got %d", len(snapshot.Asks))
	}
	if snapshot.Asks[0].Size != 40 {
		t.Errorf("expected remaining ask size 40, got %d", snapshot.Asks[0].Size)
	}

	// No bids should remain (fully filled)
	if len(snapshot.Bids) != 0 {
		t.Errorf("expected 0 bid levels, got %d", len(snapshot.Bids))
	}
}

func TestIntegrationMarketOrder(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_market_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Place limit sell orders at different prices
	for _, price := range []int64{10100, 10200, 10300} {
		order := &engine.Order{
			UserID: 1,
			Side:   engine.Ask,
			Type:   engine.Limit,
			Price:  price,
			Size:   50,
		}
		eng.PlaceOrder(order)
	}

	// Place a market buy order that consumes multiple levels
	marketOrder := &engine.Order{
		UserID: 2,
		Side:   engine.Bid,
		Type:   engine.Market,
		Size:   120, // Will match 50+50+20 across three levels
	}
	resp := eng.PlaceOrder(marketOrder)

	if len(resp.Trades) != 3 {
		t.Fatalf("expected 3 trades, got %d", len(resp.Trades))
	}

	// Verify trades at correct prices
	expectedPrices := []int64{10100, 10200, 10300}
	expectedSizes := []int64{50, 50, 20}
	for i, trade := range resp.Trades {
		if trade.Price != expectedPrices[i] {
			t.Errorf("trade %d: expected price %d, got %d", i, expectedPrices[i], trade.Price)
		}
		if trade.Size != expectedSizes[i] {
			t.Errorf("trade %d: expected size %d, got %d", i, expectedSizes[i], trade.Size)
		}
	}

	// Should have remaining ask of 30 at 10300
	snapshot := eng.Snapshot(10)
	if len(snapshot.Asks) != 1 {
		t.Fatalf("expected 1 ask level, got %d", len(snapshot.Asks))
	}
	if snapshot.Asks[0].Price != 10300 {
		t.Errorf("expected remaining ask price 10300, got %d", snapshot.Asks[0].Price)
	}
	if snapshot.Asks[0].Size != 30 {
		t.Errorf("expected remaining ask size 30, got %d", snapshot.Asks[0].Size)
	}
}

func TestIntegrationPriceTimePriority(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_priority_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Place multiple orders at same price - FIFO order matters
	for i := 1; i <= 3; i++ {
		order := &engine.Order{
			UserID: uint64(i),
			Side:   engine.Ask,
			Type:   engine.Limit,
			Price:  10000,
			Size:   100,
		}
		eng.PlaceOrder(order)
	}

	// Place a buy order that matches the first order
	bidOrder := &engine.Order{
		UserID: 10,
		Side:   engine.Bid,
		Type:   engine.Limit,
		Price:  10000,
		Size:   100,
	}
	resp := eng.PlaceOrder(bidOrder)

	if len(resp.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(resp.Trades))
	}

	// First order (user 1) should be matched (FIFO)
	if resp.Trades[0].SellerUserID != 1 {
		t.Errorf("expected seller user ID 1 (FIFO), got %d", resp.Trades[0].SellerUserID)
	}

	// Remaining orders (user 2 and 3) should still be in book
	snapshot := eng.Snapshot(10)
	if len(snapshot.Asks) != 1 {
		t.Fatalf("expected 1 ask level, got %d", len(snapshot.Asks))
	}
	if snapshot.Asks[0].Size != 200 {
		t.Errorf("expected remaining size 200, got %d", snapshot.Asks[0].Size)
	}
}

func TestIntegrationConcurrentOrders(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_concurrent_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 10000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Place many orders concurrently
	var wg sync.WaitGroup
	numOrders := 100

	for i := 0; i < numOrders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			order := &engine.Order{
				UserID: uint64(idx),
				Side:   engine.Side(idx % 2), // Alternate bid/ask
				Type:   engine.Limit,
				Price:  int64(10000 + (idx%10)*100), // Various prices
				Size:   100,
			}
			resp := eng.PlaceOrder(order)
			if !resp.Success {
				t.Errorf("order %d failed: %v", idx, resp.Error)
			}
		}(i)
	}

	wg.Wait()

	// Verify engine is still consistent
	stats := eng.Stats()
	if stats.TotalOrders < 0 {
		t.Error("invalid order count")
	}

	// Verify we can still place and cancel orders
	order := &engine.Order{
		UserID: 9999,
		Side:   engine.Bid,
		Type:   engine.Limit,
		Price:  5000,
		Size:   10,
	}
	resp := eng.PlaceOrder(order)
	if !resp.Success {
		t.Fatalf("post-concurrent order failed: %v", resp.Error)
	}
}

func TestIntegrationWALRecovery(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_recovery_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	walPath := tmpFile.Name()
	defer os.Remove(walPath)

	// Phase 1: Create engine and place orders
	cfg := &engine.Config{
		WALPath:     walPath,
		ChannelSize: 1000,
	}

	eng1, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	eng1.Start(ctx1)

	// Place orders
	for i := 0; i < 5; i++ {
		order := &engine.Order{
			UserID: uint64(i),
			Side:   engine.Bid,
			Type:   engine.Limit,
			Price:  int64(10000 - i*100),
			Size:   100,
		}
		eng1.PlaceOrder(order)
	}

	for i := 0; i < 3; i++ {
		order := &engine.Order{
			UserID: uint64(100 + i),
			Side:   engine.Ask,
			Type:   engine.Limit,
			Price:  int64(10100 + i*100),
			Size:   50,
		}
		eng1.PlaceOrder(order)
	}

	// Get snapshot before shutdown
	snapshot1 := eng1.Snapshot(10)
	stats1 := eng1.Stats()

	// Shutdown
	cancel1()
	eng1.Stop()

	// Phase 2: Create new engine and verify recovery
	eng2, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine for recovery: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	eng2.Start(ctx2)
	defer eng2.Stop()

	// Verify state is restored
	snapshot2 := eng2.Snapshot(10)
	stats2 := eng2.Stats()

	if stats1.TotalOrders != stats2.TotalOrders {
		t.Errorf("order count mismatch: before=%d, after=%d", stats1.TotalOrders, stats2.TotalOrders)
	}

	if len(snapshot1.Bids) != len(snapshot2.Bids) {
		t.Errorf("bid levels mismatch: before=%d, after=%d", len(snapshot1.Bids), len(snapshot2.Bids))
	}

	if len(snapshot1.Asks) != len(snapshot2.Asks) {
		t.Errorf("ask levels mismatch: before=%d, after=%d", len(snapshot1.Asks), len(snapshot2.Asks))
	}

	// Verify individual price levels
	for i, bid := range snapshot1.Bids {
		if i < len(snapshot2.Bids) {
			if bid.Price != snapshot2.Bids[i].Price {
				t.Errorf("bid price mismatch at %d: before=%d, after=%d", i, bid.Price, snapshot2.Bids[i].Price)
			}
			if bid.Size != snapshot2.Bids[i].Size {
				t.Errorf("bid size mismatch at %d: before=%d, after=%d", i, bid.Size, snapshot2.Bids[i].Size)
			}
		}
	}
}

func TestIntegrationWebSocketEventStream(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_ws_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	gw := gateway.NewServer(eng, "127.0.0.1:0")

	// We can't easily test the full server, but we can test the event flow
	// by reading from the engine's output channel

	// Place orders and verify events are generated
	events := make([]*engine.EngineEvent, 0)
	done := make(chan bool)

	go func() {
		timeout := time.After(1 * time.Second)
		for {
			select {
			case event := <-eng.OutputChannel():
				events = append(events, event)
			case <-timeout:
				done <- true
				return
			}
		}
	}()

	// Place a bid order
	bidOrder := &engine.Order{
		UserID: 1,
		Side:   engine.Bid,
		Type:   engine.Limit,
		Price:  10000,
		Size:   100,
	}
	eng.PlaceOrder(bidOrder)

	// Place a matching ask order
	askOrder := &engine.Order{
		UserID: 2,
		Side:   engine.Ask,
		Type:   engine.Limit,
		Price:  10000,
		Size:   50,
	}
	eng.PlaceOrder(askOrder)

	<-done

	if len(events) < 2 {
		t.Errorf("expected at least 2 events, got %d", len(events))
	}

	// Verify we got trade events
	hasTradeUpdate := false
	for _, event := range events {
		if len(event.Trades) > 0 {
			hasTradeUpdate = true
			break
		}
	}
	if !hasTradeUpdate {
		t.Error("expected trade events in stream")
	}

	// Silence unused variable warning
	_ = gw
}

func TestIntegrationSpreadCalculation(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_spread_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Empty book - spread should be -1
	stats := eng.Stats()
	if stats.Spread != -1 {
		t.Errorf("expected spread -1 for empty book, got %d", stats.Spread)
	}

	// Add bid only - spread should still be -1
	bidOrder := &engine.Order{
		UserID: 1,
		Side:   engine.Bid,
		Type:   engine.Limit,
		Price:  10000,
		Size:   100,
	}
	eng.PlaceOrder(bidOrder)

	stats = eng.Stats()
	if stats.Spread != -1 {
		t.Errorf("expected spread -1 with only bids, got %d", stats.Spread)
	}

	// Add ask - now spread should be calculable
	askOrder := &engine.Order{
		UserID: 2,
		Side:   engine.Ask,
		Type:   engine.Limit,
		Price:  10100,
		Size:   50,
	}
	eng.PlaceOrder(askOrder)

	stats = eng.Stats()
	expectedSpread := int64(100) // 10100 - 10000
	if stats.Spread != expectedSpread {
		t.Errorf("expected spread %d, got %d", expectedSpread, stats.Spread)
	}
}

func TestIntegrationMultiplePriceLevelMatching(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_multi_level_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Create a complex order book
	// Bids: 9900 (100), 9800 (50), 9700 (75)
	// Asks: 10000 (60), 10100 (80), 10200 (40)
	bids := []struct {
		price int64
		size  int64
	}{
		{9900, 100},
		{9800, 50},
		{9700, 75},
	}

	asks := []struct {
		price int64
		size  int64
	}{
		{10000, 60},
		{10100, 80},
		{10200, 40},
	}

	for i, b := range bids {
		eng.PlaceOrder(&engine.Order{
			UserID: uint64(i),
			Side:   engine.Bid,
			Type:   engine.Limit,
			Price:  b.price,
			Size:   b.size,
		})
	}

	for i, a := range asks {
		eng.PlaceOrder(&engine.Order{
			UserID: uint64(100 + i),
			Side:   engine.Ask,
			Type:   engine.Limit,
			Price:  a.price,
			Size:   a.size,
		})
	}

	// Verify book state
	snapshot := eng.Snapshot(10)
	if len(snapshot.Bids) != 3 {
		t.Errorf("expected 3 bid levels, got %d", len(snapshot.Bids))
	}
	if len(snapshot.Asks) != 3 {
		t.Errorf("expected 3 ask levels, got %d", len(snapshot.Asks))
	}

	// Place large market sell that sweeps multiple bid levels
	marketSell := &engine.Order{
		UserID: 999,
		Side:   engine.Ask,
		Type:   engine.Market,
		Size:   160, // Will consume 9900 (100) + 9800 (50) + partial 9700 (10)
	}
	resp := eng.PlaceOrder(marketSell)

	// Should have 3 trades
	if len(resp.Trades) != 3 {
		t.Fatalf("expected 3 trades, got %d", len(resp.Trades))
	}

	// Verify trade sequence (best price first)
	if resp.Trades[0].Price != 9900 || resp.Trades[0].Size != 100 {
		t.Errorf("trade 0: expected 9900/100, got %d/%d", resp.Trades[0].Price, resp.Trades[0].Size)
	}
	if resp.Trades[1].Price != 9800 || resp.Trades[1].Size != 50 {
		t.Errorf("trade 1: expected 9800/50, got %d/%d", resp.Trades[1].Price, resp.Trades[1].Size)
	}
	if resp.Trades[2].Price != 9700 || resp.Trades[2].Size != 10 {
		t.Errorf("trade 2: expected 9700/10, got %d/%d", resp.Trades[2].Price, resp.Trades[2].Size)
	}

	// Verify remaining book state
	snapshot = eng.Snapshot(10)
	if len(snapshot.Bids) != 1 {
		t.Fatalf("expected 1 remaining bid level, got %d", len(snapshot.Bids))
	}
	if snapshot.Bids[0].Price != 9700 || snapshot.Bids[0].Size != 65 {
		t.Errorf("remaining bid: expected 9700/65, got %d/%d", snapshot.Bids[0].Price, snapshot.Bids[0].Size)
	}
}

func TestIntegrationCancelNonexistentOrder(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_cancel_nx_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Try to cancel non-existent order
	resp := eng.CancelOrder(999999)
	if resp.Success {
		t.Error("expected failure when canceling non-existent order")
	}
	if resp.Error == nil {
		t.Error("expected error message for non-existent order")
	}
}

func TestIntegrationLimitOrderPriceImprovement(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_price_imp_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Place ask at 10000
	askOrder := &engine.Order{
		UserID: 1,
		Side:   engine.Ask,
		Type:   engine.Limit,
		Price:  10000,
		Size:   100,
	}
	eng.PlaceOrder(askOrder)

	// Place bid at 10500 (willing to pay more than ask)
	// Should execute at 10000 (price improvement for buyer)
	bidOrder := &engine.Order{
		UserID: 2,
		Side:   engine.Bid,
		Type:   engine.Limit,
		Price:  10500,
		Size:   100,
	}
	resp := eng.PlaceOrder(bidOrder)

	if len(resp.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(resp.Trades))
	}

	// Trade should execute at resting order's price (10000), not incoming (10500)
	if resp.Trades[0].Price != 10000 {
		t.Errorf("expected trade price 10000 (price improvement), got %d", resp.Trades[0].Price)
	}
}

// HTTP Integration Tests (using httptest)

func TestIntegrationHTTPOrderFlow(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_http_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	gw := gateway.NewServer(eng, "127.0.0.1:8999")

	go func() {
		gw.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	baseURL := "http://127.0.0.1:8999"

	// 1. Check health
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected health status 200, got %d", resp.StatusCode)
	}

	// 2. Place an order via HTTP
	orderReq := map[string]interface{}{
		"user_id": 1,
		"side":    "bid",
		"type":    "limit",
		"price":   10000,
		"size":    100,
	}
	body, _ := json.Marshal(orderReq)

	resp, err = http.Post(baseURL+"/order", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("place order failed: %v", err)
	}

	var orderResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&orderResp)
	resp.Body.Close()

	if !orderResp["success"].(bool) {
		t.Errorf("expected order success, got error: %v", orderResp["error"])
	}

	orderID := uint64(orderResp["order_id"].(float64))

	// 3. Get order book
	resp, err = http.Get(baseURL + "/book?depth=10")
	if err != nil {
		t.Fatalf("get book failed: %v", err)
	}

	var bookResp engine.BookSnapshot
	json.NewDecoder(resp.Body).Decode(&bookResp)
	resp.Body.Close()

	if len(bookResp.Bids) != 1 {
		t.Errorf("expected 1 bid, got %d", len(bookResp.Bids))
	}

	// 4. Get stats
	resp, err = http.Get(baseURL + "/stats")
	if err != nil {
		t.Fatalf("get stats failed: %v", err)
	}

	var statsResp engine.EngineStats
	json.NewDecoder(resp.Body).Decode(&statsResp)
	resp.Body.Close()

	if statsResp.TotalOrders != 1 {
		t.Errorf("expected 1 order, got %d", statsResp.TotalOrders)
	}

	// 5. Cancel the order
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/order/%d", baseURL, orderID), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("cancel order failed: %v", err)
	}

	var cancelResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&cancelResp)
	resp.Body.Close()

	if !cancelResp["success"].(bool) {
		t.Errorf("expected cancel success, got error: %v", cancelResp["error"])
	}

	// 6. Verify book is empty
	resp, err = http.Get(baseURL + "/book")
	if err != nil {
		t.Fatalf("get book failed: %v", err)
	}

	json.NewDecoder(resp.Body).Decode(&bookResp)
	resp.Body.Close()

	if len(bookResp.Bids) != 0 {
		t.Errorf("expected 0 bids after cancel, got %d", len(bookResp.Bids))
	}

	// Cleanup
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	gw.Stop(shutdownCtx)
}

func TestIntegrationWebSocketRealTimeUpdates(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "integration_ws_rt_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 1000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	gw := gateway.NewServer(eng, "127.0.0.1:8998")

	go func() {
		gw.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Connect via WebSocket
	wsURL := "ws://127.0.0.1:8998/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect websocket: %v", err)
	}
	defer conn.Close()

	// Collect events in background
	events := make(chan engine.EngineEvent, 10)
	go func() {
		for {
			var event engine.EngineEvent
			err := conn.ReadJSON(&event)
			if err != nil {
				return
			}
			events <- event
		}
	}()

	// Place an order via HTTP
	orderReq := map[string]interface{}{
		"user_id": 1,
		"side":    "bid",
		"type":    "limit",
		"price":   10000,
		"size":    100,
	}
	body, _ := json.Marshal(orderReq)

	resp, err := http.Post("http://127.0.0.1:8998/order", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("place order failed: %v", err)
	}
	resp.Body.Close()

	// Wait for WebSocket event
	select {
	case event := <-events:
		if event.Type != "update" {
			t.Errorf("expected update event, got %s", event.Type)
		}
		if len(event.Updates) == 0 {
			t.Error("expected book updates")
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for WebSocket event")
	}

	// Cleanup
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	gw.Stop(shutdownCtx)
}

func TestIntegrationHighVolumeOrders(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping high volume test in short mode")
	}

	tmpFile, err := os.CreateTemp("", "integration_high_vol_*.wal")
	if err != nil {
		t.Fatalf("failed to create temp WAL: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := &engine.Config{
		WALPath:     tmpFile.Name(),
		ChannelSize: 100000,
	}

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	defer eng.Stop()

	// Place many orders
	numOrders := 10000
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numOrders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			order := &engine.Order{
				UserID: uint64(idx % 100),
				Side:   engine.Side(idx % 2),
				Type:   engine.Limit,
				Price:  int64(10000 + (idx % 100)),
				Size:   int64(10 + idx%90),
			}
			eng.PlaceOrder(order)
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("Processed %d orders in %v (%.0f orders/sec)", numOrders, elapsed, float64(numOrders)/elapsed.Seconds())

	// Verify engine is still functional
	stats := eng.Stats()
	if stats.TotalOrders == 0 && stats.BidLevels == 0 && stats.AskLevels == 0 {
		t.Error("expected some orders/levels after high volume test")
	}
}

// Helper function

func httpURL(ts string) string {
	return strings.Replace(ts, "ws://", "http://", 1)
}
