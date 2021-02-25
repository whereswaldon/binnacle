package latest

type Worker struct {
	in, out *Chan
	work    func(interface{}) interface{}
}

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

func (w Worker) Raw() <-chan interface{} {
	return w.out.Raw()
}

func (w Worker) Pull() interface{} {
	return w.out.Pull()
}

func (w Worker) Push(data interface{}) {
	w.in.Push(data)
}
