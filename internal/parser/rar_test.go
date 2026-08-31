// 本文件用已提交的 RAR 夹具（testdata/*.cbr，由 rar 一次性生成）回归 cbr/rar 阅读路径。
// 夹具是二进制、只读，CI 无需任何 rar 工具即可用纯 Go 的 rardecode 读取；覆盖过滤 / 自然排序 / 精确读页 /
// 元数据读取与探测的解压范围，以及会话缓存下的顺序 / 随机 / 反向跳读 / 并发读取正确性。

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

// BenchmarkRarSequentialReadAllPages 度量顺序读全 40 页的成本：会话缓存下整卷是一次前向扫描（O(N)）。
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

// cachedEntryNames 快照会话缓存中已解压的条目名（按读入顺序），用作「哪些条目被解压了」的结构化判据——
// 比耗时阈值稳：与机器性能无关。
func cachedEntryNames(t *testing.T, arc Archive) []string {
	t.Helper()
	ra, ok := arc.(*RarArchive)
	if !ok {
		t.Fatalf("archive is %T, want *RarArchive", arc)
	}
	ra.mu.Lock()
	defer ra.mu.Unlock()
	return append([]string(nil), ra.cacheOrder...)
}

// seenEntryNames 快照当前游标已途经的条目名，用作「游标有没有被打回开头」的判据（重开会清空该集合）。
func seenEntryNames(t *testing.T, arc Archive) map[string]bool {
	t.Helper()
	ra, ok := arc.(*RarArchive)
	if !ok {
		t.Fatalf("archive is %T, want *RarArchive", arc)
	}
	ra.mu.Lock()
	defer ra.mu.Unlock()
	seen := make(map[string]bool, len(ra.seen))
	for name := range ra.seen {
		seen[name] = true
	}
	return seen
}

func equalNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestRarArchiveMetadataProbeDoesNotDecompressPages 锁定「为什么打开这个归档」的区分：元数据探测只解压命中的
// 那一个条目，阅读取页仍预取并缓存途经图片页。夹具 vol.cbr 的物理条目序为
// 1.jpg / 2.jpg / 10.jpg / cover.jpg / .hidden.jpg / readme.txt / ComicInfo.xml。
func TestRarArchiveMetadataProbeDoesNotDecompressPages(t *testing.T) {
	t.Run("探测命中时只解压命中的那一个条目", func(t *testing.T) {
		arc, err := OpenArchive(testRarPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer arc.Close()

		got, err := arc.ReadMetadataFile("ComicInfo.xml")
		if err != nil || string(got) != "<ComicInfo/>" {
			t.Fatalf("ReadMetadataFile(ComicInfo.xml) = %q,%v", got, err)
		}
		if cached := cachedEntryNames(t, arc); !equalNames(cached, []string{"ComicInfo.xml"}) {
			t.Fatalf("元数据探测后被解压缓存的条目 = %v, want [ComicInfo.xml]（图片页不应被顺带解压）", cached)
		}
	})

	t.Run("探测未命中时不解压任何条目", func(t *testing.T) {
		arc, err := OpenArchive(testRarPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer arc.Close()

		if _, err := arc.ReadMetadataFile("NoSuchInfo.xml"); err == nil {
			t.Fatal("expected error probing missing metadata file")
		}
		if cached := cachedEntryNames(t, arc); len(cached) != 0 {
			t.Fatalf("未命中的元数据探测后被解压缓存的条目 = %v, want []（整卷不应被解压）", cached)
		}
	})

	t.Run("阅读路径仍预取并缓存途经图片页", func(t *testing.T) {
		arc, err := OpenArchive(testRarPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer arc.Close()

		if got, err := arc.ReadPage("10.jpg"); err != nil || string(got) != "j10" {
			t.Fatalf("ReadPage(10.jpg) = %q,%v", got, err)
		}
		// 前滚途中的图片页要留在缓存里，下一页才是 O(1)——这是会话缓存存在的理由，不能被本次修复改没。
		if cached := cachedEntryNames(t, arc); !equalNames(cached, []string{"1.jpg", "2.jpg", "10.jpg"}) {
			t.Fatalf("读页后被缓存的条目 = %v, want [1.jpg 2.jpg 10.jpg]（阅读路径的预取不得退化）", cached)
		}
	})

	t.Run("探测元数据不打回阅读游标", func(t *testing.T) {
		arc, err := OpenArchive(testRarPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer arc.Close()

		if _, err := arc.ReadPage("2.jpg"); err != nil {
			t.Fatalf("ReadPage(2.jpg): %v", err)
		}
		before := seenEntryNames(t, arc)
		if !before["1.jpg"] || !before["2.jpg"] {
			t.Fatalf("读页后游标已途经的条目 = %v, want 含 1.jpg 与 2.jpg", before)
		}
		if _, err := arc.ReadMetadataFile("ComicInfo.xml"); err != nil {
			t.Fatalf("ReadMetadataFile: %v", err)
		}
		// 判据取「集合完全不变」而不是「仍含读过的页」：旧实现把游标打回开头后又一路扫到末尾的
		// ComicInfo.xml，途中会把读过的页重新记为已途经，只查包含关系看不出游标动过。
		after := seenEntryNames(t, arc)
		if len(after) != len(before) {
			t.Fatalf("元数据探测后游标已途经的条目 = %v, want 与探测前一致 %v（探测不应动阅读会话的游标）", after, before)
		}
		for name := range before {
			if !after[name] {
				t.Fatalf("元数据探测后游标已途经的条目 = %v, want 与探测前一致 %v（探测不应动阅读会话的游标）", after, before)
			}
		}
		// 游标未动，后续续读仍要正确。
		if got, err := arc.ReadPage("10.jpg"); err != nil || string(got) != "j10" {
			t.Fatalf("探测后 ReadPage(10.jpg) = %q,%v", got, err)
		}
	})
}
