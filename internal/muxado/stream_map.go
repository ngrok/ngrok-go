package muxado

import (
	"sync"

	"github.com/ngrok/libngrok-go/internal/muxado/frame"
)

const (
	initMapCapacity = 128 // not too much extra memory wasted to avoid allocations
)

// streamMap is a map of stream ids -> streams guarded by a read/write lock
type streamMap struct {
	sync.RWMutex
	table map[frame.StreamId]streamPrivate
}

func (m *streamMap) Get(id frame.StreamId) (s streamPrivate, ok bool) {
	m.RLock()
	s, ok = m.table[id]
	m.RUnlock()
	return
}

func (m *streamMap) Set(id frame.StreamId, str streamPrivate) {
	m.Lock()
	m.table[id] = str
	m.Unlock()
}

func (m *streamMap) Delete(id frame.StreamId) {
	m.Lock()
	delete(m.table, id)
	m.Unlock()
}

func (m *streamMap) Each(fn func(frame.StreamId, streamPrivate)) {
	m.RLock()
	streams := make(map[frame.StreamId]streamPrivate, len(m.table))
	for k, v := range m.table {
		streams[k] = v
	}
	m.RUnlock()

	for id, str := range streams {
		fn(id, str)
	}
}

func newStreamMap() *streamMap {
	return &streamMap{table: make(map[frame.StreamId]streamPrivate, initMapCapacity)}
}
