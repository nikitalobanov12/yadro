package engine

import (
	"testing"
)

func TestMatcherLimitOrderNoMatch(t *testing.T) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	// Place a buy order with no sells in book
	order := &Order{
		ID:     book.NextOrderID(),
		UserID: 1,
		Side:   Bid,
		Type:   Limit,
		Price:  10000,
		Size:   100,
	}

	result := matcher.ProcessOrder(order)

	if len(result.Trades) != 0 {
		t.Errorf("expected 0 trades, got %d", len(result.Trades))
	}

	if !result.OrderPlaced {
		t.Error("expected order to be placed in book")
	}

	if result.Order == nil {
		t.Error("expected order to be returned")
	}

	if book.TotalOrders() != 1 {
		t.Errorf("expected 1 order in book, got %d", book.TotalOrders())
	}
}

func TestMatcherLimitOrderFullMatch(t *testing.T) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	// Place a sell order first
	sellOrder := &Order{
		ID:     book.NextOrderID(),
		UserID: 1,
		Side:   Ask,
		Type:   Limit,
		Price:  10000,
		Size:   100,
	}
	matcher.ProcessOrder(sellOrder)

	// Place a buy order that should fully match
	buyOrder := &Order{
		ID:     book.NextOrderID(),
		UserID: 2,
		Side:   Bid,
		Type:   Limit,
		Price:  10000, // Same price, will match
		Size:   100,
	}

	result := matcher.ProcessOrder(buyOrder)

	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(result.Trades))
	}

	trade := result.Trades[0]
	if trade.Price != 10000 {
		t.Errorf("expected trade price 10000, got %d", trade.Price)
	}

	if trade.Size != 100 {
		t.Errorf("expected trade size 100, got %d", trade.Size)
	}

	if trade.BuyerUserID != 2 {
		t.Errorf("expected buyer user ID 2, got %d", trade.BuyerUserID)
	}

	if trade.SellerUserID != 1 {
		t.Errorf("expected seller user ID 1, got %d", trade.SellerUserID)
	}

	if result.OrderPlaced {
		t.Error("expected order NOT to be placed (fully filled)")
	}

	if book.TotalOrders() != 0 {
		t.Errorf("expected 0 orders in book, got %d", book.TotalOrders())
	}
}

func TestMatcherLimitOrderPartialMatch(t *testing.T) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	// Place a sell order
	sellOrder := &Order{
		ID:     book.NextOrderID(),
		UserID: 1,
		Side:   Ask,
		Type:   Limit,
		Price:  10000,
		Size:   50,
	}
	matcher.ProcessOrder(sellOrder)

	// Place a larger buy order
	buyOrder := &Order{
		ID:     book.NextOrderID(),
		UserID: 2,
		Side:   Bid,
		Type:   Limit,
		Price:  10000,
		Size:   100,
	}

	result := matcher.ProcessOrder(buyOrder)

	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(result.Trades))
	}

	if result.Trades[0].Size != 50 {
		t.Errorf("expected trade size 50, got %d", result.Trades[0].Size)
	}

	if !result.OrderPlaced {
		t.Error("expected remaining order to be placed")
	}

	if result.Order.Size != 50 {
		t.Errorf("expected remaining size 50, got %d", result.Order.Size)
	}

	// Only the partially filled buy should remain
	if book.TotalOrders() != 1 {
		t.Errorf("expected 1 order in book, got %d", book.TotalOrders())
	}
}

func TestMatcherMarketOrder(t *testing.T) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	// Place multiple sell orders at different prices
	matcher.ProcessOrder(&Order{ID: book.NextOrderID(), UserID: 1, Side: Ask, Type: Limit, Price: 10000, Size: 50})
	matcher.ProcessOrder(&Order{ID: book.NextOrderID(), UserID: 2, Side: Ask, Type: Limit, Price: 10100, Size: 50})
	matcher.ProcessOrder(&Order{ID: book.NextOrderID(), UserID: 3, Side: Ask, Type: Limit, Price: 10200, Size: 50})

	// Market buy order should match all
	buyOrder := &Order{
		ID:     book.NextOrderID(),
		UserID: 4,
		Side:   Bid,
		Type:   Market,
		Price:  0, // Ignored for market orders
		Size:   150,
	}

	result := matcher.ProcessOrder(buyOrder)

	if len(result.Trades) != 3 {
		t.Fatalf("expected 3 trades, got %d", len(result.Trades))
	}

	// Market orders don't get placed in book
	if result.OrderPlaced {
		t.Error("expected market order NOT to be placed")
	}

	if book.TotalOrders() != 0 {
		t.Errorf("expected 0 orders in book, got %d", book.TotalOrders())
	}
}

