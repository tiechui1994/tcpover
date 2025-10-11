package skiplist

// newSkipListNode creates a new node initialized with specified key, value and next slice.
func newSkipListNode(level int, key uint64, value interface{}) *skipListNode {
	// For nodes with each levels, point their next slice to the nexts array allocated together,
	// which can reduce 1 memory allocation and improve performance.
	//
	// The generics of the golang doesn't support non-type parameters like in C++,
	// so we have to generate it manually.
	switch level {
	case 1:
		n := struct {
			head  skipListNode
			nexts [1]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 2:
		n := struct {
			head  skipListNode
			nexts [2]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 3:
		n := struct {
			head  skipListNode
			nexts [3]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 4:
		n := struct {
			head  skipListNode
			nexts [4]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 5:
		n := struct {
			head  skipListNode
			nexts [5]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 6:
		n := struct {
			head  skipListNode
			nexts [6]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 7:
		n := struct {
			head  skipListNode
			nexts [7]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 8:
		n := struct {
			head  skipListNode
			nexts [8]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 9:
		n := struct {
			head  skipListNode
			nexts [9]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 10:
		n := struct {
			head  skipListNode
			nexts [10]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 11:
		n := struct {
			head  skipListNode
			nexts [11]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 12:
		n := struct {
			head  skipListNode
			nexts [12]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 13:
		n := struct {
			head  skipListNode
			nexts [13]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 14:
		n := struct {
			head  skipListNode
			nexts [14]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 15:
		n := struct {
			head  skipListNode
			nexts [15]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 16:
		n := struct {
			head  skipListNode
			nexts [16]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 17:
		n := struct {
			head  skipListNode
			nexts [17]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 18:
		n := struct {
			head  skipListNode
			nexts [18]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 19:
		n := struct {
			head  skipListNode
			nexts [19]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 20:
		n := struct {
			head  skipListNode
			nexts [20]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 21:
		n := struct {
			head  skipListNode
			nexts [21]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 22:
		n := struct {
			head  skipListNode
			nexts [22]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 23:
		n := struct {
			head  skipListNode
			nexts [23]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 24:
		n := struct {
			head  skipListNode
			nexts [24]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 25:
		n := struct {
			head  skipListNode
			nexts [25]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 26:
		n := struct {
			head  skipListNode
			nexts [26]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 27:
		n := struct {
			head  skipListNode
			nexts [27]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 28:
		n := struct {
			head  skipListNode
			nexts [28]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 29:
		n := struct {
			head  skipListNode
			nexts [29]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 30:
		n := struct {
			head  skipListNode
			nexts [30]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 31:
		n := struct {
			head  skipListNode
			nexts [31]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 32:
		n := struct {
			head  skipListNode
			nexts [32]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 33:
		n := struct {
			head  skipListNode
			nexts [33]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 34:
		n := struct {
			head  skipListNode
			nexts [34]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 35:
		n := struct {
			head  skipListNode
			nexts [35]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 36:
		n := struct {
			head  skipListNode
			nexts [36]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 37:
		n := struct {
			head  skipListNode
			nexts [37]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 38:
		n := struct {
			head  skipListNode
			nexts [38]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 39:
		n := struct {
			head  skipListNode
			nexts [39]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	case 40:
		n := struct {
			head  skipListNode
			nexts [40]*skipListNode
		}{head: skipListNode{key, value, nil}}
		n.head.next = n.nexts[:]
		return &n.head
	}

	panic("should not reach here")
}
