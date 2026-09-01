// 本文件是业务回归测试，守「书的字节读不出来时，失败必须可诊断」这条契约。
// 现场是一块外置盘被拔掉：整个资料库的书全部取页失败，而响应与日志都说不出是哪个盘掉了。
// 四个分类（存储离线 / 单文件缺失 / 路径不可读 / 归档不可读）各自的状态码、载荷字段与
// 服务端日志都在这里锁死，正常取页不退化也一并守住。

package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"testing"
)

// decodeStorageFailure 把错误响应体解出来，供各子测试断言分类字段。
func decodeStorageFailure(t testing.TB, rec *httptest.ResponseRecorder) StorageFailureResponse {
	t.Helper()
	var got StorageFailureResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode storage failure body failed: %v (body %q)", err, rec.Body.String())
	}
	return got
}

// captureSlog 把默认 logger 换成可断言的捕获器，测完还原。
func captureSlog(t testing.TB) *captureLogHandler {
	t.Helper()
	capture := &captureLogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return capture
}

// findLogRecord 返回第一条 msg 匹配的日志记录及其属性表。
func findLogRecord(capture *captureLogHandler, msg string) (map[string]string, bool) {
	for _, record := range capture.records {
		if record.Message != msg {
			continue
		}
		attrs := map[string]string{}
		record.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		return attrs, true
	}
	return nil, false
}

