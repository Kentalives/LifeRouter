package app

import "github.com/nats-io/nats.go"

// concurrentHandler bounds the number of goroutines executing a blocking NATS
// handler. When all worker slots are occupied, the NATS callback blocks on the
// semaphore instead of creating unbounded goroutines.
func concurrentHandler(workers int, handler nats.MsgHandler) nats.MsgHandler {
	if workers <= 0 {
		workers = 1
	}

	sem := make(chan struct{}, workers)
	return func(msg *nats.Msg) {
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			handler(msg)
		}()
	}
}
