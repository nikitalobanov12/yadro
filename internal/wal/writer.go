package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
)

// Event types for the WAL.
const (
	EventPlaceOrder  uint8 = 1
	EventCancelOrder uint8 = 2
	EventTrade       uint8 = 3
)

// ErrCorrupted indicates the WAL file is corrupted.
var ErrCorrupted = errors.New("wal: corrupted log file")

// OrderEvent represents a place order event in the WAL.
type OrderEvent struct {
	ID        uint64
	UserID    uint64
	Side      uint8 // 0 = Bid, 1 = Ask
	Type      uint8 // 0 = Limit, 1 = Market
	Price     int64
	Size      int64
	Timestamp int64
}

// CancelEvent represents a cancel order event in the WAL.
type CancelEvent struct {
	OrderID   uint64
	Timestamp int64
}

// TradeEvent represents a trade event in the WAL.
type TradeEvent struct {
	ID           uint64
	BuyOrderID   uint64
	SellOrderID  uint64
	BuyerUserID  uint64
	SellerUserID uint64
	Price        int64
	Size         int64
	Timestamp    int64
}

// Writer writes events to the WAL.
type Writer struct {
	file *os.File
	mu   sync.Mutex
	path string
}

// NewWriter creates a new WAL writer.
func NewWriter(path string) (*Writer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("wal: failed to open file: %w", err)
	}

	return &Writer{
		file: file,
		path: path,
	}, nil
}

// WriteOrder writes a place order event to the WAL.
func (w *Writer) WriteOrder(event *OrderEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Calculate payload size: 8+8+1+1+8+8+8 = 42 bytes
	payload := make([]byte, 42)
	binary.LittleEndian.PutUint64(payload[0:8], event.ID)
	binary.LittleEndian.PutUint64(payload[8:16], event.UserID)
	payload[16] = event.Side
	payload[17] = event.Type
	binary.LittleEndian.PutUint64(payload[18:26], uint64(event.Price))
	binary.LittleEndian.PutUint64(payload[26:34], uint64(event.Size))
	binary.LittleEndian.PutUint64(payload[34:42], uint64(event.Timestamp))

	return w.writeRecord(EventPlaceOrder, payload)
}

// WriteCancel writes a cancel order event to the WAL.
func (w *Writer) WriteCancel(event *CancelEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Payload: 8+8 = 16 bytes
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint64(payload[0:8], event.OrderID)
	binary.LittleEndian.PutUint64(payload[8:16], uint64(event.Timestamp))

	return w.writeRecord(EventCancelOrder, payload)
}

// WriteTrade writes a trade event to the WAL.
func (w *Writer) WriteTrade(event *TradeEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Payload: 8*8 = 64 bytes
	payload := make([]byte, 64)
	binary.LittleEndian.PutUint64(payload[0:8], event.ID)
	binary.LittleEndian.PutUint64(payload[8:16], event.BuyOrderID)
	binary.LittleEndian.PutUint64(payload[16:24], event.SellOrderID)
	binary.LittleEndian.PutUint64(payload[24:32], event.BuyerUserID)
	binary.LittleEndian.PutUint64(payload[32:40], event.SellerUserID)
	binary.LittleEndian.PutUint64(payload[40:48], uint64(event.Price))
	binary.LittleEndian.PutUint64(payload[48:56], uint64(event.Size))
	binary.LittleEndian.PutUint64(payload[56:64], uint64(event.Timestamp))

	return w.writeRecord(EventTrade, payload)
}

// writeRecord writes a record to the WAL.
// Format: [Size (4 bytes)] [Type (1 byte)] [Payload (N bytes)]
func (w *Writer) writeRecord(eventType uint8, payload []byte) error {
	// Write size (4 bytes)
	size := uint32(1 + len(payload)) // type + payload
	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, size)

	if _, err := w.file.Write(sizeBytes); err != nil {
		return fmt.Errorf("wal: failed to write size: %w", err)
	}

	// Write type (1 byte)
	if _, err := w.file.Write([]byte{eventType}); err != nil {
		return fmt.Errorf("wal: failed to write type: %w", err)
	}

	// Write payload
	if _, err := w.file.Write(payload); err != nil {
		return fmt.Errorf("wal: failed to write payload: %w", err)
	}

	// Sync to disk for durability
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("wal: failed to sync: %w", err)
	}

	return nil
}

// Close closes the WAL file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("wal: failed to sync on close: %w", err)
	}

	return w.file.Close()
}

// Truncate clears the WAL file (for testing or after snapshot).
func (w *Writer) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Truncate(0); err != nil {
		return fmt.Errorf("wal: failed to truncate: %w", err)
	}

	if _, err := w.file.Seek(0, 0); err != nil {
		return fmt.Errorf("wal: failed to seek: %w", err)
	}

	return nil
}
