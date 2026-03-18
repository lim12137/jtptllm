package session

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

type stubClient struct {
	mu        sync.Mutex
	next      int
	deletions []string
}

func (c *stubClient) CreateSession(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	return fmt.Sprintf("s%d", c.next), nil
}

func (c *stubClient) DeleteSession(ctx context.Context, sessionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletions = append(c.deletions, sessionID)
	return nil
}

func TestPoolUsesDifferentSessionsWhenIdle(t *testing.T) {
	cli := &stubClient{}
	m := NewPoolManager(cli, 600, 2)

	ctx := context.Background()
	l1, err := m.Acquire(ctx, "k", false)
	if err != nil {
		t.Fatalf("acquire1: %v", err)
	}
	l2, err := m.Acquire(ctx, "k", false)
	if err != nil {
		t.Fatalf("acquire2: %v", err)
	}
	if l1.SessionID() == l2.SessionID() {
		t.Fatalf("expected different sessions, got %q", l1.SessionID())
	}
	l1.Release(ctx, false)
	l2.Release(ctx, false)
}

func TestPoolQueuesWhenAllBusy(t *testing.T) {
	cli := &stubClient{}
	m := NewPoolManager(cli, 600, 2)

	ctx := context.Background()
	l1, err := m.Acquire(ctx, "k", false)
	if err != nil {
		t.Fatalf("acquire1: %v", err)
	}
	l2, err := m.Acquire(ctx, "k", false)
	if err != nil {
		t.Fatalf("acquire2: %v", err)
	}

	acquired := make(chan *Lease, 1)
	errCh := make(chan error, 1)
	go func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		l3, err := m.Acquire(ctx2, "k", false)
		if err != nil {
			errCh <- err
			return
		}
		acquired <- l3
	}()

	select {
	case <-acquired:
		t.Fatalf("expected acquire to block when all sessions are busy")
	case <-errCh:
		t.Fatalf("unexpected acquire error while waiting")
	case <-time.After(150 * time.Millisecond):
		// ok, still blocked
	}

	l1.Release(ctx, false)

	select {
	case l3 := <-acquired:
		l3.Release(ctx, false)
	case err := <-errCh:
		t.Fatalf("acquire failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for queued acquire")
	}

	l2.Release(ctx, false)
}

func TestPoolForceNewRotatesSession(t *testing.T) {
	cli := &stubClient{}
	m := NewPoolManager(cli, 600, 1)
	ctx := context.Background()

	l1, err := m.Acquire(ctx, "k", false)
	if err != nil {
		t.Fatalf("acquire1: %v", err)
	}
	id1 := l1.SessionID()
	l1.Release(ctx, false)

	l2, err := m.Acquire(ctx, "k", true)
	if err != nil {
		t.Fatalf("acquire2: %v", err)
	}
	id2 := l2.SessionID()
	l2.Release(ctx, false)

	if id1 == id2 {
		t.Fatalf("expected rotated session id, got same %q", id1)
	}
}
