// 守遍历对符号链接的处理：软链的系列目录必须被走进去，软链文件的 size/mtime 必须取自目标。
//
// 前者失效，软链进库根的系列（多盘位 NAS 的常见组织方式）会被整棵跳过，用户只看到「0 本新增」；
// 后者失效，目标文件被替换后增量扫描永远判定「毫无变化」，页数、封面、内嵌元数据停在首次入库。
// 环、菱形、以及指回同库内真实目录的链另需只遍历一次，否则同一批文件会以两条路径入库，变成假的重复书。

package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

func requireSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows 建软链需要特权")
	}
}

// collectWalked 把遍历到的非目录路径收集成有序切片，便于断言。
func collectWalked(t *testing.T, root string) []string {
	t.Helper()
	var got []string
	err := walkDirFollowingSymlinks(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			got = append(got, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walkDirFollowingSymlinks: %v", err)
	}
	sort.Strings(got)
	return got
}

// TestWalkFollowsSymlinkedDirectories：软链的系列目录必须被走到，且报出来的路径落在库根之下。
//
// 路径必须是链接这一侧而不是解析后的真实路径——调用方要靠 path 判定库归属、
// 派生系列目录，并把它当作 books.path 主键。报真实路径会让这些书归属到库外。
func TestWalkFollowsSymlinkedDirectories(t *testing.T) {
	requireSymlinks(t)

	root := t.TempDir()
	external := t.TempDir()

	if err := os.WriteFile(filepath.Join(external, "v1.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(external, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(external, "sub", "v2.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(root, "OnePiece")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("建不了软链：%v", err)
	}

	got := collectWalked(t, root)
	want := []string{
		filepath.Join(link, "sub", "v2.cbz"),
		filepath.Join(link, "v1.cbz"),
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("遍历到 %v，期望 %v —— 软链的系列目录整棵被跳过了", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("遍历到 %v，期望 %v —— 报出的路径必须落在库根之下（链接这一侧）", got, want)
		}
	}
}

// TestWalkUsesSymlinkTargetInfo：软链文件的 d.Info() 必须是**目标**的 size/mtime。
//
// 这是增量扫描的判据来源。取到链接自身的属性，目标被替换后扫描器会认为毫无变化。
func TestWalkUsesSymlinkTargetInfo(t *testing.T) {
	requireSymlinks(t)

	root := t.TempDir()
	external := t.TempDir()
	target := filepath.Join(external, "real.cbz")
	payload := []byte("0123456789abcdef")
	if err := os.WriteFile(target, payload, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// 把目标的 mtime 推到一个明显不同的时刻，让「链接的 mtime」与「目标的 mtime」可区分。
	want := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(target, want, want); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	link := filepath.Join(root, "linked.cbz")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("建不了软链：%v", err)
	}

	var gotSize int64 = -1
	var gotMod time.Time
	err := walkDirFollowingSymlinks(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			t.Fatalf("d.Info(): %v", infoErr)
		}
		gotSize, gotMod = info.Size(), info.ModTime()
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if gotSize != int64(len(payload)) {
		t.Errorf("d.Info().Size() = %d, want %d —— 拿到的是链接自身的大小，增量比对会失真",
			gotSize, len(payload))
	}
	if !gotMod.Truncate(time.Second).Equal(want) {
		t.Errorf("d.Info().ModTime() = %v, want %v —— 链接自身的 mtime 只在重建链接时才变，"+
			"用它做增量判据会让目标文件的更新永远被跳过", gotMod, want)
	}
}

// TestWalkTerminatesOnSymlinkCycle：互指的软链不能让遍历打转。
func TestWalkTerminatesOnSymlinkCycle(t *testing.T) {
	requireSymlinks(t)

	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.Symlink(b, filepath.Join(a, "toB")); err != nil {
		t.Skipf("建不了软链：%v", err)
	}
	if err := os.Symlink(a, filepath.Join(b, "toA")); err != nil {
		t.Skipf("建不了软链：%v", err)
	}
	if err := os.WriteFile(filepath.Join(a, "v1.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	done := make(chan []string, 1)
	go func() { done <- collectWalked(t, root) }()
	select {
	case got := <-done:
		// 只要能返回就说明防环生效；顺带确认真实文件没有因为防环被漏掉。
		found := false
		for _, p := range got {
			if filepath.Base(p) == "v1.cbz" {
				found = true
			}
		}
		if !found {
			t.Errorf("防环把真实文件也挡掉了：%v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("软链成环导致遍历不终止")
	}
}

// TestWalkVisitsSharedTargetOnce：两个链接指向同一目录时只遍历一次。
//
// 遍历两次的后果不是慢，而是同一批文件以两个不同路径入库，在去重视图里变成一堆假的重复书。
func TestWalkVisitsSharedTargetOnce(t *testing.T) {
	requireSymlinks(t)

	root := t.TempDir()
	shared := t.TempDir()
	if err := os.WriteFile(filepath.Join(shared, "v1.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, name := range []string{"link1", "link2"} {
		if err := os.Symlink(shared, filepath.Join(root, name)); err != nil {
			t.Skipf("建不了软链：%v", err)
		}
	}

	got := collectWalked(t, root)
	if len(got) != 1 {
		t.Fatalf("遍历到 %v —— 同一目录被两个链接各走一遍，会造出假的重复书", got)
	}
}

// TestWalkSkipsBrokenSymlink：失效链接跳过即可，不该让整次扫描看起来出了故障。
func TestWalkSkipsBrokenSymlink(t *testing.T) {
	requireSymlinks(t)

	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "nope"), filepath.Join(root, "dangling.cbz")); err != nil {
		t.Skipf("建不了软链：%v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "real.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := collectWalked(t, root)
	if len(got) != 1 || filepath.Base(got[0]) != "real.cbz" {
		t.Fatalf("遍历到 %v，期望只有 real.cbz", got)
	}
}

// TestScanImportsSymlinkedSeriesDirectory 是端到端判据：软链进来的系列必须真的入库，
// 且书籍路径落在库根之下（否则库归属、系列派生、删除清理全都会错位）。
func TestScanImportsSymlinkedSeriesDirectory(t *testing.T) {
	requireSymlinks(t)

	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()

	external := t.TempDir()
	if err := writeScannerTestCBZ(filepath.Join(external, "v1.cbz"),
		map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz: %v", err)
	}
	link := filepath.Join(libraryPath, "External Series")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("建不了软链：%v", err)
	}

	s := newFormatTestScanner(t, store)
	if err := s.ScanLibrary(ctx, lib.ID, lib.Path, false, nil); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	books, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ListBooksByLibrary: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("软链进来的系列没有入库（%d 本）—— 用户只会看到「扫描完成，0 本新增」", len(books))
	}
	if got := books[0].Path; got != filepath.Join(link, "v1.cbz") {
		t.Fatalf("books.path = %q，期望落在库根之下的链接路径 %q —— "+
			"记真实路径会让这本书归属到库外，删库/清理都定位不到它",
			got, filepath.Join(link, "v1.cbz"))
	}
}

// TestWatchRegistersSymlinkedDirectories：监听侧必须与扫描侧同口径。
// 扫描能看到、监听看不到的话，软链进来的系列改了文件永远不会触发热重载。
func TestWatchRegistersSymlinkedDirectories(t *testing.T) {
	requireSymlinks(t)

	fw := newLifecycleWatcher(t)
	t.Cleanup(fw.Stop)

	root := t.TempDir()
	external := t.TempDir()
	link := filepath.Join(root, "linked")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("建不了软链：%v", err)
	}

	report := fw.watchRecursive(root)
	if !report.OK() {
		t.Fatalf("注册报告不该判为失败：%+v", report)
	}
	if report.SymlinkDirs != 1 {
		t.Errorf("SymlinkDirs = %d, want 1 —— 软链目录要单独可见，出问题时省一轮排查", report.SymlinkDirs)
	}
	fw.mu.Lock()
	_, watched := fw.watched[link]
	fw.mu.Unlock()
	if !watched {
		t.Error("软链目录没有被监听 —— 扫描能看到、监听看不到，这类系列的改动永远不触发热重载")
	}
}

// TestWalkVisitsInLibraryTargetOnce：软链指向**同一棵被遍历的树内**的真实目录时，
// 那批文件只能被遍历一次，且报出的是真实目录那条路径。
//
// 按作者/按状态做二次组织时很容易建出这种链（<库根>/Alpha (link) -> <库根>/Series Alpha）。
// 走两遍的后果是同一个物理文件以两条路径入库，变成两本书、两个系列，book_count 翻倍。
// 留真实目录那条：软链是随手建的组织视图，删掉它不该让整个系列在下次扫描时消失。
func TestWalkVisitsInLibraryTargetOnce(t *testing.T) {
	requireSymlinks(t)

	// 前两个子测试只差链接名的字典序，用来确认留下来的那条路径与遍历顺序无关；
	// 第三个把链目标写成另一种大小写——大小写不敏感的文件系统上它仍指向同一个目录。
	cases := []struct {
		name string
		link string
		// target 是写进软链里的目标目录名，留空表示照抄真实目录的写法。
		target string
	}{
		{name: "链名排在真实目录之前", link: "Alpha (link)"},
		{name: "链名排在真实目录之后", link: "zzz (link)"},
		{name: "链目标与真实目录的大小写不同", link: "Alpha (link)", target: "series alpha"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			real := filepath.Join(root, "Series Alpha")
			if err := os.MkdirAll(real, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(real, "Vol 01.cbz"), []byte("x"), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			target := real
			if tc.target != "" {
				target = filepath.Join(root, tc.target)
			}
			if err := os.Symlink(target, filepath.Join(root, tc.link)); err != nil {
				t.Skipf("建不了软链：%v", err)
			}
			if _, err := os.Stat(filepath.Join(root, tc.link)); err != nil {
				t.Skipf("文件系统大小写敏感，%q 在这里是断链：%v", tc.target, err)
			}

			got := collectWalked(t, root)
			want := []string{filepath.Join(real, "Vol 01.cbz")}
			if len(got) != len(want) || got[0] != want[0] {
				t.Fatalf("遍历到 %v，期望 %v —— 同一个物理文件被两条路径各走一遍，会入库成两本书、两个系列", got, want)
			}
		})
	}
}

// TestWalkVisitsExternalTargetPerRoot：两个资料库各有一条软链指向同一个库外目录时，
// 两次遍历都必须看到那批文件——文档鼓励的正是这种「软链指向外置盘」的用法。
//
// 去重集合的作用域是一次遍历，跨库不共享；两个库各自建立自己的藏书，谁也不吞掉谁。
func TestWalkVisitsExternalTargetPerRoot(t *testing.T) {
	requireSymlinks(t)

	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "v1.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, name := range []string{"库一", "库二"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			link := filepath.Join(root, "Shared")
			if err := os.Symlink(external, link); err != nil {
				t.Skipf("建不了软链：%v", err)
			}
			got := collectWalked(t, root)
			want := []string{filepath.Join(link, "v1.cbz")}
			if len(got) != len(want) || got[0] != want[0] {
				t.Fatalf("遍历到 %v，期望 %v —— 跨库去重会把第二个库的内容整个吞掉", got, want)
			}
		})
	}
}

// TestScanSkipsInLibrarySymlinkToRealDirectory 是端到端判据：库内软链指向同库内的真实目录时，
// 磁盘上的一个文件只能入库成一本书、一个系列。
//
// 重复入库会让去重、统计、阅读进度各算各的，book_count 也跟着翻倍。
func TestScanSkipsInLibrarySymlinkToRealDirectory(t *testing.T) {
	requireSymlinks(t)

	// target 是写进软链里的目标目录名，留空表示照抄真实目录的写法。
	cases := []struct{ name, target string }{
		{name: "链目标与真实目录写法相同"},
		{name: "链目标与真实目录的大小写不同", target: "series alpha"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { scanInLibrarySymlinkOnce(t, tc.target) })
	}
}

// scanInLibrarySymlinkOnce 建一个「真实系列目录 + 指回它的库内软链」的库，扫完断言只入库一本书、
// 一个系列，且路径是真实目录那条。target 非空时软链按该写法指向真实目录。
func scanInLibrarySymlinkOnce(t *testing.T, target string) {
	t.Helper()

	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()

	real := filepath.Join(libraryPath, "Series Alpha")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeScannerTestCBZ(filepath.Join(real, "Vol 01.cbz"),
		map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz: %v", err)
	}
	linkTarget := real
	if target != "" {
		linkTarget = filepath.Join(libraryPath, target)
	}
	link := filepath.Join(libraryPath, "Alpha (link)")
	if err := os.Symlink(linkTarget, link); err != nil {
		t.Skipf("建不了软链：%v", err)
	}
	if _, err := os.Stat(link); err != nil {
		t.Skipf("文件系统大小写敏感，%q 在这里是断链：%v", target, err)
	}

	s := newFormatTestScanner(t, store)
	if err := s.ScanLibrary(ctx, lib.ID, lib.Path, false, nil); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	books, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ListBooksByLibrary: %v", err)
	}
	if len(books) != 1 {
		paths := make([]string, 0, len(books))
		for _, b := range books {
			paths = append(paths, b.Path)
		}
		t.Fatalf("入库 %d 本：%v —— 磁盘上只有 1 个文件，软链把它又走了一遍", len(books), paths)
	}
	if want := filepath.Join(real, "Vol 01.cbz"); books[0].Path != want {
		t.Errorf("books.path = %q，期望真实目录那条 %q —— 记链接路径的话，用户删掉那条组织用的软链，整个系列就没了",
			books[0].Path, want)
	}

	seriesList, err := store.ListSeriesByLibraryLite(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ListSeriesByLibraryLite: %v", err)
	}
	if len(seriesList) != 1 {
		paths := make([]string, 0, len(seriesList))
		for _, se := range seriesList {
			paths = append(paths, se.Path)
		}
		t.Fatalf("入库 %d 个系列：%v —— 同一部作品被拆成两个，book_count、去重与阅读进度各算各的",
			len(seriesList), paths)
	}
}