func TestMatcherPriceTimePriority(t *testing.T) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	// Place sell orders - same price, different times
	matcher.ProcessOrder(&Order{ID: book.NextOrderID(), UserID: 1, Side: Ask, Type: Limit, Price: 10000, Size: 50})
	matcher.ProcessOrder(&Order{ID: book.NextOrderID(), UserID: 2, Side: Ask, Type: Limit, Price: 10000, Size: 50})

	// Buy order that matches one
	buyOrder := &Order{
		ID:     book.NextOrderID(),
		UserID: 3,
		Side:   Bid,
		Type:   Limit,
		Price:  10000,
		Size:   50,
	}

	result := matcher.ProcessOrder(buyOrder)

	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(result.Trades))
	}

	// Should match with user 1 (first in queue - FIFO)
	if result.Trades[0].SellerUserID != 1 {
		t.Errorf("expected to match with seller 1 (FIFO), got %d", result.Trades[0].SellerUserID)
	}

	// User 2's order should still be in book
	if book.TotalOrders() != 1 {
		t.Errorf("expected 1 order in book, got %d", book.TotalOrders())
	}
}

func TestMatcherBuyPriceImprovement(t *testing.T) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	// Sell at 100
	matcher.ProcessOrder(&Order{ID: book.NextOrderID(), UserID: 1, Side: Ask, Type: Limit, Price: 100, Size: 50})

	// Buy willing to pay 110 - should get price improvement, execute at 100
	buyOrder := &Order{
		ID:     book.NextOrderID(),
		UserID: 2,
		Side:   Bid,
		Type:   Limit,
		Price:  110,
		Size:   50,
	}

	result := matcher.ProcessOrder(buyOrder)

	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(result.Trades))
	}

	// Trade should execute at the resting order's price (100), not the incoming order's price (110)
	if result.Trades[0].Price != 100 {
		t.Errorf("expected trade at 100 (price improvement), got %d", result.Trades[0].Price)
	}
}

func TestMatcherCancelOrder(t *testing.T) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	// Place an order
	order := &Order{
		ID:     book.NextOrderID(),
		UserID: 1,
		Side:   Bid,
		Type:   Limit,
		Price:  10000,
		Size:   100,
	}
	matcher.ProcessOrder(order)

	// Cancel it
	cancelled, updates, ok := matcher.CancelOrder(order.ID)
	if !ok {
		t.Error("expected cancel to succeed")
	}

	if cancelled.ID != order.ID {
		t.Error("expected cancelled order to be returned")
	}

	if len(updates) == 0 {
		t.Error("expected book updates")
	}

	if book.TotalOrders() != 0 {
		t.Errorf("expected 0 orders, got %d", book.TotalOrders())
	}
}

func TestMatcherCancelNonExistent(t *testing.T) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	_, _, ok := matcher.CancelOrder(999)
	if ok {
		t.Error("expected cancel to fail for non-existent order")
	}
}

// Benchmark matching performance
func BenchmarkMatchOrder100Levels(b *testing.B) {
	benchmarkMatch(b, 100)
}

func BenchmarkMatchOrder1000Levels(b *testing.B) {
	benchmarkMatch(b, 1000)
}

func BenchmarkMatchOrder10000Levels(b *testing.B) {
	benchmarkMatch(b, 10000)
}

func BenchmarkMatchOrder100000Levels(b *testing.B) {
	benchmarkMatch(b, 100000)
}

func benchmarkMatch(b *testing.B, numLevels int) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	// Populate book with sell orders at different prices
	for i := 0; i < numLevels; i++ {
		order := &Order{
			ID:     book.NextOrderID(),
			UserID: uint64(i),
			Side:   Ask,
			Type:   Limit,
			Price:  int64(10000 + i),
			Size:   100,
		}
		book.AddOrder(order)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create a buy order that will match
		order := &Order{
			ID:     book.NextOrderID(),
			UserID: 999999,
			Side:   Bid,
			Type:   Limit,
			Price:  int64(10000 + (i % numLevels)),
			Size:   1, // Small size so we don't deplete the book
		}
		matcher.ProcessOrder(order)
	}
}

// BenchmarkFullCycle tests the full order lifecycle
func BenchmarkFullCycle(b *testing.B) {
	book := NewOrderBook()
	matcher := NewMatcher(book)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Place a sell
		sellOrder := &Order{
			ID:     book.NextOrderID(),
			UserID: 1,
			Side:   Ask,
			Type:   Limit,
			Price:  10000,
			Size:   100,
		}
		matcher.ProcessOrder(sellOrder)

		// Place a matching buy
		buyOrder := &Order{
			ID:     book.NextOrderID(),
			UserID: 2,
			Side:   Bid,
			Type:   Limit,
			Price:  10000,
			Size:   100,
		}
		matcher.ProcessOrder(buyOrder)
	}
}
