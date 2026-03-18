package openai

import (
	"strings"
	"testing"
)

func TestDeltaDiff(t *testing.T) {
	full := []string{"你", "你好", "你好！"}
	out := DiffDeltas(full)
	if strings.Join(out, "") != "你好！" {
		t.Fatal("delta")
	}
}
