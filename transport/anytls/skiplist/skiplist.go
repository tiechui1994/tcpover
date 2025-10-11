package skiplist

import (
	"math/bits"
	"math/rand"
	"time"
)

const (
	skipListMaxLevel = 40
)

// SkipList is a probabilistic data structure that seem likely to supplant balanced trees as the
// implementation method of choice for many applications. Skip list algorithms have the same
// asymptotic expected time bounds as balanced trees and are simpler, faster and use less space.
//
// See https://en.wikipedia.org/wiki/Skip_list for more details.
type SkipList struct {
	level int          // Current level, may increase dynamically during insertion
	len   int          // Total elements numner in the skiplist.
	head  skipListNode // head.next[level] is the head of each level.
	// This cache is used to save the previous nodes when modifying the skip list to avoid
	// allocating memory each time it is called.
	prevsCache []*skipListNode
	rander     *rand.Rand
	impl       skipListImpl
}

// NewSkipList creates a new SkipList for Ordered key type.
func NewSkipList() *SkipList {
	sl := skipListOrdered{}
	sl.init()
	sl.impl = (skipListImpl)(&sl)
	return &sl.SkipList
}

// NewSkipListFromMap creates a new SkipList from a map.
func NewSkipListFromMap(m map[uint64]interface{}) *SkipList {
	sl := NewSkipList()
	for k, v := range m {
		sl.Insert(k, v)
	}
	return sl
}

// NewSkipListFunc creates a new SkipList with specified compare function keyCmp.
func NewSkipListFunc(keyCmp CompareFn) *SkipList {
	sl := skipListFunc{}
	sl.init()
	sl.keyCmp = keyCmp
	sl.impl = skipListImpl(&sl)
	return &sl.SkipList
}

// IsEmpty implements the Container interface.
func (sl *SkipList) IsEmpty() bool {
	return sl.len == 0
}

// Len implements the Container interface.
func (sl *SkipList) Len() int {
	return sl.len
}

// Clear implements the Container interface.
func (sl *SkipList) Clear() {
	for i := range sl.head.next {
		sl.head.next[i] = nil
	}
	sl.level = 1
	sl.len = 0
}

// Iterate return an iterator to the skiplist.
func (sl *SkipList) Iterate() MapIterator {
	return &skipListIterator{sl.head.next[0], nil}
}

// Insert inserts a key-value pair into the skiplist.
// If the key is already in the skip list, it's value will be updated.
func (sl *SkipList) Insert(key uint64, value interface{}) {
	node, prevs := sl.impl.findInsertPoint(key)

	if node != nil {
		// Already exist, update the value
		node.value = value
		return
	}

	level := sl.randomLevel()
	node = newSkipListNode(level, key, value)

	minLevel := level
	if sl.level < level {
		minLevel = sl.level
	}
	for i := 0; i < minLevel; i++ {
		node.next[i] = prevs[i].next[i]
		prevs[i].next[i] = node
	}

	if level > sl.level {
		for i := sl.level; i < level; i++ {
			sl.head.next[i] = node
		}
		sl.level = level
	}

	sl.len++
}

// Find returns the value associated with the passed key if the key is in the skiplist, otherwise
// returns nil.
func (sl *SkipList) Find(key uint64) interface{} {
	node := sl.impl.findNode(key)
	if node != nil {
		return node.value
	}
	return nil
}

// Has implement the Map interface.
func (sl *SkipList) Has(key uint64) bool {
	return sl.impl.findNode(key) != nil
}

// LowerBound returns an iterator to the first element in the skiplist that
// does not satisfy element < value (i.e. greater or equal to),
// or a end itetator if no such element is found.
func (sl *SkipList) LowerBound(key uint64) MapIterator {
	return &skipListIterator{sl.impl.lowerBound(key), nil}
}

// UpperBound returns an iterator to the first element in the skiplist that
// does not satisfy value < element (i.e. strictly greater),
// or a end itetator if no such element is found.
func (sl *SkipList) UpperBound(key uint64) MapIterator {
	return &skipListIterator{sl.impl.upperBound(key), nil}
}

// FindRange returns an iterator in range [first, last) (last is not includeed).
func (sl *SkipList) FindRange(first, last uint64) MapIterator {
	return &skipListIterator{sl.impl.lowerBound(first), sl.impl.upperBound(last)}
}

