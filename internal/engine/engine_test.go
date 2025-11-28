package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEngineBasic(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	cfg := &Config{
		WALPath:     walPath,
		ChannelSize: 100,
	}

	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	eng.Start(ctx)
	defer func() {
		cancel()
		eng.Stop()
	}()

	// Place a sell order
	sellOrder := &Order{
		UserID: 1,
		Side:   Ask,
		Type:   Limit,
		Price:  10000,
		Size:   100,
	}

	resp := eng.PlaceOrder(sellOrder)
	if !resp.Success {
		t.Fatalf("failed to place sell order: %v", resp.Error)
	}

	if resp.Order == nil {
		t.Fatal("expected order to be placed")
	}

	// Place a matching buy order
	buyOrder := &Order{
		UserID: 2,
		Side:   Bid,
		Type:   Limit,
		Price:  10000,
		Size:   50,
	}

	resp = eng.PlaceOrder(buyOrder)
	if !resp.Success {
		t.Fatalf("failed to place buy order: %v", resp.Error)
	}

	if len(resp.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(resp.Trades))
	}

	if resp.Trades[0].Size != 50 {
		t.Errorf("expected trade size 50, got %d", resp.Trades[0].Size)
	}

	// Verify remaining order in book
	snapshot := eng.Snapshot(10)
	if len(snapshot.Asks) != 1 {
		t.Errorf("expected 1 ask level, got %d", len(snapshot.Asks))
	}
	if snapshot.Asks[0].Size != 50 {
		t.Errorf("expected remaining ask size 50, got %d", snapshot.Asks[0].Size)
	}
}

func TestEngineRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "recovery.wal")

	cfg := &Config{
		WALPath:     walPath,
		ChannelSize: 100,
	}

	// First engine session - place orders
	eng1, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	eng1.Start(ctx1)

	// Place orders
	eng1.PlaceOrder(&Order{UserID: 1, Side: Ask, Type: Limit, Price: 10000, Size: 100})
	eng1.PlaceOrder(&Order{UserID: 2, Side: Ask, Type: Limit, Price: 10100, Size: 50})
	eng1.PlaceOrder(&Order{UserID: 3, Side: Bid, Type: Limit, Price: 9900, Size: 75})

	// Wait for WAL writes
	time.Sleep(50 * time.Millisecond)

	// Get snapshot before "crash"
	snapshotBefore := eng1.Snapshot(10)

	// Simulate crash - stop engine
	cancel1()
	eng1.Stop()

	// Verify WAL file exists
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Fatal("WAL file should exist")
	}

	// Second engine session - recover
	eng2, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine for recovery: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	eng2.Start(ctx2)
	defer func() {
		cancel2()
		eng2.Stop()
	}()

	// Verify state is recovered
	snapshotAfter := eng2.Snapshot(10)

	// Compare snapshots
	if len(snapshotBefore.Asks) != len(snapshotAfter.Asks) {
		t.Errorf("ask levels mismatch: before=%d, after=%d",
			len(snapshotBefore.Asks), len(snapshotAfter.Asks))
	}

	if len(snapshotBefore.Bids) != len(snapshotAfter.Bids) {
		t.Errorf("bid levels mismatch: before=%d, after=%d",
			len(snapshotBefore.Bids), len(snapshotAfter.Bids))
	}

	// Verify specific values
	for i, ask := range snapshotBefore.Asks {
		if snapshotAfter.Asks[i].Price != ask.Price {
			t.Errorf("ask[%d] price mismatch: %d vs %d", i, ask.Price, snapshotAfter.Asks[i].Price)
		}
		if snapshotAfter.Asks[i].Size != ask.Size {
			t.Errorf("ask[%d] size mismatch: %d vs %d", i, ask.Size, snapshotAfter.Asks[i].Size)
		}
	}

	for i, bid := range snapshotBefore.Bids {
		if snapshotAfter.Bids[i].Price != bid.Price {
			t.Errorf("bid[%d] price mismatch: %d vs %d", i, bid.Price, snapshotAfter.Bids[i].Price)
		}
		if snapshotAfter.Bids[i].Size != bid.Size {
			t.Errorf("bid[%d] size mismatch: %d vs %d", i, bid.Size, snapshotAfter.Bids[i].Size)
		}
	}
}

func TestEngineCancelOrder(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "cancel.wal")

	cfg := &Config{
		WALPath:     walPath,
		ChannelSize: 100,
	}

	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	eng.Start(ctx)
	defer func() {
		cancel()
		eng.Stop()
	}()

	// Place an order
	resp := eng.PlaceOrder(&Order{
		UserID: 1,
		Side:   Bid,
		Type:   Limit,
		Price:  10000,
		Size:   100,
	})

	if !resp.Success || resp.Order == nil {
		t.Fatal("failed to place order")
	}

	orderID := resp.Order.ID

	// Cancel it
	cancelResp := eng.CancelOrder(orderID)
	if !cancelResp.Success {
		t.Fatalf("failed to cancel order: %v", cancelResp.Error)
	}

	// Verify order is gone
	snapshot := eng.Snapshot(10)
	if len(snapshot.Bids) != 0 {
		t.Error("expected no bids after cancel")
	}
}

func TestEngineRecoveryWithCancels(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "cancel_recovery.wal")

	cfg := &Config{
		WALPath:     walPath,
		ChannelSize: 100,
	}

	// First session
	eng1, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	eng1.Start(ctx1)

	// Place orders
	resp1 := eng1.PlaceOrder(&Order{UserID: 1, Side: Ask, Type: Limit, Price: 10000, Size: 100})
	resp2 := eng1.PlaceOrder(&Order{UserID: 2, Side: Ask, Type: Limit, Price: 10100, Size: 50})

	// Cancel one
	eng1.CancelOrder(resp1.Order.ID)

	time.Sleep(50 * time.Millisecond)

	cancel1()
	eng1.Stop()

	// Second session - recover
	eng2, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine for recovery: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	eng2.Start(ctx2)
	defer func() {
		cancel2()
		eng2.Stop()
	}()

	// Should only have one order (the non-cancelled one)
	snapshot := eng2.Snapshot(10)
	if len(snapshot.Asks) != 1 {
		t.Errorf("expected 1 ask level, got %d", len(snapshot.Asks))
	}

	if snapshot.Asks[0].Price != resp2.Order.Price {
		t.Errorf("expected price %d, got %d", resp2.Order.Price, snapshot.Asks[0].Price)
	}
}
