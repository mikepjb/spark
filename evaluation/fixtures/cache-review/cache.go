package cache

import (
	"sync"
	"time"
)

type entry struct {
	value     []byte
	expiresAt time.Time
}

// Store is a small in-memory cache for response payloads.
type Store struct {
	mu      sync.RWMutex
	entries map[string]entry
}

func New() *Store {
	return &Store{entries: make(map[string]entry)}
}

func (s *Store) Set(key string, value []byte, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = entry{value: value, expiresAt: time.Now().Add(ttl)}
}

func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	item, ok := s.entries[key]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return item.value, true
}

func (s *Store) DeleteExpired(now time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, item := range s.entries {
		if !now.Before(item.expiresAt) {
			delete(s.entries, key)
		}
	}
}
