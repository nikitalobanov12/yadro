package engine

import (
	"testing"
)

func TestRBTreeInsertAndGet(t *testing.T) {
	tree := NewRBTree(false) // Ascending

	// Insert price levels
	prices := []int64{100, 50, 150, 25, 75, 125, 175}
	for _, price := range prices {
		level := &PriceLevel{Price: price}
		tree.Insert(price, level)
	}

	if tree.Size() != len(prices) {
		t.Errorf("expected size %d, got %d", len(prices), tree.Size())
	}

	// Verify all prices can be retrieved
	for _, price := range prices {
		level := tree.Get(price)
		if level == nil {
			t.Errorf("expected to find price %d", price)
		}
		if level.Price != price {
			t.Errorf("expected price %d, got %d", price, level.Price)
		}
	}

	// Verify non-existent price returns nil
	if tree.Get(999) != nil {
		t.Error("expected nil for non-existent price")
	}
}

func TestRBTreeMinMax(t *testing.T) {
	tree := NewRBTree(false) // Ascending

	prices := []int64{100, 50, 150, 25, 75, 125, 175}
	for _, price := range prices {
		tree.Insert(price, &PriceLevel{Price: price})
	}

	minLevel := tree.Min()
	if minLevel == nil || minLevel.Price != 25 {
		t.Errorf("expected min price 25, got %v", minLevel)
	}

	maxLevel := tree.Max()
	if maxLevel == nil || maxLevel.Price != 175 {
		t.Errorf("expected max price 175, got %v", maxLevel)
	}
}

func TestRBTreeBestForBids(t *testing.T) {
	tree := NewRBTree(true) // Descending (for bids)

	prices := []int64{100, 50, 150, 25, 75}
	for _, price := range prices {
		tree.Insert(price, &PriceLevel{Price: price})
	}

	best := tree.Best()
	if best == nil || best.Price != 150 {
		t.Errorf("expected best bid price 150, got %v", best)
	}
}

func TestRBTreeBestForAsks(t *testing.T) {
	tree := NewRBTree(false) // Ascending (for asks)

	prices := []int64{100, 50, 150, 25, 75}
	for _, price := range prices {
		tree.Insert(price, &PriceLevel{Price: price})
	}

	best := tree.Best()
	if best == nil || best.Price != 25 {
		t.Errorf("expected best ask price 25, got %v", best)
	}
}

func TestRBTreeDelete(t *testing.T) {
	tree := NewRBTree(false)

	prices := []int64{100, 50, 150, 25, 75, 125, 175}
	for _, price := range prices {
		tree.Insert(price, &PriceLevel{Price: price})
	}

	// Delete middle element
	if !tree.Delete(100) {
		t.Error("expected delete to succeed")
	}

	if tree.Size() != len(prices)-1 {
		t.Errorf("expected size %d, got %d", len(prices)-1, tree.Size())
	}

	if tree.Get(100) != nil {
		t.Error("expected deleted price to be nil")
	}

	// Delete non-existent
	if tree.Delete(999) {
		t.Error("expected delete of non-existent to fail")
	}
}

func TestRBTreeForEachAscending(t *testing.T) {
	tree := NewRBTree(false) // Ascending

	prices := []int64{100, 50, 150, 25, 75}
	for _, price := range prices {
		tree.Insert(price, &PriceLevel{Price: price})
	}

	var result []int64
	tree.ForEach(func(price int64, _ *PriceLevel) bool {
		result = append(result, price)
		return true
	})

	expected := []int64{25, 50, 75, 100, 150}
	for i, price := range expected {
		if result[i] != price {
			t.Errorf("at index %d: expected %d, got %d", i, price, result[i])
		}
	}
}

func TestRBTreeForEachDescending(t *testing.T) {
	tree := NewRBTree(true) // Descending

	prices := []int64{100, 50, 150, 25, 75}
	for _, price := range prices {
		tree.Insert(price, &PriceLevel{Price: price})
	}

	var result []int64
	tree.ForEach(func(price int64, _ *PriceLevel) bool {
		result = append(result, price)
		return true
	})

	expected := []int64{150, 100, 75, 50, 25}
	for i, price := range expected {
		if result[i] != price {
			t.Errorf("at index %d: expected %d, got %d", i, price, result[i])
		}
	}
}

func TestRBTreeStressTest(t *testing.T) {
	tree := NewRBTree(false)

	// Insert many elements
	n := 10000
	for i := 0; i < n; i++ {
		tree.Insert(int64(i), &PriceLevel{Price: int64(i)})
	}

	if tree.Size() != n {
		t.Errorf("expected size %d, got %d", n, tree.Size())
	}

	// Verify all can be found
	for i := 0; i < n; i++ {
		if tree.Get(int64(i)) == nil {
			t.Errorf("expected to find %d", i)
		}
	}

	// Delete half
	for i := 0; i < n/2; i++ {
		tree.Delete(int64(i))
	}

	if tree.Size() != n/2 {
		t.Errorf("expected size %d, got %d", n/2, tree.Size())
	}
}

// BenchmarkRBTreeInsert benchmarks insertion performance.
// Should show O(log n) behavior.
func BenchmarkRBTreeInsert100(b *testing.B) {
	benchmarkInsert(b, 100)
}

func BenchmarkRBTreeInsert1000(b *testing.B) {
	benchmarkInsert(b, 1000)
}

func BenchmarkRBTreeInsert10000(b *testing.B) {
	benchmarkInsert(b, 10000)
}

func BenchmarkRBTreeInsert100000(b *testing.B) {
	benchmarkInsert(b, 100000)
}

func benchmarkInsert(b *testing.B, n int) {
	for i := 0; i < b.N; i++ {
		tree := NewRBTree(false)
		for j := 0; j < n; j++ {
			tree.Insert(int64(j), &PriceLevel{Price: int64(j)})
		}
	}
}

// BenchmarkRBTreeLookup benchmarks lookup performance.
func BenchmarkRBTreeLookup100(b *testing.B) {
	benchmarkLookup(b, 100)
}

func BenchmarkRBTreeLookup1000(b *testing.B) {
	benchmarkLookup(b, 1000)
}

func BenchmarkRBTreeLookup10000(b *testing.B) {
	benchmarkLookup(b, 10000)
}

func BenchmarkRBTreeLookup100000(b *testing.B) {
	benchmarkLookup(b, 100000)
}

func benchmarkLookup(b *testing.B, n int) {
	tree := NewRBTree(false)
	for j := 0; j < n; j++ {
		tree.Insert(int64(j), &PriceLevel{Price: int64(j)})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Get(int64(i % n))
	}
}

// BenchmarkRBTreeDelete benchmarks deletion performance.
func BenchmarkRBTreeDelete100(b *testing.B) {
	benchmarkDelete(b, 100)
}

func BenchmarkRBTreeDelete1000(b *testing.B) {
	benchmarkDelete(b, 1000)
}

func BenchmarkRBTreeDelete10000(b *testing.B) {
	benchmarkDelete(b, 10000)
}

func benchmarkDelete(b *testing.B, n int) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tree := NewRBTree(false)
		for j := 0; j < n; j++ {
			tree.Insert(int64(j), &PriceLevel{Price: int64(j)})
		}
		b.StartTimer()

		for j := 0; j < n; j++ {
			tree.Delete(int64(j))
		}
	}
}