// Remove removes the key-value pair associated with the passed key and returns true if the key is
// in the skiplist, otherwise returns false.
func (sl *SkipList) Remove(key uint64) bool {
	node, prevs := sl.impl.findRemovePoint(key)
	if node == nil {
		return false
	}
	for i, v := range node.next {
		prevs[i].next[i] = v
	}
	for sl.level > 1 && sl.head.next[sl.level-1] == nil {
		sl.level--
	}
	sl.len--
	return true
}

// ForEach implements the Map interface.
func (sl *SkipList) ForEach(op func(uint64, interface{})) {
	for e := sl.head.next[0]; e != nil; e = e.next[0] {
		op(e.key, e.value)
	}
}

// ForEachIf implements the Map interface.
func (sl *SkipList) ForEachIf(op func(uint64, interface{}) bool) {
	for e := sl.head.next[0]; e != nil; e = e.next[0] {
		if !op(e.key, e.value) {
			return
		}
	}
}

/// SkipList implementation part.

type skipListNode struct {
	key   uint64
	value interface{}
	next  []*skipListNode
}

//go:generate bash ./skiplist_newnode_generate.sh skipListMaxLevel skiplist_newnode.go
// func newSkipListNode[K Ordered, V any](level int, key K, value V) *skipListNode

type skipListIterator struct {
	node, end *skipListNode
}

func (it *skipListIterator) IsNotEnd() bool {
	return it.node != it.end
}

func (it *skipListIterator) MoveToNext() {
	it.node = it.node.next[0]
}

func (it *skipListIterator) Key() uint64 {
	return it.node.key
}

func (it *skipListIterator) Value() interface{} {
	return it.node.value
}

// skipListImpl is an interface to provide different implementation for Ordered key or CompareFn.
//
// We can use CompareFn to cumpare Ordered keys, but a separated implementation is much faster.
// We don't make the whole skip list an interface, in order to share the type independented method.
// And because these methods are called directly without going through the interface, they are also
// much faster.
type skipListImpl interface {
	findNode(key uint64) *skipListNode
	lowerBound(key uint64) *skipListNode
	upperBound(key uint64) *skipListNode
	findInsertPoint(key uint64) (*skipListNode, []*skipListNode)
	findRemovePoint(key uint64) (*skipListNode, []*skipListNode)
}

func (sl *SkipList) init() {
	sl.level = 1
	// #nosec G404 -- This is not a security condition
	sl.rander = rand.New(rand.NewSource(time.Now().Unix()))
	sl.prevsCache = make([]*skipListNode, skipListMaxLevel)
	sl.head.next = make([]*skipListNode, skipListMaxLevel)
}

func (sl *SkipList) randomLevel() int {
	total := uint64(1)<<uint64(skipListMaxLevel) - 1 // 2^n-1
	k := sl.rander.Uint64() % total
	level := skipListMaxLevel - bits.Len64(k) + 1
	// Since levels are randomly generated, most should be less than log2(s.len).
	// Then make a limit according to sl.len to avoid unexpectedly large value.
	for level > 3 && 1<<(level-3) > sl.len {
		level--
	}

	return level
}

/// skipListOrdered part

// skipListOrdered is the skip list implementation for Ordered types.
type skipListOrdered struct {
	SkipList
}

func (sl *skipListOrdered) findNode(key uint64) *skipListNode {
	return sl.doFindNode(key, true)
}

func (sl *skipListOrdered) doFindNode(key uint64, eq bool) *skipListNode {
	// This function execute the job of findNode if eq is true, otherwise lowBound.
	// Passing the control variable eq is ugly but it's faster than testing node
	// again outside the function in findNode.
	prev := &sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for cur := prev.next[i]; cur != nil; cur = cur.next[i] {
			if cur.key == key {
				return cur
			}
			if cur.key > key {
				// All other node in this level must be greater than the key,
				// search the next level.
				break
			}
			prev = cur
		}
	}
	if eq {
		return nil
	}
	return prev.next[0]
}

func (sl *skipListOrdered) lowerBound(key uint64) *skipListNode {
	return sl.doFindNode(key, false)
}

