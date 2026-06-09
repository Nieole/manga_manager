// 业务说明：本文件是业务回归测试，属于漫画文件解析层，负责识别归档、目录、页序、页数和可读取图片条目。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package parser

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkZipArchiveGetPages(b *testing.B) {
	root := b.TempDir()
	archivePath := filepath.Join(root, "bench.cbz")
	if err := writeBenchmarkCBZ(archivePath, 240); err != nil {
		b.Fatalf("write benchmark cbz failed: %v", err)
	}

	archive, err := OpenArchive(archivePath)
	if err != nil {
		b.Fatalf("open archive failed: %v", err)
	}
	defer archive.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pages, err := archive.GetPages()
		if err != nil {
			b.Fatalf("get pages failed: %v", err)
		}
		if len(pages) != 240 {
			b.Fatalf("expected 240 pages, got %d", len(pages))
		}
	}
}

func BenchmarkZipArchiveReadPage(b *testing.B) {
	root := b.TempDir()
	archivePath := filepath.Join(root, "bench.cbz")
	if err := writeBenchmarkCBZ(archivePath, 240); err != nil {
		b.Fatalf("write benchmark cbz failed: %v", err)
	}

	archive, err := OpenArchive(archivePath)
	if err != nil {
		b.Fatalf("open archive failed: %v", err)
	}
	defer archive.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := archive.ReadPage("120.png")
		if err != nil {
			b.Fatalf("read page failed: %v", err)
		}
		if len(data) == 0 {
			b.Fatal("expected non-empty page data")
		}
	}
}

func writeBenchmarkCBZ(path string, pages int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	payload := []byte("benchmark-page-payload")
	for i := 1; i <= pages; i++ {
		w, err := zw.Create(fmt.Sprintf("%03d.png", i))
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := w.Write(payload); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}
