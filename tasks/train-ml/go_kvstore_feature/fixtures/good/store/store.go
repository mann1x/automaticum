// Package store is kvd's in-memory key-value store.
package store

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// Clock returns the current time. Tests pass a fake one.
type Clock func() time.Time

var (
	// ErrVersionMismatch: an If-Match write found another version (or no key).
	ErrVersionMismatch = errors.New("version mismatch")
	// ErrKeyExists: a create-only write found the key.
	ErrKeyExists = errors.New("key exists")
)

// Entry is one stored key.
type Entry struct {
	Key       string
	Value     string
	Version   int64
	UpdatedAt time.Time
	ExpiresAt time.Time // zero: no expiry
}

func (e Entry) expired(now time.Time) bool {
	return !e.ExpiresAt.IsZero() && !now.Before(e.ExpiresAt)
}

// PutOptions are the conditions and TTL of a write.
type PutOptions struct {
	TTL          time.Duration // 0: no expiry
	IfMatch      bool          // write only if the live key has version MatchVersion
	MatchVersion int64
	IfNoneMatch  bool // write only if there is no live key
}

// Store is safe for concurrent use.
type Store struct {
	mu    sync.Mutex
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

// live returns the unexpired entry for key; it drops an expired one. s.mu must be held.
func (s *Store) live(key string, now time.Time) (Entry, bool) {
	e, ok := s.data[key]
	if ok && e.expired(now) {
		delete(s.data, key)
		return Entry{}, false
	}
	return e, ok
}

// Get returns the live entry for key.
func (s *Store) Get(key string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live(key, s.clock())
}

// Put stores value under key and reports whether the key was created.
func (s *Store) Put(key, value string, opt PutOptions) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	old, exists := s.live(key, now)
	if opt.IfNoneMatch && exists {
		return old, false, ErrKeyExists
	}
	if opt.IfMatch && (!exists || old.Version != opt.MatchVersion) {
		return old, false, ErrVersionMismatch
	}
	e := Entry{Key: key, Value: value, Version: old.Version + 1, UpdatedAt: now}
	if opt.TTL > 0 {
		e.ExpiresAt = now.Add(opt.TTL)
	}
	s.data[key] = e
	return e, !exists, nil
}

// Delete removes key and reports whether a live key was there.
func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.live(key, s.clock())
	delete(s.data, key)
	return ok
}

// List returns up to limit live keys with prefix that sort after `after`, and whether more follow.
func (s *Store) List(prefix, after string, limit int) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	keys := []string{}
	for k, e := range s.data {
		if strings.HasPrefix(k, prefix) && k > after && !e.expired(now) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		return keys[:limit], true
	}
	return keys, false
}
