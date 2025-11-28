package wal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWALWriteAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Write events
	writer, err := NewWriter(walPath)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write order events
	orderEvent := &OrderEvent{
		ID:        1,
		UserID:    100,
		Side:      0, // Bid
		Type:      0, // Limit
		Price:     10000,
		Size:      50,
		Timestamp: time.Now().UnixNano(),
	}
	if err := writer.WriteOrder(orderEvent); err != nil {
		t.Fatalf("failed to write order: %v", err)
	}

	orderEvent2 := &OrderEvent{
		ID:        2,
		UserID:    200,
		Side:      1, // Ask
		Type:      0, // Limit
		Price:     10100,
		Size:      30,
		Timestamp: time.Now().UnixNano(),
	}
	if err := writer.WriteOrder(orderEvent2); err != nil {
		t.Fatalf("failed to write order: %v", err)
	}

	// Write trade event
	tradeEvent := &TradeEvent{
		ID:           1,
		BuyOrderID:   1,
		SellOrderID:  2,
		BuyerUserID:  100,
		SellerUserID: 200,
		Price:        10000,
		Size:         30,
		Timestamp:    time.Now().UnixNano(),
	}
	if err := writer.WriteTrade(tradeEvent); err != nil {
		t.Fatalf("failed to write trade: %v", err)
	}

	// Write cancel event
	cancelEvent := &CancelEvent{
		OrderID:   1,
		Timestamp: time.Now().UnixNano(),
	}
	if err := writer.WriteCancel(cancelEvent); err != nil {
		t.Fatalf("failed to write cancel: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	// Read events back
	reader, err := NewReader(walPath)
	if err != nil {
		t.Fatalf("failed to create reader: %v", err)
	}
	defer reader.Close()

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read events: %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	// Verify order 1
	if events[0].Type != EventPlaceOrder {
		t.Errorf("expected place order event, got %d", events[0].Type)
	}
	if events[0].Order.ID != 1 {
		t.Errorf("expected order ID 1, got %d", events[0].Order.ID)
	}
	if events[0].Order.Price != 10000 {
		t.Errorf("expected price 10000, got %d", events[0].Order.Price)
	}

	// Verify order 2
	if events[1].Type != EventPlaceOrder {
		t.Errorf("expected place order event, got %d", events[1].Type)
	}
	if events[1].Order.ID != 2 {
		t.Errorf("expected order ID 2, got %d", events[1].Order.ID)
	}

	// Verify trade
	if events[2].Type != EventTrade {
		t.Errorf("expected trade event, got %d", events[2].Type)
	}
	if events[2].Trade.Price != 10000 {
		t.Errorf("expected trade price 10000, got %d", events[2].Trade.Price)
	}

	// Verify cancel
	if events[3].Type != EventCancelOrder {
		t.Errorf("expected cancel event, got %d", events[3].Type)
	}
	if events[3].Cancel.OrderID != 1 {
		t.Errorf("expected cancel order ID 1, got %d", events[3].Cancel.OrderID)
	}
}

func TestWALNonExistent(t *testing.T) {
	reader, err := NewReader("/nonexistent/path/wal.log")
	if err != nil {
		t.Fatalf("expected nil error for non-existent file, got %v", err)
	}
	if reader != nil {
		t.Error("expected nil reader for non-existent file")
	}
}

func TestWALExists(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	if Exists(walPath) {
		t.Error("expected file to not exist")
	}

	// Create file
	f, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	f.Close()

	if !Exists(walPath) {
		t.Error("expected file to exist")
	}
}

func TestWALTruncate(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	writer, err := NewWriter(walPath)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write some data
	if err := writer.WriteOrder(&OrderEvent{ID: 1, Price: 100, Size: 10}); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Truncate
	if err := writer.Truncate(); err != nil {
		t.Fatalf("failed to truncate: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	// Verify file is empty
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("failed to stat: %v", err)
	}

	if info.Size() != 0 {
		t.Errorf("expected file size 0, got %d", info.Size())
	}
}

func BenchmarkWALWrite(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	writer, err := NewWriter(walPath)
	if err != nil {
		b.Fatalf("failed to create writer: %v", err)
	}
	defer writer.Close()

	event := &OrderEvent{
		ID:        1,
		UserID:    100,
		Side:      0,
		Type:      0,
		Price:     10000,
		Size:      50,
		Timestamp: time.Now().UnixNano(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event.ID = uint64(i)
		if err := writer.WriteOrder(event); err != nil {
			b.Fatalf("failed to write: %v", err)
		}
	}
}
