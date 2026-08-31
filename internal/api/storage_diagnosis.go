// 取页清单、取页图、整卷下载三条端点共用的失败分类：读不到书的字节时，
// 把「整块存储离线」「这一本没了」「路径读不出来」「归档打不开」分开，
// 各自给出状态码、可诊断日志与足够前端说人话的载荷。
// 封面不在其列——它读的是缓存目录里的缩略图，不随藏书所在的卷一起掉线。

package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"manga-manager/internal/database"
)

// 「读不到书的字节」的四种分类。前端按 reason 选提示文案，运维按 reason 读日志。
//
// 分类顺序即探测顺序，且不可颠倒：存储离线时库内**每一条**路径都会看起来不存在，
// 先探测资料库根目录才能把「整块盘掉了」与「这一本被删了」分开——这与
// scanner.CleanupLibrary 判定整库误判用的是同一条依据。
const (
	// storageReasonOffline 是资料库根目录整个不可达：拔盘、盘符漂移、UNC 断连。
	storageReasonOffline = "storage_offline"
	// storageReasonFileMissing 是库根可达、这一条路径确证不存在（os.IsNotExist）。
	storageReasonFileMissing = "file_missing"
	// storageReasonPathUnreadable 是路径 Stat 出了 IsNotExist 之外的错：权限、瞬时 IO。
	// 单列一类而不并入 file_missing，是因为 rehome.go 立下的原则——只有明确的
	// IsNotExist 才算「消失」，把权限/IO 错误说成「文件没了」会误导用户去恢复文件。
	storageReasonPathUnreadable = "path_unreadable"
	// storageReasonArchiveUnreadable 是文件在、盘也在，但归档打不开或读不出页：损坏、加密、空归档。
	storageReasonArchiveUnreadable = "archive_unreadable"
)

// storageFailureLogMessage 是这类失败在服务端日志里的唯一 msg，便于运维直接过滤。
const storageFailureLogMessage = "Book storage read failed"

// storageOfflineRetryAfter 是存储离线时给客户端的重试提示（秒）。插回盘属于人工操作，
// 给一个不会把服务器打穿、又不至于让用户干等的量级。
const storageOfflineRetryAfter = "30"

// StorageFailureResponse 是取页清单、取页图、整卷下载在读不到字节时下发的响应体。
//
// Error 保持在第一位且永远非空：既有前端与 OPDS 客户端只认 error 字段，分类信息是加法而非替换。
// Reason 是给前端选文案用的稳定枚举，资料库名与路径让提示能说出「哪个盘掉了」。
type StorageFailureResponse struct {
	Error       string `json:"error"`
	Reason      string `json:"reason"`
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name"`
	LibraryPath string `json:"library_path"`
	Path        string `json:"path"`
}

// storageTarget 是一次读取所指向的书。诊断只需要这三样，因此 database.Book 与
// bookPageSource 都能收敛到它，三条端点共用同一套判定。
type storageTarget struct {
	BookID    int64
	LibraryID int64
	Path      string
}

func storageTargetFromBook(book database.Book) storageTarget {
	return storageTarget{BookID: book.ID, LibraryID: book.LibraryID, Path: book.Path}
}

func storageTargetFromSource(source bookPageSource) storageTarget {
	return storageTarget{BookID: source.ID, LibraryID: source.LibraryID, Path: source.Path}
}

// storageFailure 是一次已经分好类的读取失败。
type storageFailure struct {
	Reason      string
	BookID      int64
	LibraryID   int64
	LibraryName string
	LibraryPath string
	Path        string
	Err         error
}

