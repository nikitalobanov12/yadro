package engine

// Color represents the color of a Red-Black Tree node.
type Color bool

const (
	Red   Color = true
	Black Color = false
)

// RBNode represents a node in the Red-Black Tree.
type RBNode struct {
	Key    int64       // Price level
	Value  *PriceLevel // Orders at this price
	Color  Color
	Left   *RBNode
	Right  *RBNode
	Parent *RBNode
}

// RBTree is a Red-Black Tree implementation for price levels.
// It provides O(log n) insertion, deletion, and lookup.
type RBTree struct {
	Root       *RBNode
	size       int
	Descending bool // If true, iterates high to low (for bids)
}

// NewRBTree creates a new Red-Black Tree.
func NewRBTree(descending bool) *RBTree {
	return &RBTree{
		Descending: descending,
	}
}

// Size returns the number of nodes in the tree.
func (t *RBTree) Size() int {
	return t.size
}

// IsEmpty returns true if the tree has no nodes.
func (t *RBTree) IsEmpty() bool {
	return t.Root == nil
}

// Get returns the price level at the given price, or nil if not found.
func (t *RBTree) Get(price int64) *PriceLevel {
	node := t.findNode(price)
	if node != nil {
		return node.Value
	}
	return nil
}

// findNode finds the node with the given key.
func (t *RBTree) findNode(key int64) *RBNode {
	node := t.Root
	for node != nil {
		if key == node.Key {
			return node
		} else if key < node.Key {
			node = node.Left
		} else {
			node = node.Right
		}
	}
	return nil
}

// Insert adds or updates a price level in the tree.
func (t *RBTree) Insert(price int64, level *PriceLevel) {
	newNode := &RBNode{
		Key:   price,
		Value: level,
		Color: Red,
	}

	if t.Root == nil {
		t.Root = newNode
		t.Root.Color = Black
		t.size++
		return
	}

	// Standard BST insert
	current := t.Root
	var parent *RBNode
	for current != nil {
		parent = current
		if price == current.Key {
			// Update existing node
			current.Value = level
			return
		} else if price < current.Key {
			current = current.Left
		} else {
			current = current.Right
		}
	}

	newNode.Parent = parent
	if price < parent.Key {
		parent.Left = newNode
	} else {
		parent.Right = newNode
	}
	t.size++

	// Fix Red-Black Tree properties
	t.insertFixup(newNode)
}

// insertFixup fixes the Red-Black Tree after insertion.
func (t *RBTree) insertFixup(node *RBNode) {
	for node != t.Root && node.Parent.Color == Red {
		if node.Parent == node.Parent.Parent.Left {
			uncle := node.Parent.Parent.Right
			if uncle != nil && uncle.Color == Red {
				// Case 1: Uncle is red
				node.Parent.Color = Black
				uncle.Color = Black
				node.Parent.Parent.Color = Red
				node = node.Parent.Parent
			} else {
				if node == node.Parent.Right {
					// Case 2: Node is right child
					node = node.Parent
					t.leftRotate(node)
				}
				// Case 3: Node is left child
				node.Parent.Color = Black
				node.Parent.Parent.Color = Red
				t.rightRotate(node.Parent.Parent)
			}
		} else {
			uncle := node.Parent.Parent.Left
			if uncle != nil && uncle.Color == Red {
				// Case 1: Uncle is red
				node.Parent.Color = Black
				uncle.Color = Black
				node.Parent.Parent.Color = Red
				node = node.Parent.Parent
			} else {
				if node == node.Parent.Left {
					// Case 2: Node is left child
					node = node.Parent
					t.rightRotate(node)
				}
				// Case 3: Node is right child
				node.Parent.Color = Black
				node.Parent.Parent.Color = Red
				t.leftRotate(node.Parent.Parent)
			}
		}
	}
	t.Root.Color = Black
}

// Delete removes a price level from the tree.
func (t *RBTree) Delete(price int64) bool {
	node := t.findNode(price)
	if node == nil {
		return false
	}
	t.deleteNode(node)
	t.size--
	return true
}

// deleteNode removes a node from the tree.
func (t *RBTree) deleteNode(z *RBNode) {
	var x, y *RBNode
	y = z
	yOriginalColor := y.Color

	if z.Left == nil {
		x = z.Right
		t.transplant(z, z.Right)
	} else if z.Right == nil {
		x = z.Left
		t.transplant(z, z.Left)
	} else {
		y = t.minimum(z.Right)
		yOriginalColor = y.Color
		x = y.Right
		if y.Parent == z {
			if x != nil {
				x.Parent = y
			}
		} else {
			t.transplant(y, y.Right)
			y.Right = z.Right
			if y.Right != nil {
				y.Right.Parent = y
			}
		}
		t.transplant(z, y)
		y.Left = z.Left
		y.Left.Parent = y
		y.Color = z.Color
	}

	if yOriginalColor == Black && x != nil {
		t.deleteFixup(x)
	}
}

// transplant replaces subtree rooted at u with subtree rooted at v.
func (t *RBTree) transplant(u, v *RBNode) {
	if u.Parent == nil {
		t.Root = v
	} else if u == u.Parent.Left {
		u.Parent.Left = v
	} else {
		u.Parent.Right = v
	}
	if v != nil {
		v.Parent = u.Parent
	}
}