func TestGetPagesByBookStorageFailureDiagnostics(t *testing.T) {
	t.Run("存储离线时返回 503 并指名是哪个资料库不可达", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		lib, _, book := seedBookFixture(t, store, rootDir, "dmzj", "Series Alpha", "Alpha 01.cbz", 12)
		capture := captureSlog(t)

		// 模拟外置盘被拔掉：整个资料库根目录连同其下的系列目录一起消失。
		if err := os.RemoveAll(lib.Path); err != nil {
			t.Fatalf("remove library root failed: %v", err)
		}

		idStr := strconv.FormatInt(book.ID, 10)
		rec := httptest.NewRecorder()
		controller.getPagesByBook(rec, requestWithRouteParam(http.MethodGet, "/api/pages/"+idStr, nil, "bookId", idStr))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("存储离线应回 503，实际 %d（body %q）", rec.Code, rec.Body.String())
		}
		if retry := rec.Header().Get("Retry-After"); retry == "" {
			t.Fatal("存储离线是可恢复故障，应带 Retry-After")
		}
		got := decodeStorageFailure(t, rec)
		if got.Reason != storageReasonOffline {
			t.Fatalf("expected reason %q, got %q", storageReasonOffline, got.Reason)
		}
		if got.LibraryID != lib.ID || got.LibraryName != "dmzj" || got.LibraryPath != lib.Path {
			t.Fatalf("载荷必须指名资料库，got %+v", got)
		}
		if got.Error == "" {
			t.Fatal("error 文案不能为空——老前端只读这个字段")
		}

		attrs, ok := findLogRecord(capture, storageFailureLogMessage)
		if !ok {
			t.Fatalf("服务端应留下一条可诊断日志，实际只有 %d 条记录", len(capture.records))
		}
		if attrs["reason"] != storageReasonOffline || attrs["library_name"] != "dmzj" || attrs["library_path"] != lib.Path {
			t.Fatalf("日志应能一眼看出是哪个资料库掉了，got %+v", attrs)
		}
		if attrs["path"] != book.Path || attrs["op"] != "list_pages" {
			t.Fatalf("日志应带上出错的路径与端点，got %+v", attrs)
		}
	})

	t.Run("单本文件缺失时返回 404 且不误报存储离线", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, _, book := seedBookFixture(t, store, rootDir, "bilicomic", "Series Beta", "Beta 01.cbz", 5)

		// 资料库根目录还在，只有这一本书没落盘。
		idStr := strconv.FormatInt(book.ID, 10)
		rec := httptest.NewRecorder()
		controller.getPagesByBook(rec, requestWithRouteParam(http.MethodGet, "/api/pages/"+idStr, nil, "bookId", idStr))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("单文件缺失应回 404，实际 %d（body %q）", rec.Code, rec.Body.String())
		}
		got := decodeStorageFailure(t, rec)
		if got.Reason != storageReasonFileMissing {
			t.Fatalf("expected reason %q, got %q", storageReasonFileMissing, got.Reason)
		}
		if got.Path != book.Path {
			t.Fatalf("载荷应指名缺失的那条路径，got %q", got.Path)
		}
	})

	t.Run("归档损坏时既不是离线也不是缺失", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, _, book := seedBookFixture(t, store, rootDir, "manga", "Series Gamma", "Gamma 01.cbz", 3)

		// 文件在、盘也在，但字节不是合法归档。
		if err := os.WriteFile(book.Path, []byte("this is not a zip archive at all"), 0o644); err != nil {
			t.Fatalf("write broken archive failed: %v", err)
		}

		idStr := strconv.FormatInt(book.ID, 10)
		rec := httptest.NewRecorder()
		controller.getPagesByBook(rec, requestWithRouteParam(http.MethodGet, "/api/pages/"+idStr, nil, "bookId", idStr))

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("归档损坏应回 422，实际 %d（body %q）", rec.Code, rec.Body.String())
		}
		got := decodeStorageFailure(t, rec)
		if got.Reason != storageReasonArchiveUnreadable {
			t.Fatalf("expected reason %q, got %q", storageReasonArchiveUnreadable, got.Reason)
		}
	})

	t.Run("存储正常时取页清单不退化", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, _, book := seedBookFixture(t, store, rootDir, "ok-lib", "Series Delta", "Delta 01.cbz", 0)
		seedBookArchivePages(t, controller, book, []byte("page-one"), []byte("page-two"))

		idStr := strconv.FormatInt(book.ID, 10)
		rec := httptest.NewRecorder()
		controller.getPagesByBook(rec, requestWithRouteParam(http.MethodGet, "/api/pages/"+idStr, nil, "bookId", idStr))

		if rec.Code != http.StatusOK {
			t.Fatalf("正常取页应回 200，实际 %d（body %q）", rec.Code, rec.Body.String())
		}
		var pages []struct {
			Number int64  `json:"number"`
			URL    string `json:"url"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &pages); err != nil {
			t.Fatalf("decode pages failed: %v", err)
		}
		if len(pages) != 2 || pages[0].Number != 1 || pages[1].URL != "/api/books/page/"+idStr+"/2" {
			t.Fatalf("页清单形状变了，got %+v", pages)
		}
	})
}

// TestPageImageAndBookFileStorageFailureDiagnostics 守另外两条阅读关键路径：取页图与整卷下载。
// 它们和取页清单一样会因为盘掉了而失败，必须给出同一套分类。
func TestPageImageAndBookFileStorageFailureDiagnostics(t *testing.T) {
	t.Run("存储离线时取页图返回 503 并指名资料库", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		lib, _, book := seedBookFixture(t, store, rootDir, "dmzj", "Series Alpha", "Alpha 01.cbz", 12)
		if err := os.RemoveAll(lib.Path); err != nil {
			t.Fatalf("remove library root failed: %v", err)
		}

		idStr := strconv.FormatInt(book.ID, 10)
		rec := httptest.NewRecorder()
		req := requestWithRouteParams(http.MethodGet, "/api/pages/"+idStr+"/1", nil, map[string]string{"bookId": idStr, "pageNumber": "1"})
		controller.servePageImage(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("存储离线取页图应回 503，实际 %d（body %q）", rec.Code, rec.Body.String())
		}
		got := decodeStorageFailure(t, rec)
		if got.Reason != storageReasonOffline || got.LibraryName != "dmzj" {
			t.Fatalf("取页图也应指名资料库，got %+v", got)
		}
	})

	t.Run("存储离线时整卷下载返回 503 而不是 404", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		lib, _, book := seedBookFixture(t, store, rootDir, "dmzj", "Series Alpha", "Alpha 01.cbz", 12)
		if err := os.RemoveAll(lib.Path); err != nil {
			t.Fatalf("remove library root failed: %v", err)
		}

		idStr := strconv.FormatInt(book.ID, 10)
		rec := httptest.NewRecorder()
		controller.serveBookFile(rec, requestWithRouteParam(http.MethodGet, "/api/books/"+idStr+"/file", nil, "bookId", idStr))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("存储离线整卷下载应回 503，实际 %d（body %q）", rec.Code, rec.Body.String())
		}
		if got := decodeStorageFailure(t, rec); got.Reason != storageReasonOffline {
			t.Fatalf("expected reason %q, got %q", storageReasonOffline, got.Reason)
		}
	})

	t.Run("存储在线但书不在时整卷下载仍是 404", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, _, book := seedBookFixture(t, store, rootDir, "bilicomic", "Series Beta", "Beta 01.cbz", 5)

		idStr := strconv.FormatInt(book.ID, 10)
		rec := httptest.NewRecorder()
		controller.serveBookFile(rec, requestWithRouteParam(http.MethodGet, "/api/books/"+idStr+"/file", nil, "bookId", idStr))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("单文件缺失整卷下载应回 404，实际 %d", rec.Code)
		}
		if got := decodeStorageFailure(t, rec); got.Reason != storageReasonFileMissing {
			t.Fatalf("expected reason %q, got %q", storageReasonFileMissing, got.Reason)
		}
	})

	t.Run("存储正常时整卷下载不退化", func(t *testing.T) {
		controller, store, _, rootDir := newTestController(t)
		_, _, book := seedBookFixture(t, store, rootDir, "ok-lib", "Series Delta", "Delta 01.cbz", 0)
		payload := []byte("PK\x03\x04 whole book bytes")
		if err := os.WriteFile(book.Path, payload, 0o644); err != nil {
			t.Fatalf("write archive failed: %v", err)
		}

		idStr := strconv.FormatInt(book.ID, 10)
		rec := httptest.NewRecorder()
		controller.serveBookFile(rec, requestWithRouteParam(http.MethodGet, "/api/books/"+idStr+"/file", nil, "bookId", idStr))

		if rec.Code != http.StatusOK {
			t.Fatalf("正常整卷下载应回 200，实际 %d", rec.Code)
		}
		if rec.Body.String() != string(payload) {
			t.Fatalf("下载字节不一致")
		}
	})
}

// TestDiagnoseStorageFailureKeepsAmbiguousErrorsOutOfMissing 守 rehome.go 立下的判定原则：
// 只有明确的 IsNotExist 才算「消失」。权限错误不能被算成文件被删。
func TestDiagnoseStorageFailureKeepsAmbiguousErrorsOutOfMissing(t *testing.T) {
	// 这条用例要的前提是「路径确实在，但 Stat 只能得到权限错误」，它靠关掉父目录的执行位来造。
	// 两种环境造不出这个前提，跳过的是夹具而非被测契约：
	//   - root 绕过目录权限位，Stat 照样成功；
	//   - Windows 的 os.Chmod 只翻 FILE_ATTRIBUTE_READONLY 这一个属性位
	//     （syscall.Chmod 见 $GOROOT/src/syscall/syscall_windows.go），既不碰 ACL，
	//     也摘不掉目录的穿越权限，因此关不掉「进得去这个目录」。
	// Windows 上的分类逻辑本身是对的：真的访问被拒时 GetFileAttributesEx 返回 ERROR_ACCESS_DENIED，
	// 而 syscall.Errno.Is 把它归到 ErrPermission、不在 ErrNotExist 那一组里，
	// 于是 os.IsNotExist 为 false，仍旧落到 storageReasonPathUnreadable。
	if os.Geteuid() == 0 {
		t.Skip("root 绕过目录权限位，无法构造权限错误")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows 的 os.Chmod 只切只读属性、摘不掉目录穿越权限，无法构造权限错误")
	}
	controller, store, _, rootDir := newTestController(t)
	lib, series, book := seedBookFixture(t, store, rootDir, "perm-lib", "Series Locked", "Locked 01.cbz", 4)

	// 书的字节必须真的写到盘上：否则「权限错误」只是父目录挡住了穿越、把「文件不存在」盖住的假象，
	// 一旦换到不挡穿越的平台，Stat 就会如实报 IsNotExist，这条用例便会连前提一起塌掉。
	if err := os.WriteFile(book.Path, []byte("PK\x03\x04 locked"), 0o644); err != nil {
		t.Fatalf("write book file failed: %v", err)
	}

	// 关掉系列目录的执行位：库根仍可 Stat，书的路径却只能得到权限错误而非 IsNotExist。
	if err := os.Chmod(series.Path, 0o000); err != nil {
		t.Fatalf("chmod series dir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(series.Path, 0o755) })

	// 先把前提本身钉死：Stat 必须是「出错了，但不是 IsNotExist」。
	// 前提立不住的话后面的断言就算过了也没有意义，这里要立刻报出来。
	if _, statErr := os.Stat(book.Path); statErr == nil || os.IsNotExist(statErr) {
		t.Fatalf("用例前提没立住：期望一个非 IsNotExist 的 Stat 错误，实际 %v", statErr)
	}

	target := storageTarget{BookID: book.ID, LibraryID: lib.ID, Path: book.Path}
	failure := controller.diagnoseStorageFailure(t.Context(), target, os.ErrInvalid)
	if failure.Reason == storageReasonFileMissing {
		t.Fatal("权限错误被误判成文件消失了")
	}
	if failure.Reason != storageReasonPathUnreadable {
		t.Fatalf("expected reason %q, got %q", storageReasonPathUnreadable, failure.Reason)
	}
	if failure.httpStatus() != http.StatusInternalServerError {
		t.Fatalf("路径不可读应回 500，got %d", failure.httpStatus())
	}
}
