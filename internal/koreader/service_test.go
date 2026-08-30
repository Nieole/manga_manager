// 守 KOReader 同步链路的三类不变量：密钥比对必须为常数时间且形态校验不能漏（否则密钥可被
// 侧信道试出，或任意字符串被当成密钥落库）；自助注册的账号归属不能靠猜（多用户站点下猜错
// 会把别人的阅读进度写进错误账户）；进度载荷字段必须限长，否则被改造过的设备能撑爆数据库。

package koreader

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
)

func newTestService(t *testing.T, matchMode string) (*Service, database.Store, string) {
	t.Helper()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &config.Config{}
	cfg.Database.Path = dbPath
	cfg.KOReader.MatchMode = matchMode
	config.NormalizeConfig(cfg)

	return NewService(store, config.NewManager(cfg)), store, tempDir
}

// fakeTaskHandle 是 TaskHandle 的手写假体：批循环干活所需的三样资格全在这里，用例据此
// 单独摆布其中任何一样。生产实现是**任务句柄**，它的闸门与令牌语义由 internal/taskrun
// 与 internal/diskwork 自己的用例守，本包不重测。
type fakeTaskHandle struct {
	// diskErr 非 nil 时 Disk 直接回绝、闭包一步不执行——那正是**暂停闸门**或**存储令牌**
	// 把一本书挡在门外的样子。
	diskErr error

	// advances 记下每次**计数推进**报到的 current，works 记下每一次经手的**磁盘作业**。
	advances []int
	works    []diskwork.Work
}

func (h *fakeTaskHandle) Advance(current, _ int) {
	h.advances = append(h.advances, current)
}

// Checkpoint 照生产闸门的语义只答取消：未暂停时闸门返回的就是上下文错误。
func (h *fakeTaskHandle) Checkpoint(ctx context.Context) error {
	return ctx.Err()
}

func (h *fakeTaskHandle) Disk(_ context.Context, work diskwork.Work, fn func() error) error {
	h.works = append(h.works, work)
	if h.diskErr != nil {
		return h.diskErr
	}
	return fn()
}

func seedServiceBook(t *testing.T, store database.Store, rootDir, libraryName, seriesName, bookName string) (database.Library, database.Book) {
	t.Helper()

	libPath := filepath.Join(rootDir, libraryName)
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("mkdir library failed: %v", err)
	}

	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:                libraryName,
		Path:                libPath,
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}

	seriesPath := filepath.Join(libPath, seriesName)
	if err := os.MkdirAll(seriesPath, 0o755); err != nil {
		t.Fatalf("mkdir series failed: %v", err)
	}

	series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        seriesName,
		Path:        seriesPath,
		NameInitial: database.SeriesInitial("", seriesName),
	})
	if err != nil {
		t.Fatalf("create series failed: %v", err)
	}

	bookPath := filepath.Join(seriesPath, bookName)
	book, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           bookName,
		Path:           bookPath,
		Size:           16,
		FileModifiedAt: time.Now(),
		PageCount:      32,
		Title:          sql.NullString{String: bookName, Valid: true},
	})
	if err != nil {
		t.Fatalf("create book failed: %v", err)
	}

	return lib, book
}

func loadBookIdentity(t *testing.T, store database.Store, bookID int64) (string, string, string) {
	t.Helper()

	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatal("store is not SqlStore")
	}

	var fileHash, pathFingerprint, pathFingerprintNoExt sql.NullString
	err := sqlStore.DB().QueryRow(`
		SELECT file_hash, path_fingerprint, path_fingerprint_no_ext
		FROM books
		WHERE id = ?
	`, bookID).Scan(&fileHash, &pathFingerprint, &pathFingerprintNoExt)
	if err != nil {
		t.Fatalf("load book identity failed: %v", err)
	}

	return fileHash.String, pathFingerprint.String, pathFingerprintNoExt.String
}

func TestRebuildBookIdentitiesProcessesAllBatches(t *testing.T) {
	service, store, rootDir := newTestService(t, config.KOReaderMatchModeBinaryHash)

	_, book1 := seedServiceBook(t, store, rootDir, "LibraryA", "SeriesA", "Book1.cbz")
	_, book2 := seedServiceBook(t, store, rootDir, "LibraryB", "SeriesB", "Book2.cbz")
	_, book3 := seedServiceBook(t, store, rootDir, "LibraryC", "SeriesC", "Book3.cbz")

	for _, book := range []database.Book{book1, book2, book3} {
		if err := os.WriteFile(book.Path, []byte(book.Name), 0o644); err != nil {
			t.Fatalf("write book file failed: %v", err)
		}
	}

	updated, total, err := service.RebuildBookIdentities(context.Background(), RebuildOptions{BatchSize: 2}, &fakeTaskHandle{})
	if err != nil {
		t.Fatalf("rebuild identities failed: %v", err)
	}
	if total != 3 || updated != 3 {
		t.Fatalf("expected updated=3 total=3, got updated=%d total=%d", updated, total)
	}

	for _, book := range []database.Book{book1, book2, book3} {
		fileHash, _, _ := loadBookIdentity(t, store, book.ID)
		if fileHash == "" {
			t.Fatalf("expected file hash for book %d", book.ID)
		}
	}
}

