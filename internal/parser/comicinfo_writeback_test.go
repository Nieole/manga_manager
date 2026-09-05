// 守的是回写不丢用户数据：归档里原有的那份 ComicInfo.xml——无论躺在哪一层——必须被认作合并基底
// 并原地写回，页条目与原子替换语义不受影响，rar/cbr 一律拒绝。破了就是就地改写抹掉用户自己维护的
// 元数据，而回写不可逆也不留备份。

package parser

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
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
		buf, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read entry %s: %v", f.Name, err)
		}
		out[f.Name] = string(buf)
	}
	return out
}

func TestWriteComicInfoIntoArchiveAddsAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vol01.cbz")
	writeTestCBZ(t, path, map[string]string{"001.jpg": "imgdata"})

	if err := WriteComicInfoIntoArchive(path, ComicInfo{Series: "v1"}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	entries := readArchiveEntries(t, path)
	if entries["001.jpg"] != "imgdata" {
		t.Fatalf("page entry not preserved: %q", entries["001.jpg"])
	}
	if !strings.Contains(entries["ComicInfo.xml"], "<Series>v1</Series>") {
		t.Fatalf("ComicInfo not written: %q", entries["ComicInfo.xml"])
	}

	// 二次写入应替换而非重复。
	if err := WriteComicInfoIntoArchive(path, ComicInfo{Series: "v2"}); err != nil {
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
	err := WriteComicInfoIntoArchive(path, ComicInfo{Series: "s"})
	if !errors.Is(err, ErrArchiveNotWritable) {
		t.Fatalf("expected ErrArchiveNotWritable, got %v", err)
	}
}

// strayComicInfoXML 扮演归档里的第二份 ComicInfo.xml：不是基底的那些一个字节都不该动。
const strayComicInfoXML = `<?xml version="1.0" encoding="utf-8"?>
<ComicInfo>
  <Series>另一份</Series>
  <Notes>不该被回写碰到</Notes>
</ComicInfo>`

func sortedEntryNames(entries map[string]string) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func countComicInfoEntries(entries map[string]string) int {
	count := 0
	for name := range entries {
		if strings.EqualFold(path.Base(name), comicInfoEntryName) {
			count++
		}
	}
	return count
}

// TestWriteComicInfoIntoArchiveMergesEntryAtAnyDepth 守的是读写两侧认同一份 ComicInfo.xml。
// ComicTagger 一类工具把它放在子目录，回写若只认根目录，就会拿空文档当基底、另建一份根目录副本，
// 而读侧优先读根目录——用户视角下 Characters/Year/ScanInformation/AgeRating 连同原本的 Series 全部消失。
func TestWriteComicInfoIntoArchiveMergesEntryAtAnyDepth(t *testing.T) {
	cases := []struct {
		name       string
		entries    map[string]string
		wantTarget string
		wantIntact map[string]string
	}{
		{
			name:       "根目录那份是基底",
			entries:    map[string]string{"001.jpg": "page", "ComicInfo.xml": taggedComicInfoXML},
			wantTarget: "ComicInfo.xml",
		},
		{
			name:       "子目录那份同样是基底，原地写回不另建根目录副本",
			entries:    map[string]string{"001.jpg": "page", "Scans/ComicInfo.xml": taggedComicInfoXML},
			wantTarget: "Scans/ComicInfo.xml",
		},
		{
			name:       "根目录与子目录并存时取根目录，另一份原样不动",
			entries:    map[string]string{"001.jpg": "page", "ComicInfo.xml": taggedComicInfoXML, "Scans/ComicInfo.xml": strayComicInfoXML},
			wantTarget: "ComicInfo.xml",
			wantIntact: map[string]string{"Scans/ComicInfo.xml": strayComicInfoXML},
		},
		{
			name:       "多个子目录各有一份时按层级浅优先、同层字典序取首个",
			entries:    map[string]string{"001.jpg": "page", "z/ComicInfo.xml": taggedComicInfoXML, "a/deep/ComicInfo.xml": strayComicInfoXML},
			wantTarget: "z/ComicInfo.xml",
			wantIntact: map[string]string{"a/deep/ComicInfo.xml": strayComicInfoXML},
		},
		{
			name:       "大小写变体按原名原地写回",
			entries:    map[string]string{"001.jpg": "page", "scans/comicinfo.XML": taggedComicInfoXML},
			wantTarget: "scans/comicinfo.XML",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "vol01.cbz")
			writeTestCBZ(t, archivePath, tc.entries)

			if err := WriteComicInfoIntoArchive(archivePath, ComicInfo{Series: "新系列"}); err != nil {
				t.Fatalf("回写失败: %v", err)
			}

			entries := readArchiveEntries(t, archivePath)
			if entries["001.jpg"] != "page" {
				t.Fatalf("页条目没保住: %q", entries["001.jpg"])
			}
			if got, want := countComicInfoEntries(entries), countComicInfoEntries(tc.entries); got != want {
				t.Fatalf("ComicInfo 条目数从 %d 变成 %d，归档条目 %v", want, got, sortedEntryNames(entries))
			}

			merged, ok := entries[tc.wantTarget]
			if !ok {
				t.Fatalf("合并结果没落在 %s 上，归档条目 %v", tc.wantTarget, sortedEntryNames(entries))
			}
			if !strings.Contains(merged, "<Series>新系列</Series>") {
				t.Fatalf("本项目的字段没写进 %s: %q", tc.wantTarget, merged)
			}
			for _, field := range []string{
				"<Year>1995</Year>",
				"<Characters>Kenzo Tenma, Johan Liebert</Characters>",
				"<ScanInformation>v2 scan</ScanInformation>",
				"<AgeRating>Mature 17+</AgeRating>",
			} {
				if !strings.Contains(merged, field) {
					t.Fatalf("原有字段 %s 被抹掉: %q", field, merged)
				}
			}
			for name, want := range tc.wantIntact {
				if entries[name] != want {
					t.Fatalf("非基底的 %s 被改动了: %q", name, entries[name])
				}
			}

			// 读侧是用户看到的那一份：回写写到哪，下次就必须从哪读回来。
			archive, err := OpenZip(archivePath)
			if err != nil {
				t.Fatalf("重新打开归档失败: %v", err)
			}
			defer archive.Close()
			raw, err := archive.ReadMetadataFile(comicInfoEntryName)
			if err != nil {
				t.Fatalf("读侧取不到 ComicInfo: %v", err)
			}
			if string(raw) != merged {
				t.Fatalf("读侧取到的不是回写的那一份:\n读到 %q\n写的 %q", raw, merged)
			}
			info, err := ParseComicInfo(raw)
			if err != nil {
				t.Fatalf("解析读侧结果失败: %v", err)
			}
			if info.Series != "新系列" {
				t.Fatalf("读侧看到的 Series 是 %q", info.Series)
			}
		})
	}
}