// httpStatus 给出每一类的状态码。
//
//	storage_offline     503：依赖的存储暂时不可用，服务本身没坏，插回盘即恢复——
//	                         这不是 500，且 503 会让客户端与缓存把它当可重试的临时故障。
//	file_missing        404：这条路径上的资源确实没了，库里的记录是陈的。
//	archive_unreadable  422：请求与路径都对，但归档内容处理不了（损坏/加密/空档）；
//	                         重试不会好转，也不该让运维去查服务器。
//	path_unreadable     500：权限或 IO 出错，是服务端环境问题，该进错误看板。
func (f storageFailure) httpStatus() int {
	switch f.Reason {
	case storageReasonOffline:
		return http.StatusServiceUnavailable
	case storageReasonFileMissing:
		return http.StatusNotFound
	case storageReasonArchiveUnreadable:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// message 是不认 reason 的老客户端所能看到的全部信息，因此每一类都要自带足够的定位线索。
func (f storageFailure) message() string {
	switch f.Reason {
	case storageReasonOffline:
		if f.LibraryName != "" {
			return fmt.Sprintf("Library %q (%s) is unreachable; check that the storage is connected", f.LibraryName, f.LibraryPath)
		}
		return "The library storage is unreachable; check that the storage is connected"
	case storageReasonFileMissing:
		return fmt.Sprintf("Book file is missing on disk: %s", f.Path)
	case storageReasonPathUnreadable:
		return fmt.Sprintf("Book file cannot be accessed (permission or I/O error): %s", f.Path)
	default:
		return fmt.Sprintf("Book archive cannot be read (corrupt, encrypted or empty): %s", f.Path)
	}
}

func (f storageFailure) response() StorageFailureResponse {
	return StorageFailureResponse{
		Error:       f.message(),
		Reason:      f.Reason,
		LibraryID:   f.LibraryID,
		LibraryName: f.LibraryName,
		LibraryPath: f.LibraryPath,
		Path:        f.Path,
	}
}

// diagnoseStorageFailure 把一个「读不到字节」的错误分到四类之一。
//
// cause 只作为日志证据保留，不参与判定：底层错误的措辞随归档库、平台而变，
// 而磁盘现状是可以当场探测的。探测顺序见分类常量的说明。
func (c *Controller) diagnoseStorageFailure(ctx context.Context, target storageTarget, cause error) storageFailure {
	failure := storageFailure{
		Reason:    storageReasonArchiveUnreadable,
		BookID:    target.BookID,
		LibraryID: target.LibraryID,
		Path:      target.Path,
		Err:       cause,
	}

	if target.LibraryID > 0 {
		if library, err := c.store.GetLibrary(ctx, target.LibraryID); err == nil {
			failure.LibraryName = library.Name
			failure.LibraryPath = library.Path
			// 任何 Stat 错误或「不是目录」都算不可达：拔盘、盘符漂移与 UNC 断连
			// 在各平台上给出的 errno 并不一致，只有「拿不到一个目录」是稳定信号。
			if info, statErr := os.Stat(library.Path); statErr != nil || !info.IsDir() {
				failure.Reason = storageReasonOffline
				if statErr != nil {
					failure.Err = statErr
				}
				return failure
			}
		}
	}

	if _, statErr := os.Stat(target.Path); statErr != nil {
		if os.IsNotExist(statErr) {
			failure.Reason = storageReasonFileMissing
		} else {
			failure.Reason = storageReasonPathUnreadable
		}
		failure.Err = statErr
	}
	return failure
}

// writeStorageFailure 记一条可诊断日志，再按分类下发响应。
// op 标识出错的端点（list_pages / page_image / download_book），日志里据此还原用户当时在做什么。
func writeStorageFailure(w http.ResponseWriter, failure storageFailure, op string) {
	status := failure.httpStatus()
	slog.Error(storageFailureLogMessage,
		"op", op,
		"reason", failure.Reason,
		"status", status,
		"book_id", failure.BookID,
		"library_id", failure.LibraryID,
		"library_name", failure.LibraryName,
		"library_path", failure.LibraryPath,
		"path", failure.Path,
		"error", failure.Err,
	)
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", storageOfflineRetryAfter)
	}
	jsonResponse(w, status, failure.response())
}
