package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	server "github.com/lim12137/jtptllm/internal/http"
	"github.com/lim12137/jtptllm/internal/tray"
)

func main() {
	var apiTxt string
	var apiMD string
	var appKey string
	var agentCode string
	var agentVersion string
	var baseURL string
	var defaultModel string
	var host string
	var port int
	var sessionTTL int
	var authToken string
	var trayMode bool

	flag.StringVar(&apiTxt, "api-txt", "api.txt", "Path to api.txt (key/agentCode/agentVersion)")
	flag.StringVar(&apiMD, "api-md", "", "Path to markdown API document for compatible chat completions access")
	flag.StringVar(&appKey, "app-key", "", "App key used for direct invocation without api.txt")
	flag.StringVar(&agentCode, "agent-code", "", "Agent code used for direct invocation without api.txt")
	flag.StringVar(&agentVersion, "agent-version", "", "Agent version used for direct invocation without api.txt")
	flag.StringVar(&baseURL, "base-url", "", "Override gateway base url")
	flag.StringVar(&defaultModel, "default-model", "DeepSeek-V3.2", "Default model name")
	flag.StringVar(&host, "host", "0.0.0.0", "Listen host")
	flag.IntVar(&port, "port", 8022, "Listen port")
	flag.IntVar(&sessionTTL, "session-ttl", 600, "Session idle TTL seconds (0=disable reuse)")
	flag.StringVar(&authToken, "auth-token", "", "Bearer token required to access /v1/* endpoints; empty disables auth (read from api.txt if unset)")
	flag.BoolVar(&trayMode, "tray", false, "Minimize to system tray (Windows desktop only)")
	flag.Parse()

	addr := net.JoinHostPort(host, strconv.Itoa(port))

	if trayMode {
		runTray(opts{
			addr: addr, apiTxt: apiTxt, apiMD: apiMD,
			appKey: appKey, agentCode: agentCode, agentVersion: agentVersion,
			baseURL: baseURL, defaultModel: defaultModel,
			sessionTTL: sessionTTL, authToken: authToken,
		})
		return
	}

	// 控制台模式：行为与历史版本完全一致。
	// 若是双击 exe（explorer 启动）而非命令行：
	//   1. 自动生成托盘快捷方式「代理-托盘.lnk」
	//   2. 自动生成开箱即用的 api.md（compatible 模式地址写好，key 留空走内置）
	if tray.IsLaunchedByExplorer() {
		ensureTrayShortcut()
		ensurePlaceholderAPIMD()
	}

	if err := server.Run(server.Options{
		Addr:         addr,
		ApiTxt:       apiTxt,
		ApiMD:        apiMD,
		AppKey:       appKey,
		AgentCode:    agentCode,
		AgentVersion: agentVersion,
		BaseURL:      baseURL,
		DefaultModel: defaultModel,
		SessionTTL:   sessionTTL,
		AuthToken:    authToken,
	}); err != nil {
		log.Fatal(err)
	}
}

type opts struct {
	addr         string
	apiTxt       string
	apiMD        string
	appKey       string
	agentCode    string
	agentVersion string
	baseURL      string
	defaultModel string
	sessionTTL   int
	authToken    string
}

// runTray 在后台 goroutine 启动 HTTP 服务，主 goroutine 进入托盘消息循环。
// 用户从托盘菜单「退出」时，Shutdown 关闭 HTTP 服务，托盘循环返回，进程退出。
func runTray(o opts) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srvErr := make(chan error, 1)
	go func() {
		srvErr <- server.Run(server.Options{
			Addr:         o.addr,
			ApiTxt:       o.apiTxt,
			ApiMD:        o.apiMD,
			AppKey:       o.appKey,
			AgentCode:    o.agentCode,
			AgentVersion: o.agentVersion,
			BaseURL:      o.baseURL,
			DefaultModel: o.defaultModel,
			SessionTTL:   o.sessionTTL,
			AuthToken:    o.authToken,
		})
	}()

	// 允许 Ctrl+C 也能优雅退出（托盘模式下控制台虽然隐藏，但进程仍可接收信号）。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	tray.Run(tray.Options{
		Title:    "jtptllm-proxy",
		Addr:     o.addr,
		Shutdown: cancel,
		OnReady: func() {
			log.Printf("已最小化到托盘。监听 %s，右键托盘图标可显示控制台或退出。", o.addr)
		},
	})

	// 托盘退出后，若 HTTP 服务仍在运行，等待它收尾。
	select {
	case <-ctx.Done():
	case err := <-srvErr:
		if err != nil {
			log.Printf("server stopped: %v", err)
		}
	}
}

// ensureTrayShortcut 在 exe 同目录生成「代理-托盘.lnk」快捷方式（带 --tray）。
// 失败仅记录日志，不影响控制台模式启动。
func ensureTrayShortcut() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	lnk := filepath.Join(dir, "代理-托盘.lnk")
	if _, err := os.Stat(lnk); err == nil {
		return // 已存在
	}
	if err := tray.EnsureShortcut(exe, lnk, "--tray"); err != nil {
		log.Printf("生成托盘快捷方式失败（可忽略）: %v", err)
		return
	}
	log.Printf("已在 %s 生成托盘快捷方式「代理-托盘.lnk」，双击即可最小化到托盘启动。", dir)
}

// ensurePlaceholderAPIMD 在 exe 同目录生成开箱即用的 api.md（compatible 模式）。
// key 留空时程序自动使用内置 APP_KEY；用户填入 key 后覆盖内置值。
// 已存在则跳过；失败仅记录日志，不影响启动。
func ensurePlaceholderAPIMD() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	mdPath := filepath.Join(dir, "api.md")
	if _, err := os.Stat(mdPath); err == nil {
		return // 已存在
	}
	if err := server.EnsurePlaceholderAPIMD(mdPath); err != nil {
		log.Printf("生成 api.md 失败（可忽略）: %v", err)
		return
	}
	log.Printf("已在 %s 生成 api.md（compatible 模式，key 留空走内置 APP_KEY）。", dir)
}
