package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.bundle"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.bundle"), make([]byte, 50), 0o644); err != nil {
		t.Fatal(err)
	}

	size, files, err := scanDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if size != 150 || files != 2 {
		t.Fatalf("scanDir = %d bytes, %d files; want 150 bytes, 2 files", size, files)
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0:       "0 B",
		1023:    "1023 B",
		1024:    "1.0 KB",
		1536:    "1.5 KB",
		1 << 30: "1.0 GB",
		3 << 30: "3.0 GB",
		1 << 40: "1.0 TB",
	}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultSourcePath(t *testing.T) {
	got := filepath.Base(defaultSourcePath())
	if got != "ProjectMoon_LimbusCompany" {
		t.Fatalf("default source folder = %q, want ProjectMoon_LimbusCompany", got)
	}
}

func TestCopyAndVerifyTree(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "AssetBundles", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "AssetBundles", "a.bundle"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "AssetBundles", "sub", "中文资源.bundle"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := countTree(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 || stats.Bytes != 1028 {
		t.Fatalf("countTree = %+v, want 2 files / 1028 bytes", stats)
	}

	dst := t.TempDir()
	if err := copyTree(src, dst, stats, nil); err != nil {
		t.Fatal(err)
	}
	if err := verifyTrees(src, dst, stats, nil); err != nil {
		t.Fatalf("verifyTrees failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dst, "extra.bundle"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyTrees(src, dst, stats, nil); err == nil {
		t.Fatal("verifyTrees accepted an extra file")
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := &RelocationState{
		Source:      `C:\Users\im\AppData\LocalLow\Unity\ProjectMoon_LimbusCompany`,
		Target:      `D:\LimbusCompanyAssets`,
		Files:       12,
		Bytes:       4096,
		RelocatedAt: time.Now().Truncate(time.Second),
		LinkType:    "symlink",
	}
	if err := saveStateFile(path, st); err != nil {
		t.Fatal(err)
	}
	got, err := loadStateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Target != st.Target || got.Files != st.Files || got.Bytes != st.Bytes {
		t.Fatalf("state round trip = %+v, want %+v", got, st)
	}
	if err := removeStateFile(path); err != nil {
		t.Fatal(err)
	}
	got, err = loadStateFile(path)
	if err != nil || got != nil {
		t.Fatalf("state should be gone, got %+v, err %v", got, err)
	}
}

func TestPathRelations(t *testing.T) {
	if !isSubPath(`C:\a\b`, `C:\a`) {
		t.Fatal("isSubPath missed a child")
	}
	if isSubPath(`C:\ab`, `C:\a`) {
		t.Fatal("isSubPath matched a sibling prefix")
	}
	if !samePath(`C:\A\B`, `c:\a\b`) {
		t.Fatal("samePath should be case-insensitive")
	}
}
