package engine

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nikita/yadro/internal/wal"
)

// Engine is the main trading engine that coordinates the order book,
// matching, and WAL. It runs on a single goroutine for thread safety.
type Engine struct {
	book    *OrderBook
	matcher *Matcher
	wal     *wal.Writer

	// Channels
	inputCh  chan *OrderRequest
	outputCh chan *EngineEvent

	// State
	running bool
	mu      sync.RWMutex
	wg      sync.WaitGroup
}

// OrderRequest represents an incoming order request.
type OrderRequest struct {
	Type     RequestType
	Order    *Order
	OrderID  uint64 // For cancel requests
	Response chan *OrderResponse
}

// RequestType defines the type of request.
type RequestType uint8

const (
	RequestPlaceOrder RequestType = iota
	RequestCancelOrder
)

// OrderResponse is the response to an order request.
type OrderResponse struct {
	Success bool
	Order   *Order
	Trades  []*Trade
	Error   error
}

// EngineEvent is an event emitted by the engine for broadcasting.
type EngineEvent struct {
	Type    string       // "update", "trade"
	Updates []BookUpdate // Book updates
	Trades  []*Trade     // Trade events
}

// Config holds engine configuration.
type Config struct {
	WALPath     string
	ChannelSize int
}

// DefaultConfig returns the default engine configuration.
func DefaultConfig() *Config {
	return &Config{
		WALPath:     "yadro.wal",
		ChannelSize: 10000,
	}
}

// NewEngine creates a new trading engine.
func NewEngine(cfg *Config) (*Engine, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	book := NewOrderBook()
	matcher := NewMatcher(book)

	walWriter, err := wal.NewWriter(cfg.WALPath)
	if err != nil {
		return nil, fmt.Errorf("engine: failed to create WAL: %w", err)
	}

	e := &Engine{
		book:     book,
		matcher:  matcher,
		wal:      walWriter,
		inputCh:  make(chan *OrderRequest, cfg.ChannelSize),
		outputCh: make(chan *EngineEvent, cfg.ChannelSize),
	}

	// Recover from WAL if it exists
	if err := e.recover(cfg.WALPath); err != nil {
		return nil, fmt.Errorf("engine: recovery failed: %w", err)
	}

	return e, nil
}

// recover replays events from the WAL to restore state.
func (e *Engine) recover(walPath string) error {
	if !wal.Exists(walPath) {
		return nil
	}

	reader, err := wal.NewReader(walPath)
	if err != nil {
		return err
	}
	if reader == nil {
		return nil
	}
	defer reader.Close()

	events, err := reader.ReadAll()
	if err != nil {
		log.Printf("engine: partial recovery, read %d events before error: %v", len(events), err)
	}

	var maxOrderID, maxTradeID uint64
	cancelledOrders := make(map[uint64]bool)
	tradedSizes := make(map[uint64]int64) // orderID -> total traded size

	// First pass: identify cancelled orders and traded sizes
	for _, event := range events {
		switch event.Type {
		case wal.EventCancelOrder:
			cancelledOrders[event.Cancel.OrderID] = true
		case wal.EventTrade:
			tradedSizes[event.Trade.BuyOrderID] += event.Trade.Size
			tradedSizes[event.Trade.SellOrderID] += event.Trade.Size
			if event.Trade.ID > maxTradeID {
				maxTradeID = event.Trade.ID
			}
		case wal.EventPlaceOrder:
			if event.Order.ID > maxOrderID {
				maxOrderID = event.Order.ID
			}
		}
	}

	// Second pass: replay orders with adjusted sizes
	for _, event := range events {
		if event.Type != wal.EventPlaceOrder {
			continue
		}

		orderEvent := event.Order
		if cancelledOrders[orderEvent.ID] {
			continue // Skip cancelled orders
		}

		// Calculate remaining size
		remainingSize := orderEvent.Size - tradedSizes[orderEvent.ID]
		if remainingSize <= 0 {
			continue // Fully filled
		}

		// Add to book directly (no matching during recovery)
		order := &Order{
			ID:        orderEvent.ID,
			UserID:    orderEvent.UserID,
			Side:      Side(orderEvent.Side),
			Type:      OrderType(orderEvent.Type),
			Price:     orderEvent.Price,
			Size:      remainingSize,
			Timestamp: orderEvent.Timestamp,
		}
		e.book.AddOrder(order)
	}

	// Set ID counters
	e.book.SetNextOrderID(maxOrderID)
	e.book.SetNextTradeID(maxTradeID)

	log.Printf("engine: recovered %d orders from WAL", e.book.TotalOrders())
	return nil
}

// Start starts the engine's event loop.
func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	e.wg.Add(1)
	go e.eventLoop(ctx)
}

// Stop stops the engine.
func (e *Engine) Stop() {
	e.mu.Lock()
	e.running = false
	e.mu.Unlock()

	close(e.inputCh)
	e.wg.Wait()

	if err := e.wal.Close(); err != nil {
		log.Printf("engine: failed to close WAL: %v", err)
	}
}

