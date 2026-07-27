// 业务说明：本文件收拢元数据 Provider 对外错误的脱敏与截断。
//
// 这些错误会一路传到 HTTP 层并被写进响应体（刮削失败提示、AI 推荐 500 等），
// 因此它们是面向用户的输出，而不只是日志——凡是可能夹带密钥或上游整段响应的地方，
// 都必须在这里先处理干净。
//
// 维护要点：新增 Provider 时，transport 错误一律经 sanitizeTransportError 包装，
// 非 2xx 的上游响应体一律经 truncateUpstreamBody 截断后再进错误串。

package metadata

import (
	"errors"
	"net/url"
	"unicode/utf8"
)

// maxUpstreamBodyInError 是允许出现在错误串里的上游响应体长度上限。
// 上游出错时可能返回整页 HTML（几十 KB），原样拼进错误会灌满日志、撑大响应体，
// 也更容易把上游内部信息带出去。
const maxUpstreamBodyInError = 512

// sanitizeTransportError 剥掉 *url.Error 携带的请求 URL。
//
// http.Client.Do 失败时返回的是 *url.Error，其 Error() 会把完整请求 URL 打印出来。
// Comic Vine 的凭据就在 query 里（api_key=...），于是一次 DNS 失败或超时就足以让
// 明文密钥出现在 HTTP 500 的响应体中——任何已登录用户都能读到。
// 这里只保留底层错误（超时/连接拒绝等），语义不损失，URL 不外泄。
func sanitizeTransportError(err error) error {
	if err == nil {
		return nil
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
}

// truncateUpstreamBody 把上游响应体截到可安全回显的长度，并保证不切断 UTF-8 字符。
func truncateUpstreamBody(body []byte) string {
	if len(body) <= maxUpstreamBodyInError {
		return string(body)
	}
	cut := body[:maxUpstreamBodyInError]
	// 按字节截断可能切在多字节字符中间，逐字节回退到合法边界。
	for len(cut) > 0 && !utf8.Valid(cut) {
		cut = cut[:len(cut)-1]
	}
	return string(cut) + "…(truncated)"
}