func TestRebuildBookIdentitiesUsesPathIndexesInFilePathMode(t *testing.T) {
	service, store, rootDir := newTestService(t, config.KOReaderMatchModeFilePath)

	lib, book := seedServiceBook(t, store, rootDir, "Library", "Parent/Series", "Volume01.cbz")
	_ = lib

	updated, total, err := service.RebuildBookIdentities(context.Background(), RebuildOptions{BatchSize: 1}, &fakeTaskHandle{})
	if err != nil {
		t.Fatalf("rebuild identities failed: %v", err)
	}
	if total != 1 || updated != 1 {
		t.Fatalf("expected updated=1 total=1, got updated=%d total=%d", updated, total)
	}

	fileHash, pathFingerprint, pathFingerprintNoExt := loadBookIdentity(t, store, book.ID)
	if fileHash != "" {
		t.Fatalf("expected file hash to remain empty in file_path mode, got %q", fileHash)
	}
	if pathFingerprint == "" || pathFingerprintNoExt == "" {
		t.Fatalf("expected path fingerprints to be built, got exact=%q noext=%q", pathFingerprint, pathFingerprintNoExt)
	}
}

func TestAuthenticateUsesKOReaderSyncKey(t *testing.T) {
	service, store, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)

	if _, err := store.CreateKOReaderAccount(context.Background(), database.CreateKOReaderAccountParams{
		Username: "reader",
		SyncKey:  "secret-key",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateKOReaderAccount failed: %v", err)
	}

	if _, err := service.Authenticate(context.Background(), Credentials{
		Username: "reader",
		Key:      HashKey("secret-key"),
	}); err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
}

func TestAuthenticateRejectsLegacyInvalidStoredKey(t *testing.T) {
	service, store, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)

	if _, err := store.CreateKOReaderAccount(context.Background(), database.CreateKOReaderAccountParams{
		Username: "reader",
		SyncKey:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateKOReaderAccount failed: %v", err)
	}

	if _, err := service.Authenticate(context.Background(), Credentials{
		Username: "reader",
		Key:      HashKey("secret-key"),
	}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for mismatched client hash, got %v", err)
	}
}

func TestCreateAccountGeneratesKey(t *testing.T) {
	service, _, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)

	account, err := service.CreateAccount(context.Background(), "reader")
	if err != nil {
		t.Fatalf("CreateAccount failed: %v", err)
	}
	if account.Username != "reader" || !IsValidSyncKey(account.SyncKey) {
		t.Fatalf("unexpected account payload: %+v", account)
	}
}

func TestCreateAccountAuthenticatesWithClientHashedHeader(t *testing.T) {
	service, _, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)

	account, err := service.CreateAccount(context.Background(), "reader")
	if err != nil {
		t.Fatalf("CreateAccount failed: %v", err)
	}

	if _, err := service.Authenticate(context.Background(), Credentials{
		Username: account.Username,
		Key:      HashKey(account.SyncKey),
	}); err != nil {
		t.Fatalf("Authenticate failed with client-hashed key: %v", err)
	}
}

