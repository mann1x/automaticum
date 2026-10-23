// Package store is kvd's in-memory key-value store.
package store

import (
	"sync"
	"time"
)

// Clock returns the current time. Tests pass a fake one.
type Clock func() time.Time

// Entry is one stored key.
type Entry struct {
	Key       string
	Value     string
	Version   int64
	UpdatedAt time.Time
}

// Store is safe for concurrent use.
type Store struct {
	mu    sync.RWMutex
	clock Clock
	data  map[string]Entry
}

// New returns an empty store. A nil clock means time.Now.
func New(clock Clock) *Store {
	if clock == nil {
		clock = time.Now
	}
	return &Store{clock: clock, data: make(map[string]Entry)}
}

// Get returns the entry for key.
func (s *Store) Get(key string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.data[key]
	return e, ok
}

// Put stores value under key. The version starts at 1 and grows by one on every write.
// It reports whether the key was created.
func (s *Store) Put(key, value string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.data[key]
	e := Entry{Key: key, Value: value, Version: old.Version + 1, UpdatedAt: s.clock()}
	s.data[key] = e
	return e, !exists
}

// Delete removes key and reports whether it was there.
func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	delete(s.data, key)
	return ok
}
