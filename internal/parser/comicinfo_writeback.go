// 把 ComicInfo.xml 元数据写回 zip/cbz 归档：读出归档里原有的那份、合并、原子替换。
// 维护时应关注：仅支持可写的 zip/cbz（rar/cbr 无写库）、原子替换避免损坏原文件、
// Windows 下重命名前必须先关闭源句柄。合并规则见 MergeComicInfoXML。

package parser

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	stage, err := PrepareComicInfo(context.Background(), archivePath, info)
	if err != nil {
		return err
	}
	defer stage.Close()
	_, err = stage.Publish(context.Background())
	return err
}

// PreparedComicInfo owns a temporary archive and the original content fingerprint.
// Close removes only this operation's staging file. Publish is single-use.
type PreparedComicInfo struct {
	path, temporary string
	original        os.FileInfo
	digest          [32]byte
}

// ErrArchiveChanged preserves an externally changed source instead of overwriting it.
var ErrArchiveChanged = errors.New("archive changed before publication")

func (p *PreparedComicInfo) Close() error {
	if p.temporary == "" {
		return nil
	}
	err := os.Remove(p.temporary)
	if err == nil || os.IsNotExist(err) {
		p.temporary = ""
		return nil
	}
	return err
}

// Publish reserves a bounded, cancellable window, drains readers, verifies the source,
// then atomically replaces it. The returned FileInfo describes the committed file.
func (p *PreparedComicInfo) Publish(ctx context.Context) (os.FileInfo, error) {
	if p.temporary == "" {
		return nil, errors.New("ComicInfo staging is already closed")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, err := AcquireArchivePublication(ctx, p.path)
	if err != nil {
		return nil, err
	}
	defer release()
	EvictArchiveFromPool(p.path)
	file, err := os.Open(p.path)
	if err != nil {
		return nil, err
	}
	current, statErr := file.Stat()
	hash := sha256.New()
	_, hashErr := io.Copy(hash, &contextReader{ctx: ctx, reader: file})
	_ = file.Close()
	if statErr != nil {
		return nil, statErr
	}
	if hashErr != nil {
		return nil, hashErr
	}
	if !os.SameFile(p.original, current) || !p.original.ModTime().Equal(current.ModTime()) || p.original.Size() != current.Size() || string(hash.Sum(nil)) != string(p.digest[:]) {
		return nil, ErrArchiveChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(p.temporary)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(p.temporary, p.path); err != nil {
		return nil, err
	}
	p.temporary = ""
	return info, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// PrepareComicInfo builds a conservative merged archive while readers remain admitted.
// Its own source borrow is returned before the staging object is handed to the publisher.
func PrepareComicInfo(ctx context.Context, archivePath string, info ComicInfo) (*PreparedComicInfo, error) {
	ext := strings.ToLower(filepath.Ext(archivePath))
	if ext != ".zip" && ext != ".cbz" {
		return nil, ErrArchiveNotWritable
	}

	source, err := OpenContentFile(archivePath)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	original, err := source.Stat()
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, &contextReader{ctx: ctx, reader: source}); err != nil {
		return nil, err
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	reader, err := zip.NewReader(source, original.Size())
	if err != nil {
		return nil, err
	}
	originalMode := original.Mode().Perm()
	base := pickZipMetadataEntry(reader.File, comicInfoEntryName)
	xmlData, err := mergeArchiveComicInfo(base, info)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(archivePath)
	tmp, err := os.CreateTemp(dir, ".comicinfo-*.tmp")
	if err != nil {
		return nil, err
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
		if err := ctx.Err(); err != nil {
			_ = zw.Close()
			return nil, err
		}
		if f == base {
			continue
		}
		if err := copyZipEntry(zw, f); err != nil {
			_ = zw.Close()
			return nil, err
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
		return nil, err
	}
	if _, err := entry.Write(xmlData); err != nil {
		_ = zw.Close()
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}

	if err := os.Chmod(tmpName, originalMode); err != nil {
		return nil, err
	}

	committed = true
	return &PreparedComicInfo{path: archivePath, temporary: tmpName, original: original, digest: digest}, nil
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
