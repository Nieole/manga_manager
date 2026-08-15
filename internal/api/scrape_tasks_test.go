// 守全库/单库两条刮削路径经启动入口之后的三件事：**终态**文案码各按自己的**作用域**落地、
// 进度是一整帧报出去的，以及**重启函数**仍能从任务参数里读回刮削源。
//
// 文案码串了，用户分不清刚才点的是全库还是某个资料库；帧撕开了，任务面板上的指标与进度条来自
// 两个不同的时刻；刮削源丢了，一次重试会静默换成默认源去请求。

package api

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// scrapeTaskStore 只实现两个刮削启动点真正会调到的那几个方法。其余方法留给内嵌的 nil 接口——
// 任务体若走到没实现的那条路会当场炸掉，而不是拿到一个零值继续跑完。
type scrapeTaskStore struct {
	database.Store

	libraries []database.Library
	series    map[int64][]database.Series
}

// newScrapeTaskStore 造一个库、两个系列。两个是下限：取消用例要让第一条把任务取消掉、
// 第二条撞上**暂停闸门**，帧完整性用例要比对相邻两帧。
//
// 系列的简介与出版社留空，否则单库入口会把它们当作「元数据已齐」跳过，entries 就是空的。
func newScrapeTaskStore() *scrapeTaskStore {
	return &scrapeTaskStore{
		libraries: []database.Library{{ID: 7, Name: "Library A"}},
		series: map[int64][]database.Series{
			7: {{ID: 1, Name: "Alpha"}, {ID: 2, Name: "Beta"}},
		},
	}
}

func (s *scrapeTaskStore) ListLibraries(context.Context) ([]database.Library, error) {
	return s.libraries, nil
}

func (s *scrapeTaskStore) ListSeriesByLibraryLite(_ context.Context, libraryID int64) ([]database.Series, error) {
	return s.series[libraryID], nil
}

func (s *scrapeTaskStore) GetLibrary(_ context.Context, id int64) (database.Library, error) {
	for _, lib := range s.libraries {
		if lib.ID == id {
			return lib, nil
		}
	}
	return database.Library{}, sql.ErrNoRows
}

// scrapeTestProvider 是一个不落库的刮削源：每次请求按 fetchErr 决定这一条是「源报错」还是
// 「没找到」。两条路都在写入审阅队列之前折返，因此用例既不需要数据库，也不必等那 500ms 的
// 速率限制——它只在成功入队之后才走到。
//
// 每次请求顺带把假时钟推过一个投递窗口：用例够不到两次进度事件之间的那一刻，不推的话同一
// **阶段**里连续几帧只有第一帧能被投递出去，相邻两帧就没得比对了。
type scrapeTestProvider struct {
	clock    *fakeClock
	fetchErr error

	// requestedKeys 记下 getProvider 每次收到的刮削源键，供重试用例断言那个键确实从任务参数里读回。
	requestedKeys []string
	titles        []string
	onFetch       func(title string)
}

func (p *scrapeTestProvider) Name() string { return "TestProvider" }

func (p *scrapeTestProvider) FetchSeriesMetadata(_ context.Context, title string) (*metadata.SeriesMetadata, error) {
	p.titles = append(p.titles, title)
	p.clock.advance(taskProgressPublishInterval * 2)
	if p.onFetch != nil {
		p.onFetch(title)
	}
	return nil, p.fetchErr
}

func (p *scrapeTestProvider) SearchMetadata(context.Context, string, int, int) ([]*metadata.SeriesMetadata, int, error) {
	return nil, 0, nil
}

// newScrapeTaskRig 拼出两个刮削任务体需要的那几样：任务引擎（仍经它唯一的 seam 构造，后台能力
// 换成同步执行版）、存储与刮削源工厂。
func newScrapeTaskRig(t *testing.T, provider *scrapeTestProvider) (*Controller, func() []TaskStatus) {
	t.Helper()
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	provider.clock = clock
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)
	e.now = clock.Now

	cfg := &config.Config{}
	config.NormalizeConfig(cfg)
	c := &Controller{taskEngine: e, store: newScrapeTaskStore(), config: config.NewManager(cfg)}
	c.providerFactory = func(name string) metadata.Provider {
		provider.requestedKeys = append(provider.requestedKeys, name)
		return provider
	}
	return c, snapshots
}

