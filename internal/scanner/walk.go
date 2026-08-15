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
func walkDirFollowingSymlinks(root string, fn fs.WalkDirFunc) error {
	visited := make(map[string]struct{})
	return walkFollow(root, fn, visited, 0)
}

func walkFollow(dir string, fn fs.WalkDirFunc, visited map[string]struct{}, depth int) error {
	// 以解析后的真实路径为键：这样 a->b、b->a 这类环，以及两个链接指向同一目录的菱形，
	// 都只会被遍历一次——菱形若遍历两次，同一批文件会以两个不同路径入库，变成一堆假的重复书。
	// 解析失败时退回原路径，至少还能防住自引用。
	key := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		key = resolved
	}
	if _, seen := visited[key]; seen {
		return nil
	}
	visited[key] = struct{}{}

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return fn(path, d, err)
		}
		if d.Type()&fs.ModeSymlink == 0 {
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

		if depth >= maxSymlinkWalkDepth {
			slog.Warn("Symlink nesting too deep, not descending",
				"path", path, "max_depth", maxSymlinkWalkDepth)
			return nil
		}

		// 递归走**解析后的真实路径**，而不是链接路径本身：
		// filepath.WalkDir 对它的 root 也做 lstat，直接传链接路径会让这个回调
		// 再次判定为软链而原地打转。改写前缀把报出去的 path 拉回链接这一侧。
		realTarget, evalErr := filepath.EvalSymlinks(path)
		if evalErr != nil {
			slog.Warn("Cannot resolve symlinked directory", "path", path, "error", evalErr)
			return nil
		}
		return walkFollow(realTarget, rewriteWalkPrefix(fn, realTarget, path), visited, depth+1)
	})
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
