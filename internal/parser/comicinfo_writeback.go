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

	// 先记下原文件权限：os.CreateTemp 建出来的是 0600，直接 rename 覆盖会把归档变成
	// 仅属主可读，其他账号（媒体服务器、家庭共享）从此打不开这本书。
	var originalMode os.FileMode = 0o644
	if info, statErr := os.Stat(archivePath); statErr == nil {
		originalMode = info.Mode().Perm()
	}

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

	if err := os.Chmod(tmpName, originalMode); err != nil {
		return err
	}

	// Windows 下无法 rename 覆盖仍被打开的目标文件，必须先关闭源归档句柄。
	closeReader()
	// 归档池里可能缓存着同一路径的共享句柄（阅读器刚翻过这本书就会有）。
	// Windows 上那个句柄未持有 FILE_SHARE_DELETE，会让 os.Rename 直接失败——
	// 也就是说「阅读过的书写不回元数据」，而失败原因对用户完全不可见。
	EvictArchiveFromPool(archivePath)
	if err := os.Rename(tmpName, archivePath); err != nil {
		return err
	}
	// rename 期间可能又有请求把旧路径重新打开进池，此时池里那个句柄指向的是已被替换掉的
	// 旧 inode，再驱逐一次让后续请求重新打开新文件。
	EvictArchiveFromPool(archivePath)
	committed = true
	return nil
}

// copyZipEntry 把源归档中的一个条目搬到目标 writer，保留其头部（名称/压缩方法/时间等）。
//
// 优先用 zip.Writer.Copy：它直接搬运已压缩的原始字节，不解压也不重压。
// 退回逐条解压+重压时，写回一本 200 页的漫画要把整卷重新压一遍——CPU 白烧一轮，
// 还可能因压缩级别不同而让文件变大。Copy 对少数条目会返回不支持，此时才走老路径。
func copyZipEntry(zw *zip.Writer, f *zip.File) error {
	if err := zw.Copy(f); err == nil {
		return nil
	}

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
