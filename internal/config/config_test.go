package config

import "testing"

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