// 业务说明：本文件是元数据聚合链路的共享测试工具，提供浮点近似比较与 HTTP mock 响应构造器，
// 供各 Provider（AniList/MangaDex/MAL/ComicVine/Bangumi）的解析与置信度回归测试复用。
package metadata

import (
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

// metaFloatClose 用于对置信度、评分等浮点结果做容差比较，规避 float64 运算的末位误差。
func metaFloatClose(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// metaJSONResponse 构造一个带 JSON body 的 200 响应，供 roundTripFunc 返回。
// roundTripFunc 定义在 openai_legacy_test.go，本包内复用。
func metaJSONResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func assertMetaFloat(t *testing.T, label string, got, want float64) {
	t.Helper()
	if !metaFloatClose(got, want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}
