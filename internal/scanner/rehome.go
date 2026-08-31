// 文件改名/移动后的身份重连：把注定要被 CleanupLibrary 删掉的旧记录改挂到新路径，保留 books.id。
//
// 扫描器以 books.path 为书的身份，改名因此被看成「旧书消失 + 新书出现」；而阅读进度、书签、
// 合集归属、阅读清单条目都以 books.id 为外键 ON DELETE CASCADE——不重连就等于改名一本书
// 丢掉它的全部阅读记录。改挂后 UpsertBookByPath 经 ON CONFLICT(path) 命中同一行照常刷新元数据。

package scanner

import (
	"os"
	"sync"
	"time"

	"manga-manager/internal/database"
)

// rehomeCandidate 是一条**可能**已被改名的已入库记录。
type rehomeCandidate struct {
	id        int64
	path      string
	size      int64
	modTime   time.Time
	fileHash  string
	quickHash string
}

// bookRehome 记录一次已认领的重连：把 bookID 这一行从 oldPath 改挂到新路径。
// reason 只用于日志，让「为什么这本书被判成改名」在运维时可追溯。
type bookRehome struct {
	bookID  int64
	oldPath string
	reason  string
}

// sizeModKey 是 size+mtime 复合键。mtime 取 UnixNano 而非 time.Time：
// time.Time 含单调时钟与时区字段，直接做 map key 会让「同一时刻」的两个值不相等。
type sizeModKey struct {
	size      int64
	modUnixNs int64
}

// ambiguousID 是「该键对应多条候选，不可用于匹配」的哨兵。
// 用哨兵而非删除键，是为了让后续构建中再出现同一键时仍保持不可用。
const ambiguousID int64 = -1

// renameIndex 是一次扫描期间的改名候选索引，由扫描起始时那份已入库清单构建（不额外查库）。
//
// 并发：认领发生在单个入库 goroutine 里，理论上无需加锁；仍保留互斥锁，
// 以免将来有人从解析 worker 里调用而静默破坏 claimed 的一次性语义。
type renameIndex struct {
	mu sync.Mutex

	knownPaths  map[string]struct{}
	pathByID    map[int64]string
	byQuickHash map[string]int64
	byFileHash  map[string]int64
	bySizeMod   map[sizeModKey]int64
	claimed     map[int64]struct{}
}

func newRenameIndex(candidates []rehomeCandidate) *renameIndex {
	idx := &renameIndex{
		knownPaths:  make(map[string]struct{}, len(candidates)),
		pathByID:    make(map[int64]string, len(candidates)),
		byQuickHash: make(map[string]int64),
		byFileHash:  make(map[string]int64),
		bySizeMod:   make(map[sizeModKey]int64, len(candidates)),
		claimed:     make(map[int64]struct{}),
	}
	for _, c := range candidates {
		idx.knownPaths[c.path] = struct{}{}
		idx.pathByID[c.id] = c.path
		if c.quickHash != "" {
			putUnique(idx.byQuickHash, c.quickHash, c.id)
		}
		if c.fileHash != "" {
			putUnique(idx.byFileHash, c.fileHash, c.id)
		}
		// size<=0 的归档（空文件/读取失败）没有区分度，不进 size+mtime 索引。
		if c.size > 0 {
			putUnique(idx.bySizeMod, sizeModKey{size: c.size, modUnixNs: c.modTime.UTC().UnixNano()}, c.id)
		}
	}
	return idx
}

// putUnique 写入索引；键重复即整体标记为不可用（见 ambiguousID）。
func putUnique[K comparable](m map[K]int64, key K, id int64) {
	if _, exists := m[key]; exists {
		m[key] = ambiguousID
		return
	}
	m[key] = id
}

// rehomeRequest 是一次认领请求：本次扫描刚读到的那个文件。
type rehomeRequest struct {
	path      string
	size      int64
	modTime   time.Time
	quickHash string
	fileHash  string
}

