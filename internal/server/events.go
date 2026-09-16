package server

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DefaultKeepAlive is how often an idle events stream gets a comment line, so
// proxies and browsers do not time the connection out.
const DefaultKeepAlive = 20 * time.Second

// broadcaster fans a "something changed" signal out to every subscriber
// without ever blocking the publisher. Each subscriber has a one-slot buffer:
// a publish that finds the slot full is dropped, because the pending signal
// already tells that client to refetch. A slow client therefore costs the
// others nothing and only ever has one change queued.
type broadcaster struct {
	mu       sync.Mutex
	subs     map[chan struct{}]struct{}
	done     chan struct{}
	isClosed bool
}

func newBroadcaster() *broadcaster {
	return &broadcaster{
		subs: make(map[chan struct{}]struct{}),
		done: make(chan struct{}),
	}
}

// subscribe registers a subscriber. The caller must call unsubscribe when it
// stops listening. After close, the returned channel never fires; callers
// should also watch closed().
func (b *broadcaster) subscribe() (ch <-chan struct{}, unsubscribe func()) {
	c := make(chan struct{}, 1)
	b.mu.Lock()
	if !b.isClosed {
		b.subs[c] = struct{}{}
	}
	b.mu.Unlock()
	return c, func() {
		b.mu.Lock()
		delete(b.subs, c)
		b.mu.Unlock()
	}
}

// publish signals every subscriber that has no signal pending.
func (b *broadcaster) publish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.subs {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

// close tells every subscriber to stop, via closed(). It is safe to call more
// than once.
func (b *broadcaster) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.isClosed {
		b.isClosed = true
		close(b.done)
	}
}

func (b *broadcaster) closed() <-chan struct{} { return b.done }

func (b *broadcaster) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// handleEvents streams Server-Sent Events. It starts with a ": connected"
// comment, sends
//
//	event: changed
//	data: {}
//
// whenever the database changes, and a ": keep-alive" comment after each idle
// keep-alive interval. It returns when the client goes away or the server
// shuts down.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)

	changed, unsubscribe := s.events.subscribe()
	defer unsubscribe()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(msg string) bool {
		if _, err := io.WriteString(w, msg); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send(": connected\n\n") {
		return
	}

	keepAlive := time.NewTicker(s.keepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.events.closed():
			return
		case <-changed:
			if !send(fmt.Sprintf("event: %s\ndata: {}\n\n", eventChanged)) {
				return
			}
			keepAlive.Reset(s.keepAlive)
		case <-keepAlive.C:
			if !send(": keep-alive\n\n") {
				return
			}
		}
	}
}

const eventChanged = "changed"
