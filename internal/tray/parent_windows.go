//go:build windows

package tray

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsLaunchedByExplorer 判断当前进程是否由资源管理器（explorer.exe）直接启动，
// 即用户「双击 exe」而非从 cmd/powershell 调用。
//
// 通过 CreateToolhelp32Snapshot 遍历进程快照，取当前进程的父 PID，
// 再比对父进程可执行文件名是否为 explorer.exe。
func IsLaunchedByExplorer() bool {
	pid := windows.GetCurrentProcessId()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	procByID := make(map[uint32]windows.ProcessEntry32, 256)
	for err := windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		procByID[entry.ProcessID] = entry
	}

	me, ok := procByID[pid]
	if !ok {
		return false
	}
	parent, ok := procByID[me.ParentProcessID]
	if !ok {
		return false
	}
	name := strings.ToLower(windows.UTF16ToString(parent.ExeFile[:]))
	return name == "explorer.exe"
}
