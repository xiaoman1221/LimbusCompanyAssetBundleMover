package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const appTitle = "LIMBUS COMPANY // 资源包迁移工具"

type srcState int

const (
	srcUnknown srcState = iota
	srcPresent
	srcMissing
)

type logKind int

const (
	logInfo logKind = iota
	logOK
	logWarn
	logErr
)

type logEntry struct {
	ts   string
	msg  string
	kind logKind
}

type hitArea struct {
	id string
	r  walk.Rectangle
}

type app struct {
	mw        *walk.MainWindow
	cw        *walk.CustomWidget
	th        *fonts
	stateFile string

	source    string
	target    string
	srcState  srcState
	relocated bool
	scanning  bool
	srcSize   int64
	srcFiles  int

	srcFree, srcTotal    uint64
	tgtFree, tgtTotal    uint64
	srcDiskOK, tgtDiskOK bool

	busy            bool
	opKind          string
	opLabel         string
	opPercent       float64
	opMessage       string
	opPhase         string
	opLastLoggedPct int
	opLastKey       string
	promptOpen      bool

	log []logEntry

	hover, pressed string
	hits           []hitArea

	mouseTracked bool
	procCb       uintptr
	prevProc     uintptr
}

func newApp() *app {
	a := &app{
		th:        loadFonts(),
		source:    defaultSourcePath(),
		target:    suggestTarget(),
		stateFile: defaultStatePath(),
	}
	if isDir(a.source) {
		a.srcState = srcPresent
	} else {
		a.srcState = srcMissing
	}
	return a
}

func (a *app) run() error {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return err
	}
	a.mw = mw

	_ = mw.SetTitle(appTitle)
	mw.SetSize(walk.Size{Width: 1120, Height: 760})
	_ = mw.SetMinMaxSize(walk.Size{Width: 960, Height: 700}, walk.Size{})

	if bg, err := walk.NewSolidColorBrush(colBG); err == nil {
		mw.SetBackground(bg)
	}
	if icon, err := walk.NewIconFromImage(makeAppIcon()); err == nil {
		_ = mw.SetIcon(icon)
	}

	layout := walk.NewVBoxLayout()
	_ = layout.SetMargins(walk.Margins{})
	if err := mw.SetLayout(layout); err != nil {
		return err
	}

	cw, err := walk.NewCustomWidgetPixels(mw, 0, a.paint)
	if err != nil {
		return err
	}
	cw.SetPaintMode(walk.PaintBuffered)
	cw.SetInvalidatesOnResize(true)
	a.cw = cw

	a.installMouseHook()
	applyDarkFrame(mw.Handle())

	a.logf(logInfo, "资源管理终端已启动")
	a.loadRelocation()
	if a.relocated {
		a.logf(logOK, "源目录已迁移并链接至目标目录，可执行校验或重新链接")
	} else if a.srcState == srcPresent {
		a.logf(logOK, "已检测到源资源目录")
	} else {
		a.logf(logErr, "未找到源资源目录")
	}
	if a.target != "" {
		a.logf(logInfo, "目标暂存目录：%s", a.target)
	} else {
		a.logf(logWarn, "未检测到可用副盘符 — 请选择目标目录")
	}
	a.logf(logInfo, "等待操作（执行迁移/还原时将请求管理员权限）")
	if a.relocated {
		a.promptOpen = true
		a.logf(logInfo, "检测到已迁移的数据：请选择移动到新目录或还原")
	}

	mw.Starting().Attach(func() {
		a.refreshDisks()
		a.startScan()
		time.AfterFunc(700*time.Millisecond, func() {
			a.mw.Synchronize(func() { a.ensureGameClosed() })
		})
	})

	mw.Show()
	centerWindow(mw)
	mw.Run()
	return nil
}

func centerWindow(w *walk.MainWindow) {
	b := w.BoundsPixels()
	sw := int(win.GetSystemMetrics(win.SM_CXSCREEN))
	sh := int(win.GetSystemMetrics(win.SM_CYSCREEN))
	if sw == 0 || sh == 0 || b.Width <= 0 || b.Height <= 0 {
		return
	}
	_ = w.SetBoundsPixels(walk.Rectangle{
		X:      (sw - b.Width) / 2,
		Y:      (sh - b.Height) / 2,
		Width:  b.Width,
		Height: b.Height,
	})
}

