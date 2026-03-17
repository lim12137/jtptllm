package config

import (
	"errors"
	"regexp"
	"strings"
)

const DefaultBaseURL = "http://10.54.102.36:80/xlm-gateway--vinrl/sfm-api-gateway/gateway/agent/api"

type Config struct {
	AppKey       string
	AgentCode    string
	AgentVersion string
	BaseURL      string
}

var lineRe = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*[:：]\s*(.*?)\s*$`)

func ParseApiTxt(data []byte) (Config, error) {
	cfg := Config{BaseURL: DefaultBaseURL}
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
		}
	}
	if cfg.AppKey == "" || cfg.AgentCode == "" {
		return Config{}, errors.New("api.txt 缺少必要字段: key/agentCode")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	return cfg, nil
}
