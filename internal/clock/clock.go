package clock

import (
	"sync"
	"time"
)

// Clock supplies the current time to code that needs deterministic time control.
type Clock interface {
	Now() time.Time
}

// RealClock returns the system time.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

// FakeClock is a thread-safe clock for tests.
type FakeClock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewFakeClock(now time.Time) *FakeClock {
	return &FakeClock{now: now}
}

func (c *FakeClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *FakeClock) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
