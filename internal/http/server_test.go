package http

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lim12137/jtptllm/internal/config"
)

func TestEnsurePlaceholderAPITxt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.txt")
	if err := ensurePlaceholderAPITxt(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := "# auto-generated placeholder api.txt\n" +
		"# fill APP_KEY and agentCode before using agent gateway mode\n" +
		"# authToken 留空时自动使用内置默认值（默认开启鉴权）；填入则覆盖内置值。\n" +
		"key: \n" +
		"agentCode: \n" +
		"authToken: \n"
	if string(data) != expected {
		t.Fatalf("placeholder content: %q", string(data))
	}
}

func TestDefaultAPIMDPathReturnsEmptyWhenNoFile(t *testing.T) {
	if path := defaultAPIMDPath(); path != "" {
		t.Fatalf("expected empty path, got %s", path)
	}
}

// TestEnsurePlaceholderAPIMD 验证自动生成的 api.md：
//   - 包含 compatible 模式的 /v1/chat/completions 地址
//   - key 行为空（开箱即用走内置 APP_KEY）
//   - 已存在则不覆盖
func TestEnsurePlaceholderAPIMD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.md")

	if err := EnsurePlaceholderAPIMD(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// 必须包含 compatible 模式的 chat completions 地址。
	if !strings.Contains(content, "/compatible-mode/v1/chat/completions") {
		t.Fatalf("api.md missing compatible-mode URL:\n%s", content)
	}
	// key 行必须存在且为空（key: 后无值）。
	if !strings.Contains(content, "key: \n") {
		t.Fatalf("api.md should have empty key line:\n%s", content)
	}

	// 用 config.Load 解析生成的内容，验证端到端：key 为空 → 走内置。
	cfg, err := config.Load(config.LoadOptions{MarkdownPath: path})
	if err != nil {
		t.Fatalf("config.Load failed on generated api.md: %v", err)
	}
	if cfg.Mode != config.BackendModeCompatible {
		t.Fatalf("mode: %s", cfg.Mode)
	}
	if cfg.AppKey != config.DefaultAppKey {
		t.Fatalf("empty key should use builtin, got %q", cfg.AppKey)
	}
	if !strings.HasSuffix(cfg.ChatURL, "/compatible-mode/v1/chat/completions") {
		t.Fatalf("chat url: %s", cfg.ChatURL)
	}
}

// TestEnsurePlaceholderAPIMDDoesNotOverwrite 验证已存在的 api.md 不会被覆盖。
func TestEnsurePlaceholderAPIMDDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.md")
	original := []byte("# my custom api.md\nkey: my-key\nPOST http://x.com/v1/chat/completions\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePlaceholderAPIMD(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(original) {
		t.Fatalf("existing api.md was overwritten")
	}
}
