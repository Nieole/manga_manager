package parser

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nwaples/rardecode/v2"
)

// RarArchive 处理 cbr/rar 等标准归档
type RarArchive struct {
	path string
}

func OpenRar(path string) (Archive, error) {
	// rardecode stream design means we must re-open sequential reads instead of caching a single reader
	return &RarArchive{path: path}, nil
}

func (r *RarArchive) Close() error {
	return nil // No-op as we handle lifecycle internally per request
}

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
	rr, err := rardecode.OpenReader(r.path)
	if err != nil {
		return nil, err
	}
	defer rr.Close()

	for {
		header, err := rr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if header.Name == name {
			buf := bytes.NewBuffer(make([]byte, 0, header.UnPackedSize))
			if _, err := io.Copy(buf, rr); err != nil {
				return nil, err
			}
			return buf.Bytes(), nil
		}
	}

	return nil, errors.New("page not found")
}

func (r *RarArchive) ReadMetadataFile(name string) ([]byte, error) {
	return r.ReadPage(name)
}
