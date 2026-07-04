// 业务说明：本文件是业务回归测试，覆盖 ComicInfo.xml 写回 zip/cbz 归档的行为。
// 保护点：保留原有页条目、替换而非重复写入 ComicInfo、原子替换后归档仍可正常打开、rar/cbr 拒绝写入。

package parser

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTestCBZ(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create cbz: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func readArchiveEntries(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer r.Close()
	out := make(map[string]string)
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		buf := make([]byte, f.UncompressedSize64)
		_, _ = rc.Read(buf)
		rc.Close()
		out[f.Name] = string(buf)
	}
	return out
}

func TestWriteComicInfoIntoArchiveAddsAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vol01.cbz")
	writeTestCBZ(t, path, map[string]string{"001.jpg": "imgdata"})

	if err := WriteComicInfoIntoArchive(path, []byte("<ComicInfo>v1</ComicInfo>")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	entries := readArchiveEntries(t, path)
	if entries["001.jpg"] != "imgdata" {
		t.Fatalf("page entry not preserved: %q", entries["001.jpg"])
	}
	if entries["ComicInfo.xml"] != "<ComicInfo>v1</ComicInfo>" {
		t.Fatalf("ComicInfo not written: %q", entries["ComicInfo.xml"])
	}

	// 二次写入应替换而非重复。
	if err := WriteComicInfoIntoArchive(path, []byte("<ComicInfo>v2</ComicInfo>")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r.Close()
	comicInfoCount := 0
	for _, f := range r.File {
		if f.Name == "ComicInfo.xml" {
			comicInfoCount++
		}
	}
	if comicInfoCount != 1 {
		t.Fatalf("expected exactly one ComicInfo.xml, got %d", comicInfoCount)
	}
}

func TestWriteComicInfoIntoArchiveRejectsRar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vol01.cbr")
	if err := os.WriteFile(path, []byte("not a real rar"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := WriteComicInfoIntoArchive(path, []byte("<ComicInfo/>"))
	if !errors.Is(err, ErrArchiveNotWritable) {
		t.Fatalf("expected ErrArchiveNotWritable, got %v", err)
	}
}
