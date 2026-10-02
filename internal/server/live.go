package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"geotracker/internal/store"
)

// Hub fans events out to a user's open browser tabs over Server-Sent Events (ADR-004).
type Hub struct {
	mu   sync.Mutex
	subs map[int64]map[chan []byte]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[int64]map[chan []byte]struct{}{}} }

func (h *Hub) Subscribe(userID int64) (chan []byte, func()) {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan []byte]struct{}{}
	}
	h.subs[userID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[userID], ch)
		if len(h.subs[userID]) == 0 {
			delete(h.subs, userID)
		}
		h.mu.Unlock()
	}
}

// Publish never blocks: a slow tab just misses events and refetches on the next one.
func (h *Hub) Publish(userID int64, event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, b))
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[userID] {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Server) live(w http.ResponseWriter, r *http.Request, u *store.User) {
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // tell nginx not to buffer
	ch, cancel := s.hub.Subscribe(u.ID)
	defer cancel()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	fmt.Fprint(w, "retry: 5000\n\n")
	rc.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			w.Write(msg)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n") // keeps proxies from closing idle streams
		}
		if rc.Flush() != nil {
			return
		}
	}
}
