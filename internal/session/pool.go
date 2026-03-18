package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Client is the minimal gateway API required for session pooling.
// It is implemented by internal/gateway.Client and by HTTP test stubs.
type Client interface {
	CreateSession(ctx context.Context) (string, error)
	DeleteSession(ctx context.Context, sessionID string) error
}

// Lease represents an acquired upstream session. Call Release when the request is finished.
type Lease struct {
	key string
	idx int
	id  string
	kp  *keyPool
}

func (l *Lease) SessionID() string { return l.id }
func (l *Lease) Key() string       { return l.key }

// Release returns the session back to the pool.
// If closeAfter is true, the session is rotated (new session created, old one deleted best-effort).
func (l *Lease) Release(ctx context.Context, closeAfter bool) {
	if l == nil || l.kp == nil {
		return
	}
	l.kp.release(ctx, l.idx, closeAfter)
}

type pooledSession struct {
	id       string
	lastUsed time.Time
}

type keyPool struct {
	client Client
	ttl    time.Duration
	size   int

	initOnce sync.Once
	initErr  error

	mu       sync.Mutex
	sessions []pooledSession // length == size
	avail    chan int        // indices of idle sessions
}

func (kp *keyPool) init(ctx context.Context) error {
	kp.initOnce.Do(func() {
		kp.mu.Lock()
		kp.sessions = make([]pooledSession, kp.size)
		kp.avail = make(chan int, kp.size)
		kp.mu.Unlock()

		now := time.Now()
		var lastErr error
		created := 0
		for i := 0; i < kp.size; i++ {
			id, err := kp.client.CreateSession(ctx)
			if err != nil {
				lastErr = err
				continue
			}
			kp.mu.Lock()
			kp.sessions[i] = pooledSession{id: id, lastUsed: now}
			kp.mu.Unlock()
			kp.avail <- i
			created++
		}

		if created == 0 {
			if lastErr == nil {
				lastErr = errors.New("createSession failed")
			}
			kp.initErr = lastErr
		}
	})
	return kp.initErr
}

func (kp *keyPool) tryProvisionOne(ctx context.Context) bool {
	// Find an empty slot and fill it with a new session.
	kp.mu.Lock()
	slot := -1
	for i := 0; i < len(kp.sessions); i++ {
		if kp.sessions[i].id == "" {
			slot = i
			// Mark as in-progress to avoid duplicate provisioning.
			kp.sessions[i].id = "__creating__"
			break
		}
	}
	kp.mu.Unlock()
	if slot < 0 {
		return false
	}

	id, err := kp.client.CreateSession(ctx)
	if err != nil {
		// Undo marker so a future request can retry.
		kp.mu.Lock()
		kp.sessions[slot].id = ""
		kp.mu.Unlock()
		return false
	}

	kp.mu.Lock()
	kp.sessions[slot] = pooledSession{id: id, lastUsed: time.Now()}
	kp.mu.Unlock()
	kp.avail <- slot
	return true
}

func (kp *keyPool) rotateSession(ctx context.Context, idx int) string {
	// Create first to keep capacity, then delete old best-effort.
	newID, err := kp.client.CreateSession(ctx)
	if err != nil || newID == "" {
		// Best-effort: keep old session if we couldn't create a replacement.
		kp.mu.Lock()
		oldID := kp.sessions[idx].id
		kp.mu.Unlock()
		return oldID
	}

	kp.mu.Lock()
	oldID := kp.sessions[idx].id
	kp.sessions[idx] = pooledSession{id: newID, lastUsed: time.Now()}
	kp.mu.Unlock()

	if oldID != "" && oldID != "__creating__" {
		_ = kp.client.DeleteSession(ctx, oldID)
	}
	return newID
}

func (kp *keyPool) acquire(ctx context.Context, forceNew bool) (int, string, error) {
	if err := kp.init(ctx); err != nil {
		return -1, "", err
	}

	// If no idle sessions right now, try provisioning missing slots before queueing.
	for {
		select {
		case idx := <-kp.avail:
			kp.mu.Lock()
			ent := kp.sessions[idx]
			kp.mu.Unlock()
			if ent.id == "" || ent.id == "__creating__" {
				// Should not happen (we only enqueue valid slots), but be defensive.
				_ = kp.tryProvisionOne(ctx)
				continue
			}
			expired := false
			if kp.ttl > 0 && !ent.lastUsed.IsZero() && time.Since(ent.lastUsed) > kp.ttl {
				expired = true
			}
			if forceNew || expired {
				ent.id = kp.rotateSession(ctx, idx)
			}
			return idx, ent.id, nil
		default:
			// No idle sessions immediately available.
			if kp.tryProvisionOne(ctx) {
				continue
			}
			// Pool is full (or provisioning failed). Queue.
			select {
			case idx := <-kp.avail:
				kp.mu.Lock()
				ent := kp.sessions[idx]
				kp.mu.Unlock()
				if ent.id == "" || ent.id == "__creating__" {
					continue
				}
				expired := false
				if kp.ttl > 0 && !ent.lastUsed.IsZero() && time.Since(ent.lastUsed) > kp.ttl {
					expired = true
				}
				if forceNew || expired {
					ent.id = kp.rotateSession(ctx, idx)
				}
				return idx, ent.id, nil
			case <-ctx.Done():
				return -1, "", ctx.Err()
			}
		}
	}
}

func (kp *keyPool) release(ctx context.Context, idx int, closeAfter bool) {
	if closeAfter {
		_ = kp.rotateSession(ctx, idx)
	} else {
		kp.mu.Lock()
		if idx >= 0 && idx < len(kp.sessions) && kp.sessions[idx].id != "" && kp.sessions[idx].id != "__creating__" {
			kp.sessions[idx].lastUsed = time.Now()
		}
		kp.mu.Unlock()
	}
	// Return token; channel has capacity=size and we removed one on acquire.
	select {
	case kp.avail <- idx:
	default:
		// Should never happen; avoid blocking if called twice.
	}
}

// PoolManager manages a pool of upstream sessions per session key.
// For each key, it maintains up to `size` sessions and hands out idle sessions in FIFO order.
// If all sessions are busy, Acquire blocks (queues) until one becomes available.
type PoolManager struct {
	client Client
	ttl    time.Duration
	size   int

	mu    sync.Mutex
	pools map[string]*keyPool
}

func NewPoolManager(client Client, ttlSeconds int, size int) *PoolManager {
	if size <= 0 {
		size = 1
	}
	return &PoolManager{
		client: client,
		ttl:    time.Duration(ttlSeconds) * time.Second,
		size:   size,
		pools:  make(map[string]*keyPool),
	}
}

func (m *PoolManager) getPool(key string) *keyPool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if kp, ok := m.pools[key]; ok {
		return kp
	}
	kp := &keyPool{
		client: m.client,
		ttl:    m.ttl,
		size:   m.size,
	}
	m.pools[key] = kp
	return kp
}

// Acquire returns an upstream session lease for the given key.
// It will provision sessions up to the configured size, then queue when all are busy.
func (m *PoolManager) Acquire(ctx context.Context, key string, forceNew bool) (*Lease, error) {
	if m == nil || m.client == nil {
		return nil, errors.New("session pool not configured")
	}
	kp := m.getPool(key)
	idx, id, err := kp.acquire(ctx, forceNew)
	if err != nil {
		return nil, err
	}
	return &Lease{key: key, idx: idx, id: id, kp: kp}, nil
}
