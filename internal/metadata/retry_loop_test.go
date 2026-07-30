// 业务说明：本文件守卫「重试时请求被重建」这条不变量。
//
// 五个数据源的重试循环形状一致：429 或 5xx 且未超次数就退避重试。其中 Bangumi 与 AniList
// 发的是 POST，body 是一个 bytes.Reader——**读一次就空了**。所以 request 必须在循环内
// 每轮重建；把 http.NewRequestWithContext 提到循环外（一个看起来很自然的「优化」）
// 会让第二次请求带着空 body 发出去，上游返回 400，而日志里只会留下一条
// 「重试后仍失败」，看不出是 body 空了。
//
// 只覆盖这两个 POST 源是刻意的：另外三个走 GET、body 恒为 nil，
// 「两次请求体相同」在它们身上是 0 == 0，一条恒等式，没有判别力。
//
// 注意两点：
//   - provider 实例必须在**每个子测试内部**新建。建在表格行上再配 t.Parallel()
//     会让多个子测试并发改写同一个 httpClient，CI 的 go test -race 必报 DATA RACE。
//   - 不测「重试耗尽」：退避是 1+2+4 秒（base delay 是包级 const，注入不进去），
//     那条路径要 7 秒，不值得拖慢整个套件。

package metadata

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingTransport 记下每次请求的 body，并按脚本返回状态码。
type recordingTransport struct {
	mu       sync.Mutex
	bodies   []string
	statuses []int
	body     string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var payload string
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		payload = string(raw)
	}
	t.bodies = append(t.bodies, payload)

	status := http.StatusOK
	if len(t.bodies) <= len(t.statuses) {
		status = t.statuses[len(t.bodies)-1]
	}
	header := make(http.Header)
	if status == http.StatusTooManyRequests {
		// 1 秒是能拿到的最小退避：Retry-After 的粒度是整数秒，
		// 而填 0 会被判成「无法解析」并回落到 backoffDelay(0)，同样是 1 秒。
		header.Set("Retry-After", "1")
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Request:    req,
	}, nil
}

func (t *recordingTransport) recorded() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.bodies))
	copy(out, t.bodies)
	return out
}

func TestPostProvidersRebuildRequestOnRetry(t *testing.T) {
	cases := []struct {
		name string
		// newProvider 在子测试内部构造 provider 并装上 stub，返回可调用的搜索闭包。
		newProvider func(rt http.RoundTripper) func(ctx context.Context, title string) error
		okBody      string
	}{
		{
			name:   "Bangumi",
			okBody: `{"data":[]}`,
			newProvider: func(rt http.RoundTripper) func(context.Context, string) error {
				p := NewBangumiProvider()
				// 整体替换 client 会连带丢掉 withProviderBudget 装的并发闸门。
				// 这与既有测试的做法一致且是刻意的——闸门本身由 provider_budget_test.go 覆盖，
				// 这里要验的是重试循环，不要让闸门的排队掺进时序。
				p.httpClient = &http.Client{Transport: rt, Timeout: 5 * time.Second}
				return func(ctx context.Context, title string) error {
					_, _, err := p.SearchMetadata(ctx, title, 1, 0)
					return err
				}
			},
		},
		{
			name:   "AniList",
			okBody: `{"data":{"Page":{"pageInfo":{"total":0},"media":[]}}}`,
			newProvider: func(rt http.RoundTripper) func(context.Context, string) error {
				p := NewAniListProvider()
				p.httpClient = &http.Client{Transport: rt, Timeout: 5 * time.Second}
				return func(ctx context.Context, title string) error {
					_, _, err := p.SearchMetadata(ctx, title, 1, 0)
					return err
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &recordingTransport{
				statuses: []int{http.StatusTooManyRequests, http.StatusOK},
				body:     tc.okBody,
			}
			search := tc.newProvider(rt)

			if err := search(context.Background(), "One Piece"); err != nil {
				t.Fatalf("429 之后重试仍失败: %v", err)
			}

			bodies := rt.recorded()
			if len(bodies) != 2 {
				t.Fatalf("发了 %d 次请求，期望 2 次（一次 429 + 一次重试）", len(bodies))
			}
			if bodies[0] == "" {
				t.Fatalf("%s 是 POST 源，第一次请求就没有 body，用例失去判别力", tc.name)
			}
			if bodies[1] != bodies[0] {
				t.Fatalf("重试请求的 body 与首次不同：首次 %d 字节、重试 %d 字节 —— "+
					"body 是 bytes.Reader，读一次就空了，request 必须在循环内每轮重建",
					len(bodies[0]), len(bodies[1]))
			}
		})
	}
}

// TestRetryLoopHonoursContextCancellation 保证退避期间的取消能立刻返回。
//
// 与 provider_budget_test.go 里那条「取消时不等令牌」看起来像重复，实际是两条不同路径：
// 那条在 budgetedTransport 等并发令牌时被取消，这条在 sleepWithContext 退避时被取消。
// 少了这条，用户点「取消刮削」后任务还要在退避里干等最多 4 秒才响应。
func TestRetryLoopHonoursContextCancellation(t *testing.T) {
	rt := &recordingTransport{
		statuses: []int{http.StatusTooManyRequests, http.StatusOK},
		body:     `{"data":[]}`,
	}
	p := NewBangumiProvider()
	p.httpClient = &http.Client{Transport: rt, Timeout: 5 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	// 首次请求返回 429 之后进入退避（1 秒），此时取消。
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, _, err := p.SearchMetadata(ctx, "One Piece", 1, 0)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("退避期间被取消却返回了成功")
	}
	if elapsed >= time.Second {
		t.Fatalf("取消后等了 %v 才返回 —— 退避没有响应取消信号", elapsed)
	}
}
