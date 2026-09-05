// 把 ComicInfo.xml 元数据写回 zip/cbz 归档：读出归档里原有的那份、合并、原子替换。
// 维护时应关注：仅支持可写的 zip/cbz（rar/cbr 无写库）、原子替换避免损坏原文件、
// Windows 下重命名前必须先关闭源句柄。合并规则见 MergeComicInfoXML。

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

// maxComicInfoMergeBytes 是合并基底的读取上限。ComicInfo.xml 是几 KB 的东西，
// 上限拦的是把整卷伪装成 ComicInfo.xml 的畸形归档，避免解压炸弹撑爆内存。
const maxComicInfoMergeBytes = 8 << 20

// comicInfoEntryName 是归档内标准的内嵌元数据文件名。
const comicInfoEntryName = "ComicInfo.xml"

// comicInfoRootName 是 ComicInfo.xml 的根元素名。
const comicInfoRootName = "ComicInfo"

// WriteComicInfoIntoArchive 把 info 合并进归档里已有的 ComicInfo.xml（没有就新建）。
//
// info 里为空的字段保留归档原值，本项目不建模的字段原样不动——回写不可逆也不备份，
// 整份重建会把用户用别的工具打好的标一次性抹掉。合并规则见 MergeComicInfoXML。
// 合并基底由 pickZipMetadataEntry 选出，新内容写回基底所在的路径——可能在子目录里；归档里一条都
// 没有时才新建在根目录。其余同名条目原样留下，不删也不改。
// 采用“同目录临时文件 + 原子 rename 覆盖”，中途失败不损坏原文件。
// 仅支持 .zip / .cbz —— .rar / .cbr 返回 ErrArchiveNotWritable（Go 无 rar 写库）；
// 原有的 ComicInfo.xml 无法安全改写时返回 ErrComicInfoNotMergeable，归档保持原样。
func WriteComicInfoIntoArchive(archivePath string, info ComicInfo) error {
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
	if stat, statErr := os.Stat(archivePath); statErr == nil {
		originalMode = stat.Mode().Perm()
	}

	base := pickZipMetadataEntry(reader.File, comicInfoEntryName)
	xmlData, err := mergeArchiveComicInfo(base, info)
	if err != nil {
		return err
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
	// 复制原有条目，只跳过合并基底那一条——它由下面的新内容原地取代。
	// 别处的同名条目留着：本包没有依据判定它是这本书的元数据还是随卷附带的别的东西，
	// 而回写就地改写、不留备份，删错了拿不回来。
	for _, f := range reader.File {
		if f == base {
			continue
		}
		if err := copyZipEntry(zw, f); err != nil {
			_ = zw.Close()
			return err
		}
	}

	// 写入新的 ComicInfo.xml（Deflate 压缩）。路径必须与基底一致，否则归档里会多出一份，
	// 而读侧仍按自己的判据挑，挑中的可能是没被合并的那条。
	targetName := comicInfoEntryName
	if base != nil {
		targetName = base.Name
	}
	header := &zip.FileHeader{Name: targetName, Method: zip.Deflate}
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

// mergeArchiveComicInfo 读出基底条目的内容并把 info 合并进去。
// base 为 nil、或条目读不出来时按新文档处理：读不出的条目本来也带不出任何字段，
// 拿它当合并基底只会把写入整个挡掉。
func mergeArchiveComicInfo(base *zip.File, info ComicInfo) ([]byte, error) {
	var original []byte
	if base != nil {
		if rc, err := base.Open(); err == nil {
			data, readErr := io.ReadAll(io.LimitReader(rc, maxComicInfoMergeBytes))
			_ = rc.Close()
			if readErr == nil {
				original = data
			}
		}
	}
	return MergeComicInfoXML(original, info)
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