// deleteFixup fixes the Red-Black Tree after deletion.
func (t *RBTree) deleteFixup(x *RBNode) {
	for x != t.Root && x.Color == Black {
		if x == x.Parent.Left {
			w := x.Parent.Right
			if w != nil && w.Color == Red {
				w.Color = Black
				x.Parent.Color = Red
				t.leftRotate(x.Parent)
				w = x.Parent.Right
			}
			if w == nil {
				x = x.Parent
				continue
			}
			if (w.Left == nil || w.Left.Color == Black) && (w.Right == nil || w.Right.Color == Black) {
				w.Color = Red
				x = x.Parent
			} else {
				if w.Right == nil || w.Right.Color == Black {
					if w.Left != nil {
						w.Left.Color = Black
					}
					w.Color = Red
					t.rightRotate(w)
					w = x.Parent.Right
				}
				if w != nil {
					w.Color = x.Parent.Color
					x.Parent.Color = Black
					if w.Right != nil {
						w.Right.Color = Black
					}
					t.leftRotate(x.Parent)
				}
				x = t.Root
			}
		} else {
			w := x.Parent.Left
			if w != nil && w.Color == Red {
				w.Color = Black
				x.Parent.Color = Red
				t.rightRotate(x.Parent)
				w = x.Parent.Left
			}
			if w == nil {
				x = x.Parent
				continue
			}
			if (w.Right == nil || w.Right.Color == Black) && (w.Left == nil || w.Left.Color == Black) {
				w.Color = Red
				x = x.Parent
			} else {
				if w.Left == nil || w.Left.Color == Black {
					if w.Right != nil {
						w.Right.Color = Black
					}
					w.Color = Red
					t.leftRotate(w)
					w = x.Parent.Left
				}
				if w != nil {
					w.Color = x.Parent.Color
					x.Parent.Color = Black
					if w.Left != nil {
						w.Left.Color = Black
					}
					t.rightRotate(x.Parent)
				}
				x = t.Root
			}
		}
	}
	if x != nil {
		x.Color = Black
	}
}

// leftRotate performs a left rotation.
func (t *RBTree) leftRotate(x *RBNode) {
	y := x.Right
	x.Right = y.Left
	if y.Left != nil {
		y.Left.Parent = x
	}
	y.Parent = x.Parent
	if x.Parent == nil {
		t.Root = y
	} else if x == x.Parent.Left {
		x.Parent.Left = y
	} else {
		x.Parent.Right = y
	}
	y.Left = x
	x.Parent = y
}

// rightRotate performs a right rotation.
func (t *RBTree) rightRotate(x *RBNode) {
	y := x.Left
	x.Left = y.Right
	if y.Right != nil {
		y.Right.Parent = x
	}
	y.Parent = x.Parent
	if x.Parent == nil {
		t.Root = y
	} else if x == x.Parent.Right {
		x.Parent.Right = y
	} else {
		x.Parent.Left = y
	}
	y.Right = x
	x.Parent = y
}

// minimum finds the minimum node in a subtree.
func (t *RBTree) minimum(node *RBNode) *RBNode {
	for node.Left != nil {
		node = node.Left
	}
	return node
}

// maximum finds the maximum node in a subtree.
func (t *RBTree) maximum(node *RBNode) *RBNode {
	for node.Right != nil {
		node = node.Right
	}
	return node
}

// Min returns the minimum price level (best ask for ascending tree).
func (t *RBTree) Min() *PriceLevel {
	if t.Root == nil {
		return nil
	}
	return t.minimum(t.Root).Value
}

// Max returns the maximum price level (best bid for descending tree).
func (t *RBTree) Max() *PriceLevel {
	if t.Root == nil {
		return nil
	}
	return t.maximum(t.Root).Value
}

// Best returns the best price level based on tree ordering.
// For bids (descending): returns maximum (highest price).
// For asks (ascending): returns minimum (lowest price).
func (t *RBTree) Best() *PriceLevel {
	if t.Descending {
		return t.Max()
	}
	return t.Min()
}

// BestNode returns the best node based on tree ordering.
func (t *RBTree) BestNode() *RBNode {
	if t.Root == nil {
		return nil
	}
	if t.Descending {
		return t.maximum(t.Root)
	}
	return t.minimum(t.Root)
}

// ForEach iterates over all price levels in order.
// For ascending tree: low to high.
// For descending tree: high to low.
func (t *RBTree) ForEach(fn func(price int64, level *PriceLevel) bool) {
	if t.Descending {
		t.forEachDescending(t.Root, fn)
	} else {
		t.forEachAscending(t.Root, fn)
	}
}

func (t *RBTree) forEachAscending(node *RBNode, fn func(int64, *PriceLevel) bool) bool {
	if node == nil {
		return true
	}
	if !t.forEachAscending(node.Left, fn) {
		return false
	}
	if !fn(node.Key, node.Value) {
		return false
	}
	return t.forEachAscending(node.Right, fn)
}

func (t *RBTree) forEachDescending(node *RBNode, fn func(int64, *PriceLevel) bool) bool {
	if node == nil {
		return true
	}
	if !t.forEachDescending(node.Right, fn) {
		return false
	}
	if !fn(node.Key, node.Value) {
		return false
	}
	return t.forEachDescending(node.Left, fn)
}

// ToSlice returns all price levels as a slice in order.
func (t *RBTree) ToSlice() []*PriceLevel {
	result := make([]*PriceLevel, 0, t.size)
	t.ForEach(func(_ int64, level *PriceLevel) bool {
		result = append(result, level)
		return true
	})
	return result
}
