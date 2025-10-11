package skiplist

type CompareFn func(a, b uint64) int

// Iterator is the interface for container's iterator.
type Iterator interface {
	IsNotEnd() bool     // Whether it is point to the end of the range.
	MoveToNext()        // Let it point to the next element.
	Value() interface{} // Return the value of current element.
}

// MapIterator is the interface for map's iterator.
type MapIterator interface {
	Iterator
	Key() uint64 // The key of the element
}