func TestSaveProgressLastWriteWinsAllowsRollback(t *testing.T) {
	service, store, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)
	ctx := context.Background()

	if _, err := store.CreateKOReaderAccount(ctx, database.CreateKOReaderAccountParams{
		Username: "reader",
		SyncKey:  "secret-key",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateKOReaderAccount failed: %v", err)
	}

	creds := Credentials{Username: "reader", Key: HashKey("secret-key")}

	if _, err := service.SaveProgress(ctx, creds, ProgressPayload{
		Document:   "doc-1",
		Progress:   "/body/DocFragment[3]",
		Percentage: 0.80,
		Device:     "Kobo",
		DeviceID:   "device-a",
	}); err != nil {
		t.Fatalf("first SaveProgress failed: %v", err)
	}

	// A lower-percentage push must now win (rollback / re-read).
	result, err := service.SaveProgress(ctx, creds, ProgressPayload{
		Document:   "doc-1",
		Progress:   "/body/DocFragment[1]",
		Percentage: 0.10,
		Device:     "Kobo",
		DeviceID:   "device-a",
	})
	if err != nil {
		t.Fatalf("second SaveProgress failed: %v", err)
	}
	if result.Record.Percentage != 0.10 {
		t.Fatalf("expected rollback to win, stored percentage = %v, want 0.10", result.Record.Percentage)
	}

	stored, err := service.GetProgress(ctx, creds, "doc-1")
	if err != nil {
		t.Fatalf("GetProgress failed: %v", err)
	}
	if stored.Percentage != 0.10 || stored.Progress != "/body/DocFragment[1]" {
		t.Fatalf("GetProgress returned %+v, want percentage 0.10 progress /body/DocFragment[1]", stored)
	}

	// The regressed push must be recorded as a diagnostic sync_event.
	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatal("store is not SqlStore")
	}
	var count int
	if err := sqlStore.DB().QueryRow(`
		SELECT COUNT(*) FROM koreader_sync_events
		WHERE direction = 'push' AND status = 'progress_regressed' AND document = 'doc-1'
	`).Scan(&count); err != nil {
		t.Fatalf("count regression events failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 progress_regressed event, got %d", count)
	}
}

// TestSaveProgressRejectsOversizedFields 锁住 kosync 进度载荷的字段长度上限。
// 这些字段直接落库（document 还进 UNIQUE 索引），无上限时被改造过的设备可以
// 把单条记录撑到任意大小，反复推送即可撑爆数据库。
func TestSaveProgressRejectsOversizedFields(t *testing.T) {
	cases := []struct {
		name    string
		payload ProgressPayload
	}{
		{"document too long", ProgressPayload{
			Document: strings.Repeat("a", maxProgressDocumentLen+1),
			Progress: "0.5", Device: "kobo", DeviceID: "dev1",
		}},
		{"progress too long", ProgressPayload{
			Document: "doc", Progress: strings.Repeat("a", maxProgressValueLen+1),
			Device: "kobo", DeviceID: "dev1",
		}},
		{"device too long", ProgressPayload{
			Document: "doc", Progress: "0.5",
			Device: strings.Repeat("a", maxProgressDeviceLen+1), DeviceID: "dev1",
		}},
		{"device id too long", ProgressPayload{
			Document: "doc", Progress: "0.5", Device: "kobo",
			DeviceID: strings.Repeat("a", maxProgressDeviceLen+1),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateProgressPayloadLengths(tc.payload); err == nil {
				t.Fatal("expected oversized field to be rejected")
			}
		})
	}

	ok := ProgressPayload{Document: "series/vol01.cbz", Progress: "0.42", Device: "kobo", DeviceID: "dev1"}
	if err := validateProgressPayloadLengths(ok); err != nil {
		t.Fatalf("expected ordinary payload to pass, got %v", err)
	}
}

