package parser

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/nwaples/rardecode/v2"
)

// rarPageCacheMaxBytes 是单个归档会话缓存的字节上限（超出按 FIFO 淘汰最早读入的页）。设为包级变量以便测试
// 收紧上限来验证淘汰 + 重开路径。默认 64 MiB：漫画常见几十~几百页、单页数 MB，足以覆盖顺序阅读的滑动窗口。
var rarPageCacheMaxBytes = 64 << 20

// ErrArchiveClosed 表示归档句柄已被关闭（通常是被归档池淘汰），调用方应重新获取一个句柄。
var ErrArchiveClosed = errors.New("parser: archive handle is closed")

// RarArchive 处理 cbr/rar 等标准归档。RAR 是前向只读流（rardecode 无法 seek），故维护一个
// 随读取前滚的持久游标，把途经页的字节填入有界 FIFO 缓存，翻页命中缓存即 O(1)，只有反向跳读
// 到已被淘汰的页才重开归档；整卷顺序阅读因此是 O(N) 而非每页重开重扫的 O(N²)。
// 游标 / 缓存全程由 mu 保护，故被归档池共享的同一 *RarArchive 可安全并发读取（同档读取串行化，
// 这与 RAR 解码本就顺序一致）。
type RarArchive struct {
	path string

	mu     sync.Mutex
	closed bool                  // Close 后置位：句柄进入终态，任何读取直接报错而非静默重开
	rr     *rardecode.ReadCloser // 持久游标；nil 表示未打开
	atEOF  bool                  // 游标已扫到 EOF
	seen   map[string]bool       // 当前游标已途经的条目名（用于判断目标是否在游标之后）
	// cache 只保留「图片页」与显式读取过的目标条目的字节。阅读前滚途经的非图片条目（元数据、
	// 说明文本等）一律跳过不解压：它们不是顺序阅读的下一批目标，解出来纯占内存预算。
	cache      map[string][]byte
	cacheOrder []string // FIFO 淘汰顺序
	cacheBytes int
}

func OpenRar(path string) (Archive, error) {
	// 惰性打开：即使文件暂不存在也不报错，延迟到实际读取时。
	return &RarArchive{
		path:  path,
		seen:  make(map[string]bool),
		cache: make(map[string][]byte),
	}, nil
}

func (r *RarArchive) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rr != nil {
		r.rr.Close()
		r.rr = nil
	}
	// closed 必须先于置空各 map：否则归档池淘汰本句柄后，仍持有它的在途请求会走
	// readPageLocked → reopenLocked（只重建 seen，不重建 cache）→ cachePutLocked 向
	// nil map 赋值，直接 panic。置位后所有读取路径都在入口返回 ErrArchiveClosed。
	r.closed = true
	r.seen = nil
	r.cache = nil
	r.cacheOrder = nil
	r.cacheBytes = 0
	r.atEOF = false
	return nil
}

// GetPages 独立于会话缓存：单开一个只读头部的临时 reader 列出可读图片页（不解字节、不动游标）。
func (r *RarArchive) GetPages() ([]PageMetadata, error) {
	rr, err := rardecode.OpenReader(r.path)
	if err != nil {
		return nil, err
	}
	defer rr.Close()

	var pages []PageMetadata
	for {
		header, err := rr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.IsDir {
			continue
		}
		if strings.HasPrefix(filepath.Base(header.Name), ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(header.Name))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".avif" {
			pages = append(pages, PageMetadata{
				Name:      header.Name,
				Size:      header.UnPackedSize,
				MediaType: getMediaType(ext),
			})
		}
	}

	sort.Slice(pages, func(i, j int) bool {
		return naturalCompare(pages[i].Name, pages[j].Name)
	})
	return pages, nil
}

func (r *RarArchive) ReadPage(name string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrArchiveClosed
	}
	return r.readPageLocked(name, false)
}

// readPageLocked 在持锁下按会话缓存语义读取一页。reopened 表示本次调用已从头重开过（避免对不存在的页无限重开）。
func (r *RarArchive) readPageLocked(name string, reopened bool) ([]byte, error) {
	if data, ok := r.cache[name]; ok {
		return append([]byte(nil), data...), nil
	}
	// 需要一个「还能前滚到 name」的游标：无游标 / 已 EOF / name 已被当前游标越过（在其之前）时，从头重开。
	if !reopened && (r.rr == nil || r.atEOF || r.seen[name]) {
		if err := r.reopenLocked(); err != nil {
			return nil, err
		}
		reopened = true
	}
	data, found, err := r.advanceLocked(name)
	if err != nil {
		return nil, err
	}
	if found {
		return append([]byte(nil), data...), nil
	}
	// 前方未找到：若本次尚未从头重开，则重开再扫一遍（name 可能在游标之前但未记入 seen 的边缘情形）。
	if !reopened {
		if err := r.reopenLocked(); err != nil {
			return nil, err
		}
		return r.readPageLocked(name, true)
	}
	return nil, errors.New("page not found")
}

// readMetadataLocked 以宽松匹配（大小写不敏感 + 任意层级）查找元数据文件。
func (r *RarArchive) readMetadataLocked(name string) ([]byte, error) {
	// 缓存里可能已有（阅读前滚时按原名缓存，或上一次探测存下），先按宽松规则扫一遍。
	for cached, data := range r.cache {
		if matchesArchiveEntry(cached, name, true) {
			return append([]byte(nil), data...), nil
		}
	}
	entry, data, found, err := r.probeMetadataLocked(name)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("parser: metadata file %q not found", name)
	}
	r.cachePutLocked(entry, data)
	return append([]byte(nil), data...), nil
}

