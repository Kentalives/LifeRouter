package app

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestConcurrentHandlerAllowsConfiguredOverlap(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	done := make(chan struct{}, 2)
	var running int32
	var maxRunning int32

	handler := concurrentHandler(2, func(*nats.Msg) {
		current := atomic.AddInt32(&running, 1)
		for {
			maxSeen := atomic.LoadInt32(&maxRunning)
			if current <= maxSeen || atomic.CompareAndSwapInt32(&maxRunning, maxSeen, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		atomic.AddInt32(&running, -1)
		done <- struct{}{}
	})

	handler(&nats.Msg{})
	handler(&nats.Msg{})

	waitForTestSignal(t, started, "first handler start")
	waitForTestSignal(t, started, "second handler start")

	if got := atomic.LoadInt32(&maxRunning); got != 2 {
		t.Fatalf("expected two overlapping handlers, got max running %d", got)
	}

	close(release)
	waitForTestSignal(t, done, "first handler finish")
	waitForTestSignal(t, done, "second handler finish")
}

func TestConcurrentHandlerAppliesBackpressureAtLimit(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	done := make(chan struct{}, 2)
	secondReturned := make(chan struct{})

	handler := concurrentHandler(1, func(*nats.Msg) {
		started <- struct{}{}
		<-release
		done <- struct{}{}
	})

	handler(&nats.Msg{})
	waitForTestSignal(t, started, "first handler start")

	go func() {
		handler(&nats.Msg{})
		close(secondReturned)
	}()

	select {
	case <-started:
		t.Fatal("second handler started before worker slot was released")
	case <-secondReturned:
		t.Fatal("second handler call returned before worker slot was released")
	case <-time.After(25 * time.Millisecond):
	}

	release <- struct{}{}
	waitForTestSignal(t, done, "first handler finish")
	waitForTestSignal(t, secondReturned, "second handler call return")
	waitForTestSignal(t, started, "second handler start")
	release <- struct{}{}
	waitForTestSignal(t, done, "second handler finish")
}

func waitForTestSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}
