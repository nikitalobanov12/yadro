package engine

import (
	"testing"
)

func TestOrderBookAddOrder(t *testing.T) {
	book := NewOrderBook()

	order := &Order{
		ID:     1,
		UserID: 100,
		Side:   Bid,
		Type:   Limit,
		Price:  10000,
		Size:   50,
	}

	updates := book.AddOrder(order)

	if len(updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(updates))
	}

	if updates[0].Price != 10000 {
		t.Errorf("expected price 10000, got %d", updates[0].Price)
	}

	if updates[0].Size != 50 {
		t.Errorf("expected size 50, got %d", updates[0].Size)
	}

	if book.TotalOrders() != 1 {
		t.Errorf("expected 1 order, got %d", book.TotalOrders())
	}
}

func TestOrderBookRemoveOrder(t *testing.T) {
	book := NewOrderBook()

	order := &Order{
		ID:     1,
		UserID: 100,
		Side:   Ask,
		Type:   Limit,
		Price:  10500,
		Size:   25,
	}

	book.AddOrder(order)

	updates, ok := book.RemoveOrder(1)
	if !ok {
		t.Error("expected remove to succeed")
	}

	if len(updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(updates))
	}

	if updates[0].Size != 0 {
		t.Errorf("expected size 0 (level removed), got %d", updates[0].Size)
	}

	if book.TotalOrders() != 0 {
		t.Errorf("expected 0 orders, got %d", book.TotalOrders())
	}
}

func TestOrderBookBestBidAsk(t *testing.T) {
	book := NewOrderBook()

	// Add bids
	book.AddOrder(&Order{ID: 1, Side: Bid, Price: 100, Size: 10})
	book.AddOrder(&Order{ID: 2, Side: Bid, Price: 105, Size: 20})
	book.AddOrder(&Order{ID: 3, Side: Bid, Price: 95, Size: 15})

	// Add asks
	book.AddOrder(&Order{ID: 4, Side: Ask, Price: 110, Size: 10})
	book.AddOrder(&Order{ID: 5, Side: Ask, Price: 108, Size: 20})
	book.AddOrder(&Order{ID: 6, Side: Ask, Price: 115, Size: 15})

	bestBid := book.BestBid()
	if bestBid == nil || bestBid.Price != 105 {
		t.Errorf("expected best bid 105, got %v", bestBid)
	}

	bestAsk := book.BestAsk()
	if bestAsk == nil || bestAsk.Price != 108 {
		t.Errorf("expected best ask 108, got %v", bestAsk)
	}

	spread := book.Spread()
	if spread != 3 {
		t.Errorf("expected spread 3, got %d", spread)
	}
}

func TestOrderBookSnapshot(t *testing.T) {
	book := NewOrderBook()

	// Add bids (should be ordered high to low)
	book.AddOrder(&Order{ID: 1, Side: Bid, Price: 100, Size: 10})
	book.AddOrder(&Order{ID: 2, Side: Bid, Price: 105, Size: 20})
	book.AddOrder(&Order{ID: 3, Side: Bid, Price: 95, Size: 15})

	// Add asks (should be ordered low to high)
	book.AddOrder(&Order{ID: 4, Side: Ask, Price: 110, Size: 10})
	book.AddOrder(&Order{ID: 5, Side: Ask, Price: 108, Size: 20})

	snapshot := book.Snapshot(10)

	if len(snapshot.Bids) != 3 {
		t.Errorf("expected 3 bids, got %d", len(snapshot.Bids))
	}

	if len(snapshot.Asks) != 2 {
		t.Errorf("expected 2 asks, got %d", len(snapshot.Asks))
	}

	// Bids should be high to low
	if snapshot.Bids[0].Price != 105 {
		t.Errorf("expected first bid at 105, got %d", snapshot.Bids[0].Price)
	}

	// Asks should be low to high
	if snapshot.Asks[0].Price != 108 {
		t.Errorf("expected first ask at 108, got %d", snapshot.Asks[0].Price)
	}
}

func TestOrderBookMultipleOrdersSamePrice(t *testing.T) {
	book := NewOrderBook()

	// Add multiple orders at same price
	book.AddOrder(&Order{ID: 1, Side: Bid, Price: 100, Size: 10})
	book.AddOrder(&Order{ID: 2, Side: Bid, Price: 100, Size: 20})
	book.AddOrder(&Order{ID: 3, Side: Bid, Price: 100, Size: 30})

	level := book.Bids.Get(100)
	if level == nil {
		t.Fatal("expected price level to exist")
	}

	if level.TotalSize() != 60 {
		t.Errorf("expected total size 60, got %d", level.TotalSize())
	}

	if len(level.Orders) != 3 {
		t.Errorf("expected 3 orders, got %d", len(level.Orders))
	}
}
