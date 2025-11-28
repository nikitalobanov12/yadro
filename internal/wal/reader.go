package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// Reader reads events from the WAL for recovery.
type Reader struct {
	file *os.File
	path string
}

// NewReader creates a new WAL reader.
func NewReader(path string) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No WAL file, nothing to recover
		}
		return nil, fmt.Errorf("wal: failed to open file: %w", err)
	}

	return &Reader{
		file: file,
		path: path,
	}, nil
}

// Event represents a generic WAL event.
type Event struct {
	Type   uint8
	Order  *OrderEvent
	Cancel *CancelEvent
	Trade  *TradeEvent
}

// ReadAll reads all events from the WAL.
func (r *Reader) ReadAll() ([]*Event, error) {
	if r == nil || r.file == nil {
		return nil, nil
	}

	var events []*Event

	for {
		event, err := r.readNext()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return events, err // Return what we have plus error
		}
		events = append(events, event)
	}

	return events, nil
}

// readNext reads the next event from the WAL.
func (r *Reader) readNext() (*Event, error) {
	// Read size (4 bytes)
	sizeBytes := make([]byte, 4)
	if _, err := io.ReadFull(r.file, sizeBytes); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(sizeBytes)

	if size == 0 {
		return nil, ErrCorrupted
	}

	// Read type and payload
	data := make([]byte, size)
	if _, err := io.ReadFull(r.file, data); err != nil {
		return nil, fmt.Errorf("wal: failed to read record: %w", err)
	}

	eventType := data[0]
	payload := data[1:]

	switch eventType {
	case EventPlaceOrder:
		return r.parseOrderEvent(payload)
	case EventCancelOrder:
		return r.parseCancelEvent(payload)
	case EventTrade:
		return r.parseTradeEvent(payload)
	default:
		return nil, fmt.Errorf("wal: unknown event type: %d", eventType)
	}
}

func (r *Reader) parseOrderEvent(payload []byte) (*Event, error) {
	if len(payload) < 42 {
		return nil, ErrCorrupted
	}

	return &Event{
		Type: EventPlaceOrder,
		Order: &OrderEvent{
			ID:        binary.LittleEndian.Uint64(payload[0:8]),
			UserID:    binary.LittleEndian.Uint64(payload[8:16]),
			Side:      payload[16],
			Type:      payload[17],
			Price:     int64(binary.LittleEndian.Uint64(payload[18:26])),
			Size:      int64(binary.LittleEndian.Uint64(payload[26:34])),
			Timestamp: int64(binary.LittleEndian.Uint64(payload[34:42])),
		},
	}, nil
}

func (r *Reader) parseCancelEvent(payload []byte) (*Event, error) {
	if len(payload) < 16 {
		return nil, ErrCorrupted
	}

	return &Event{
		Type: EventCancelOrder,
		Cancel: &CancelEvent{
			OrderID:   binary.LittleEndian.Uint64(payload[0:8]),
			Timestamp: int64(binary.LittleEndian.Uint64(payload[8:16])),
		},
	}, nil
}

func (r *Reader) parseTradeEvent(payload []byte) (*Event, error) {
	if len(payload) < 64 {
		return nil, ErrCorrupted
	}

	return &Event{
		Type: EventTrade,
		Trade: &TradeEvent{
			ID:           binary.LittleEndian.Uint64(payload[0:8]),
			BuyOrderID:   binary.LittleEndian.Uint64(payload[8:16]),
			SellOrderID:  binary.LittleEndian.Uint64(payload[16:24]),
			BuyerUserID:  binary.LittleEndian.Uint64(payload[24:32]),
			SellerUserID: binary.LittleEndian.Uint64(payload[32:40]),
			Price:        int64(binary.LittleEndian.Uint64(payload[40:48])),
			Size:         int64(binary.LittleEndian.Uint64(payload[48:56])),
			Timestamp:    int64(binary.LittleEndian.Uint64(payload[56:64])),
		},
	}, nil
}

// Close closes the WAL reader.
func (r *Reader) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	return r.file.Close()
}

// Exists checks if the WAL file exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