// scrapeScopes 是两条刮削路径的全部差异：启动方式、**任务键**、**作用域**显示名与三条终态文案码。
// 每条用例都按它跑两遍——这批任务的形状本就是「同一个任务体、两套作用域文案」。
var scrapeScopes = []struct {
	name         string
	key          string
	scopeName    string
	launch       func(*Controller) error
	completeCode string
	cancelCode   string
}{
	{
		name: "全库", key: "scrape_all_series", scopeName: "全库",
		launch:       func(c *Controller) error { return c.launchBatchScrapeAllSeriesTask(context.Background(), "test") },
		completeCode: "task.msg.scrape.complete_all", cancelCode: "task.msg.scrape.cancelled_all",
	},
	{
		name: "单库", key: "scrape_library_7", scopeName: "Library A",
		launch:       func(c *Controller) error { return c.launchLibraryScrapeTask(context.Background(), 7, "test") },
		completeCode: "task.msg.scrape.complete_library", cancelCode: "task.msg.scrape.cancelled_library",
	},
}

// TestScrapeCompletionCodeSplitsByScope 守**完成**文案码按作用域分岔，且成功计数由占位参数承载
// 而不是后端拼成一句。一条都没刮到不是失败——外部源查无此条是这个任务最常见的结果。
func TestScrapeCompletionCodeSplitsByScope(t *testing.T) {
	for _, tc := range scrapeScopes {
		t.Run(tc.name, func(t *testing.T) {
			c, snapshots := newScrapeTaskRig(t, &scrapeTestProvider{})

			if err := tc.launch(c); err != nil {
				t.Fatalf("启动刮削失败: %v", err)
			}

			task := lastPublishedTask(t, snapshots(), tc.key)
			if task.Status != "completed" || task.MessageCode != tc.completeCode {
				t.Fatalf("终态为 %q / %q, want completed + %q", task.Status, task.MessageCode, tc.completeCode)
			}
			if task.MessageParams["success"] != "0" || task.MessageParams["total"] != "2" {
				t.Fatalf("完成文案的占位参数为 %v, want success=0 total=2", task.MessageParams)
			}
		})
	}
}

// TestScrapeCancellationCodeSplitsByScope 守取消分支：任务体只把取消错误返回上去，终态由引擎裁决，
// 文案码同样按作用域分岔。任务体自己写终态的话，「忘了收尾」与「收了两次」都不会有编译错误。
func TestScrapeCancellationCodeSplitsByScope(t *testing.T) {
	for _, tc := range scrapeScopes {
		t.Run(tc.name, func(t *testing.T) {
			provider := &scrapeTestProvider{}
			c, snapshots := newScrapeTaskRig(t, provider)
			// 第一条请求刚发出就按下取消：第二轮循环的**暂停闸门**因此返回取消错误。
			provider.onFetch = func(string) { _ = c.taskEngine.cancel(tc.key) }

			if err := tc.launch(c); err != nil {
				t.Fatalf("启动刮削失败: %v", err)
			}

			if len(provider.titles) != 1 {
				t.Fatalf("取消后仍请求了 %d 次刮削源, want 1", len(provider.titles))
			}
			task := lastPublishedTask(t, snapshots(), tc.key)
			if task.Status != "cancelled" || task.MessageCode != tc.cancelCode {
				t.Fatalf("终态为 %q / %q, want cancelled + %q", task.Status, task.MessageCode, tc.cancelCode)
			}
		})
	}
}

// TestScrapeTaskDeclarationLandsWhole 守任务诞生那一刻就带齐**作用域**显示名、刮削源参数与标签。
// 拆成启动之后的补写会留下一个「任务已在列表里、却还不知道用的哪个源」的窗口，任务列表接口
// 能观察到它；而参数里必须是刮削源的**键**，重启函数拿显示名去 getProvider 会回落到默认源。
func TestScrapeTaskDeclarationLandsWhole(t *testing.T) {
	for _, tc := range scrapeScopes {
		t.Run(tc.name, func(t *testing.T) {
			c, snapshots := newScrapeTaskRig(t, &scrapeTestProvider{})

			if err := tc.launch(c); err != nil {
				t.Fatalf("启动刮削失败: %v", err)
			}

			first := firstPublishedTask(t, snapshots(), tc.key)
			if first.ScopeName != tc.scopeName {
				t.Fatalf("首帧的作用域显示名为 %q, want %q", first.ScopeName, tc.scopeName)
			}
			if first.Params["provider"] != "test" {
				t.Fatalf("首帧的刮削源参数为 %v, want provider=test", first.Params)
			}
			if first.Labels["provider"] != "test" || first.Labels["provider_name"] != "TestProvider" {
				t.Fatalf("首帧的刮削源标签为 %v", first.Labels)
			}
			if first.Total != 2 || !first.CanCancel || !first.CanPause {
				t.Fatalf("首帧为 total=%d canCancel=%v canPause=%v, want 2/true/true", first.Total, first.CanCancel, first.CanPause)
			}
			if first.MessageParams["provider"] != "TestProvider" {
				t.Fatalf("**起始**文案的占位参数为 %v, want provider=TestProvider", first.MessageParams)
			}
		})
	}
}

