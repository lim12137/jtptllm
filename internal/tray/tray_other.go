//go:build !windows

package tray

import (
	"os"
	"os/signal"
	"syscall"
)

// Run 在非 Windows 平台上是一个 no-op 兜底：阻塞等待中断信号。
// 实际上托盘模式只应在 Windows 上通过 --tray 触发，这里仅为保证跨平台编译。
func Run(opts Options) {
	if opts.OnReady != nil {
		opts.OnReady()
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	if opts.Shutdown != nil {
		opts.Shutdown()
	}
}

// EnsureShortcut 在非 Windows 平台不做任何事。
func EnsureShortcut(exePath, lnkPath, args string) error { return nil }

// IsLaunchedByExplorer 在非 Windows 平台永远返回 false。
func IsLaunchedByExplorer() bool { return false }