// applyDarkFrame asks DWM for a dark caption, matching the terminal palette.
func applyDarkFrame(hwnd win.HWND) {
	dwm := windows.NewLazySystemDLL("dwmapi.dll")
	proc := dwm.NewProc("DwmSetWindowAttribute")
	if err := proc.Find(); err != nil {
		return
	}
	set := func(attr uint32, value int32) {
		_, _, _ = proc.Call(
			uintptr(hwnd),
			uintptr(attr),
			uintptr(unsafe.Pointer(&value)),
			unsafe.Sizeof(value),
		)
	}
	set(20, 1)              // DWMWA_USE_IMMERSIVE_DARK_MODE
	set(35, int32(colBG))   // DWMWA_CAPTION_COLOR
	set(34, int32(colLine)) // DWMWA_BORDER_COLOR
	set(36, int32(colText)) // DWMWA_TEXT_COLOR
}

// installMouseHook subclasses the custom widget so the flat interface can
// receive hover, click and cursor events (walk's CustomWidget has no mouse API).
func (a *app) installMouseHook() {
	a.procCb = syscall.NewCallback(a.windowProc)
	a.prevProc = win.SetWindowLongPtr(a.cw.Handle(), win.GWL_WNDPROC, a.procCb)
}

func (a *app) windowProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_MOUSEMOVE:
		x, y := mouseXY(lParam)
		if !a.mouseTracked {
			var tme win.TRACKMOUSEEVENT
			tme.CbSize = uint32(unsafe.Sizeof(tme))
			tme.DwFlags = win.TME_LEAVE
			tme.HwndTrack = hwnd
			win.TrackMouseEvent(&tme)
			a.mouseTracked = true
		}
		a.setHover(a.hitTest(x, y))

	case win.WM_MOUSELEAVE:
		a.mouseTracked = false
		a.setHover("")

	case win.WM_LBUTTONDOWN:
		x, y := mouseXY(lParam)
		a.pressed = a.hitTest(x, y)
		if a.pressed != "" {
			a.cw.Invalidate()
		}

	case win.WM_LBUTTONUP:
		x, y := mouseXY(lParam)
		id := a.hitTest(x, y)
		pressed := a.pressed
		a.pressed = ""
		a.setHover(id)
		if id != "" && id == pressed {
			a.activate(id)
		}
		a.cw.Invalidate()

	case win.WM_SETCURSOR:
		if uint16(lParam&0xffff) == win.HTCLIENT {
			var pt win.POINT
			if win.GetCursorPos(&pt) && win.ScreenToClient(hwnd, &pt) {
				if a.hitTest(int(pt.X), int(pt.Y)) != "" {
					win.SetCursor(win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_HAND)))
				} else {
					win.SetCursor(win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)))
				}
				return 1
			}
		}
	}

	return win.CallWindowProc(a.prevProc, hwnd, msg, wParam, lParam)
}

func mouseXY(lParam uintptr) (int, int) {
	return int(int16(lParam & 0xffff)), int(int16((lParam >> 16) & 0xffff))
}

func (a *app) setHover(id string) {
	if id == a.hover {
		return
	}
	a.hover = id
	if a.cw != nil {
		a.cw.Invalidate()
	}
}

func (a *app) hitTest(x, y int) string {
	if a.busy {
		return ""
	}
	for i := len(a.hits) - 1; i >= 0; i-- {
		h := a.hits[i]
		if a.promptOpen && !strings.HasPrefix(h.id, "prompt.") {
			continue
		}
		r := h.r
		if x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
			return h.id
		}
	}
	return ""
}

