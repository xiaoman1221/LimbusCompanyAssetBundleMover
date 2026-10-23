package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errElevationCancelled = errors.New("已取消管理员授权")

type progressWriter struct {
	mu sync.Mutex
	f  *os.File
}

func newProgressWriter(path string) (*progressWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return &progressWriter{f: f}, nil
}

func (w *progressWriter) Report(r ProgressReport) {
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return
	}
	_, _ = w.f.Write(append(data, '\n'))
	_ = w.f.Sync()
}

func (w *progressWriter) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
}

func latestReport(path string) (ProgressReport, bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ProgressReport{}, false
	}
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var r ProgressReport
		if err := json.Unmarshal(line, &r); err == nil {
			return r, true
		}
	}
	return ProgressReport{}, false
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskFlagNoUI       = 0x00000400
)

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.HWND
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     unsafe.Pointer
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

func shellExecuteRunas(owner windows.HWND, exe, args string) (windows.Handle, error) {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(args)
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))

	info := shellExecuteInfo{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:        seeMaskNoCloseProcess | seeMaskFlagNoUI,
		hwnd:         owner,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		lpDirectory:  dir,
		nShow:        0, // SW_HIDE: the elevated helper is headless
	}
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return 0, errElevationCancelled
		}
		if callErr == nil || callErr == syscall.Errno(0) {
			return 0, errors.New("无法启动管理员进程")
		}
		return 0, fmt.Errorf("无法启动管理员进程：%w", callErr)
	}
	if info.hProcess == 0 {
		return 0, errors.New("未能获取管理员进程句柄")
	}
	return info.hProcess, nil
}

func runElevatedOperation(owner windows.HWND, op, source, target, progressFile, stateFile string, onProgress func(ProgressReport)) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return -1, err
	}
	args := "--elevated-op=" + op +
		" --source=" + syscall.EscapeArg(source) +
		" --target=" + syscall.EscapeArg(target) +
		" --progress=" + syscall.EscapeArg(progressFile) +
		" --state=" + syscall.EscapeArg(stateFile)

	h, err := shellExecuteRunas(owner, exe, args)
	if err != nil {
		return -1, err
	}
	defer windows.CloseHandle(h)

	for {
		if r, ok := latestReport(progressFile); ok && onProgress != nil {
			onProgress(r)
		}
		event, _ := windows.WaitForSingleObject(h, 150)
		if event == windows.WAIT_OBJECT_0 {
			break
		}
	}
	if r, ok := latestReport(progressFile); ok && onProgress != nil {
		onProgress(r)
	}

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return -1, err
	}
	return int(code), nil
}

// runHeadless executes an operation for the elevated helper process. Results
// are streamed to the progress file; the exit code signals success.
func runHeadless(op, source, target, progressFile, stateFile string) int {
	if progressFile == "" {
		return 2
	}
	if stateFile == "" {
		stateFile = defaultStatePath()
	}
	w, err := newProgressWriter(progressFile)
	if err != nil {
		return 2
	}
	defer w.Close()

	report := w.Report
	if op == "relocate" || op == "restore" {
		if pids := gameProcessIDs(); len(pids) > 0 {
			report(ProgressReport{
				Phase:   "error",
				Percent: 100,
				Done:    true,
				OK:      false,
				Message: fmt.Sprintf("Limbus Company 正在运行（PID %s），请先关闭游戏", pidList(pids)),
			})
			return 1
		}
	}

	switch op {
	case "relocate":
		err = RunRelocate(source, target, stateFile, report)
	case "restore":
		err = RunRestore(source, target, stateFile, report)
	case "verify":
		err = RunVerify(source, target, stateFile, report)
	default:
		err = fmt.Errorf("未知操作：%s", op)
	}
	if err != nil {
		report(ProgressReport{
			Phase:   "error",
			Percent: 100,
			Done:    true,
			OK:      false,
			Message: err.Error(),
		})
		return 1
	}
	return 0
}
