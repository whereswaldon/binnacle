package latest

// Worker provides a worker goroutine that uses the Chan type from
// this package to send and receive data. This makes it ideal for
// coordinating work between goroutines when new work always invalidates
// old work and neither goroutine can afford to block waiting on the other.
//
// Worker should always be constructed by calling NewWorker.
type Worker struct {
	in, out *Chan
	work    func(interface{}) interface{}
}

// NewWorker constructs a worker and launches a goroutine. The provided
// function will be run on each input to the Push method, and the return
// value will be available via Pull() or Raw(). Because the input and
// output are managed wtih Chans, the worker goroutine will only ever block
// waiting when there is no new input. It will never block trying to send
// output.
func NewWorker(work func(in interface{}) interface{}) Worker {
	w := Worker{
		in:   NewChan(),
		out:  NewChan(),
		work: work,
	}

	go w.run()
	return w
}

func (w Worker) run() {
	for input := range w.in.Raw() {
		w.out.Push(w.work(input))
	}
}

// Raw returns the output channel for use in a select statement.
func (w Worker) Raw() <-chan interface{} {
	return w.out.Raw()
}

// Pull blocks until data is available on the output channel.
func (w Worker) Pull() interface{} {
	return w.out.Pull()
}

// Push sends data on the input channel. It will never block.
func (w Worker) Push(data interface{}) {
	w.in.Push(data)
}

// Close shuts down the input channel. This will cause the worker
// goroutine to terminate once it finishes its current work (if
// any). You can tell when the worker is stopped when the channel
// returned by Raw() closes.
func (w Worker) Close() {
    w.in.Close()
}
