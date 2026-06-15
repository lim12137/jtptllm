package http

import (
	"log"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lim12137/jtptllm/internal/config"
	"github.com/lim12137/jtptllm/internal/gateway"
	"github.com/lim12137/jtptllm/internal/session"
)

type Options struct {
	Addr         string
	ApiTxt       string
	ApiMD        string
	AppKey       string
	AgentCode    string
	AgentVersion string
	BaseURL      string
	DefaultModel string
	SessionTTL   int
	AuthToken    string
}

func Run(opts Options) error {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = ":8022"
	}
	apiTxt := strings.TrimSpace(opts.ApiTxt)
	if apiTxt == "" {
		apiTxt = defaultAPITxtPath()
	}
	apiMD := strings.TrimSpace(opts.ApiMD)
	if apiMD == "" {
		apiMD = defaultAPIMDPath()
	}
	model := strings.TrimSpace(opts.DefaultModel)
	if model == "" {
		model = "agent"
	}
	if opts.SessionTTL < 0 {
		opts.SessionTTL = 0
	}
	if err := ensurePlaceholderAPITxt(apiTxt); err != nil {
		return err
	}

	cfg, err := config.Load(config.LoadOptions{
		ApiTxtPath:   apiTxt,
		MarkdownPath: apiMD,
		AppKey:       opts.AppKey,
		AgentCode:    opts.AgentCode,
		AgentVersion: opts.AgentVersion,
		BaseURL:      opts.BaseURL,
		AuthToken:    opts.AuthToken,
	})
	if err != nil {
		return err
	}

	client := gateway.NewClient(cfg, nil)
	var mgr *session.Manager
	if opts.SessionTTL > 0 {
		mgr = session.NewManager(opts.SessionTTL)
	}

	if cfg.AuthToken != "" {
		log.Printf("auth enabled for /v1/* (token masked: %s)", maskKey(cfg.AuthToken))
	}

	h := NewHandler(HandlerDeps{Client: client, Sessions: mgr, DefaultModel: model, AuthToken: cfg.AuthToken})
	srv := &stdhttp.Server{Addr: addr, Handler: h}
	log.Printf("proxy listening on %s (appKey masked: %s)", addr, maskKey(cfg.AppKey))
	return srv.ListenAndServe()
}

func defaultAPITxtPath() string {
	dir := executableDir()
	if dir != "" {
		return filepath.Join(dir, "api.txt")
	}
	return "api.txt"
}

func defaultAPIMDPath() string {
	dir := executableDir()
	if dir == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(dir, "api.md"),
		filepath.Join(dir, "大语言模型推理.md"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if dir == "." {
		return ""
	}
	return dir
}

func ensurePlaceholderAPITxt(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	content := "# auto-generated placeholder api.txt\n" +
		"# fill APP_KEY and agentCode before using agent gateway mode\n" +
		"# authToken 留空时自动使用内置默认值（默认开启鉴权）；填入则覆盖内置值。\n" +
		"key: \n" +
		"agentCode: \n" +
		"authToken: \n"
	return os.WriteFile(path, []byte(content), 0o600)
}

// EnsurePlaceholderAPIMD 在 exe 同目录生成一份开箱即用的 api.md。
// 内容包含完整的 compatible 模式接口地址；key 行留空，程序会自动使用内置 APP_KEY。
// 用户填入 key 后会覆盖内置值。
func EnsurePlaceholderAPIMD(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	// compatible 模式地址：把内置网关 BaseURL 末尾的 agent/api 替换为 compatible-mode。
	compatibleBase := strings.TrimSuffix(strings.TrimRight(config.DefaultBaseURL, "/"), "/agent/api") + "/compatible-mode"
	content := "# jtptllm compatible-mode 接口文档\n" +
		"#\n" +
		"# key 留空时自动使用内置 APP_KEY；填入则覆盖内置值。\n" +
		"key: \n" +
		"\n" +
		"## 接口调用地址\n" +
		"\n" +
		"```http\n" +
		"POST " + compatibleBase + "/v1/chat/completions\n" +
		"```\n"
	return os.WriteFile(path, []byte(content), 0o600)
}
