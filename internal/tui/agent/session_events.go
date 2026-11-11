package agent

import (
	"context"

	"github.com/keshon/gektor/internal/events"
)

// sessionEventQueue delivers to the screen's existing event loop. The screen
// owns the queue and cancellation; Handle neither closes it nor starts work.
type sessionEventQueue struct {
	ctx   context.Context
	queue chan<- events.Event
}

func (q sessionEventQueue) Handle(event events.Event) {
	select {
	case q.queue <- event:
	case <-q.ctx.Done():
	}
}
