package monitor

import (
	"sync"
	"time"
)

// MonitorEvent represents an event emitted by monitor goroutines.
type MonitorEvent struct {
	Type       string      `json:"type"`                 // checking, found_queries, no_queries, slow_query, notified, error
	DatabaseID int64       `json:"database_id"`
	DBName     string      `json:"db_name"`
	Message    string      `json:"message"`
	Timestamp  time.Time   `json:"timestamp"`
	Data       interface{} `json:"data,omitempty"`
}

// EventBus is a simple in-process pub/sub for MonitorEvents.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan MonitorEvent]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[chan MonitorEvent]struct{})}
}

func (eb *EventBus) Subscribe() chan MonitorEvent {
	ch := make(chan MonitorEvent, 64)
	eb.mu.Lock()
	eb.subscribers[ch] = struct{}{}
	eb.mu.Unlock()
	return ch
}

func (eb *EventBus) Unsubscribe(ch chan MonitorEvent) {
	eb.mu.Lock()
	delete(eb.subscribers, ch)
	eb.mu.Unlock()
}

func (eb *EventBus) Publish(event MonitorEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for ch := range eb.subscribers {
		select {
		case ch <- event:
		default:
			// subscriber too slow, drop event
		}
	}
}
