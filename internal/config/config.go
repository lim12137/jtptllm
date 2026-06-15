package config

import (
	"errors"
	"os"
	"regexp"
	"strings"
)

const DefaultBaseURL = "http://10.54.102.36:80/xlm-gateway--vinrl/sfm-api-gateway/gateway/agent/api"
const DefaultAppKey = "FRQ6udhCtWPb4VpIWnA3WLBwZ3K84qKO"
const DefaultAuthToken = "key-hopemyl"
const (
	BackendModeAgent      = "agent"
	BackendModeCompatible = "compatible"
)

type Config struct {
	AppKey       string
	AgentCode    string
	AgentVersion string
	BaseURL      string
	Mode         string
	ChatURL      string
	AuthToken    string
}

type LoadOptions struct {
	ApiTxtPath   string
	MarkdownPath string
	AppKey       string
	AgentCode    string
	AgentVersion string
	BaseURL      string
	AuthToken    string
}

var lineRe = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*[:：]\s*(.*?)\s*$`)
var postURLRe = regexp.MustCompile(`(?im)\bPOST\s+(https?://[^\s` + "`" + `]+)`)

func parseApiTxtFields(data []byte) Config {
	cfg := Config{BaseURL: DefaultBaseURL, Mode: BackendModeAgent}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		m := lineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := strings.TrimSpace(m[1])
		val := strings.TrimSpace(m[2])
		switch key {
		case "key", "APP_KEY", "app_key":
			cfg.AppKey = val
		case "agentCode", "agent_code":
			cfg.AgentCode = val
		case "agentVersion", "agent_version":
			cfg.AgentVersion = val
		case "baseUrl", "base_url":
			if val != "" {
				cfg.BaseURL = val
			}
		case "authToken", "auth_token", "access_token", "bearer", "auth":
			if val != "" {
				cfg.AuthToken = val
			}
		}
	}
	return cfg
}

func parseMarkdownFields(data []byte) (Config, error) {
	text := string(data)
	m := postURLRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return Config{}, errors.New("md 文档中未找到 POST 接口地址")
	}
	url := strings.TrimSpace(m[1])
	cfg := Config{
		Mode:    BackendModeCompatible,
		ChatURL: url,
	}
	if idx := strings.LastIndex(url, "/v1/chat/completions"); idx > 0 {
		cfg.BaseURL = url[:idx]
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		match := lineRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		key := strings.TrimSpace(match[1])
		val := strings.TrimSpace(match[2])
		switch key {
		case "key", "APP_KEY", "app_key":
			if val != "" {
				cfg.AppKey = val
			}
		}
	}
	return cfg, nil
}

func validateParsedConfig(cfg Config, source string) (Config, error) {
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = BackendModeAgent
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}

	if cfg.Mode == BackendModeCompatible {
		if cfg.AppKey == "" {
			return Config{}, errors.New("缺少必要字段: 请提供 key/app-key（可通过 md 文档或命令行参数传入）")
		}
		if strings.TrimSpace(cfg.ChatURL) == "" {
			return Config{}, errors.New("缺少必要字段: compatible 模式需要 chat completions 地址（可通过 md 文档传入）")
		}
		return cfg, nil
	}

	if source == "api.txt" && (cfg.AppKey == "" || cfg.AgentCode == "") {
		return Config{}, errors.New("api.txt 缺少必要字段: key/agentCode")
	}
	if cfg.AppKey == "" || cfg.AgentCode == "" {
		return Config{}, errors.New("缺少必要字段: 请提供 key/app-key 和 agentCode（可通过 api.txt 或命令行参数传入）")
	}
	return cfg, nil
}

func LoadFromFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return ParseApiTxt(data)
}

func Load(opts LoadOptions) (Config, error) {
	cfg := Config{
		AppKey:       strings.TrimSpace(opts.AppKey),
		AgentCode:    strings.TrimSpace(opts.AgentCode),
		AgentVersion: strings.TrimSpace(opts.AgentVersion),
		BaseURL:      strings.TrimSpace(opts.BaseURL),
		AuthToken:    strings.TrimSpace(opts.AuthToken),
		Mode:         BackendModeAgent,
	}
	if cfg.AppKey == "" {
		cfg.AppKey = DefaultAppKey
	}

	apiTxtPath := strings.TrimSpace(opts.ApiTxtPath)
	if apiTxtPath != "" {
		data, err := os.ReadFile(apiTxtPath)
		if err != nil {
			if !(errors.Is(err, os.ErrNotExist) && cfg.AppKey != "" && cfg.AgentCode != "") {
				return Config{}, err
			}
		} else {
			fileCfg := parseApiTxtFields(data)
			if cfg.AppKey == "" {
				cfg.AppKey = fileCfg.AppKey
			}
			if cfg.AgentCode == "" {
				cfg.AgentCode = fileCfg.AgentCode
			}
			if cfg.AgentVersion == "" {
				cfg.AgentVersion = fileCfg.AgentVersion
			}
			if cfg.BaseURL == "" {
				cfg.BaseURL = fileCfg.BaseURL
			}
			if cfg.AuthToken == "" {
				cfg.AuthToken = fileCfg.AuthToken
			}
		}
	}

	markdownPath := strings.TrimSpace(opts.MarkdownPath)
	if markdownPath != "" {
		data, err := os.ReadFile(markdownPath)
		if err != nil {
			return Config{}, err
		}
		mdCfg, err := parseMarkdownFields(data)
		if err != nil {
			return Config{}, err
		}
		cfg.Mode = mdCfg.Mode
		cfg.ChatURL = mdCfg.ChatURL
		cfg.BaseURL = mdCfg.BaseURL
		if mdCfg.AppKey != "" {
			cfg.AppKey = mdCfg.AppKey
		}
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	// AuthToken 留空时使用内置默认值，保证 /v1/* 默认开启鉴权。
	// 命令行 -auth-token 与 api.txt 的 authToken 都可覆盖；显式填空串仍可关闭鉴权。
	if cfg.AuthToken == "" {
		cfg.AuthToken = DefaultAuthToken
	}
	return validateParsedConfig(cfg, "")
}

func ParseApiTxt(data []byte) (Config, error) {
	return validateParsedConfig(parseApiTxtFields(data), "api.txt")
}
