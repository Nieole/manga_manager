package parser

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// maxPageUncompressedBytes 是单条归档项解压后的字节硬上限，防止解压炸弹与恶意声明的超大项导致 OOM。
// 远高于任何真实漫画页（即便 10000x10000 无损 PNG 通常也 <150MB），又能拦住 GB 级炸弹。
const maxPageUncompressedBytes = 256 << 20 // 256 MiB

// readEntryLimited 从解压流读取单条归档项，封堵两条 OOM 向量：
// (1) 按归档头声明的解压大小预分配——声明超限直接拒绝，不做超大预分配；
// (2) 解压炸弹——用 io.LimitReader 夹住实际拷贝字节，超限报错。
func readEntryLimited(rc io.Reader, declared int64, name string) ([]byte, error) {
	if declared > maxPageUncompressedBytes {
		return nil, fmt.Errorf("parser: entry %q declared size %d exceeds limit %d", name, declared, maxPageUncompressedBytes)
	}
	capHint := declared
	if capHint < 0 || capHint > maxPageUncompressedBytes {
		capHint = 0
	}
	buf := bytes.NewBuffer(make([]byte, 0, capHint))
	if _, err := io.Copy(buf, io.LimitReader(rc, maxPageUncompressedBytes+1)); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > maxPageUncompressedBytes {
		return nil, fmt.Errorf("parser: entry %q decompressed size exceeds limit %d (possible decompression bomb)", name, maxPageUncompressedBytes)
	}
	return buf.Bytes(), nil
}

// Archive 支持的文件能力抽象接口
type Archive interface {
	io.Closer
	GetPages() ([]PageMetadata, error)
	ReadPage(name string) ([]byte, error)
	ReadMetadataFile(name string) ([]byte, error)
}

type PageMetadata struct {
	Name      string
	Size      int64
	MediaType string
}

// ZipArchive 处理 zip/cbz 等标准归档
type ZipArchive struct {
	mu     sync.RWMutex
	reader *zip.ReadCloser
	path   string
}

func OpenZip(path string) (Archive, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip %s: %w", path, err)
	}
	return &ZipArchive{reader: r, path: path}, nil
}

func (z *ZipArchive) Close() error {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.reader.Close()
}

func (z *ZipArchive) GetPages() ([]PageMetadata, error) {
	z.mu.RLock()
	defer z.mu.RUnlock()

	var pages []PageMetadata

	for _, f := range z.reader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// 过滤隐藏文件比如 MacOS 的 __MACOSX 结构
		if strings.HasPrefix(filepath.Base(f.Name), ".") {
			continue
		}

		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".avif" {
			pages = append(pages, PageMetadata{
				Name:      f.Name,
				Size:      f.FileInfo().Size(),
				MediaType: getMediaType(ext),
			})
		}
	}

	// 按内置路径名智能排序以确立页码（Komga 的标准模式）
	sort.Slice(pages, func(i, j int) bool {
		return naturalCompare(pages[i].Name, pages[j].Name)
	})

	return pages, nil
}

func (z *ZipArchive) ReadPage(name string) ([]byte, error) {
	z.mu.RLock()
	defer z.mu.RUnlock()

	for _, f := range z.reader.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			return readEntryLimited(rc, f.FileInfo().Size(), name)
		}
	}
	return nil, errors.New("page not found")
}

// ReadMetadataFile 查找并读取归档内的元数据文件（目前只有 ComicInfo.xml）。
//
// 匹配语义刻意比 ReadPage 宽松：按 basename 大小写不敏感命中、允许位于任意层级——真实归档里
// comicinfo.xml、ComicInfo.XML、Scans/ComicInfo.xml 都很常见。多条命中时选哪一条由
// pickZipMetadataEntry 定，回写侧必须共用它：两侧选中不同条目，回写就会在别处另建一份，
// 读侧仍取原先那条，用户看到的于是不是刚写进去的元数据。
func (z *ZipArchive) ReadMetadataFile(name string) ([]byte, error) {
	z.mu.RLock()
	defer z.mu.RUnlock()

	if f := pickZipMetadataEntry(z.reader.File, name); f != nil {
		return readZipEntry(f)
	}
	return nil, fmt.Errorf("parser: metadata file %q not found", name)
}

// pickZipMetadataEntry 选出归档里代表元数据文件 name 的那一条，没有命中时返回 nil。
//
// 「这本书的 ComicInfo 是哪一条」全包只有这一个答案：读取与回写都从这里拿，
// 否则回写会写到读侧不看的位置上，用户的元数据在他自己的视角里就凭空消失了。
func pickZipMetadataEntry(files []*zip.File, name string) *zip.File {
	var best *zip.File
	for _, f := range files {
		if f.FileInfo().IsDir() || !strings.EqualFold(path.Base(f.Name), name) {
			continue
		}
		if best == nil || metadataEntryPrecedes(f.Name, best.Name) {
			best = f
		}
	}
	return best
}

// metadataEntryPrecedes 给多条命中定序：层级浅的优先（因此根目录那条总是胜出），同层按名字字典序。
// 判据不得依赖归档里的条目顺序——同一份归档换个工具重打包顺序就变了，选中的条目会跟着变。
func metadataEntryPrecedes(a, b string) bool {
	depthA, depthB := strings.Count(a, "/"), strings.Count(b, "/")
	if depthA != depthB {
		return depthA < depthB
	}
	return a < b
}

// readZipEntry 打开并读取一个 zip 条目（带解压炸弹上限）。
func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return readEntryLimited(rc, f.FileInfo().Size(), f.Name)
}

func getMediaType(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	default:
		return "application/octet-stream"
	}
}
