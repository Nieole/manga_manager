// 业务说明：本文件是业务实现，属于漫画文件解析层，负责识别归档、目录、页序、页数和可读取图片条目。
// 它是扫描入库、封面提取、阅读器翻页和存储 IO 调度的底层事实来源。
// 维护时应保证多格式兼容、自然排序一致、异常归档可诊断，并避免重复解压造成性能浪费。

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
// 匹配语义刻意比 ReadPage 宽松：大小写不敏感、允许位于任意层级（根目录优先）。
// 真实归档里 comicinfo.xml、ComicInfo.XML、Scans/ComicInfo.xml 都很常见，而写回侧
// （comicinfo_writeback.go）本来就用 EqualFold 匹配——读侧要求精确全名会造成
// 「读不到但能写」的语义分叉：内嵌元数据被静默漏读，用户却看不出原因。
func (z *ZipArchive) ReadMetadataFile(name string) ([]byte, error) {
	z.mu.RLock()
	defer z.mu.RUnlock()

	var fallback *zip.File
	for _, f := range z.reader.File {
		if !strings.EqualFold(path.Base(f.Name), name) {
			continue
		}
		// 根目录命中优先；子目录里的先记下来作为兜底。
		if !strings.ContainsRune(f.Name, '/') {
			return readZipEntry(f)
		}
		if fallback == nil {
			fallback = f
		}
	}
	if fallback != nil {
		return readZipEntry(fallback)
	}
	return nil, fmt.Errorf("parser: metadata file %q not found", name)
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
