// 业务说明：本文件用已提交的 RAR 夹具（testdata/*.cbr，由 rar 一次性生成）回归 cbr/rar 阅读路径。
// 夹具是二进制、只读，CI 无需任何 rar 工具即可用纯 Go 的 rardecode 读取；覆盖过滤 / 自然排序 / 精确读页 /
// 元数据读取，以及会话缓存下的顺序 / 随机 / 反向跳读 / 并发读取正确性。

package parser

import (
	"fmt"
	"sync"
	"testing"
)

const testRarPath = "testdata/vol.cbr"

func TestRarArchiveGetPagesFiltersAndSorts(t *testing.T) {
	arc, err := OpenArchive(testRarPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer arc.Close()

	pages, err := arc.GetPages()
	if err != nil {
		t.Fatalf("GetPages: %v", err)
	}
	gotNames := make([]string, len(pages))
	for i, p := range pages {
		gotNames[i] = p.Name
	}
	// 过滤隐藏 (.hidden.jpg) / 非图片 (readme.txt) / 元数据 (ComicInfo.xml)；封面优先 + 自然排序。
	want := []string{"cover.jpg", "1.jpg", "2.jpg", "10.jpg"}
	if len(gotNames) != len(want) {
		t.Fatalf("GetPages = %v, want %v", gotNames, want)
	}
	for i := range want {
		if gotNames[i] != want[i] {
			t.Fatalf("page order[%d] = %q, want %q (all=%v)", i, gotNames[i], want[i], gotNames)
		}
	}
	if pages[0].MediaType != "image/jpeg" {
		t.Fatalf("cover media type = %q, want image/jpeg", pages[0].MediaType)
	}
}

func TestRarArchiveReadPageAndMetadata(t *testing.T) {
	arc, err := OpenArchive(testRarPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer arc.Close()

	// 精确命中：每页字节可区分。
	cases := map[string]string{"1.jpg": "j1", "2.jpg": "j2", "10.jpg": "j10", "cover.jpg": "cov"}
	for name, want := range cases {
		got, err := arc.ReadPage(name)
		if err != nil || string(got) != want {
			t.Fatalf("ReadPage(%q) = %q,%v, want %q", name, got, err, want)
		}
	}
	// 缺失页报错。
	if _, err := arc.ReadPage("nope.jpg"); err == nil {
		t.Fatal("expected error reading missing page")
	}
	// 元数据可读。
	if got, err := arc.ReadMetadataFile("ComicInfo.xml"); err != nil || string(got) != "<ComicInfo/>" {
		t.Fatalf("ReadMetadataFile(ComicInfo.xml) = %q,%v", got, err)
	}
}

// TestRarArchiveReadOrdersConsistent 验证任意读取顺序都返回正确字节：顺序、随机、反向跳读、重复读。
// 会话缓存实现后，这些顺序都应与逐次全档重开的结果一致（缓存不得串页 / 返回陈旧字节）。
func TestRarArchiveReadOrdersConsistent(t *testing.T) {
	arc, err := OpenArchive(testRarPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer arc.Close()

	want := map[string]string{"1.jpg": "j1", "2.jpg": "j2", "10.jpg": "j10", "cover.jpg": "cov"}
	// 顺序 → 反向 → 随机跳读 → 重复读，全部必须正确。
	orders := [][]string{
		{"cover.jpg", "1.jpg", "2.jpg", "10.jpg"},          // forward
		{"10.jpg", "2.jpg", "1.jpg", "cover.jpg"},          // backward
		{"2.jpg", "cover.jpg", "10.jpg", "1.jpg", "2.jpg"}, // random + repeat
	}
	for _, order := range orders {
		for _, name := range order {
			got, err := arc.ReadPage(name)
			if err != nil || string(got) != want[name] {
				t.Fatalf("order %v: ReadPage(%q) = %q,%v, want %q", order, name, got, err, want[name])
			}
		}
	}
}

// TestRarArchiveConcurrentReadsSafe 验证并发读取同一档不串页、无数据竞争（配合 -race）。
func TestRarArchiveConcurrentReadsSafe(t *testing.T) {
	arc, err := OpenArchive(testRarPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer arc.Close()

	want := map[string]string{"1.jpg": "j1", "2.jpg": "j2", "10.jpg": "j10", "cover.jpg": "cov"}
	names := []string{"cover.jpg", "1.jpg", "2.jpg", "10.jpg"}

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 4; i++ {
				name := names[(g+i)%len(names)]
				got, err := arc.ReadPage(name)
				if err != nil || string(got) != want[name] {
					errs <- fmt.Errorf("ReadPage(%q) = %q,%v, want %q", name, got, err, want[name])
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}

// TestRarArchiveSessionCacheEvictionCorrect 收紧会话缓存到极小，强制读入即淘汰，验证淘汰后顺序/反向/随机
// 跳读仍全部正确（反向读已淘汰页会走重开路径）。用 40 页的 bench.cbr。
func TestRarArchiveSessionCacheEvictionCorrect(t *testing.T) {
	orig := rarPageCacheMaxBytes
	rarPageCacheMaxBytes = 8 // 小于单页内容，几乎立即淘汰
	defer func() { rarPageCacheMaxBytes = orig }()

	arc, err := OpenArchive("testdata/bench.cbr")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer arc.Close()

	check := func(n int) {
		t.Helper()
		name := fmt.Sprintf("page%02d.jpg", n)
		want := fmt.Sprintf("page-%02d-payload", n)
		got, err := arc.ReadPage(name)
		if err != nil || string(got) != want {
			t.Fatalf("ReadPage(%q) = %q,%v, want %q", name, got, err, want)
		}
	}
	for i := 1; i <= 40; i++ { // 顺序
		check(i)
	}
	for i := 40; i >= 1; i-- { // 反向（每次都需重开）
		check(i)
	}
	for _, i := range []int{7, 33, 1, 40, 19, 7} { // 随机 + 重复
		check(i)
	}
}

// TestRarArchiveSequentialReadAll 顺序读全 40 页（默认缓存），全部内容正确——顺序阅读的常见路径。
func TestRarArchiveSequentialReadAll(t *testing.T) {
	arc, err := OpenArchive("testdata/bench.cbr")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer arc.Close()
	for i := 1; i <= 40; i++ {
		name := fmt.Sprintf("page%02d.jpg", i)
		want := fmt.Sprintf("page-%02d-payload", i)
		got, err := arc.ReadPage(name)
		if err != nil || string(got) != want {
			t.Fatalf("ReadPage(%q) = %q,%v, want %q", name, got, err, want)
		}
	}
}

// BenchmarkRarSequentialReadAllPages 度量顺序读全 40 页的成本。会话缓存下整卷是一次前向扫描（O(N)）；
// 此前每次 ReadPage 重开并从头扫描是 O(N²)。
func BenchmarkRarSequentialReadAllPages(b *testing.B) {
	names := make([]string, 40)
	for i := 1; i <= 40; i++ {
		names[i-1] = fmt.Sprintf("page%02d.jpg", i)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		arc, err := OpenArchive("testdata/bench.cbr")
		if err != nil {
			b.Fatal(err)
		}
		for _, name := range names {
			if _, err := arc.ReadPage(name); err != nil {
				b.Fatal(err)
			}
		}
		arc.Close()
	}
}
