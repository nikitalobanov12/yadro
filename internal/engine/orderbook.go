package engine

import "sync/atomic"

// OrderBook maintains two Red-Black Trees for bids and asks.
// Bids are ordered high to low (best bid = highest price).
// Asks are ordered low to high (best ask = lowest price).
type OrderBook struct {
	Bids   *RBTree           // Buy orders: high to low
	Asks   *RBTree           // Sell orders: low to high
	Orders map[uint64]*Order // All orders indexed by ID for O(1) lookup

	nextOrderID atomic.Uint64
	nextTradeID atomic.Uint64
}

// NewOrderBook creates a new order book.
func NewOrderBook() *OrderBook {
	return &OrderBook{
		Bids:   NewRBTree(true),  // Descending: highest bid first
		Asks:   NewRBTree(false), // Ascending: lowest ask first
		Orders: make(map[uint64]*Order),
	}
}

// NextOrderID generates the next order ID.
func (ob *OrderBook) NextOrderID() uint64 {
	return ob.nextOrderID.Add(1)
}

// NextTradeID generates the next trade ID.
func (ob *OrderBook) NextTradeID() uint64 {
	return ob.nextTradeID.Add(1)
}

// SetNextOrderID sets the next order ID (used during WAL recovery).
func (ob *OrderBook) SetNextOrderID(id uint64) {
	ob.nextOrderID.Store(id)
}

// SetNextTradeID sets the next trade ID (used during WAL recovery).
func (ob *OrderBook) SetNextTradeID(id uint64) {
	ob.nextTradeID.Store(id)
}

// GetTree returns the appropriate tree for the given side.
func (ob *OrderBook) GetTree(side Side) *RBTree {
	if side == Bid {
		return ob.Bids
	}
	return ob.Asks
}

// GetOppositeTree returns the opposite tree for matching.
func (ob *OrderBook) GetOppositeTree(side Side) *RBTree {
	if side == Bid {
		return ob.Asks
	}
	return ob.Bids
}

// AddOrder adds an order to the book at its price level.
// Returns the list of book updates for broadcasting.
func (ob *OrderBook) AddOrder(order *Order) []BookUpdate {
	tree := ob.GetTree(order.Side)
	level := tree.Get(order.Price)

	if level == nil {
		level = &PriceLevel{Price: order.Price}
		tree.Insert(order.Price, level)
	}

	level.AddOrder(order)
	ob.Orders[order.ID] = order

	return []BookUpdate{{
		Type:  "update",
		Price: order.Price,
		Size:  level.TotalSize(),
		Side:  order.Side.String(),
	}}
}

// RemoveOrder removes an order from the book by ID.
// Returns the list of book updates for broadcasting.
func (ob *OrderBook) RemoveOrder(orderID uint64) ([]BookUpdate, bool) {
	order, exists := ob.Orders[orderID]
	if !exists {
		return nil, false
	}

	tree := ob.GetTree(order.Side)
	level := tree.Get(order.Price)
	if level == nil {
		return nil, false
	}

	level.RemoveOrder(orderID)
	delete(ob.Orders, orderID)

	var updates []BookUpdate
	if level.IsEmpty() {
		tree.Delete(order.Price)
		updates = append(updates, BookUpdate{
			Type:  "update",
			Price: order.Price,
			Size:  0, // Level removed
			Side:  order.Side.String(),
		})
	} else {
		updates = append(updates, BookUpdate{
			Type:  "update",
			Price: order.Price,
			Size:  level.TotalSize(),
			Side:  order.Side.String(),
		})
	}

	return updates, true
}

// UpdateOrderSize updates an order's size after partial fill.
// Returns the book update for broadcasting.
func (ob *OrderBook) UpdateOrderSize(orderID uint64, newSize int64) *BookUpdate {
	order, exists := ob.Orders[orderID]
	if !exists {
		return nil
	}

	order.Size = newSize

	if newSize <= 0 {
		ob.RemoveOrder(orderID)
		return nil
	}

	tree := ob.GetTree(order.Side)
	level := tree.Get(order.Price)
	if level == nil {
		return nil
	}

	return &BookUpdate{
		Type:  "update",
		Price: order.Price,
		Size:  level.TotalSize(),
		Side:  order.Side.String(),
	}
}

// GetOrder returns an order by ID.
func (ob *OrderBook) GetOrder(orderID uint64) *Order {
	return ob.Orders[orderID]
}

// BestBid returns the best (highest) bid price level.
func (ob *OrderBook) BestBid() *PriceLevel {
	return ob.Bids.Best()
}

// BestAsk returns the best (lowest) ask price level.
func (ob *OrderBook) BestAsk() *PriceLevel {
	return ob.Asks.Best()
}

// Spread returns the current bid-ask spread.
// Returns -1 if either side is empty.
func (ob *OrderBook) Spread() int64 {
	bid := ob.BestBid()
	ask := ob.BestAsk()
	if bid == nil || ask == nil {
		return -1
	}
	return ask.Price - bid.Price
}

// Depth returns the number of price levels on each side.
func (ob *OrderBook) Depth() (bidLevels, askLevels int) {
	return ob.Bids.Size(), ob.Asks.Size()
}

// TotalOrders returns the total number of orders in the book.
func (ob *OrderBook) TotalOrders() int {
	return len(ob.Orders)
}

// BookSnapshot represents a snapshot of the order book.
type BookSnapshot struct {
	Bids []PriceLevelSnapshot `json:"bids"`
	Asks []PriceLevelSnapshot `json:"asks"`
}

// PriceLevelSnapshot represents a price level in a snapshot.
type PriceLevelSnapshot struct {
	Price int64 `json:"price"`
	Size  int64 `json:"size"`
}

// Snapshot returns a snapshot of the order book for REST API.
func (ob *OrderBook) Snapshot(depth int) BookSnapshot {
	snapshot := BookSnapshot{
		Bids: make([]PriceLevelSnapshot, 0, depth),
		Asks: make([]PriceLevelSnapshot, 0, depth),
	}

	count := 0
	ob.Bids.ForEach(func(price int64, level *PriceLevel) bool {
		if count >= depth {
			return false
		}
		snapshot.Bids = append(snapshot.Bids, PriceLevelSnapshot{
			Price: price,
			Size:  level.TotalSize(),
		})
		count++
		return true
	})

	count = 0
	ob.Asks.ForEach(func(price int64, level *PriceLevel) bool {
		if count >= depth {
			return false
		}
		snapshot.Asks = append(snapshot.Asks, PriceLevelSnapshot{
			Price: price,
			Size:  level.TotalSize(),
		})
		count++
		return true
	})

	return snapshot
}
