// 跟进符号链接的目录遍历，扫描器与文件监听器共用。
//
// filepath.WalkDir 用 lstat 且从不跟进软链，于是软链进库根的系列目录会被整棵跳过（多盘位 NAS
// 上把外置盘目录软链进来是常见组织方式）；软链的归档文件比对到的又是链接自身的 mtime，目标
// 文件被替换后增量扫描永远判定「毫无变化」，页数、封面、内嵌元数据停在首次入库时的状态。

package scanner

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// maxSymlinkWalkDepth 限制软链的嵌套层数。
// 真实的库布局不会深过两三层；给到 16 是留足余量，同时保证即使 visited 去重出现意外，
// 递归也不会无限下去。
const maxSymlinkWalkDepth = 16

// walkDirFollowingSymlinks 是 filepath.WalkDir 的替代品：额外跟进指向目录的软链，
// 并把软链文件的 d.Info() 修正为**目标**的 size/mtime。
//
// 报给 fn 的 path 始终落在 root 之下（软链目标的真实路径会被改写回链接路径），
// 因为调用方要靠 path 判定库归属、派生系列目录、以及作为 books.path 的主键。
//
// visited 每次调用现建，作用域只到这一趟遍历：两个资料库各自软链到同一块外置盘的同一个
// 目录时，两趟遍历互不相干，谁也不会把对方的内容吞掉。
func walkDirFollowingSymlinks(root string, fn fs.WalkDirFunc) error {
	visited := make(map[string][]string)
	return walkFollow(root, fn, visited, 0)
}

// dirClaimKey 把一条目录路径折成 visited 的**桶键**，另外交回解析后的路径。
//
// 先用 EvalSymlinks 解析软链，再折叠大小写。EvalSymlinks 逐段解析但保留调用方写的大小写：
// 链目标写作 "series alpha"、真实目录名为 "Series Alpha" 时它给出两个不相等的字符串，
// Windows 上盘符大小写（D:\ 与 d:\）同理，折叠一并盖住。解析失败时退回原路径。
// 折叠只用来分桶、不用来判等——大小写敏感的文件系统上 /Data 与 /data 是两个不同目录。
func dirClaimKey(dir string) (key, resolved string) {
	resolved = dir
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		resolved = r
	}
	return strings.ToLower(resolved), resolved
}

// claimDir 给目录登记一笔，交回解析后的路径与「它是不是第一次被走到」。
//
// 身份是「同一个目录」这件事本身：同桶的候选逐个用 os.SameFile 定夺，它在大小写敏感与不敏感的
// 文件系统上都答得对，也不像 syscall.Stat_t 的 Dev/Ino 那样是 Unix-only（build.sh 要交叉编译
// 到 Windows）。线性比对只发生在桶内，而只有互为别名或只差大小写的路径才会同桶，几万个目录的
// 库里桶长仍是 1；os.Stat 也因此只在同桶时才做。取不到属性（目录已消失、不可读）时退回字符串
// 判等，宁可多走一趟也不误合并。
func claimDir(dir string, visited map[string][]string) (resolved string, claimed bool) {
	key, resolved := dirClaimKey(dir)
	for _, seen := range visited[key] {
		if seen == resolved || sameDir(seen, resolved) {
			return resolved, false
		}
	}
	visited[key] = append(visited[key], resolved)
	return resolved, true
}

