// 本文件收拢元数据 Provider 交出上游失败时的脱敏与截断。上游响应体有两个出口——写进 HTTP 响应体
// 的错误串（刮削失败提示、AI 推荐 500）和写进日志的行——两者都可能夹带密钥或上游整段响应，
// 必须在这里处理干净。新增 Provider 时：transport 错误经 sanitizeTransportError 包装；
// 非 2xx 一律调 logUpstreamFailure，它同时产出日志行与可回显的错误串，不要自己拼 slog。

package metadata

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"
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

// logUpstreamFailure 是「上游返回非 2xx」的唯一出口：写一条日志，并返回同一份可安全回显的
// 响应体片段给调用方拼错误串——日志与错误串因此不可能再各用一套截断标准。
//
// 响应体必须先截断：日志文件是按行读的，一行未截断的整页 HTML 错误页落盘后，日志查看端点
// 读到它就再也读不出后面的内容。secret 为空表示该 Provider 的凭据不在 URL 里。
func logUpstreamFailure(msg string, status int, body []byte, secret string, attrs ...any) string {
	safe := redactSecret(truncateUpstreamBody(body), secret)
	slog.Error(msg, append([]any{"status", status, "body", safe}, attrs...)...)
	return safe
}

// redactSecret 把 s 里出现的 secret 换成占位符，原文与 URL 转义两种形态都覆盖。
//
// 用在「上游响应体要被写进日志或 HTTP 错误响应」的路径上：网关的错误页经常把被请求的
// 完整 URI 原样回显，而我们的凭据就在那个 URI 的 query 里。不脱敏等于把密钥从后端搬到前端。
func redactSecret(s, secret string) string {
	if secret == "" || s == "" {
		return s
	}
	s = strings.ReplaceAll(s, secret, "[REDACTED]")
	if escaped := url.QueryEscape(secret); escaped != secret {
		s = strings.ReplaceAll(s, escaped, "[REDACTED]")
	}
	return s
}