// probeMetadataLocked 单开一个临时 reader 找元数据条目，只解压命中的那一个：途经条目由 rr.Next() 零解压跳过。
// 与 advanceLocked 的分工即「为什么打开这个归档」——探测只要那一个条目，故既不预取图片页，也不动阅读会话的
// 游标（扫描期每卷都会探测一次 ComicInfo.xml，走会话游标会把整卷解压一遍并把正在阅读的游标打回开头）。
func (r *RarArchive) probeMetadataLocked(target string) (name string, data []byte, found bool, err error) {
	rr, err := rardecode.OpenReader(r.path)
	if err != nil {
		return "", nil, false, err
	}
	defer rr.Close()
	for {
		header, nextErr := rr.Next()
		if nextErr == io.EOF {
			return "", nil, false, nil
		}
		if nextErr != nil {
			return "", nil, false, nextErr
		}
		if header.IsDir || !matchesArchiveEntry(header.Name, target, true) {
			continue
		}
		b, readErr := readEntryLimited(rr, header.UnPackedSize, header.Name)
		if readErr != nil {
			return "", nil, false, readErr
		}
		return header.Name, b, true, nil
	}
}

// matchesArchiveEntry 判定归档条目名是否命中目标。
// loose=false 时要求全名精确相等（页图读取，名字来自 GetPages，必然精确）；
// loose=true 时按 basename 大小写不敏感比较（元数据文件，真实归档里大小写与层级都很杂）。
func matchesArchiveEntry(entryName, target string, loose bool) bool {
	if entryName == target {
		return true
	}
	if !loose {
		return false
	}
	return strings.EqualFold(path.Base(entryName), target)
}

// advanceLocked 前滚游标，把途经的图片页读入缓存，直到遇到 target（返回其字节）或 EOF（found=false）。
// 只服务阅读取页：预取是顺序阅读的下一批目标，元数据探测另走 probeMetadataLocked。
func (r *RarArchive) advanceLocked(target string) (data []byte, found bool, err error) {
	if r.rr == nil {
		if err := r.reopenLocked(); err != nil {
			return nil, false, err
		}
	}
	for {
		header, nextErr := r.rr.Next()
		if nextErr == io.EOF {
			r.atEOF = true
			return nil, false, nil
		}
		if nextErr != nil {
			return nil, false, nextErr
		}
		if header.IsDir {
			continue
		}
		isTarget := matchesArchiveEntry(header.Name, target, false)
		// 只对目标条目和图片页解字节：rr.Next() 本身会零解压跳到下一个条目头，
		// 所以途经的非图片条目直接跳过即可。
		if !isTarget && !isCacheablePage(header.Name) {
			r.seen[header.Name] = true
			continue
		}
		b, readErr := readEntryLimited(r.rr, header.UnPackedSize, header.Name)
		if readErr != nil {
			if isTarget {
				return nil, false, readErr
			}
			// 途经条目损坏或超限不应连累本次前滚：记为已途经但不缓存，继续找目标。
			slog.Warn("Skipping unreadable RAR entry during scan-ahead",
				"archive", r.path, "entry", header.Name, "error", readErr)
			r.seen[header.Name] = true
			continue
		}
		r.seen[header.Name] = true
		r.cachePutLocked(header.Name, b)
		if isTarget {
			return b, true, nil
		}
	}
}

// isCacheablePage 判断某个归档条目是否值得在前滚途中解压并缓存。
// 只有可读图片页才值得——它们是顺序阅读的下一批目标；其余条目解压纯属浪费 CPU 与内存预算。
func isCacheablePage(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return getMediaType(ext) != "application/octet-stream"
}

// reopenLocked 关闭旧游标、从头重开，并重置「已途经」集合与 EOF 标记；字节缓存按名寻址、与游标位置无关，保留。
func (r *RarArchive) reopenLocked() error {
	if r.rr != nil {
		r.rr.Close()
		r.rr = nil
	}
	rr, err := rardecode.OpenReader(r.path)
	if err != nil {
		return err
	}
	r.rr = rr
	r.atEOF = false
	r.seen = make(map[string]bool)
	return nil
}

// cachePutLocked 把一页字节写入会话缓存，超上限时按 FIFO 淘汰最早读入的页（顺序阅读中即最早读过、最不可能
// 再被前向读到的页）。同名已在缓存则不重复写。
func (r *RarArchive) cachePutLocked(name string, data []byte) {
	if _, ok := r.cache[name]; ok {
		return
	}
	for r.cacheBytes+len(data) > rarPageCacheMaxBytes && len(r.cacheOrder) > 0 {
		oldest := r.cacheOrder[0]
		r.cacheOrder = r.cacheOrder[1:]
		r.cacheBytes -= len(r.cache[oldest])
		delete(r.cache, oldest)
	}
	r.cache[name] = data
	r.cacheOrder = append(r.cacheOrder, name)
	r.cacheBytes += len(data)
}

// ReadMetadataFile 与 zip 侧保持同一匹配语义：大小写不敏感、允许任意层级。
// RAR 是前向只读流，无法先枚举再挑选，故在前滚过程中用宽松比较判定目标。
func (r *RarArchive) ReadMetadataFile(name string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrArchiveClosed
	}
	return r.readMetadataLocked(name)
}
