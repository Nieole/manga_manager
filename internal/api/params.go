// 业务说明：本文件是业务实现，属于后端 HTTP API 层，集中提供查询参数的解析与边界钳制。
// 它是分页、列表上限这类「所有端点都要做、做漏一处就出事」的公共约束的唯一事实来源。
// 维护时应保证新增端点复用这里的助手，而不是各自再写一份 Atoi + 手工判断。

package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// 分页与列表规模的统一上界。
//
// 这些常量存在的原因是历史上每个端点各写一套解析：有的钳了上限、有的没钳，
// 于是 /api/series/search 加固之后，/api/stats/recent-read 仍能被要求返回整库。
// 更糟的是分页无上界时 (page-1)*limit 会溢出 int，OPDS 侧算出负 start 后切片直接 panic。
const (
	// maxPageNumber 是任何分页端点允许的最大页码。
	// 取值只需保证 maxPageNumber * maxListLimit 远小于 int64 上限即可安全计算 offset，
	// 同时又远大于任何真实客户端会翻到的页数。
	maxPageNumber = 100_000
	// maxListLimit 是单次列表请求允许返回的最大条数。
	maxListLimit = 500
	// maxListOffset 是 offset 型分页允许的最大偏移。
	maxListOffset = maxPageNumber * maxListLimit

	// 外部元数据源的搜索分页上限。这两个值会原样透传给第三方 API，
	// 上限比站内列表更严：既是保护自己的配额，也是不去踩对方的限流。
	maxScrapeSearchLimit  = 50
	maxScrapeSearchOffset = 1000

	// maxCollectionBatchSize 是「一次批量操作最多涉及多少个条目」的上限，
	// 用于合集批量添加、阅读清单重排这类接受 ID 数组的端点。
	maxCollectionBatchSize = 1000

	// maxBulkReadStateBooks / maxBulkReadStateSeries 是批量标记读状态的两道上限。
	//
	// 单靠 1 MiB 的请求体上限挡不住这条路径：bulkUpdateSeriesProgress 收到的是**系列** id，
	// 每个都会被展开成该系列的全部书，几十个 id 就能变成整库几万本。
	// 而每本书都要写一次进度并刷新所属系列的聚合，全部在写锁下进行。
	maxBulkReadStateBooks  = 20000
	maxBulkReadStateSeries = 2000

	// maxExternalTransferSeries 是外部库传输一次能选多少个系列。
	// 与 maxBulkReadStateSeries 同构、同量级：这个端点收到的也是系列 id，
	// 每个都会被展开成整个系列的书，再逐本读盘拷贝到外接盘。
	maxExternalTransferSeries = 2000
)

// clampLimit 解析 limit 型参数并钳到 [1, max]。
// 空串或非法值回落到 def；超过 max 一律截到 max（而非报错，保持列表接口的宽松语义）。
func clampLimit(raw string, def, max int) int {
	if max <= 0 {
		max = maxListLimit
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		value = def
	}
	if value > max {
		value = max
	}
	if value < 1 {
		value = 1
	}
	return value
}

// clampOffset 解析 offset 型参数并钳到 [0, maxListOffset]。
// 上界的意义不只是防滥用：offset 最终会被转成 int32 传给 SQLite，
// 未钳制时超大值会截断成 0，让「第 100 万页」静默返回第一页。
func clampOffset(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0
	}
	if value > maxListOffset {
		return maxListOffset
	}
	return value
}

// clampPage 解析 page 型参数并钳到 [1, maxPageNumber]。
func clampPage(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return 1
	}
	if value > maxPageNumber {
		return maxPageNumber
	}
	return value
}

// queryLimit / queryOffset / queryPage 是直接从请求取值的便捷包装。
func queryLimit(r *http.Request, key string, def, max int) int {
	return clampLimit(r.URL.Query().Get(key), def, max)
}

func queryOffset(r *http.Request, key string) int {
	return clampOffset(r.URL.Query().Get(key))
}

func queryPage(r *http.Request, key string) int {
	return clampPage(r.URL.Query().Get(key))
}

// pageOffset 由已钳制的 page/limit 算出偏移量。
// 两个入参都过了上面的钳制时，乘积恒在 int64 安全范围内，不会像旧代码那样溢出成负数。
func pageOffset(page, limit int) int64 {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 1
	}
	return int64(page-1) * int64(limit)
}

// 请求体大小上限。
//
// 此前全站没有任何 body 闸门：未鉴权的 /api/auth/login、KOReader 协议端点都能被单个
// 巨型 JSON 打爆内存（json.Decode 会把整个 body 读进来）。这里统一在中间件层设闸，
// 只有明确需要大 body 的上传端点走放宽档。
const (
	// defaultMaxRequestBody 覆盖所有 JSON 端点。1 MiB 足够放下最大的批量操作载荷
	// （约十万个 int64 ID），同时把未鉴权端点的内存放大面收敛掉。
	defaultMaxRequestBody int64 = 1 << 20
	// uploadMaxRequestBody 用于封面上传这类 multipart 端点，在图片上限之上留出表单开销。
	uploadMaxRequestBody int64 = maxCoverUploadBytes + (1 << 20)
)

// bodyLimitExemptSuffixes 列出需要放宽 body 上限的路径后缀（上传类端点）。
var bodyLimitExemptSuffixes = []string{
	"/cover/upload",
}

// RequestBodyLimit 给每个请求的 Body 套上 http.MaxBytesReader。
// 超限时后续的 Decode/ParseMultipartForm 会拿到错误，handler 的既有错误分支即可回 400，
// 且底层不会真的把超量字节读进内存。
func RequestBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			limit := defaultMaxRequestBody
			for _, suffix := range bodyLimitExemptSuffixes {
				if strings.HasSuffix(r.URL.Path, suffix) {
					limit = uploadMaxRequestBody
					break
				}
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// isUniqueConstraintError 判断错误是否为 SQLite 的唯一约束冲突。
//
// 这类冲突是「用户输入撞车」而非服务端故障：改名撞到同名合集/智能筛选时回 500，
// 前端只能显示「服务器错误」，用户不知道改个名字就能过。用文本匹配是因为 modernc/sqlite
// 的错误类型不导出稳定的错误码常量，而这条消息在 SQLite 各版本间是稳定的。
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToUpper(err.Error()), "UNIQUE CONSTRAINT FAILED")
}

// contentDispositionAttachment 生成同时兼容新旧客户端的下载文件名头。
//
// filename= 只允许 ASCII，直接塞中文卷名会让浏览器保存出乱码文件名；RFC 5987 的
// filename*= 才能携带 UTF-8。两者并存：老客户端读前者、新客户端优先后者。
// serveBookFile 早就这么写了，ComicInfo 的两个导出端点却只发 ASCII 版，
// 于是中文标题导出后文件名全是乱码——这里抽出来供三处共用。
func contentDispositionAttachment(asciiFallback, utf8Name string) string {
	if strings.TrimSpace(asciiFallback) == "" {
		asciiFallback = "download"
	}
	if strings.TrimSpace(utf8Name) == "" {
		utf8Name = asciiFallback
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", asciiFallback, url.PathEscape(utf8Name))
}
