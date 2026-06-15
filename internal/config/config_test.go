package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseApiTxt(t *testing.T) {
	txt := "key： abc\nagentCode： code\nagentVersion： 123\n"
	cfg, err := ParseApiTxt([]byte(txt))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppKey != "abc" {
		t.Fatal("app key")
	}
	if cfg.AgentCode != "code" {
		t.Fatal("agentCode")
	}
	if cfg.AgentVersion != "123" {
		t.Fatal("agentVersion")
	}
}

func TestLoadSupportsDirectArgsWithoutApiTxt(t *testing.T) {
	cfg, err := Load(LoadOptions{
		ApiTxtPath:   "missing-api.txt",
		AppKey:       "abc",
		AgentCode:    "code",
		AgentVersion: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppKey != "abc" || cfg.AgentCode != "code" || cfg.AgentVersion != "v1" {
		t.Fatal("direct args not loaded")
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Fatal("default base url")
	}
}

func TestLoadUsesBuiltinAppKeyByDefault(t *testing.T) {
	cfg, err := Load(LoadOptions{
		ApiTxtPath: "missing-api.txt",
		AgentCode:  "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppKey != DefaultAppKey {
		t.Fatalf("app key: %s", cfg.AppKey)
	}
}

func TestLoadMergesFileAndDirectArgs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.txt")
	if err := os.WriteFile(path, []byte("key: from-file\nagentCode: file-code\nagentVersion: file-v\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		ApiTxtPath: path,
		AppKey:     "from-arg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppKey != "from-arg" {
		t.Fatal("app key override")
	}
	if cfg.AgentCode != "file-code" || cfg.AgentVersion != "file-v" {
		t.Fatal("file fallback")
	}
}

func TestLoadAllowsIncompleteApiTxtWhenDirectArgsProvided(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.txt")
	if err := os.WriteFile(path, []byte("baseUrl: http://example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		ApiTxtPath: path,
		AppKey:     "from-arg",
		AgentCode:  "arg-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://example.com" {
		t.Fatal("base url from file")
	}
	if cfg.AppKey != "from-arg" || cfg.AgentCode != "arg-code" {
		t.Fatal("direct args missing")
	}
}

func TestLoadSupportsMarkdownCompatibleConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.md")
	content := []byte("key: from-md\n\n## 接口调用地址\n\n```http\nPOST http://example.com/gateway/compatible-mode/v1/chat/completions\n```")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		MarkdownPath: path,
		AppKey:       "from-arg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != BackendModeCompatible {
		t.Fatalf("mode: %s", cfg.Mode)
	}
	if cfg.ChatURL != "http://example.com/gateway/compatible-mode/v1/chat/completions" {
		t.Fatalf("chat url: %s", cfg.ChatURL)
	}
	if cfg.AppKey != "from-md" {
		t.Fatalf("app key: %s", cfg.AppKey)
	}
}

// TestLoadMarkdownEmptyKeyKeepsBuiltin 验证：api.md 里 key 行存在但值为空时，
// 不覆盖内置 APP_KEY（开箱即用场景）。
func TestLoadMarkdownEmptyKeyKeepsBuiltin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.md")
	// key: 后面是空的 —— 模拟自动生成的占位 api.md
	content := []byte("key: \n\n## 接口调用地址\n\n```http\nPOST http://example.com/gateway/compatible-mode/v1/chat/completions\n```")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{MarkdownPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != BackendModeCompatible {
		t.Fatalf("mode: %s", cfg.Mode)
	}
	if cfg.AppKey != DefaultAppKey {
		t.Fatalf("empty key should fall back to builtin, got %q", cfg.AppKey)
	}
}

func TestParseApiTxtReadsAuthToken(t *testing.T) {
	txt := "key: abc\nagentCode: code\nauthToken: my-secret\n"
	cfg, err := ParseApiTxt([]byte(txt))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthToken != "my-secret" {
		t.Fatalf("expected auth token my-secret, got %q", cfg.AuthToken)
	}
}

func TestParseApiTxtReadsAuthTokenAliases(t *testing.T) {
	// 多个常见别名都应被识别。
	for _, key := range []string{"auth_token", "access_token", "bearer", "auth"} {
		txt := "key: abc\nagentCode: code\n" + key + ": tok-" + key + "\n"
		cfg, err := ParseApiTxt([]byte(txt))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AuthToken != "tok-"+key {
			t.Fatalf("alias %q: expected token %q, got %q", key, "tok-"+key, cfg.AuthToken)
		}
	}
}

func TestLoadAuthTokenFromArgOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.txt")
	if err := os.WriteFile(path, []byte("key: abc\nagentCode: code\nauthToken: from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{ApiTxtPath: path, AuthToken: "from-arg"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthToken != "from-arg" {
		t.Fatalf("expected arg to override file, got %q", cfg.AuthToken)
	}
}

func TestLoadAuthTokenFallsBackToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.txt")
	if err := os.WriteFile(path, []byte("key: abc\nagentCode: code\nauthToken: from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{ApiTxtPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthToken != "from-file" {
		t.Fatalf("expected file fallback, got %q", cfg.AuthToken)
	}
}

func TestLoadAuthTokenDefaultsBuiltIn(t *testing.T) {
	// 不传 token、文件里也没有：应使用内置默认值（默认开启鉴权）。
	cfg, err := Load(LoadOptions{
		ApiTxtPath: "missing-api.txt",
		AppKey:     "abc",
		AgentCode:  "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthToken != DefaultAuthToken {
		t.Fatalf("expected built-in default auth token %q, got %q", DefaultAuthToken, cfg.AuthToken)
	}
}
