package session

import "testing"

func TestSessionReuse(t *testing.T) {
	m := NewManager(600)
	m.Set("k", "s1")
	if got := m.Get("k"); got != "s1" {
		t.Fatal("reuse")
	}
}
