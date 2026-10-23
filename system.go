package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const gameProcessName = "LimbusCompany.exe"

// defaultSourcePath returns the Unity LocalLow folder where Limbus Company
// stores its downloaded AssetBundles.
func defaultSourcePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, "AppData", "LocalLow", "Unity", "ProjectMoon_LimbusCompany")
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// suggestTarget picks the first secondary volume that can hold the bundles.
func suggestTarget() string {
	for _, drive := range []string{"D:", "E:", "F:", "G:"} {
		root := drive + `\`
		if isDir(root) {
			return filepath.Join(root, "LimbusCompanyAssets")
		}
	}
	return ""
}

func volumeOf(path string) string {
	v := filepath.VolumeName(path)
	if v == "" {
		return ""
	}
	return strings.ToUpper(v)
}

// diskFree reports the free and total bytes of the volume that holds path.
func diskFree(path string) (free, total uint64, ok bool) {
	if path == "" {
		return 0, 0, false
	}
	root := volumeOf(path) + `\`
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, 0, false
	}
	var freeToCaller, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &totalBytes, &totalFree); err != nil {
		return 0, 0, false
	}
	return totalFree, totalBytes, true
}

// scanDir totals the regular files below root. Symlinks and junctions are not
// followed, so a relocated folder cannot send the scan into a loop.
func scanDir(root string) (size int64, files int, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path != root && isReparsePoint(path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		size += info.Size()
		files++
		return nil
	})
	return size, files, err
}

// gameProcessIDs lists every running process named LimbusCompany.exe.
func gameProcessIDs() []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil
	}

	var pids []uint32
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, gameProcessName) {
			pids = append(pids, entry.ProcessID)
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return pids
}

func terminateProcess(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

func pidList(pids []uint32) string {
	parts := make([]string, len(pids))
	for i, pid := range pids {
		parts[i] = strconv.FormatUint(uint64(pid), 10)
	}
	return strings.Join(parts, "、")
}

func humanSize(n int64) string {
	if n < 0 {
		return "—"
	}
	units := [...]string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d %s", n, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// existingDir walks up from path until it finds a directory that exists, so a
// folder picker always opens somewhere useful.
func existingDir(path string) string {
	for path != "" {
		if isDir(path) {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return ""
}
