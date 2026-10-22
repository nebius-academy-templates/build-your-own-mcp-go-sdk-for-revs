// Package scans keeps the results of expensive calls for a while, under
// random ids, per owner.
package scans

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// PerOwner is how many scans one owner holds; a newer scan drops the oldest.
const PerOwner = 20

// Store keeps values under random ids until they expire. It is safe for
// concurrent use.
type Store[T any] struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	byID    map[string]*entry[T]
	byOwner map[string][]string // ids, oldest first
}

type entry[T any] struct {
	owner   string
	expires time.Time
	value   T
}

// New returns a store whose scans live for ttl.
func New[T any](ttl time.Duration) *Store[T] {
	return &Store[T]{
		ttl:     ttl,
		now:     time.Now,
		byID:    map[string]*entry[T]{},
		byOwner: map[string][]string{},
	}
}

// Add keeps value for owner and returns its new id and the time it expires.
func (s *Store[T]) Add(owner string, value T) (string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.dropExpired(owner, now)
	id := s.newID()
	expires := now.Add(s.ttl)
	s.byID[id] = &entry[T]{owner: owner, expires: expires, value: value}
	ids := append(s.byOwner[owner], id)
	for len(ids) > PerOwner {
		delete(s.byID, ids[0])
		ids = ids[1:]
	}
	s.byOwner[owner] = ids
	return id, expires
}

// Get returns the value kept under id for owner. An unknown or expired id,
// and an id of another owner, give false.
func (s *Store[T]) Get(owner, id string) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var zero T
	e, found := s.byID[id]
	if !found || e.owner != owner {
		return zero, false
	}
	if !s.now().Before(e.expires) {
		s.dropExpired(owner, s.now())
		return zero, false
	}
	return e.value, true
}

// dropExpired forgets the owner's scans whose time is up.
func (s *Store[T]) dropExpired(owner string, now time.Time) {
	ids := s.byOwner[owner]
	kept := ids[:0]
	for _, id := range ids {
		if e := s.byID[id]; e != nil && now.Before(e.expires) {
			kept = append(kept, id)
		} else {
			delete(s.byID, id)
		}
	}
	s.byOwner[owner] = kept
}

// newID returns s_ and 16 random hex characters that no kept scan uses.
func (s *Store[T]) newID() string {
	for {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			panic(err)
		}
		id := "s_" + hex.EncodeToString(b[:])
		if _, taken := s.byID[id]; !taken {
			return id
		}
	}
}