// TestClaimDirKeepsDistinctDirsInSameBucket 是反向判据：折叠大小写只用来分桶，不能用来判等。
//
// 大小写敏感的文件系统上 /Data 与 /data 是两个不同的目录（很常见的两个挂载点），折叠后同桶。
// 若同桶就算同一个，第二棵会被整个跳过——那是漏扫一整棵树，比重复入库更难被发现。
// 这里手工把另一个真实目录塞进同一个桶，因此不依赖运行环境的文件系统是否大小写敏感。
func TestClaimDirKeepsDistinctDirsInSameBucket(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	visited := make(map[string][]string)
	if !claimDir(first, visited) {
		t.Fatal("第一个目录就没登记上")
	}
	// 显式制造桶冲突：把 first 挂到 second 的桶键上，等价于两条只差大小写的路径撞在一起。
	firstKey, _ := dirClaimKey(first)
	secondKey, _ := dirClaimKey(second)
	visited[secondKey] = append(visited[secondKey], visited[firstKey]...)

	if !claimDir(second, visited) {
		t.Fatal("两个不同的真实目录被折叠成同一个 —— 大小写敏感的文件系统上会漏扫一整棵树")
	}
	if claimDir(first, visited) {
		t.Fatal("同一个目录被登记了两次 —— 去重落空，同一批文件会以两条路径入库")
	}
}