func (a *app) activate(id string) {
	if a.busy {
		return
	}
	switch id {
	case "prompt.relink":
		a.promptOpen = false
		a.cw.Invalidate()
		a.chooseRelinkTarget()

	case "prompt.restore":
		a.promptOpen = false
		a.cw.Invalidate()
		a.beginOperation("restore")

	case "prompt.later":
		a.promptOpen = false
		a.cw.Invalidate()
		a.logf(logInfo, "已暂不处理，可在主界面选择重新链接或还原")

	case "source.locate":
		a.pickFolder(a.source, "选择源资源目录", func(path string) {
			a.source = path
			a.srcSize, a.srcFiles = 0, 0
			a.relocated = false
			a.loadRelocation()
			if isDir(path) {
				a.srcState = srcPresent
			} else {
				a.srcState = srcMissing
			}
			a.refreshDisks()
			a.logf(logInfo, "源目录已设置：%s", path)
			a.startScan()
			if a.relocated {
				a.promptOpen = true
				a.logf(logInfo, "检测到已迁移的数据：请选择移动到新目录或还原")
			}
		})

	case "source.scan":
		a.startScan()

	case "target.browse":
		a.pickFolder(a.target, "选择目标存放目录", func(path string) {
			a.target = path
			a.refreshDisks()
			a.logf(logInfo, "目标目录已设置：%s", path)
			if a.tgtDiskOK {
				a.logf(logInfo, "目标磁盘剩余空间 %s", humanSize(int64(a.tgtFree)))
			}
		})

	case "op.relocate":
		a.beginOperation("relocate")

	case "op.verify":
		a.beginOperation("verify")

	case "op.restore":
		a.beginOperation("restore")
	}
}

func (a *app) chooseRelinkTarget() {
	a.pickFolder(a.target, "选择新的目标目录", func(path string) {
		if linkTarget, ok := resolveLink(a.source); ok && samePath(path, linkTarget) {
			a.logf(logWarn, "新目标不能与当前链接目标相同")
			return
		}
		if err := targetMustBeEmpty(path); err != nil {
			a.logf(logErr, "新目标目录不可用：%v", err)
			return
		}
		a.target = path
		a.refreshDisks()
		a.logf(logInfo, "新目标目录已选择：%s", path)
		a.beginOperation("relocate")
	})
}

func (a *app) pickFolder(current, title string, apply func(string)) {
	dlg := new(walk.FileDialog)
	dlg.Title = title
	dlg.InitialDirPath = existingDir(current)
	ok, err := dlg.ShowBrowseFolder(a.mw)
	if err != nil {
		a.logf(logErr, "文件夹选择器打开失败：%v", err)
		return
	}
	if !ok || dlg.FilePath == "" {
		return
	}
	apply(dlg.FilePath)
}

func (a *app) refreshDisks() {
	a.srcFree, a.srcTotal, a.srcDiskOK = diskFree(a.source)
	a.tgtFree, a.tgtTotal, a.tgtDiskOK = diskFree(a.target)
}

func (a *app) loadRelocation() {
	prev := a.relocated
	if st, err := loadStateFile(a.stateFile); err == nil && st != nil {
		if samePath(st.Source, a.source) && a.target == "" {
			a.target = st.Target
		}
	}
	if target, ok := resolveLink(a.source); ok {
		a.relocated = true
		a.target = target
		if !prev {
			a.logf(logInfo, "源目录已链接至：%s", target)
		}
	} else {
		a.relocated = false
		if prev {
			a.logf(logInfo, "源目录已恢复为普通目录")
		}
	}
	if a.relocated {
		if isDir(a.source) {
			a.srcState = srcPresent
		} else {
			a.srcState = srcMissing
		}
	}
}

func (a *app) startScan() {
	if a.scanning {
		return
	}
	scanPath := a.source
	if target, ok := resolveLink(a.source); ok {
		scanPath = target
	}
	if !isDir(scanPath) {
		a.srcState = srcMissing
		a.logf(logErr, "未找到源资源目录：%s", a.source)
		if a.cw != nil {
			a.cw.Invalidate()
		}
		return
	}

	a.srcState = srcPresent
	a.scanning = true
	displayPath := a.source
	a.logf(logInfo, "正在扫描源数据 ...")

	go func() {
		size, files, err := scanDir(scanPath)
		a.mw.Synchronize(func() {
			if a.source != displayPath {
				return
			}
			a.scanning = false
			if err != nil {
				a.srcState = srcMissing
				a.logf(logErr, "扫描失败：%v", err)
			} else {
				a.srcState = srcPresent
				a.srcSize, a.srcFiles = size, files
				a.logf(logOK, "数据扫描完成 — %s / %d 个文件", humanSize(size), files)
			}
			a.refreshDisks()
			a.cw.Invalidate()
		})
	}()
}

