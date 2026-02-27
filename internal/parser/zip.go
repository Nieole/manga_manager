package parser

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

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

			buf := bytes.NewBuffer(make([]byte, 0, f.FileInfo().Size()))
			if _, err := io.Copy(buf, rc); err != nil {
				return nil, err
			}
			return buf.Bytes(), nil
		}
	}
	return nil, errors.New("page not found")
}

func (z *ZipArchive) ReadMetadataFile(name string) ([]byte, error) {
	// 用于提取 ComicInfo.xml 等
	return z.ReadPage(name)
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