func (sl *skipListOrdered) upperBound(key uint64) *skipListNode {
	node := sl.lowerBound(key)
	if node != nil && node.key == key {
		return node.next[0]
	}
	return node
}

// findInsertPoint returns (*node, nil) to the existed node if the key exists,
// or (nil, []*node) to the previous nodes if the key doesn't exist
func (sl *skipListOrdered) findInsertPoint(key uint64) (*skipListNode, []*skipListNode) {
	prevs := sl.prevsCache[0:sl.level]
	prev := &sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for next := prev.next[i]; next != nil; next = next.next[i] {
			if next.key == key {
				// The key is already existed, prevs are useless because no new node insertion.
				// stop searching.
				return next, nil
			}
			if next.key > key {
				// All other node in this level must be greater than the key,
				// search the next level.
				break
			}
			prev = next
		}
		prevs[i] = prev
	}
	return nil, prevs
}

// findRemovePoint finds the node which match the key and it's previous nodes.
func (sl *skipListOrdered) findRemovePoint(key uint64) (*skipListNode, []*skipListNode) {
	prevs := sl.findPrevNodes(key)
	node := prevs[0].next[0]
	if node == nil || node.key != key {
		return nil, nil
	}
	return node, prevs
}

func (sl *skipListOrdered) findPrevNodes(key uint64) []*skipListNode {
	prevs := sl.prevsCache[0:sl.level]
	prev := &sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for next := prev.next[i]; next != nil; next = next.next[i] {
			if next.key >= key {
				break
			}
			prev = next
		}
		prevs[i] = prev
	}
	return prevs
}

/// skipListFunc part

// skipListFunc is the skip list implementation which compare keys with func.
type skipListFunc struct {
	SkipList
	keyCmp CompareFn
}

func (sl *skipListFunc) findNode(key uint64) *skipListNode {
	node := sl.lowerBound(key)
	if node != nil && sl.keyCmp(node.key, key) == 0 {
		return node
	}
	return nil
}

func (sl *skipListFunc) lowerBound(key uint64) *skipListNode {
	var prev = &sl.head
	for i := sl.level - 1; i >= 0; i-- {
		cur := prev.next[i]
		for ; cur != nil; cur = cur.next[i] {
			cmpRet := sl.keyCmp(cur.key, key)
			if cmpRet == 0 {
				return cur
			}
			if cmpRet > 0 {
				break
			}
			prev = cur
		}
	}
	return prev.next[0]
}

func (sl *skipListFunc) upperBound(key uint64) *skipListNode {
	node := sl.lowerBound(key)
	if node != nil && sl.keyCmp(node.key, key) == 0 {
		return node.next[0]
	}
	return node
}

// findInsertPoint returns (*node, nil) to the existed node if the key exists,
// or (nil, []*node) to the previous nodes if the key doesn't exist
func (sl *skipListFunc) findInsertPoint(key uint64) (*skipListNode, []*skipListNode) {
	prevs := sl.prevsCache[0:sl.level]
	prev := &sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for cur := prev.next[i]; cur != nil; cur = cur.next[i] {
			r := sl.keyCmp(cur.key, key)
			if r == 0 {
				// The key is already existed, prevs are useless because no new node insertion.
				// stop searching.
				return cur, nil
			}
			if r > 0 {
				// All other node in this level must be greater than the key,
				// search the next level.
				break
			}
			prev = cur
		}
		prevs[i] = prev
	}
	return nil, prevs
}

// findRemovePoint finds the node which match the key and it's previous nodes.
func (sl *skipListFunc) findRemovePoint(key uint64) (*skipListNode, []*skipListNode) {
	prevs := sl.findPrevNodes(key)
	node := prevs[0].next[0]
	if node == nil || sl.keyCmp(node.key, key) != 0 {
		return nil, nil
	}
	return node, prevs
}

func (sl *skipListFunc) findPrevNodes(key uint64) []*skipListNode {
	prevs := sl.prevsCache[0:sl.level]
	prev := &sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for next := prev.next[i]; next != nil; next = next.next[i] {
			if sl.keyCmp(next.key, key) >= 0 {
				break
			}
			prev = next
		}
		prevs[i] = prev
	}
	return prevs
}