// TestScrapeFrameIsPublishedWhole 守一次上报只投递一条载荷，且那条载荷内部自洽：计数、指标、
// 当前条目与占位参数都来自同一个系列。拆成 Advance / Metrics / Item / Labels 四次分报即变红——
// 后三次会被投递水位吞掉（**阶段**与文案码一字未变），载荷里的指标就此停在上一个系列
// （撕开的样子见 TaskProgress.Report）。
func TestScrapeFrameIsPublishedWhole(t *testing.T) {
	c, snapshots := newScrapeTaskRig(t, &scrapeTestProvider{fetchErr: errors.New("provider offline")})

	if err := c.launchBatchScrapeAllSeriesTask(context.Background(), "test"); err != nil {
		t.Fatalf("启动全库刮削失败: %v", err)
	}

	// 第二个系列那一帧：此时第一个系列已经失败，两组数字必须同时前进。
	frame := publishedTaskWithCode(t, snapshots(), "scrape_all_series", "task.msg.scrape.requesting_provider")
	if frame.Current != 1 || frame.Total != 2 || frame.Phase != "requesting_provider" {
		t.Fatalf("**计数推进**为 %d/%d, phase=%q, want 1/2 requesting_provider", frame.Current, frame.Total, frame.Phase)
	}
	if frame.CurrentItem != "Beta" || frame.MessageParams["name"] != "Beta" {
		t.Fatalf("当前条目与占位参数为 %q / %v, want Beta", frame.CurrentItem, frame.MessageParams)
	}
	if frame.Metrics["provider_requests"] != 2 || frame.Metrics["failed_count"] != 1 || frame.Metrics["processed_series"] != int64(frame.Current) {
		t.Fatalf("指标与计数不是同一个系列：metrics=%v current=%d", frame.Metrics, frame.Current)
	}
	if frame.Labels["current_series_name"] != "Beta" || frame.Labels["provider_name"] != "TestProvider" {
		t.Fatalf("标签为 %v, want 逐条目的系列名叠在任务声明的刮削源上", frame.Labels)
	}

	task := lastPublishedTask(t, snapshots(), "scrape_all_series")
	if task.Status != "completed" {
		t.Fatalf("终态为 %q；刮削源逐条报错只进失败计数，不该让整个任务失败", task.Status)
	}
}

// TestScrapeRetryReadsProviderFromTaskParams 守**重启函数**这条自有的重试路径：它不是简单转发
// 启动方法，而要先从终态任务的参数里读回刮削源。读丢了不会有编译错误，后果是重试静默换成默认源。
func TestScrapeRetryReadsProviderFromTaskParams(t *testing.T) {
	for _, tc := range scrapeScopes {
		t.Run(tc.name, func(t *testing.T) {
			provider := &scrapeTestProvider{}
			c, snapshots := newScrapeTaskRig(t, provider)

			if err := tc.launch(c); err != nil {
				t.Fatalf("启动刮削失败: %v", err)
			}
			done := lastPublishedTask(t, snapshots(), tc.key)
			if done.Status != "completed" {
				t.Fatalf("重试之前任务停在 %q, want completed", done.Status)
			}

			if err := c.retryScrapeTask(done); err != nil {
				t.Fatalf("重试刮削失败: %v", err)
			}

			if len(provider.requestedKeys) != 2 || provider.requestedKeys[1] != "test" {
				t.Fatalf("重试取到的刮削源键为 %v, want 两次都是 test", provider.requestedKeys)
			}
			retried := lastPublishedTask(t, snapshots(), tc.key)
			if retried.Status != "completed" || retried.Params["provider"] != "test" {
				t.Fatalf("重试后的任务为 %q / %v", retried.Status, retried.Params)
			}
		})
	}
}
