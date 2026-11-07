// Package guard enforces resource limits (element counts and context
// cancellation) while parsing and laying out diagrams.
package guard

import (
	"context"
	"errors"
	"fmt"
)

// ErrTooLarge reports that an input exceeded one of the configured limits.
var ErrTooLarge = errors.New("too large")

// checkInterval controls how often Tick consults the context. Some loop
// bodies cost milliseconds (date arithmetic, layout of large levels), so the
// interval is small; ctx.Err is cheap compared with any of them.
const checkInterval = 16

// Guard carries the context and limits for one render call. It is not safe
// for concurrent use.
type Guard struct {
	ctx      context.Context
	maxNodes int
	ticks    int
}

// New returns a Guard. A nil ctx is treated as context.Background.
func New(ctx context.Context, maxNodes int) *Guard {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Guard{ctx: ctx, maxNodes: maxNodes}
}

// Tick should be called once per loop iteration whose count depends on the
// input. It returns the context error every checkInterval calls.
func (g *Guard) Tick() error {
	g.ticks++
	if g.ticks%checkInterval == 0 {
		return g.ctx.Err()
	}
	return nil
}

// Err returns the context error immediately.
func (g *Guard) Err() error { return g.ctx.Err() }

// Count returns ErrTooLarge when n exceeds the element limit.
func (g *Guard) Count(what string, n int) error {
	if g.maxNodes > 0 && n > g.maxNodes {
		return fmt.Errorf("%w: %d %s exceed limit %d", ErrTooLarge, n, what, g.maxNodes)
	}
	return nil
}

// Limit returns ErrTooLarge when n exceeds an explicit limit. A negative n
// (a count that overflowed in the caller) exceeds it too: callers return
// Limit's result as the error of a failed check, and a nil there let a
// packet field ending at MaxInt through with a nil diagram.
func Limit(what string, n, limit int) error {
	if n > limit || n < 0 {
		return fmt.Errorf("%w: %d %s exceed limit %d", ErrTooLarge, n, what, limit)
	}
	return nil
}