func (a *app) logf(kind logKind, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	a.log = append(a.log, logEntry{
		ts:   time.Now().Format("15:04:05"),
		msg:  msg,
		kind: kind,
	})
	if len(a.log) > 200 {
		a.log = a.log[len(a.log)-200:]
	}
	if a.cw != nil {
		a.cw.Invalidate()
	}
}

// ensureGameClosed detects a running Limbus Company and, with operator
// consent, terminates it. It returns true when the game is not running.
func (a *app) ensureGameClosed() bool {
	pids := gameProcessIDs()
	if len(pids) == 0 {
		a.logf(logInfo, "未检测到 Limbus Company 进程")
		return true
	}

	a.logf(logWarn, "检测到 Limbus Company 正在运行（PID %s）", pidList(pids))
	msg := fmt.Sprintf(
		"检测到 Limbus Company 正在运行：\n\nPID  %s\n\n迁移资源前必须结束游戏进程，是否立即结束？",
		pidList(pids),
	)
	if walk.MsgBox(a.mw, "游戏进程检测", msg, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		a.logf(logWarn, "已保留游戏进程，请在操作前手动关闭")
		return false
	}

	var failed []string
	for _, pid := range pids {
		if err := terminateProcess(pid); err != nil {
			failed = append(failed, fmt.Sprintf("%d（%v）", pid, err))
			continue
		}
		a.logf(logOK, "已结束游戏进程 PID %d", pid)
	}
	if len(failed) > 0 {
		a.logf(logErr, "部分进程结束失败：%s", strings.Join(failed, "、"))
	}

	time.Sleep(350 * time.Millisecond)
	if left := gameProcessIDs(); len(left) > 0 {
		a.logf(logErr, "游戏进程仍在运行：PID %s", pidList(left))
		return false
	}
	a.logf(logOK, "游戏进程已全部结束")
	return true
}

// ---------------------------------------------------------------------------
// operations

func operationLabel(kind string) string {
	switch kind {
	case "relocate":
		return "迁移中"
	case "verify":
		return "校验中"
	case "restore":
		return "还原中"
	}
	return "执行中"
}

func (a *app) beginOperation(kind string) {
	if a.busy {
		a.logf(logWarn, "已有操作正在进行，请稍候")
		return
	}
	switch kind {
	case "relocate":
		if a.source == "" {
			a.logf(logWarn, "请先设置源目录")
			return
		}
		if a.target == "" {
			a.logf(logWarn, "请先选择目标目录")
			return
		}
		if samePath(a.source, a.target) {
			a.logf(logErr, "源目录与目标目录不能相同")
			return
		}
		if linkTarget, ok := resolveLink(a.source); ok {
			if samePath(linkTarget, a.target) {
				a.logf(logWarn, "请选择一个新的目标目录（当前链接目标：%s）", linkTarget)
				return
			}
		} else if !isDir(a.source) {
			a.logf(logErr, "源目录不存在：%s", a.source)
			return
		}
	case "restore":
		if _, ok := resolveLink(a.source); !ok {
			a.logf(logWarn, "源目录不是链接，无法还原")
			return
		}
	case "verify":
		if a.source == "" {
			a.logf(logWarn, "请先设置源目录")
			return
		}
	}

	if kind != "verify" && !a.ensureGameClosed() {
		return
	}

	a.busy = true
	a.opKind = kind
	a.opLabel = operationLabel(kind)
	if kind == "relocate" {
		if _, ok := resolveLink(a.source); ok {
			a.opLabel = "重新链接中"
		}
	}
	a.opPercent = 0
	a.opMessage = "正在准备..."
	a.opPhase = ""
	a.opLastLoggedPct = -1
	a.opLastKey = ""
	a.logf(logInfo, "%s：开始", a.opLabel)
	a.cw.Invalidate()

	if kind == "verify" || isElevated() {
		go a.runInProcess(kind)
		return
	}
	a.startElevated(kind)
}

