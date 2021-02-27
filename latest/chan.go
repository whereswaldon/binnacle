/*
Package latest provides concurrent data structures that waste the
least possible work on state data.
*/
package latest

// Chan acts like a normal channel except that sending will not
// block. Either it will send immediately, buffer into the single
// buffer slot, or overwrite the data already in that buffer slot.
//
// Chan is safe to use with a single writer and any number of readers.
type Chan struct {
	in chan interface{}
}

// NewChan initializes a chan.
func NewChan() *Chan {
	return &Chan{
		in: make(chan interface{}, 1),
	}
}

// Push sends x. It should never block.
func (n *Chan) Push(x interface{}) {
	for {
		// try to push our new value
		select {
		case n.in <- x:
			// if we can push the new value, we're done
			return
		default:
			// if we can't push the new value without
			// blocking, we need to clear the queue
			// ahead of us
		}
		// try to clear the queue ahead of us
		select {
		case <-n.in:
			// we removed the thing ahead of us; awesome!
		default:
			// someone else already consumed the thing ahead of us
		}
	}
}

// Pull blocks until it is able to receive on the channel.
func (n *Chan) Pull() interface{} {
	return <-n.in
}

// Raw exposes the underlying channel of the Chan for use in
// select statements.
func (n *Chan) Raw() <-chan interface{} {
	return n.in
}

// Close closes the underlying channel. After closing, you
// should never Push data.
func (n *Chan) Close() {
	close(n.in)
}
