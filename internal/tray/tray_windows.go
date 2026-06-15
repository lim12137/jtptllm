//go:build windows

package tray

import (
	_ "embed"
	"log"
	"strings"

	"github.com/getlantern/systray"
	"golang.org/x/sys/windows"
)

//go:embed icon.ico
var defaultIconBytes []byte

const (
	swHide    = 0 // SW_HIDE
	swShow    = 5 // SW_SHOW
	swRestore = 9 // SW_RESTORE
)

// consoleProcs 封装控制台窗口操作所需的几个 Win32 过程。
// 全部用 LazyProc + 预检查（Find）加载，任何过程缺失都不会 panic，
// 只会让对应的「隐藏/显示控制台」功能静默不可用——保证程序主体仍能常驻。
var consoleProcs struct {
	getConsoleWindow *windows.LazyProc
	showWindowAsync  *windows.LazyProc
	isIconic         *windows.LazyProc
	available        bool
}

func init() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	user32 := windows.NewLazySystemDLL("user32.dll")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	showWindowAsync := user32.NewProc("ShowWindowAsync")
	isIconic := user32.NewProc("IsIconic")

	// Find 仅检查导出表是否包含该过程，不调用，不会 panic。
	consoleProcs.getConsoleWindow = getConsoleWindow
	consoleProcs.showWindowAsync = showWindowAsync
	consoleProcs.isIconic = isIconic
	consoleProcs.available = getConsoleWindow.Find() == nil &&
		showWindowAsync.Find() == nil &&
		isIconic.Find() == nil
	if !consoleProcs.available {
		log.Printf("tray: console window APIs unavailable (likely stripped Windows / non-standard desktop); " +
			"console hide/show will be disabled, proxy still runs in background")
	}
}

// Run 启动系统托盘消息循环。必须在主 goroutine 调用（systray 要求）。
//
// 流程：
//  1. onReady 中设置图标、标题、菜单（显示/隐藏控制台、退出）。
//  2. 立即隐藏控制台窗口（黑窗一闪后消失）。
//  3. 进入消息循环，监听菜单点击。
//  4. 用户点「退出」→ 调 Shutdown → systray.Quit → onExit。
func Run(opts Options) {
	iconBytes := opts.IconBytes
	if len(iconBytes) == 0 {
		iconBytes = defaultIconBytes
	}
	shutdownFn := opts.Shutdown

	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "jtptllm-proxy"
	}
	if opts.Addr != "" {
		title = title + " @ " + opts.Addr
	}

	onReady := func() {
		systray.SetIcon(iconBytes)
		systray.SetTitle(title)
		systray.SetTooltip(title)

		consoleVisible := false
		mShowConsole := systray.AddMenuItemCheckbox("显示控制台", "显示或隐藏控制台窗口", consoleVisible)
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "关闭代理并退出")

		// 控制台先隐藏（黑窗一闪后隐藏到托盘）。
		hideConsoleWindow()

		if opts.OnReady != nil {
			opts.OnReady()
		}

		// 菜单事件循环必须在本 goroutine 内，因为 systray.Run 已占用主线程。
		go func() {
			for {
				select {
				case <-mShowConsole.ClickedCh:
					if mShowConsole.Checked() {
						hideConsoleWindow()
						mShowConsole.Uncheck()
					} else {
						showConsoleWindow()
						mShowConsole.Check()
					}
				case <-mQuit.ClickedCh:
					if shutdownFn != nil {
						shutdownFn()
					}
					systray.Quit()
					return
				}
			}
		}()
	}

	onExit := func() {
		// 退出前恢复控制台可见，方便看到最后的日志。
		showConsoleWindow()
	}

	systray.Run(onReady, onExit)
}

// hideConsoleWindow 隐藏本进程附加的控制台窗口。
// 任何失败（API 缺失、调用异常）都被吞掉，绝不 panic。
func hideConsoleWindow() {
	safeShowConsole(swHide)
}

// showConsoleWindow 显示并前置本进程的控制台窗口。
func showConsoleWindow() {
	hwnd := getConsoleHwnd()
	if hwnd == 0 {
		return
	}
	if isIconicWindow(hwnd) {
		safeCallProc(consoleProcs.showWindowAsync, hwnd, swRestore)
	}
	safeCallProc(consoleProcs.showWindowAsync, hwnd, swShow)
}

// safeShowConsole 是 hideConsoleWindow 的内部辅助：取句柄后发 SW 命令。
func safeShowConsole(cmd uintptr) {
	hwnd := getConsoleHwnd()
	if hwnd == 0 {
		return
	}
	safeCallProc(consoleProcs.showWindowAsync, hwnd, cmd)
}

// getConsoleHwnd 返回附加控制台窗口句柄；不可用或无控制台时返回 0。
func getConsoleHwnd() uintptr {
	if !consoleProcs.available {
		return 0
	}
	defer func() { recover() }() // 防御 LazyProc.Call 的任何意外 panic
	r, _, _ := consoleProcs.getConsoleWindow.Call()
	return r
}

// isIconicWindow 判断窗口是否最小化；任何异常返回 false。
func isIconicWindow(hwnd uintptr) bool {
	ret := safeCallProc(consoleProcs.isIconic, hwnd)
	return ret != 0
}

// safeCallProc 调用一个 LazyProc 并捕获 panic。
// 若过程不可用或调用异常，返回 0 且不向上传播错误。
func safeCallProc(p *windows.LazyProc, args ...uintptr) uintptr {
	if p == nil {
		return 0
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("tray: recovered from syscall: %v", r)
		}
	}()
	r, _, _ := p.Call(args...)
	return r
}