// TestAuthenticateKeyComparisonMatrix 覆盖密钥比对的全部分支。
//
// 这三个 kosync 端点未鉴权、无失败限流也无锁定，客户端提交值完全可控——密钥比对因此是
// 时序侧信道最理想的靶子。用例本身测不出「是否恒定时间」（Go 里没有可靠的计时断言），
// 它保证的是改成 subtle 之后语义没走样：两种历史形态的 sync_key 都仍能通过，其余一律拒。
func TestAuthenticateKeyComparisonMatrix(t *testing.T) {
	rawSecret := "secret-key"
	selfRegHash := HashKey("device-password")

	cases := []struct {
		name      string
		username  string
		seed      bool
		storedKey string
		enabled   bool
		clientKey string
		wantErr   error
	}{
		{
			name: "管理端建号：客户端发 md5(原始密钥)", username: "admin-made", seed: true,
			storedKey: rawSecret, enabled: true, clientKey: HashKey(rawSecret), wantErr: nil,
		},
		{
			name: "设备自助注册：库里存的就是客户端 md5", username: "self-reg", seed: true,
			storedKey: selfRegHash, enabled: true, clientKey: selfRegHash, wantErr: nil,
		},
		{
			name: "密钥不匹配", username: "wrong-key", seed: true,
			storedKey: rawSecret, enabled: true, clientKey: HashKey("nope"), wantErr: ErrUnauthorized,
		},
		{
			// 长度不同的分支：ConstantTimeCompare 在长度不等时直接返 0。
			name: "客户端提交的密钥长度不同", username: "short-key", seed: true,
			storedKey: rawSecret, enabled: true, clientKey: "abc", wantErr: ErrUnauthorized,
		},
		{
			name: "账号不存在", username: "ghost", seed: false,
			clientKey: HashKey(rawSecret), wantErr: ErrForbidden,
		},
		{
			name: "账号已禁用", username: "disabled", seed: true,
			storedKey: rawSecret, enabled: false, clientKey: HashKey(rawSecret), wantErr: ErrForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)
			ctx := context.Background()
			if tc.seed {
				if _, err := store.CreateKOReaderAccount(ctx, database.CreateKOReaderAccountParams{
					Username: tc.username, SyncKey: tc.storedKey, Enabled: tc.enabled,
				}); err != nil {
					t.Fatalf("CreateKOReaderAccount: %v", err)
				}
			}

			_, err := svc.Authenticate(ctx, Credentials{Username: tc.username, Key: tc.clientKey})
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Authenticate = %v, want success", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Authenticate = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestRegisterDeviceRejectsMalformedCredentials 守卫未鉴权注册端点的输入形状。
//
// kosync 协议里 password 就是 md5 十六进制串，必须校验其形状——不校验的话任意字符串
// 都能被当成密钥存进 koreader_accounts（username 还进 UNIQUE 索引）。每条拒绝用例都
// 额外断言**账号没有落库**——只返错却把行写进去，等于没挡住。
func TestRegisterDeviceRejectsMalformedCredentials(t *testing.T) {
	validHash := HashKey("pw")

	cases := []struct {
		name     string
		username string
		password string
	}{
		{"空用户名", "", validHash},
		{"空密码", "device", ""},
		{"密码不是 md5 十六进制", "device", "not-a-md5-hash"},
		{"密码长度不足 32", "device", "abcdef0123456789"},
		{"密码含非十六进制字符", "device", strings.Repeat("z", 32)},
		{"用户名超长", strings.Repeat("a", 1024), validHash},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)
			ctx := context.Background()

			if _, err := svc.RegisterDevice(ctx, tc.username, tc.password); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("RegisterDevice = %v, want ErrUnauthorized", err)
			}
			if tc.username != "" {
				if _, err := store.GetKOReaderAccountByUsername(ctx, tc.username); !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("被拒绝的注册不应落库，GetKOReaderAccountByUsername = %v", err)
				}
			}
		})
	}

	t.Run("合法凭据仍可注册", func(t *testing.T) {
		svc, _, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)
		ctx := context.Background()
		account, err := svc.RegisterDevice(ctx, "device", validHash)
		if err != nil {
			t.Fatalf("RegisterDevice: %v", err)
		}
		// 注册后必须立刻能用同一个 md5 认证（该值同时也是后续请求的 x-auth-key）。
		if _, err := svc.Authenticate(ctx, Credentials{Username: account.Username, Key: validHash}); err != nil {
			t.Fatalf("注册后立即认证失败: %v", err)
		}
	})
}

// createServiceTestUser 建一个站点用户（role 由调用方指定）。
func createServiceTestUser(t *testing.T, store database.Store, username, role string) database.User {
	t.Helper()
	user, err := store.CreateUser(context.Background(), database.CreateUserParams{
		Username:     username,
		PasswordHash: "x",
		Role:         role,
		DisplayName:  username,
	})
	if err != nil {
		t.Fatalf("create user %q failed: %v", username, err)
	}
	return user
}

