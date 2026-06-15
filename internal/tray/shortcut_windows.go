//go:build windows

package tray

import (
	"fmt"
	"os/exec"
	"strings"
)

// EnsureShortcut 在 exePath 同目录创建一个带 args 的快捷方式 lnkPath。
// 使用 PowerShell 的 WScript.Shell COM 对象创建，无需 cgo。
//
// 已存在则跳过；失败仅返回错误由调用方记录日志，不中断启动。
func EnsureShortcut(exePath, lnkPath, args string) error {
	if strings.TrimSpace(exePath) == "" || strings.TrimSpace(lnkPath) == "" {
		return fmt.Errorf("EnsureShortcut: exePath or lnkPath is empty")
	}

	// PowerShell 单行脚本：用单引号包裹路径，并对内部单引号转义。
	psExe := psQuote(exePath)
	psLnk := psQuote(lnkPath)
	psArgs := psQuote(args)
	script := fmt.Sprintf(
		"$ws=New-Object -ComObject WScript.Shell;"+
			"$sc=$ws.CreateShortcut(%s);"+
			"$sc.TargetPath=%s;"+
			"$sc.Arguments=%s;"+
			"$sc.WorkingDirectory=%s;"+
			"$sc.WindowStyle=7;"+
			"$sc.Save()",
		psLnk, psExe, psArgs, psQuote(parentDir(exePath)),
	)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create shortcut failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// psQuote 把一个 Go 字符串包装成 PowerShell 单引号字面量，
// 并把其中的单引号按 PS 规则替换为两个单引号。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func parentDir(path string) string {
	idx := strings.LastIndexAny(path, `/\`)
	if idx < 0 {
		return ""
	}
	return path[:idx]
}
