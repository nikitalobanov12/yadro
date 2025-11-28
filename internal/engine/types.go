package engine

import "time"

// Side represents the order side (Bid or Ask).
type Side uint8

const (
	Bid Side = iota // Buy order
	Ask             // Sell order
)

func (s Side) String() string {
	if s == Bid {
		return "bid"
	}
	return "ask"
}

// Opposite returns the opposite side.
func (s Side) Opposite() Side {
	if s == Bid {
		return Ask
	}
	return Bid
}

// OrderType represents the type of order.
type OrderType uint8

const (
	Limit  OrderType = iota // Limit order - only execute at specified price or better
	Market                  // Market order - execute at any available price
)

func (t OrderType) String() string {
	if t == Limit {
		return "limit"
	}
	return "market"
}

// Order represents a single order in the order book.
// All prices are in atomic units (cents, satoshis, etc.) - NO FLOATS.
type Order struct {
	ID        uint64    // Unique order identifier
	UserID    uint64    // User who placed the order
	Side      Side      // Bid (buy) or Ask (sell)
	Type      OrderType // Limit or Market
	Price     int64     // Price in atomic units (ignored for market orders)
	Size      int64     // Quantity remaining
	Timestamp int64     // Unix nanoseconds when order was placed
}

// NewOrder creates a new order with the current timestamp.
func NewOrder(id, userID uint64, side Side, orderType OrderType, price, size int64) *Order {
	return &Order{
		ID:        id,
		UserID:    userID,
		Side:      side,
		Type:      orderType,
		Price:     price,
		Size:      size,
		Timestamp: time.Now().UnixNano(),
	}
}

// IsFilled returns true if the order has been completely filled.
func (o *Order) IsFilled() bool {
	return o.Size <= 0
}

// Trade represents a matched trade between two orders.
type Trade struct {
	ID           uint64 // Unique trade identifier
	BuyOrderID   uint64 // The buy order involved
	SellOrderID  uint64 // The sell order involved
	BuyerUserID  uint64 // Buyer's user ID
	SellerUserID uint64 // Seller's user ID
	Price        int64  // Execution price in atomic units
	Size         int64  // Quantity traded
	Timestamp    int64  // Unix nanoseconds when trade occurred
}

// PriceLevel represents all orders at a single price point.
type PriceLevel struct {
	Price  int64    // The price of this level
	Orders []*Order // Orders at this price (FIFO queue)
}

// TotalSize returns the total quantity available at this price level.
func (pl *PriceLevel) TotalSize() int64 {
	var total int64
	for _, order := range pl.Orders {
		total += order.Size
	}
	return total
}

// AddOrder adds an order to this price level (FIFO).
func (pl *PriceLevel) AddOrder(order *Order) {
	pl.Orders = append(pl.Orders, order)
}

// RemoveOrder removes an order from this price level by ID.
func (pl *PriceLevel) RemoveOrder(orderID uint64) bool {
	for i, order := range pl.Orders {
		if order.ID == orderID {
			pl.Orders = append(pl.Orders[:i], pl.Orders[i+1:]...)
			return true
		}
	}
	return false
}

// IsEmpty returns true if there are no orders at this level.
func (pl *PriceLevel) IsEmpty() bool {
	return len(pl.Orders) == 0
}

// BookUpdate represents a change to the order book for broadcasting.
type BookUpdate struct {
	Type  string `json:"type"`  // "update" or "trade"
	Price int64  `json:"price"` // Price level that changed
	Size  int64  `json:"size"`  // New total size at this level (0 = level removed)
	Side  string `json:"side"`  // "bid" or "ask"
}

// OrderResult represents the result of placing an order.
type OrderResult struct {
	Order       *Order   // The order (may be partially filled or nil if fully filled)
	Trades      []*Trade // Any trades that occurred
	IsPlaced    bool     // True if order was added to the book (not fully filled)
	IsCancelled bool     // True if order was cancelled
}