// TestRegisterDeviceDoesNotBindToAdminInMultiUserSite 守卫「自助注册的账号不会被错误归属」。
//
// 设备自助注册没有站点用户上下文，账号归属不能靠猜——多用户站点里把新账号绑到某个管理员
// 就是错误归属：SaveProgress 会顺着账户的 user_id 把进度写进**管理员**的 user_book_progress
// 与 user_reading_activity，普通用户读到哪管理员的进度就跳到哪（还会计入管理员的热力图与
// 连续天数），而他自己账号下的进度全丢。
func TestRegisterDeviceDoesNotBindToAdminInMultiUserSite(t *testing.T) {
	service, store, rootDir := newTestService(t, "file_path")
	ctx := context.Background()

	admin := createServiceTestUser(t, store, "admin", database.RoleAdmin)
	createServiceTestUser(t, store, "bob", database.RoleRegular)

	_, book := seedServiceBook(t, store, rootDir, "Library A", "Series Alpha", "Volume01.cbz")
	// 预置一个**小于总页数**的已读进度：seedServiceBook 的书 PageCount=32，
	// 而 applyBookProgress 有「不回退」保护（page < prev 直接返回），
	// 预置值若 >= 32，缺陷代码下第二条断言会假绿。
	if err := store.SetUserBookProgress(ctx, admin.ID, book.ID, 4, time.Now()); err != nil {
		t.Fatalf("预置管理员进度失败: %v", err)
	}

	account, err := service.RegisterDevice(ctx, "stranger-device", HashKey("hunter2"))
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}

	// 三条断言彼此独立，用 Errorf 而不是 Fatalf：否则第一条挂掉后另两条不求值，
	// 回退修复时看不出它们是否也守得住。
	userID, err := store.GetKOReaderAccountUserID(ctx, account.Username)
	if err != nil {
		t.Fatalf("GetKOReaderAccountUserID: %v", err)
	}
	if userID != 0 {
		t.Errorf("自助注册的账号被绑到了 user_id=%d —— 多用户站点无从判断它属于谁，"+
			"挂到首个管理员名下会让别人的阅读进度写进管理员账户", userID)
	}

	// 走一次真实的进度上报，确认管理员的数据没有被改写。
	// 先建立书籍指纹索引：CreateBook 不写 path_fingerprint，不跑这一步的话下面的上报
	// 匹配不到任何书，applyBookProgress 根本不会被调用——两条断言就成了永远为真的空断言。
	if _, _, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 10}, &fakeTaskHandle{}); err != nil {
		t.Fatalf("RebuildBookIdentities: %v", err)
	}

	// file_path 模式下服务端拿到的 document 是**设备上的原始路径**，由服务端自己算指纹，
	// 所以这里要传相对路径而不是已经算好的哈希（传哈希会被再哈希一遍，永远匹配不上）。
	// 下面的 res.Matched 守卫保证这次上报确实走到了写进度的分支——否则两条断言都是空断言。
	res, err := service.SaveProgress(ctx, Credentials{Username: account.Username, Key: HashKey("hunter2")}, ProgressPayload{
		Document:   "Series Alpha/Volume01.cbz",
		Percentage: 1.0,
		Progress:   "/body/DocFragment[32]",
		Device:     "Kindle",
		DeviceID:   "device-x",
	})
	if err != nil {
		t.Fatalf("SaveProgress 失败: %v —— 用例必须真的走到写进度的分支才有判别力", err)
	}
	if !res.Matched {
		t.Fatalf("上报没有匹配到书籍（%+v）—— 用例必须真的走到写进度的分支才有判别力", res)
	}

	progress, ok, err := store.GetUserBookProgress(ctx, admin.ID, book.ID)
	if err != nil {
		t.Fatalf("GetUserBookProgress: %v", err)
	}
	if ok && progress.LastReadPage.Valid && progress.LastReadPage.Int64 > 4 {
		t.Errorf("管理员的阅读进度被推到了第 %d 页 —— 陌生设备的进度写进了管理员账户",
			progress.LastReadPage.Int64)
	}

	sqlStore, ok2 := store.(*database.SqlStore)
	if !ok2 {
		t.Fatalf("需要 *SqlStore 才能直查活动表，得到 %T", store)
	}
	var activityRows int
	if err := sqlStore.DB().QueryRow(
		`SELECT COUNT(*) FROM user_reading_activity WHERE user_id = ?`, admin.ID).Scan(&activityRows); err != nil {
		t.Fatalf("查 user_reading_activity: %v", err)
	}
	if activityRows != 0 {
		t.Errorf("管理员的活动记录多出了 %d 行 —— 陌生设备的阅读会计入管理员的热力图与连续天数",
			activityRows)
	}
}

// TestRegisterDeviceBindsToSoleUserInSingleUserSite 是反向护栏。
//
// 单用户站点里唯一的用户就是唯一可能的归属，这个猜测无歧义，必须保留
// ——否则绝大多数部署会从「进度能同步到我账户」退化成「进度谁也看不到」。
func TestRegisterDeviceBindsToSoleUserInSingleUserSite(t *testing.T) {
	service, store, _ := newTestService(t, "file_path")
	ctx := context.Background()

	admin := createServiceTestUser(t, store, "admin", database.RoleAdmin)

	account, err := service.RegisterDevice(ctx, "my-kindle", HashKey("hunter2"))
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	userID, err := store.GetKOReaderAccountUserID(ctx, account.Username)
	if err != nil {
		t.Fatalf("GetKOReaderAccountUserID: %v", err)
	}
	if userID != admin.ID {
		t.Fatalf("单用户站点的自助注册账号 user_id=%d，期望 %d —— "+
			"绝大多数部署会因此变成「进度谁也看不到」", userID, admin.ID)
	}
}
