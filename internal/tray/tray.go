// Package tray 提供系统托盘能力（仅 Windows 实际启用）。
//
// 设计要点：
//   - 托盘模式只在 Windows 上真正工作；其它平台（Linux/Docker）编译出的二进制
//     仍然是纯控制台服务，行为零变化。
//   - 调用方通过 Run(opts) 进入托盘消息循环；opts 提供 HTTP 服务关闭回调，
//     以便用户从托盘菜单「退出」时优雅关闭。
//   - EnsureShortcut / IsLaunchedByExplorer 在非 Windows 平台返回 no-op / false，
//     保证 cmd/proxy 主流程在所有平台都能正常编译。
package tray

// Options 是启动托盘所需的依赖。
type Options struct {
	// Title 托盘图标悬停提示文字。
	Title string

	// IconBytes 托盘图标数据（.ico 字节）。为空时使用内置默认图标。
	IconBytes []byte

	// Addr 当前监听地址，用于菜单提示。
	Addr string

	// Shutdown 在用户选择「退出」时调用，用于优雅关闭 HTTP 服务。
	// 调用完成后 Run 会自行返回。
	Shutdown func()

	// OnServerReady 在托盘图标与菜单就绪后、进入消息循环前调用一次。
	// 用于在控制台被隐藏前打印最后一条提示。
	OnReady func()
}