// eventLoop is the main event loop (single-threaded).
func (e *Engine) eventLoop(ctx context.Context) {
	defer e.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-e.inputCh:
			if !ok {
				return
			}
			e.processRequest(req)
		}
	}
}

// processRequest handles a single order request.
func (e *Engine) processRequest(req *OrderRequest) {
	var resp *OrderResponse

	switch req.Type {
	case RequestPlaceOrder:
		resp = e.handlePlaceOrder(req.Order)
	case RequestCancelOrder:
		resp = e.handleCancelOrder(req.OrderID)
	}

	if req.Response != nil {
		req.Response <- resp
	}
}

// handlePlaceOrder processes a place order request.
func (e *Engine) handlePlaceOrder(order *Order) *OrderResponse {
	// Assign order ID if not set
	if order.ID == 0 {
		order.ID = e.book.NextOrderID()
	}
	if order.Timestamp == 0 {
		order.Timestamp = time.Now().UnixNano()
	}

	// Write to WAL before processing
	walEvent := &wal.OrderEvent{
		ID:        order.ID,
		UserID:    order.UserID,
		Side:      uint8(order.Side),
		Type:      uint8(order.Type),
		Price:     order.Price,
		Size:      order.Size,
		Timestamp: order.Timestamp,
	}
	if err := e.wal.WriteOrder(walEvent); err != nil {
		return &OrderResponse{Success: false, Error: err}
	}

	// Match order
	result := e.matcher.ProcessOrder(order)

	// Write trades to WAL
	for _, trade := range result.Trades {
		tradeEvent := &wal.TradeEvent{
			ID:           trade.ID,
			BuyOrderID:   trade.BuyOrderID,
			SellOrderID:  trade.SellOrderID,
			BuyerUserID:  trade.BuyerUserID,
			SellerUserID: trade.SellerUserID,
			Price:        trade.Price,
			Size:         trade.Size,
			Timestamp:    trade.Timestamp,
		}
		if err := e.wal.WriteTrade(tradeEvent); err != nil {
			log.Printf("engine: failed to write trade to WAL: %v", err)
		}
	}

	// Emit events for broadcasting
	if len(result.Updates) > 0 || len(result.Trades) > 0 {
		e.emit(&EngineEvent{
			Type:    "update",
			Updates: result.Updates,
			Trades:  result.Trades,
		})
	}

	return &OrderResponse{
		Success: true,
		Order:   result.Order,
		Trades:  result.Trades,
	}
}

// handleCancelOrder processes a cancel order request.
func (e *Engine) handleCancelOrder(orderID uint64) *OrderResponse {
	order, updates, ok := e.matcher.CancelOrder(orderID)
	if !ok {
		return &OrderResponse{Success: false, Error: fmt.Errorf("order not found")}
	}

	// Write to WAL
	cancelEvent := &wal.CancelEvent{
		OrderID:   orderID,
		Timestamp: time.Now().UnixNano(),
	}
	if err := e.wal.WriteCancel(cancelEvent); err != nil {
		log.Printf("engine: failed to write cancel to WAL: %v", err)
	}

	// Emit events
	if len(updates) > 0 {
		e.emit(&EngineEvent{
			Type:    "update",
			Updates: updates,
		})
	}

	return &OrderResponse{
		Success: true,
		Order:   order,
	}
}

// emit sends an event to the output channel.
func (e *Engine) emit(event *EngineEvent) {
	select {
	case e.outputCh <- event:
	default:
		log.Println("engine: output channel full, dropping event")
	}
}

// PlaceOrder submits an order for processing.
func (e *Engine) PlaceOrder(order *Order) *OrderResponse {
	respCh := make(chan *OrderResponse, 1)
	req := &OrderRequest{
		Type:     RequestPlaceOrder,
		Order:    order,
		Response: respCh,
	}

	e.inputCh <- req
	return <-respCh
}

// CancelOrder cancels an existing order.
func (e *Engine) CancelOrder(orderID uint64) *OrderResponse {
	respCh := make(chan *OrderResponse, 1)
	req := &OrderRequest{
		Type:     RequestCancelOrder,
		OrderID:  orderID,
		Response: respCh,
	}

	e.inputCh <- req
	return <-respCh
}

// OutputChannel returns the channel for engine events.
func (e *Engine) OutputChannel() <-chan *EngineEvent {
	return e.outputCh
}

// Snapshot returns the current order book snapshot.
func (e *Engine) Snapshot(depth int) BookSnapshot {
	return e.book.Snapshot(depth)
}

// Stats returns engine statistics.
func (e *Engine) Stats() EngineStats {
	bidLevels, askLevels := e.book.Depth()
	return EngineStats{
		TotalOrders: e.book.TotalOrders(),
		BidLevels:   bidLevels,
		AskLevels:   askLevels,
		Spread:      e.book.Spread(),
	}
}

// EngineStats holds engine statistics.
type EngineStats struct {
	TotalOrders int   `json:"total_orders"`
	BidLevels   int   `json:"bid_levels"`
	AskLevels   int   `json:"ask_levels"`
	Spread      int64 `json:"spread"`
}