// sameDir 报告两条路径是不是同一个目录。
func sameDir(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// walkFollow 遍历 dir 这一棵，本层的软链目录攒下来、等本层走完再跟进。
//
// 先把本层所有真实目录登记进 visited、再处理软链，指回同一棵树的软链才认得出来：
// 否则 <库根>/Alpha (link) -> <库根>/Series Alpha 这类按作者二次组织的链会把同一批文件再走
// 一遍，同一个物理文件以两条路径入库，变成两本书、两个系列，book_count 翻倍，去重、统计与
// 阅读进度各算各的。a->b、b->a 这类环，以及两个链接指向同一目录的菱形，一并被同一个集合挡住。
//
// 这个次序也定死了两条路径都通时留下来的是**真实目录**那条，与链接名的字典序无关：
// 软链是随手建的组织视图，用户删掉它不该让整个系列在下次扫描时消失。
func walkFollow(dir string, fn fs.WalkDirFunc, visited map[string][]string, depth int) error {
	walkRoot, claimed := claimDir(dir, visited)
	if !claimed {
		return nil
	}

	// 交给 WalkDir 的必须是**解析后**的路径。dir 自身是软链时（软链的资料库根、软链的系列
	// 目录）WalkDir 对它也只做 lstat，d.IsDir() 为 false，一层都不下降；回调里那次跟进又会
	// 撞上上面这笔登记而直接返回，于是 fn 一次都不被调用、遍历还报成功。报出去的 path 改写回
	// dir 这一侧：调用方要靠它判库归属、派生系列目录、写 books.path。
	//
	// 登记的仍是解析后的路径，所以指回 dir 的软链照旧被挡在去重集合外，不会走第二遍。
	if walkRoot != dir {
		fn = rewriteWalkPrefix(fn, walkRoot, dir)
	}

	var pendingLinks []string
	walkErr := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return fn(path, d, err)
		}
		if d.Type()&fs.ModeSymlink == 0 {
			if d.IsDir() && path != walkRoot {
				if _, ok := claimDir(path, visited); !ok {
					// 这一棵已经从别的路径走过了（例如本次遍历更早跟进的某条软链就指向它）。
					// 不报给 fn：报了就是第二条入库路径。
					return filepath.SkipDir
				}
			}
			return fn(path, d, nil)
		}

		target, statErr := os.Stat(path)
		if statErr != nil {
			// 断链：目标不存在或不可达，谁也读不了它。跳过而不是报错——
			// 库里留着一个失效链接是很常见的，不该让整次扫描看起来出了故障。
			slog.Debug("Skipping broken symlink", "path", path, "error", statErr)
			return nil
		}

		if !target.IsDir() {
			// 指向文件的软链：交给 fn 当普通文件处理，但把 Info() 换成目标的属性，
			// 否则增量比对读到的是链接自身那个永不变化的 mtime。
			return fn(path, symlinkedEntry{DirEntry: d, info: target}, nil)
		}

		pendingLinks = append(pendingLinks, path)
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	for _, link := range pendingLinks {
		if depth >= maxSymlinkWalkDepth {
			slog.Warn("Symlink nesting too deep, not descending",
				"path", link, "max_depth", maxSymlinkWalkDepth)
			continue
		}

		// 递归走**解析后的真实路径**，而不是链接路径本身：
		// filepath.WalkDir 对它的 root 也做 lstat，直接传链接路径会让这个回调
		// 再次判定为软链而原地打转。改写前缀把报出去的 path 拉回链接这一侧。
		realTarget, evalErr := filepath.EvalSymlinks(link)
		if evalErr != nil {
			slog.Warn("Cannot resolve symlinked directory", "path", link, "error", evalErr)
			continue
		}
		if err := walkFollow(realTarget, rewriteWalkPrefix(fn, realTarget, link), visited, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// symlinkedEntry 让软链文件的 Info() 返回目标的 FileInfo。
// Type/IsDir 一并按目标口径给出，避免调用方拿到自相矛盾的组合。
type symlinkedEntry struct {
	fs.DirEntry
	info os.FileInfo
}

func (e symlinkedEntry) Info() (os.FileInfo, error) { return e.info, nil }
func (e symlinkedEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e symlinkedEntry) IsDir() bool                { return e.info.IsDir() }

// rewriteWalkPrefix 把 from 下的路径改写成 to 下的对应路径。
func rewriteWalkPrefix(fn fs.WalkDirFunc, from, to string) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if path == from {
			path = to
		} else if rel, relErr := filepath.Rel(from, path); relErr == nil {
			path = filepath.Join(to, rel)
		}
		return fn(path, d, err)
	}
}