// claim 判断 req 是否其实是某条已入库记录的改名；是则返回该记录并把它标记为已认领。
// 返回 nil 表示「按新书处理」——这是保守的默认，任何一条守卫不满足都走这里。
//
// 匹配信号按可信度从高到低，任一命中即止：
//
//	quick_hash   抽样内容指纹——只有 identity / repair 档位才会计算并落库
//	file_hash    全量内容指纹——同上
//	size+mtime   任何档位都有，且扫描器本来就 stat 过，零额外 IO
//
// 默认档位 metadata 不算两个 hash，绝大多数重连因此走 size+mtime。os.Rename 在 POSIX 与
// NTFS 上都保留 size 与 mtime，改名场景下这个信号足够稳定。
//
// 四道守卫，任一不满足即返回 nil：
//
//   - 旧记录的文件必须确实已从磁盘消失。源文件还在说明用户做的是**复制**而非改名，
//     搬走旧行会让他平白少一本书。
//   - 新路径必须尚未入库。已入库说明这是一次普通的增量更新。
//   - 每个匹配键必须唯一。两条待删记录共享同一 size+mtime（例如 `cp -p` 出来的副本）时，
//     无从判断该搬哪一条，宁可都不搬。
//   - 每条旧记录至多被认领一次，避免一次扫描里多个新文件抢同一行。
//
// 已知边界：唯一性守卫只覆盖旧记录一侧。一个**真正新增**的文件若与某条待删记录的 size 和
// mtime（纳秒级）都相同，可能抢走那条记录，使进度落到错书上。解析结果流式到达，认领时无法
// 预知后面还会出现哪些新文件，这一侧做不了同样的判断。代价是「一本书的进度挂错」，而不重连
// 的代价是「进度必然全丢」；把扫描档位切到 identity/repair 让内容指纹参与匹配可彻底规避。
func (idx *renameIndex) claim(req rehomeRequest) *bookRehome {
	if idx == nil || req.path == "" {
		return nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 新路径已在库里 = 这是普通的增量更新，不是改名。
	if _, known := idx.knownPaths[req.path]; known {
		return nil
	}

	type signal struct {
		reason string
		id     int64
		ok     bool
	}
	signals := make([]signal, 0, 3)
	if req.quickHash != "" {
		id, ok := idx.byQuickHash[req.quickHash]
		signals = append(signals, signal{"quick_hash", id, ok})
	}
	if req.fileHash != "" {
		id, ok := idx.byFileHash[req.fileHash]
		signals = append(signals, signal{"file_hash", id, ok})
	}
	if req.size > 0 {
		id, ok := idx.bySizeMod[sizeModKey{size: req.size, modUnixNs: req.modTime.UTC().UnixNano()}]
		signals = append(signals, signal{"size_mtime", id, ok})
	}

	for _, s := range signals {
		if !s.ok || s.id == ambiguousID {
			continue
		}
		if _, taken := idx.claimed[s.id]; taken {
			continue
		}
		oldPath, hasPath := idx.pathByID[s.id]
		if !hasPath || oldPath == "" || oldPath == req.path {
			continue
		}
		if !pathIsGone(oldPath) {
			// 源文件还在 = 复制而非改名。继续试下一个信号是没有意义的（都指向同一批候选），
			// 但成本可忽略，且让「hash 命中 A、size 命中 B」这种情形仍有机会落在 B 上。
			continue
		}

		idx.claimed[s.id] = struct{}{}
		// 索引里同步反映这次搬迁，使后续认领不会再把 oldPath 当成存量路径。
		delete(idx.knownPaths, oldPath)
		idx.knownPaths[req.path] = struct{}{}
		idx.pathByID[s.id] = req.path
		return &bookRehome{bookID: s.id, oldPath: oldPath, reason: s.reason}
	}
	return nil
}

// pathIsGone 判断路径是否确已从磁盘消失。
// 只有明确的 IsNotExist 才算「消失」：权限错误、IO 错误等一律当作「还在」，
// 因为把一次瞬时故障误判成改名会真的搬走用户的数据。
func pathIsGone(path string) bool {
	_, err := os.Stat(path)
	return err != nil && os.IsNotExist(err)
}

// rehomeCandidatesFromLibraryRows 把整库清单转成候选集（ScanLibrary 用）。
func rehomeCandidatesFromLibraryRows(rows []database.ListBooksByLibraryRow) []rehomeCandidate {
	candidates := make([]rehomeCandidate, 0, len(rows))
	for _, r := range rows {
		candidates = append(candidates, rehomeCandidate{
			id:        r.ID,
			path:      r.Path,
			size:      r.Size,
			modTime:   r.FileModifiedAt,
			fileHash:  r.FileHash.String,
			quickHash: r.QuickHash.String,
		})
	}
	return candidates
}

// rehomeCandidatesFromBooks 把单个系列的书籍转成候选集（ScanSeries 用）。
// 系列级扫描只走该系列目录，因此候选也只限于该系列——跨系列的移动要靠整库扫描才能重连。
func rehomeCandidatesFromBooks(books []database.Book) []rehomeCandidate {
	candidates := make([]rehomeCandidate, 0, len(books))
	for _, b := range books {
		candidates = append(candidates, rehomeCandidate{
			id:        b.ID,
			path:      b.Path,
			size:      b.Size,
			modTime:   b.FileModifiedAt,
			fileHash:  b.FileHash.String,
			quickHash: b.QuickHash.String,
		})
	}
	return candidates
}
