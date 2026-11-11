package session

import (
	"context"
	"sync"
	"time"

	"github.com/hoangnecon/golabv2/internal/domain"
)

// EventBus broadcasts typed WorkspaceEvents to local SSE subscribers.
type EventBus struct {
	mu      sync.RWMutex
	nextID  int
	subs    map[int]chan domain.WorkspaceEvent
}

// NewEventBus creates an in-memory typed event broadcaster.
func NewEventBus() *EventBus {
	return &EventBus{
		subs: make(map[int]chan domain.WorkspaceEvent),
	}
}

// Publish sends evt to all active SSE subscribers without blocking slow readers.
func (b *EventBus) Publish(evt domain.WorkspaceEvent) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- evt:
		default:
			// Drop oldest event and push latest if subscriber buffer is full
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- evt:
			default:
			}
		}
	}
}

// Subscribe registers a listener channel that closes automatically when ctx is cancelled.
func (b *EventBus) Subscribe(ctx context.Context) <-chan domain.WorkspaceEvent {
	ch := make(chan domain.WorkspaceEvent, 128)
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.subs[id] = ch
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		if existing, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(existing)
		}
		b.mu.Unlock()
	}()
	return ch
}
