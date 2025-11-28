package engine

import "time"

// Matcher handles order matching logic.
type Matcher struct {
	Book *OrderBook
}

// NewMatcher creates a new matcher with the given order book.
func NewMatcher(book *OrderBook) *Matcher {
	return &Matcher{Book: book}
}

// MatchResult contains the result of matching an order.
type MatchResult struct {
	Trades      []*Trade     // Trades that occurred
	Updates     []BookUpdate // Book updates for broadcasting
	Order       *Order       // Remaining order (nil if fully filled)
	OrderPlaced bool         // True if order was added to book
}

// ProcessOrder processes an incoming order, matching it against the book.
// This is the core matching algorithm with O(log n) price level lookup.
func (m *Matcher) ProcessOrder(order *Order) *MatchResult {
	result := &MatchResult{
		Trades:  make([]*Trade, 0),
		Updates: make([]BookUpdate, 0),
	}

	// Get the opposite side's tree for matching
	oppositeTree := m.Book.GetOppositeTree(order.Side)

	// Match against opposite side until order is filled or no more matches
	for order.Size > 0 {
		bestLevel := oppositeTree.Best()
		if bestLevel == nil {
			break
		}

		// Check if price is acceptable
		if !m.isPriceAcceptable(order, bestLevel.Price) {
			break
		}

		// Match against orders at this price level (FIFO)
		for len(bestLevel.Orders) > 0 && order.Size > 0 {
			restingOrder := bestLevel.Orders[0]

			// Calculate trade size
			tradeSize := min(order.Size, restingOrder.Size)

			// Create trade
			trade := m.createTrade(order, restingOrder, bestLevel.Price, tradeSize)
			result.Trades = append(result.Trades, trade)

			// Update order sizes
			order.Size -= tradeSize
			restingOrder.Size -= tradeSize

			// Remove filled resting order
			if restingOrder.Size <= 0 {
				bestLevel.Orders = bestLevel.Orders[1:]
				delete(m.Book.Orders, restingOrder.ID)
			}
		}

		// Update or remove the price level
		if bestLevel.IsEmpty() {
			oppositeTree.Delete(bestLevel.Price)
			result.Updates = append(result.Updates, BookUpdate{
				Type:  "update",
				Price: bestLevel.Price,
				Size:  0,
				Side:  order.Side.Opposite().String(),
			})
		} else {
			result.Updates = append(result.Updates, BookUpdate{
				Type:  "update",
				Price: bestLevel.Price,
				Size:  bestLevel.TotalSize(),
				Side:  order.Side.Opposite().String(),
			})
		}
	}

	// Add remaining order to book if it's a limit order with remaining size
	if order.Size > 0 && order.Type == Limit {
		updates := m.Book.AddOrder(order)
		result.Updates = append(result.Updates, updates...)
		result.Order = order
		result.OrderPlaced = true
	}

	// Broadcast trade updates
	for _, trade := range result.Trades {
		result.Updates = append(result.Updates, BookUpdate{
			Type:  "trade",
			Price: trade.Price,
			Size:  trade.Size,
			Side:  order.Side.String(),
		})
	}

	return result
}

// isPriceAcceptable checks if the price is acceptable for matching.
func (m *Matcher) isPriceAcceptable(order *Order, bookPrice int64) bool {
	// Market orders accept any price
	if order.Type == Market {
		return true
	}

	// For limit orders:
	// - Buy order: book price must be <= order price (willing to pay up to limit)
	// - Sell order: book price must be >= order price (willing to sell at least at limit)
	if order.Side == Bid {
		return bookPrice <= order.Price
	}
	return bookPrice >= order.Price
}

// createTrade creates a trade between two orders.
func (m *Matcher) createTrade(incomingOrder, restingOrder *Order, price, size int64) *Trade {
	var buyOrderID, sellOrderID uint64
	var buyerUserID, sellerUserID uint64

	if incomingOrder.Side == Bid {
		buyOrderID = incomingOrder.ID
		buyerUserID = incomingOrder.UserID
		sellOrderID = restingOrder.ID
		sellerUserID = restingOrder.UserID
	} else {
		buyOrderID = restingOrder.ID
		buyerUserID = restingOrder.UserID
		sellOrderID = incomingOrder.ID
		sellerUserID = incomingOrder.UserID
	}

	return &Trade{
		ID:           m.Book.NextTradeID(),
		BuyOrderID:   buyOrderID,
		SellOrderID:  sellOrderID,
		BuyerUserID:  buyerUserID,
		SellerUserID: sellerUserID,
		Price:        price,
		Size:         size,
		Timestamp:    time.Now().UnixNano(),
	}
}

// CancelOrder cancels an order by ID.
func (m *Matcher) CancelOrder(orderID uint64) (*Order, []BookUpdate, bool) {
	order := m.Book.GetOrder(orderID)
	if order == nil {
		return nil, nil, false
	}

	updates, ok := m.Book.RemoveOrder(orderID)
	return order, updates, ok
}

// min returns the minimum of two int64 values.
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