func (a *app) runInProcess(kind string) {
	sink := func(r ProgressReport) {
		a.mw.Synchronize(func() { a.applyProgress(r) })
	}
	var err error
	switch kind {
	case "relocate":
		err = RunRelocate(a.source, a.target, a.stateFile, sink)
	case "verify":
		err = RunVerify(a.source, a.target, a.stateFile, sink)
	case "restore":
		err = RunRestore(a.source, a.target, a.stateFile, sink)
	}
	a.mw.Synchronize(func() { a.finishInProcess(kind, err) })
}

func (a *app) startElevated(kind string) {
	progressFile := filepath.Join(os.TempDir(),
		fmt.Sprintf("limbus-mover-%d.progress", time.Now().UnixNano()))
	source, target, state := a.source, a.target, a.stateFile
	a.opMessage = "正在请求管理员权限..."

	go func() {
		code, err := runElevatedOperation(windows.HWND(a.mw.Handle()), kind, source, target, progressFile, state,
			func(r ProgressReport) {
				a.mw.Synchronize(func() { a.applyProgress(r) })
			})
		_ = os.Remove(progressFile)
		a.mw.Synchronize(func() { a.finishElevated(kind, code, err) })
	}()
}

func (a *app) applyProgress(r ProgressReport) {
	if r.Message != "" {
		a.opMessage = r.Message
	}
	a.opPercent = r.Percent
	if r.Done {
		a.cw.Invalidate()
		return
	}

	key := fmt.Sprintf("%.0f|%s|%s", r.Percent, r.Phase, r.Message)
	if key == a.opLastKey {
		return
	}
	a.opLastKey = key

	if r.Phase != a.opPhase {
		a.opPhase = r.Phase
		a.opLastLoggedPct = int(r.Percent)
		if r.Message != "" {
			a.logf(logInfo, "%s", r.Message)
		}
	} else if int(r.Percent) >= a.opLastLoggedPct+5 {
		a.opLastLoggedPct = int(r.Percent)
		if r.Message != "" {
			a.logf(logInfo, "%s", r.Message)
		}
	}
	a.cw.Invalidate()
}

func (a *app) finishInProcess(kind string, err error) {
	a.busy = false
	if err != nil {
		a.logf(logErr, "%s失败：%v", a.opLabel, err)
	} else {
		msg := a.opMessage
		if msg == "" {
			msg = "操作完成"
		}
		a.logf(logOK, "%s", msg)
	}
	a.afterOperation(kind)
}

func (a *app) finishElevated(kind string, code int, err error) {
	a.busy = false
	switch {
	case err != nil:
		if errors.Is(err, errElevationCancelled) {
			a.logf(logWarn, "已取消管理员授权，操作未执行")
		} else {
			a.logf(logErr, "%s失败：%v", a.opLabel, err)
		}
	case code != 0:
		msg := a.opMessage
		if msg == "" {
			msg = fmt.Sprintf("提权进程退出码 %d", code)
		}
		a.logf(logErr, "%s失败：%s", a.opLabel, msg)
	default:
		msg := a.opMessage
		if msg == "" {
			msg = "操作完成"
		}
		a.logf(logOK, "%s", msg)
	}
	a.afterOperation(kind)
}

func (a *app) afterOperation(kind string) {
	a.loadRelocation()
	a.refreshDisks()
	a.startScan()
	a.cw.Invalidate()
}

func (a *app) terminalStatus() (string, walk.Color) {
	if a.busy {
		pct := int(a.opPercent)
		if pct > 0 {
			return fmt.Sprintf("%s %d%%", a.opLabel, pct), colGold
		}
		return a.opLabel + "...", colGold
	}
	switch {
	case a.scanning:
		return "扫描中", colOrange
	case a.srcState == srcMissing:
		return "源目录缺失", colRedHi
	default:
		return "待命", colGold
	}
}
