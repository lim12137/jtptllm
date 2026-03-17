package session

import (
	"sync"
	"time"
)

type entry struct {
	id       string
	lastSeen time.Time
}

type Manager struct {
	mu   sync.Mutex
	ttl  time.Duration
	data map[string]entry
}

func NewManager(ttlSeconds int) *Manager {
	return &Manager{
		ttl:  time.Duration(ttlSeconds) * time.Second,
		data: make(map[string]entry),
	}
}

func (m *Manager) Get(key string) string {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanup(now)
	if ent, ok := m.data[key]; ok {
		if m.expired(now, ent) {
			delete(m.data, key)
			return ""
		}
		return ent.id
	}
	return ""
}

func (m *Manager) Set(key, id string) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = entry{id: id, lastSeen: now}
	m.cleanup(now)
}

func (m *Manager) Invalidate(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

func (m *Manager) expired(now time.Time, ent entry) bool {
	if m.ttl <= 0 {
		return false
	}
	return now.Sub(ent.lastSeen) > m.ttl
}

func (m *Manager) cleanup(now time.Time) {
	if m.ttl <= 0 {
		return
	}
	for k, ent := range m.data {
		if now.Sub(ent.lastSeen) > m.ttl {
			delete(m.data, k)
		}
	}
}
