package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// ProgressReport is both the UI progress feed and the JSON line written by an
// elevated helper process.
type ProgressReport struct {
	Phase      string  `json:"phase"`
	Percent    float64 `json:"percent"`
	Files      int     `json:"files,omitempty"`
	TotalFiles int     `json:"totalFiles,omitempty"`
	Bytes      int64   `json:"bytes,omitempty"`
	TotalBytes int64   `json:"totalBytes,omitempty"`
	Message    string  `json:"message,omitempty"`
	Done       bool    `json:"done,omitempty"`
	OK         bool    `json:"ok,omitempty"`
}

type ProgressFunc func(ProgressReport)

type RelocationState struct {
	Source      string    `json:"source"`
	Target      string    `json:"target"`
	Files       int       `json:"files"`
	Bytes       int64     `json:"bytes"`
	RelocatedAt time.Time `json:"relocatedAt"`
	LinkType    string    `json:"linkType,omitempty"`
}

type treeStat struct {
	Files int
	Bytes int64
}

func defaultStatePath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.Getenv("APPDATA")
	}
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "LimbusCompanyAssetBundleMover", "state.json")
}

func loadStateFile(path string) (*RelocationState, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var st RelocationState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func saveStateFile(path string, st *RelocationState) error {
	if path == "" {
		return errors.New("状态文件路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func removeStateFile(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func isReparsePoint(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return false
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

// entryIsLink reports reparse points, including junctions that Go maps to
// ModeIrregular rather than ModeSymlink.
func entryIsLink(d fs.DirEntry) bool {
	return d.Type()&(os.ModeSymlink|os.ModeIrregular) != 0
}

// resolveLink returns the target of a directory symlink or junction.
func resolveLink(path string) (string, bool) {
	if path == "" || !isReparsePoint(path) {
		return "", false
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target), true
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func isSubPath(child, parent string) bool {
	c := strings.ToLower(filepath.Clean(child))
	p := strings.ToLower(filepath.Clean(parent))
	rel, err := filepath.Rel(p, c)
	if err != nil || rel == "." {
		return false
	}
	return !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func countTree(root string, progress func(files int, bytes int64)) (treeStat, error) {
	var st treeStat
	last := time.Now()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entryIsLink(d) {
			return fmt.Errorf("目录中包含链接或重解析点：%s", path)
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		st.Files++
		st.Bytes += info.Size()
		if progress != nil && time.Since(last) > 250*time.Millisecond {
			last = time.Now()
			progress(st.Files, st.Bytes)
		}
		return nil
	})
	return st, err
}

func copyTree(src, dst string, total treeStat, report ProgressFunc) error {
	buf := make([]byte, 1<<20)
	var copiedBytes int64
	var copiedFiles int
	last := time.Now()

	emit := func(force bool) {
		if report == nil {
			return
		}
		if !force && time.Since(last) < 150*time.Millisecond {
			return
		}
		last = time.Now()
		pct := 0.0
		if total.Bytes > 0 {
			pct = float64(copiedBytes) / float64(total.Bytes)
		} else if total.Files > 0 {
			pct = float64(copiedFiles) / float64(total.Files)
		}
		if pct > 1 {
			pct = 1
		}
		report(ProgressReport{
			Phase:      "copy",
			Percent:    pct * 75,
			Files:      copiedFiles,
			TotalFiles: total.Files,
			Bytes:      copiedBytes,
			TotalBytes: total.Bytes,
			Message:    fmt.Sprintf("复制中 %d%%（%d/%d 个文件）", int(pct*100), copiedFiles, total.Files),
		})
	}

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entryIsLink(d) {
			return fmt.Errorf("源目录中包含链接或重解析点：%s", path)
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		in, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		if merr := os.MkdirAll(filepath.Dir(out), 0o755); merr != nil {
			in.Close()
			return merr
		}
		outFile, oerr := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if oerr != nil {
			in.Close()
			return oerr
		}
		n, cerr := io.CopyBuffer(outFile, in, buf)
		closeErr := outFile.Close()
		in.Close()
		if cerr != nil {
			return cerr
		}
		if closeErr != nil {
			return closeErr
		}
		copiedBytes += n
		copiedFiles++
		if info, ierr := d.Info(); ierr == nil {
			_ = os.Chtimes(out, info.ModTime(), info.ModTime())
		}
		emit(false)
		return nil
	})
	if err != nil {
		return err
	}
	emit(true)
	return nil
}

func verifyTrees(src, dst string, expected treeStat, report ProgressFunc) error {
	sizes := make(map[string]int64)
	var srcStat treeStat
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entryIsLink(d) {
			return fmt.Errorf("源目录中包含链接：%s", path)
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		rel, _ := filepath.Rel(src, path)
		sizes[rel] = info.Size()
		srcStat.Files++
		srcStat.Bytes += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	if expected.Files > 0 && (srcStat.Files != expected.Files || srcStat.Bytes != expected.Bytes) {
		return fmt.Errorf("源目录在校验期间发生变化（%d 个文件 / %d 字节）", srcStat.Files, srcStat.Bytes)
	}

	checked := 0
	last := time.Now()
	var dstStat treeStat
	err = filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entryIsLink(d) {
			return fmt.Errorf("目标目录中出现链接：%s", path)
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dst, path)
		want, ok := sizes[rel]
		if !ok {
			return fmt.Errorf("目标目录存在多余文件：%s", rel)
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if info.Size() != want {
			return fmt.Errorf("文件大小不一致：%s", rel)
		}
		delete(sizes, rel)
		dstStat.Files++
		dstStat.Bytes += info.Size()
		checked++
		if report != nil && time.Since(last) > 250*time.Millisecond {
			last = time.Now()
			pct := 0.0
			if srcStat.Files > 0 {
				pct = float64(checked) / float64(srcStat.Files)
			}
			report(ProgressReport{
				Phase:      "verify",
				Percent:    75 + pct*5,
				Files:      checked,
				TotalFiles: srcStat.Files,
				Message:    fmt.Sprintf("校验中 %d%%（%d/%d）", int(pct*100), checked, srcStat.Files),
			})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(sizes) > 0 {
		return fmt.Errorf("目标目录缺少 %d 个文件", len(sizes))
	}
	if dstStat != srcStat {
		return errors.New("校验失败：文件数或总大小不一致")
	}
	if report != nil {
		report(ProgressReport{
			Phase:      "verify",
			Percent:    80,
			Files:      srcStat.Files,
			TotalFiles: srcStat.Files,
			Message:    "数据校验通过",
		})
	}
	return nil
}

func removeTree(root string, totalFiles int, report ProgressFunc, phase string, basePct, span float64, label string) error {
	removed := 0
	last := time.Now()

	var walk func(string) error
	walk = func(p string) error {
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			cp := filepath.Join(p, e.Name())
			if e.IsDir() && !entryIsLink(e) {
				if err := walk(cp); err != nil {
					return err
				}
				continue
			}
			if err := os.Remove(cp); err != nil {
				return err
			}
			removed++
			if report != nil && time.Since(last) > 250*time.Millisecond {
				last = time.Now()
				pct := 0.0
				if totalFiles > 0 {
					pct = float64(removed) / float64(totalFiles)
				}
				if pct > 1 {
					pct = 1
				}
				report(ProgressReport{
					Phase:      phase,
					Percent:    basePct + pct*span,
					Files:      removed,
					TotalFiles: totalFiles,
					Message:    fmt.Sprintf("%s %d%%（%d/%d 个文件）", label, int(pct*100), removed, totalFiles),
				})
			}
		}
		return os.Remove(p)
	}
	if err := walk(root); err != nil {
		return err
	}
	if report != nil {
		report(ProgressReport{
			Phase:      phase,
			Percent:    basePct + span,
			Files:      removed,
			TotalFiles: totalFiles,
			Message:    label + "完成",
		})
	}
	return nil
}

func createDirLink(link, target string) (string, error) {
	lp, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return "", err
	}
	tp, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", err
	}
	if err := windows.CreateSymbolicLink(lp, tp, windows.SYMBOLIC_LINK_FLAG_DIRECTORY); err == nil {
		return "symlink", nil
	} else if !errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
		return "", fmt.Errorf("创建符号链接失败：%w", err)
	}

	if err := createJunction(link, target); err != nil {
		return "", fmt.Errorf("创建目录联接失败：%w", err)
	}
	return "junction", nil
}

func createJunction(link, target string) error {
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(link)
	if err != nil {
		_ = os.Remove(link)
		return err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		_ = os.Remove(link)
		return err
	}
	defer windows.CloseHandle(h)

	targetAbs, err := filepath.Abs(target)
	if err != nil {
		_ = os.Remove(link)
		return err
	}
	subst := `\??\` + targetAbs
	printName := targetAbs
	subU := utf16.Encode([]rune(subst))
	printU := utf16.Encode([]rune(printName))

	buf := make([]byte, 16+(len(subU)+1)*2+(len(printU)+1)*2)
	binary.LittleEndian.PutUint32(buf[0:], 0xA0000003) // IO_REPARSE_TAG_MOUNT_POINT
	binary.LittleEndian.PutUint16(buf[4:], uint16(len(buf)-8))
	binary.LittleEndian.PutUint16(buf[6:], 0)
	binary.LittleEndian.PutUint16(buf[8:], 0)
	binary.LittleEndian.PutUint16(buf[10:], uint16(len(subU)*2))
	binary.LittleEndian.PutUint16(buf[12:], uint16((len(subU)+1)*2))
	binary.LittleEndian.PutUint16(buf[14:], uint16(len(printU)*2))

	off := 16
	for _, r := range subU {
		binary.LittleEndian.PutUint16(buf[off:], r)
		off += 2
	}
	off += 2
	for _, r := range printU {
		binary.LittleEndian.PutUint16(buf[off:], r)
		off += 2
	}

	var returned uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT,
		&buf[0], uint32(len(buf)), nil, 0, &returned, nil); err != nil {
		_ = os.Remove(link)
		return err
	}
	return nil
}

func linkTypeLabel(linkType string) string {
	if linkType == "junction" {
		return "目录联接"
	}
	return "符号链接"
}

func targetMustBeEmpty(target string) error {
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() {
			return errors.New("目标路径不是目录")
		}
		entries, err := os.ReadDir(target)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return errors.New("目标目录必须为空")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func ensureFreeSpace(target string, stats treeStat) error {
	if free, _, ok := diskFree(target); ok {
		need := uint64(stats.Bytes) + 64*1024*1024
		if need > free {
			return fmt.Errorf("目标磁盘空间不足：需要 %s，可用 %s",
				humanSize(stats.Bytes), humanSize(int64(free)))
		}
	}
	return nil
}

func writeState(stateFile, source, target, linkType string, stats treeStat) []string {
	st := &RelocationState{
		Source:      source,
		Target:      target,
		Files:       stats.Files,
		Bytes:       stats.Bytes,
		RelocatedAt: time.Now(),
		LinkType:    linkType,
	}
	var warnings []string
	if err := saveStateFile(stateFile, st); err != nil {
		warnings = append(warnings, "状态文件写入失败："+err.Error())
	}
	return warnings
}

// RunRelocate moves the game data to target and points source at it. If source
// is already a link, the data is moved from the current target to the new one
// and the link is rebuilt.
func RunRelocate(source, target, stateFile string, report ProgressFunc) error {
	if report == nil {
		report = func(ProgressReport) {}
	}
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	if source == "" || target == "" {
		return errors.New("源目录或目标目录为空")
	}
	if samePath(source, target) {
		return errors.New("源目录与目标目录不能相同")
	}
	if isSubPath(source, target) || isSubPath(target, source) {
		return errors.New("源目录与目标目录不能互相包含")
	}

	if linkTarget, ok := resolveLink(source); ok {
		return reLink(source, linkTarget, target, stateFile, report)
	}

	if !isDir(source) {
		return fmt.Errorf("源目录不存在：%s", source)
	}
	if err := targetMustBeEmpty(target); err != nil {
		return err
	}

	report(ProgressReport{Phase: "prepare", Message: "正在统计源目录..."})
	stats, err := countTree(source, func(files int, _ int64) {
		report(ProgressReport{Phase: "prepare", Message: fmt.Sprintf("正在统计源目录... 已读取 %d 个文件", files)})
	})
	if err != nil {
		return err
	}
	if err := ensureFreeSpace(target, stats); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}

	if err := copyTree(source, target, stats, report); err != nil {
		_ = os.RemoveAll(target)
		return fmt.Errorf("复制失败：%w", err)
	}
	if err := verifyTrees(source, target, stats, report); err != nil {
		_ = os.RemoveAll(target)
		return fmt.Errorf("复制校验失败：%w", err)
	}
	if err := gameRunningError(); err != nil {
		_ = os.RemoveAll(target)
		return err
	}

	backup := source + fmt.Sprintf(".mover-backup-%d", time.Now().Unix())
	if err := os.Rename(source, backup); err != nil {
		_ = os.RemoveAll(target)
		return fmt.Errorf("重命名源目录失败：%w", err)
	}
	linkType, err := createDirLink(source, target)
	if err != nil {
		_ = os.Rename(backup, source)
		return err
	}
	if _, err := os.Stat(source); err != nil {
		_ = os.Remove(source)
		_ = os.Rename(backup, source)
		return fmt.Errorf("链接创建后无法访问：%w", err)
	}

	warnings := writeState(stateFile, source, target, linkType, stats)
	report(ProgressReport{
		Phase:   "link",
		Percent: 82,
		Message: "已创建" + linkTypeLabel(linkType) + "，正在清理旧数据备份...",
	})
	if err := removeTree(backup, stats.Files, report, "cleanup", 82, 18, "清理备份"); err != nil {
		warnings = append(warnings, "旧数据备份未能完全清理，请手动删除 "+backup)
	}

	msg := fmt.Sprintf("迁移完成：%d 个文件 / %s", stats.Files, humanSize(stats.Bytes))
	if len(warnings) > 0 {
		msg += "；" + strings.Join(warnings, "；")
	}
	report(ProgressReport{
		Phase:      "done",
		Percent:    100,
		Files:      stats.Files,
		TotalFiles: stats.Files,
		Bytes:      stats.Bytes,
		TotalBytes: stats.Bytes,
		Done:       true,
		OK:         true,
		Message:    msg,
	})
	return nil
}

// reLink moves data from the current link target to a new target and updates
// the link at source.
func reLink(source, oldTarget, target, stateFile string, report ProgressFunc) error {
	if samePath(oldTarget, target) {
		return fmt.Errorf("新目标与当前链接目标相同：%s", oldTarget)
	}
	if samePath(source, target) ||
		isSubPath(source, target) || isSubPath(target, source) ||
		isSubPath(oldTarget, target) || isSubPath(target, oldTarget) {
		return errors.New("源目录、当前目标和新的目标目录不能互相包含")
	}
	if !isDir(oldTarget) {
		return fmt.Errorf("当前链接目标不存在：%s", oldTarget)
	}
	if err := targetMustBeEmpty(target); err != nil {
		return err
	}

	oldState, _ := loadStateFile(stateFile)

	report(ProgressReport{Phase: "prepare", Message: "正在统计当前目标目录..."})
	stats, err := countTree(oldTarget, func(files int, _ int64) {
		report(ProgressReport{Phase: "prepare", Message: fmt.Sprintf("正在统计当前目标目录... 已读取 %d 个文件", files)})
	})
	if err != nil {
		return err
	}
	if err := ensureFreeSpace(target, stats); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}

	if err := copyTree(oldTarget, target, stats, report); err != nil {
		_ = os.RemoveAll(target)
		return fmt.Errorf("复制到新目标失败：%w", err)
	}
	if err := verifyTrees(oldTarget, target, stats, report); err != nil {
		_ = os.RemoveAll(target)
		return fmt.Errorf("新目标校验失败：%w", err)
	}
	if err := gameRunningError(); err != nil {
		_ = os.RemoveAll(target)
		return err
	}

	backupLink := source + fmt.Sprintf(".mover-link-%d", time.Now().Unix())
	if err := os.Rename(source, backupLink); err != nil {
		_ = os.RemoveAll(target)
		return fmt.Errorf("移走旧链接失败：%w", err)
	}
	linkType, err := createDirLink(source, target)
	if err != nil {
		_ = os.Rename(backupLink, source)
		_ = os.RemoveAll(target)
		return err
	}
	if _, err := os.Stat(source); err != nil {
		_ = os.Remove(source)
		_ = os.Rename(backupLink, source)
		_ = os.RemoveAll(target)
		return fmt.Errorf("新链接创建后无法访问：%w", err)
	}

	warnings := writeState(stateFile, source, target, linkType, stats)
	report(ProgressReport{
		Phase:   "link",
		Percent: 82,
		Message: "已重新创建" + linkTypeLabel(linkType) + "，正在清理旧目标...",
	})
	if err := os.Remove(backupLink); err != nil {
		warnings = append(warnings, "旧链接备份未能删除："+backupLink)
	}

	stateMatches := oldState != nil &&
		samePath(oldState.Source, source) && samePath(oldState.Target, oldTarget)
	if stateMatches {
		if now, err := countTree(oldTarget, nil); err == nil && now == stats {
			if err := removeTree(oldTarget, stats.Files, report, "cleanup", 82, 18, "清理旧目标"); err != nil {
				warnings = append(warnings, "旧目标目录未能完全清理，请手动删除 "+oldTarget)
			}
		} else {
			warnings = append(warnings, "旧目标目录内容已变化，未自动删除："+oldTarget)
		}
	} else {
		warnings = append(warnings, "未找到匹配的迁移记录，已保留旧目标目录："+oldTarget)
	}

	msg := fmt.Sprintf("重新链接完成：新目标 %s（%d 个文件 / %s）",
		target, stats.Files, humanSize(stats.Bytes))
	if len(warnings) > 0 {
		msg += "；" + strings.Join(warnings, "；")
	}
	report(ProgressReport{
		Phase:      "done",
		Percent:    100,
		Files:      stats.Files,
		TotalFiles: stats.Files,
		Bytes:      stats.Bytes,
		TotalBytes: stats.Bytes,
		Done:       true,
		OK:         true,
		Message:    msg,
	})
	return nil
}

// RunVerify checks the link and the data at the target.
func RunVerify(source, target, stateFile string, report ProgressFunc) error {
	if report == nil {
		report = func(ProgressReport) {}
	}
	st, _ := loadStateFile(stateFile)
	if target == "" && st != nil {
		target = st.Target
	}

	linkTarget, isLink := resolveLink(source)
	if !isLink {
		if st != nil {
			return errors.New("存在迁移记录，但源目录不是链接")
		}
		if !isDir(source) {
			return fmt.Errorf("源目录不存在：%s", source)
		}
		stats, err := countTree(source, func(files int, _ int64) {
			report(ProgressReport{Phase: "verify", Message: fmt.Sprintf("正在读取源目录... %d 个文件", files)})
		})
		if err != nil {
			return err
		}
		report(ProgressReport{
			Phase:      "done",
			Percent:    100,
			Files:      stats.Files,
			TotalFiles: stats.Files,
			Bytes:      stats.Bytes,
			TotalBytes: stats.Bytes,
			Done:       true,
			OK:         true,
			Message:    fmt.Sprintf("源目录尚未迁移，当前目录可读：%d 个文件 / %s", stats.Files, humanSize(stats.Bytes)),
		})
		return nil
	}

	if target == "" {
		target = linkTarget
	}
	if !samePath(linkTarget, target) {
		return fmt.Errorf("链接指向 %s，与记录目标 %s 不一致", linkTarget, target)
	}
	if !isDir(target) {
		return fmt.Errorf("链接目标不存在：%s", target)
	}

	report(ProgressReport{Phase: "verify", Message: "正在统计目标目录..."})
	stats, err := countTree(target, func(files int, _ int64) {
		report(ProgressReport{Phase: "verify", Message: fmt.Sprintf("正在统计目标目录... %d 个文件", files)})
	})
	if err != nil {
		return err
	}
	if st != nil && (st.Files != stats.Files || st.Bytes != stats.Bytes) {
		return fmt.Errorf("校验不一致：记录 %d 个文件 / %s，实际 %d 个文件 / %s",
			st.Files, humanSize(st.Bytes), stats.Files, humanSize(stats.Bytes))
	}
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("通过源目录无法访问数据：%w", err)
	}

	msg := fmt.Sprintf("校验通过：%d 个文件 / %s", stats.Files, humanSize(stats.Bytes))
	if st == nil {
		msg += "（未找到迁移记录，仅按链接校验）"
	}
	report(ProgressReport{
		Phase:      "done",
		Percent:    100,
		Files:      stats.Files,
		TotalFiles: stats.Files,
		Bytes:      stats.Bytes,
		TotalBytes: stats.Bytes,
		Done:       true,
		OK:         true,
		Message:    msg,
	})
	return nil
}

// RunRestore copies target data back into source and removes the link.
func RunRestore(source, target, stateFile string, report ProgressFunc) error {
	if report == nil {
		report = func(ProgressReport) {}
	}
	source = filepath.Clean(source)
	linkTarget, isLink := resolveLink(source)
	if !isLink {
		return errors.New("源目录不是链接，无法还原")
	}
	st, _ := loadStateFile(stateFile)
	if target == "" {
		if st != nil {
			target = st.Target
		} else {
			target = linkTarget
		}
	}
	if !samePath(linkTarget, target) {
		target = linkTarget
	}
	target = filepath.Clean(target)
	if !isDir(target) {
		return fmt.Errorf("链接目标不存在：%s", target)
	}
	if samePath(source, target) || isSubPath(source, target) || isSubPath(target, source) {
		return errors.New("源目录与目标目录不能互相包含")
	}

	report(ProgressReport{Phase: "prepare", Message: "正在统计目标目录..."})
	stats, err := countTree(target, func(files int, _ int64) {
		report(ProgressReport{Phase: "prepare", Message: fmt.Sprintf("正在统计目标目录... 已读取 %d 个文件", files)})
	})
	if err != nil {
		return err
	}
	if err := gameRunningError(); err != nil {
		return err
	}

	backupLink := source + fmt.Sprintf(".mover-link-%d", time.Now().Unix())
	if err := os.Rename(source, backupLink); err != nil {
		return fmt.Errorf("移走符号链接失败：%w", err)
	}
	rolledBack := false
	rollback := func() {
		if rolledBack {
			return
		}
		rolledBack = true
		_ = os.RemoveAll(source)
		_ = os.Rename(backupLink, source)
	}

	if err := os.MkdirAll(source, 0o755); err != nil {
		rollback()
		return err
	}
	if err := copyTree(target, source, stats, report); err != nil {
		rollback()
		return fmt.Errorf("复制回源目录失败：%w", err)
	}
	if err := verifyTrees(target, source, stats, report); err != nil {
		rollback()
		return fmt.Errorf("还原校验失败：%w", err)
	}

	_ = os.Remove(backupLink)

	stateMatches := st != nil && samePath(st.Source, source) && samePath(st.Target, target)
	var warnings []string
	if !stateMatches {
		warnings = append(warnings, "未找到匹配的迁移记录，已保留目标目录："+target)
	} else if now, err := countTree(target, nil); err == nil && now.Files == stats.Files && now.Bytes == stats.Bytes {
		if err := removeTree(target, stats.Files, report, "cleanup", 80, 20, "清理目标目录"); err != nil {
			warnings = append(warnings, "目标目录未能完全清理："+err.Error())
		}
		if err := removeStateFile(stateFile); err != nil {
			warnings = append(warnings, "迁移记录清理失败："+err.Error())
		}
	} else {
		warnings = append(warnings, "目标目录内容已变化，未自动删除："+target)
	}

	msg := fmt.Sprintf("还原完成：%d 个文件 / %s 已移回源目录", stats.Files, humanSize(stats.Bytes))
	if len(warnings) > 0 {
		msg += "；" + strings.Join(warnings, "；")
	}
	report(ProgressReport{
		Phase:      "done",
		Percent:    100,
		Files:      stats.Files,
		TotalFiles: stats.Files,
		Bytes:      stats.Bytes,
		TotalBytes: stats.Bytes,
		Done:       true,
		OK:         true,
		Message:    msg,
	})
	return nil
}

func gameRunningError() error {
	if pids := gameProcessIDs(); len(pids) > 0 {
		return fmt.Errorf("Limbus Company 正在运行（PID %s），操作已中止", pidList(pids))
	}
	return nil
}

// isElevated reports whether the current process has an elevated token.
func isElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
