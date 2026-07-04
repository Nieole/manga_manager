// 业务说明：本文件属于漫画文件解析层，负责把 ComicInfo.xml 元数据写回 zip/cbz 归档。
// 收藏者精心刮削/修订的元数据可选择烧进自己的归档文件，提升数据可迁移性（换软件也带得走）。
// 维护时应关注：仅支持可写的 zip/cbz（rar/cbr 无写库）、原子替换避免损坏原文件、Windows 下重命名前必须先关闭源句柄。

package parser

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrArchiveNotWritable 表示归档格式不支持写入（如 rar/cbr）。调用方据此给出可读提示。
var ErrArchiveNotWritable = errors.New("archive format does not support writing ComicInfo")

// comicInfoEntryName 是归档内标准的内嵌元数据文件名。
const comicInfoEntryName = "ComicInfo.xml"

// WriteComicInfoIntoArchive 把 ComicInfo.xml 写入（或替换）zip/cbz 归档。
// 采用“同目录临时文件 + 原子 rename 覆盖”，中途失败不损坏原文件。
// 仅支持 .zip / .cbz —— .rar / .cbr 返回 ErrArchiveNotWritable（Go 无 rar 写库）。
func WriteComicInfoIntoArchive(archivePath string, xmlData []byte) error {
	ext := strings.ToLower(filepath.Ext(archivePath))
	if ext != ".zip" && ext != ".cbz" {
		return ErrArchiveNotWritable
	}

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	readerClosed := false
	closeReader := func() {
		if !readerClosed {
			_ = reader.Close()
			readerClosed = true
		}
	}
	defer closeReader()

	dir := filepath.Dir(archivePath)
	tmp, err := os.CreateTemp(dir, ".comicinfo-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	zw := zip.NewWriter(tmp)
	// 复制原有条目，跳过任何已存在的 ComicInfo.xml（将被新内容替换）。
	for _, f := range reader.File {
		if strings.EqualFold(f.Name, comicInfoEntryName) {
			continue
		}
		if err := copyZipEntry(zw, f); err != nil {
			_ = zw.Close()
			return err
		}
	}

	// 写入新的 ComicInfo.xml（根目录，Deflate 压缩）。
	header := &zip.FileHeader{Name: comicInfoEntryName, Method: zip.Deflate}
	header.SetMode(0o644)
	entry, err := zw.CreateHeader(header)
	if err != nil {
		_ = zw.Close()
		return err
	}
	if _, err := entry.Write(xmlData); err != nil {
		_ = zw.Close()
		return err
	}

	if err := zw.Close(); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// Windows 下无法 rename 覆盖仍被打开的目标文件，必须先关闭源归档句柄。
	closeReader()
	if err := os.Rename(tmpName, archivePath); err != nil {
		return err
	}
	committed = true
	return nil
}

// copyZipEntry 把源归档中的一个条目原样复制到目标 writer，保留其头部（名称/压缩方法/时间等）。
func copyZipEntry(zw *zip.Writer, f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	header := f.FileHeader
	dst, err := zw.CreateHeader(&header)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, rc)
	return err
}
