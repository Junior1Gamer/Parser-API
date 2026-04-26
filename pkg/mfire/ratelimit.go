package mfire

import (
	"sync"
	"time"
)

// SharedRateLimiter is a token-bucket rate limiter safe for concurrent use
// across multiple goroutines. It replenishes tokens at a fixed interval.
type SharedRateLimiter struct {
	tokens chan struct{}
	done   chan struct{}
	ticker *time.Ticker
	once   sync.Once
}

// NewSharedRateLimiter creates a limiter that allows up to burst requests
// at once, replenishing at ratePerSec tokens per second.
func NewSharedRateLimiter(ratePerSec int) *SharedRateLimiter {
	if ratePerSec < 1 {
		ratePerSec = 1
	}
	burst := ratePerSec * 2
	if burst < 2 {
		burst = 2
	}
	sl := &SharedRateLimiter{
		tokens: make(chan struct{}, burst),
		done:   make(chan struct{}),
		ticker: time.NewTicker(time.Second / time.Duration(ratePerSec)),
	}
	// Seed with initial tokens.
	for i := 0; i < burst; i++ {
		sl.tokens <- struct{}{}
	}
	go sl.refill()
	return sl
}

func (sl *SharedRateLimiter) refill() {
	for {
		select {
		case <-sl.ticker.C:
			// Non-blocking push — discards if buffer is full.
			select {
			case sl.tokens <- struct{}{}:
			default:
			}
		case <-sl.done:
			return
		}
	}
}

// Acquire blocks until a token is available.
func (sl *SharedRateLimiter) Acquire() {
	<-sl.tokens
}

// Close stops the refill goroutine and releases resources.
func (sl *SharedRateLimiter) Close() {
	sl.once.Do(func() {
		close(sl.done)
		sl.ticker.Stop()
	})
}
