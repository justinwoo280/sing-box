package xhttp

import (
	"sync"
	"time"
)

type readDeadlineState struct {
	mu      sync.Mutex
	timer   *time.Timer
	expired chan struct{}
	active  bool
}

func newReadDeadlineState() readDeadlineState {
	return readDeadlineState{expired: make(chan struct{})}
}

func isClosedChan(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (d *readDeadlineState) set(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if isClosedChan(d.expired) {
		d.expired = make(chan struct{})
	}
	if t.IsZero() {
		d.active = false
		return
	}
	d.active = true
	if dur := time.Until(t); dur > 0 {
		ch := d.expired
		d.timer = time.AfterFunc(dur, func() {
			close(ch)
		})
	} else {
		close(d.expired)
	}
}

func (d *readDeadlineState) channel() chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.active {
		return nil
	}
	return d.expired
}
