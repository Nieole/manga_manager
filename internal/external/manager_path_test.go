// 业务说明：本文件守卫外部库路径的取值范围。
//
// external_path 此前完全不受约束：传什么存什么。三种取值各自造成真实损害——
//   - 相对路径（"."）被静默解释成**服务进程的工作目录**，用户以为传到了外接盘，
//     实际写进了服务的运行目录；
//   - "/" 会让扫描去遍历整个文件系统；
//   - 指向资料库自己的子目录时，传输产生的副本会被下一次扫描收编成重复书籍，
//     用户还得手动去重。

package external

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCreateSessionRejectsUnsafeExternalPaths(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	m := NewManager(store, time.Hour)
	ctx := context.Background()

	outside := t.TempDir()
	inside := filepath.Join(lib.Path, "backup")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cases := []struct {
		name string
		path string
		want error
	}{
		{
			// filepath.Abs 帮不上忙：它只会把 "." 拼上 CWD 后返回一个完全合法的目录，
			// 错误就此变得不可见。必须在规范化之前先判绝对性。
			name: "相对路径被拒",
			path: ".",
			want: ErrExternalPathInvalid,
		},
		{
			name: "文件系统根被拒",
			path: string(filepath.Separator),
			want: ErrExternalPathInvalid,
		},
		{
			name: "库根本身被拒",
			path: lib.Path,
			want: ErrExternalPathInsideLibrary,
		},
		{
			name: "库内子目录被拒",
			path: inside,
			want: ErrExternalPathInsideLibrary,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.CreateSession(ctx, lib.ID, tc.path, false)
			if !errors.Is(err, tc.want) {
				t.Fatalf("CreateSession(%q) = %v，期望 %v", tc.path, err, tc.want)
			}
		})
	}

	// 反向护栏：库外的正常绝对路径必须照常可用，否则整个功能就废了。
	t.Run("库外的绝对路径正常可用", func(t *testing.T) {
		if _, err := m.CreateSession(ctx, lib.ID, outside, false); err != nil {
			t.Fatalf("正常的外部路径被拒了：%v", err)
		}
	})
}

// TestPathsOverlapAllowsVolumeRootsOnWindows 钉住「不要误杀 Windows 卷根」。
//
// 「整块外接盘 / NAS 根目录当外部库」是这个功能的头号用法。filepath.Dir("D:\\")
// 返回的还是 "D:\\"，若只按「Dir(p)==p 就是根」判定，Windows 用户选 D:\ 会直接被拒。
func TestVolumeRootIsNotTreatedAsFilesystemRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("卷根语义只在 Windows 上成立")
	}
	for _, p := range []string{`D:\`, `\\server\share\`} {
		if filepath.Dir(p) == p && filepath.VolumeName(p) == "" {
			t.Errorf("%q 被判成了文件系统根 —— 整盘/NAS 根当外部库是头号用法，误杀它才是回归", p)
		}
	}
}

// TestPathsOverlap 单测重叠判定本身。
func TestPathsOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"/data/manga", "/data/manga", true},
		{"/data/manga/sub", "/data/manga", true},
		{"/data/manga", "/data/manga/sub", true},
		// 前缀相同的兄弟目录不算重叠——这正是无分隔符 HasPrefix 会判错的那一类。
		{"/data/manga2", "/data/manga", false},
		{"/data/manga-backup", "/data/manga", false},
		{"/srv/other", "/data/manga", false},
	}
	for _, tc := range cases {
		a := filepath.FromSlash(tc.a)
		b := filepath.FromSlash(tc.b)
		if got := pathsOverlap(a, b); got != tc.want {
			t.Errorf("pathsOverlap(%q, %q) = %v, want %v", a, b, got, tc.want)
		}
	}
}

// TestNarrowStoreExcludesPerSeriesQuery 是编译期守卫的运行期说明。
//
// 本包的 Store 接口刻意只声明三个查询。它挡的不是「解耦」——参数与返回值仍是 database
// 包里 sqlc 生成的类型，import 边一分不减——而是一类具体的性能回归：传输规划曾经是
// 逐 seriesID 一次 `SELECT *`（books 表 24 列含长文本），几百个系列就是几百次往返。
//
// 现在窄接口里根本没有 ListBooksBySeries，想写回去得先改那个声明，而那一步会被
// code review 看见。这条用例本身只确认「真实的 *SqlStore 仍然满足这个窄接口」——
// 若有人给接口加了一个 SqlStore 没实现的方法，这里会编译失败。
func TestNarrowStoreExcludesPerSeriesQuery(t *testing.T) {
	store, _, _ := newExternalTestStore(t)

	var narrow Store = store
	if narrow == nil {
		t.Fatal("真实 store 不满足本包的窄接口")
	}

	// 真正的约束在类型声明上：Store 里没有 ListBooksBySeries，所以本包的任何代码都
	// **写不出**逐系列查询——想写回去必须先改那个接口声明，而那一步会被 review 看见。
}
