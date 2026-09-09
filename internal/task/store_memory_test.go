// 落盘端口的纯内存实现，供契约用例使用。它守着与真实索引同样的两条准入约束——
// 不守的话，本包的准入用例会在一个比生产宽松的底座上跑绿，而生产那边才是判据所在。

package task

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
)

// memStore 是 Store 的纯内存实现：一次运行一行，两条部分唯一约束在 CreateRun 里判。
type memStore struct {
	mu sync.Mutex

	tasks    map[Identity]*Task
	nextTask int64
	runs     map[int64]Run
	nextRun  int64
	limits   map[int64]Limits
	args     map[int64]map[string]string
	labels   map[int64]map[string]string
	metrics  map[int64]map[string]int64
	events   map[int64][]Event
	// loadRuns 数 LoadRun 被调用了多少次，见 loadRunCalls。
	loadRuns int
	samples  map[int64][]Sample
	// sampleReads 数 ListRunSamples 被调用了多少次，见 sampleReadCalls。
	sampleReads int
	createErr   error
}

func newMemStore() *memStore {
	return &memStore{
		tasks:   make(map[Identity]*Task),
		runs:    make(map[int64]Run),
		limits:  make(map[int64]Limits),
		args:    make(map[int64]map[string]string),
		labels:  make(map[int64]map[string]string),
		metrics: make(map[int64]map[string]int64),
		events:  make(map[int64][]Event),
		samples: make(map[int64][]Sample),
	}
}

func (s *memStore) EnsureTask(_ context.Context, id Identity) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.tasks[id]; ok {
		return *existing, nil
	}
	s.nextTask++
	created := &Task{ID: s.nextTask, Identity: id}
	s.tasks[id] = created
	return *created, nil
}

func (s *memStore) LoadTasks(_ context.Context, taskIDs []int64) (map[int64]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wanted := make(map[int64]bool, len(taskIDs))
	for _, id := range taskIDs {
		wanted[id] = true
	}
	owners := make(map[int64]Task, len(taskIDs))
	for _, owner := range s.tasks {
		if wanted[owner.ID] {
			owners[owner.ID] = *owner
		}
	}
	return owners, nil
}

func (s *memStore) ListTasks(ctx context.Context, filter TaskFilter) ([]Task, error) {
	s.mu.Lock()
	owners := make([]Task, 0, len(s.tasks))
	for _, owner := range s.tasks {
		owners = append(owners, *owner)
	}
	s.mu.Unlock()

	taskIDs := make([]int64, 0, len(owners))
	for _, owner := range owners {
		taskIDs = append(taskIDs, owner.ID)
	}
	latest, err := s.LatestRuns(ctx, taskIDs)
	if err != nil {
		return nil, err
	}

	matched := make([]Task, 0, len(owners))
	for _, owner := range owners {
		if matchesTaskFilter(owner, latest[owner.ID], filter) {
			matched = append(matched, owner)
		}
	}
	// 定序与生产一致：末次运行的序号降序，一次都没跑过的排在最后（序号为零）。
	sort.Slice(matched, func(i, j int) bool {
		left, right := latest[matched[i].ID].Sequence, latest[matched[j].ID].Sequence
		if left != right {
			return left > right
		}
		return matched[i].ID > matched[j].ID
	})
	if filter.Limit > 0 && len(matched) > filter.Limit {
		matched = matched[:filter.Limit]
	}
	return matched, nil
}

func (s *memStore) LatestRuns(_ context.Context, taskIDs []int64) (map[int64]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wanted := make(map[int64]bool, len(taskIDs))
	for _, id := range taskIDs {
		wanted[id] = true
	}
	latest := make(map[int64]Run, len(taskIDs))
	for _, run := range s.runs {
		if !wanted[run.TaskID] {
			continue
		}
		if best, ok := latest[run.TaskID]; ok && (best.Sequence > run.Sequence || (best.Sequence == run.Sequence && best.ID > run.ID)) {
			continue
		}
		latest[run.TaskID] = cloneRun(run)
	}
	return latest, nil
}

// matchesTaskFilter 判一个任务是否满足清单谓词。末次两项判在 latest 上，
// 零值的 latest 表示这个任务一次都没跑过——它因此不满足任何一条末次谓词。
func matchesTaskFilter(owner Task, latest Run, filter TaskFilter) bool {
	if len(filter.Types) > 0 && !slices.Contains(filter.Types, owner.Type) {
		return false
	}
	if filter.Scope != "" && owner.Scope != filter.Scope {
		return false
	}
	if filter.ScopeID != nil && owner.ScopeID != *filter.ScopeID {
		return false
	}
	if len(filter.LastRunStatuses) > 0 && !slices.Contains(filter.LastRunStatuses, latest.Status) {
		return false
	}
	if filter.LastRunQuery != "" {
		haystack := strings.ToLower(latest.ScopeName + " " + latest.MessageCode + " " + latest.Error)
		if latest.ID == 0 || !strings.Contains(haystack, strings.ToLower(filter.LastRunQuery)) {
			return false
		}
	}
	return true
}

func (s *memStore) SaveTaskAttributes(_ context.Context, taskID int64, attrs TaskAttributes) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, owner := range s.tasks {
		if owner.ID == taskID {
			owner.TaskAttributes = attrs
			return nil
		}
	}
	return errors.New("task not found")
}

func (s *memStore) CreateRun(_ context.Context, run Run) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return Run{}, s.createErr
	}
	for _, existing := range s.runs {
		if existing.TaskID != run.TaskID {
			continue
		}
		if existing.Status.IsActive() && run.Status.IsActive() {
			return Run{}, ErrRunAlreadyActive
		}
		if existing.Status == StatusQueued && run.Status == StatusQueued {
			return Run{}, ErrRunAlreadyQueued
		}
	}
	s.nextRun++
	run.ID = s.nextRun
	s.runs[run.ID] = cloneRun(run)
	return cloneRun(run), nil
}

func (s *memStore) SaveRun(_ context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return ErrRunNotFound
	}
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func (s *memStore) LoadRun(_ context.Context, runID int64) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadRuns++
	run, ok := s.runs[runID]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	return cloneRun(run), nil
}

// loadRunCalls 数至今问过多少次「这条运行现在什么样」。
//
// 它与 sampleReadCalls 是这份桩上仅有的两个调用计数，各自专给一条用例用：条目失败的上限之外
// 必须**一次库都不查**，而那条路的正确性只有「有没有查」能证明——查回来的答案在两种实现下
// 完全一样。
func (s *memStore) loadRunCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadRuns
}

// sampleReadCalls 数至今读过多少次**采样**表。
//
// 它守的是本票那条硬约束：**计数的事实来源是运行行本身，不是采样点**。一整轮运行下来
// 这个数必须是零——采样只被详情页那条按需拉取读，任何判断读了它，这个计数就不是零了。
func (s *memStore) sampleReadCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sampleReads
}

func (s *memStore) ListRuns(_ context.Context, filter RunFilter) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	matched := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		if s.matchesFilterLocked(run, filter) {
			matched = append(matched, cloneRun(run))
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		// 与 taskstore 的 orderClause 同形：手动优先那一组先分，组内仍按序号。
		if filter.Order == OrderManualFirst {
			if left, right := manualFirstRank(matched[i]), manualFirstRank(matched[j]); left != right {
				return left < right
			}
			return matched[i].Sequence > matched[j].Sequence
		}
		if filter.Order == OrderSequenceDesc {
			return matched[i].Sequence > matched[j].Sequence
		}
		return matched[i].Sequence < matched[j].Sequence
	})
	if filter.Limit > 0 && len(matched) > filter.Limit {
		matched = matched[:filter.Limit]
	}
	return matched, nil
}

// manualFirstRank 把一条运行分进「手动」与「其余」两组，手动为 0。
func manualFirstRank(run Run) int {
	if run.Trigger == TriggerManual {
		return 0
	}
	return 1
}

func (s *memStore) CountRuns(ctx context.Context, filter RunFilter) (int, error) {
	filter.Limit = 0
	runs, err := s.ListRuns(ctx, filter)
	return len(runs), err
}

func (s *memStore) MaxRunSequence(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var max int64
	for _, run := range s.runs {
		if run.Sequence > max {
			max = run.Sequence
		}
	}
	return max, nil
}

func (s *memStore) MaxNthRun(_ context.Context, taskID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, run := range s.runs {
		if run.TaskID == taskID && run.NthRun > highest {
			highest = run.NthRun
		}
	}
	return highest, nil
}

func (s *memStore) SaveRunLimits(_ context.Context, runID int64, limits Limits) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits[runID] = limits
	return nil
}

func (s *memStore) MergeRunArgs(_ context.Context, runID int64, args map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mergeInto(s.args, runID, args)
	return nil
}

func (s *memStore) MergeRunLabels(_ context.Context, runID int64, labels map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mergeInto(s.labels, runID, labels)
	return nil
}

func (s *memStore) SetRunMetrics(_ context.Context, runID int64, values map[string]int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.metrics[runID] == nil {
		s.metrics[runID] = make(map[string]int64, len(values))
	}
	for key, value := range values {
		s.metrics[runID][key] = value
	}
	return nil
}

func (s *memStore) AddRunMetrics(_ context.Context, runID int64, increments map[string]int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.metrics[runID] == nil {
		s.metrics[runID] = make(map[string]int64, len(increments))
	}
	for key, value := range increments {
		s.metrics[runID][key] += value
	}
	return nil
}

func (s *memStore) AppendRunEvents(_ context.Context, runID int64, events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[runID] = append(s.events[runID], events...)
	return nil
}

// ListRunEvents 交回插入顺序：那正是生产实现按自增主键定序拿到的顺序。
func (s *memStore) ListRunEvents(_ context.Context, runID int64, limit int) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := append([]Event(nil), s.events[runID]...)
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (s *memStore) AppendRunSamples(_ context.Context, runID int64, samples []Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples[runID] = append(s.samples[runID], samples...)
	return nil
}

// ListRunSamples 交回插入顺序（也就是时刻升序），limit 截的是**最近的**那一段——与生产同一条口径。
//
// 它顺手数一次被读了几次：**没有任何判断依赖采样表**这条约束，用例断言的正是这个计数为零。
func (s *memStore) ListRunSamples(_ context.Context, runID int64, limit int) ([]Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampleReads++
	samples := append([]Sample(nil), s.samples[runID]...)
	if limit > 0 && len(samples) > limit {
		samples = samples[len(samples)-limit:]
	}
	return samples, nil
}

func (s *memStore) LoadRunSideData(_ context.Context, runIDs []int64) (map[int64]SideData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	side := make(map[int64]SideData, len(runIDs))
	for _, runID := range runIDs {
		data := SideData{
			Args:   cloneStrings(s.args[runID]),
			Labels: cloneStrings(s.labels[runID]),
		}
		if len(s.metrics[runID]) > 0 {
			data.Metrics = make(map[string]int64, len(s.metrics[runID]))
			for key, value := range s.metrics[runID] {
				data.Metrics[key] = value
			}
		}
		if limits, ok := s.limits[runID]; ok {
			stored := limits
			data.Limits = &stored
		}
		side[runID] = data
	}
	return side, nil
}

// DeleteRuns 守着与生产同一条前提：仍会变化的运行永不被删。
func (s *memStore) DeleteRuns(_ context.Context, filter RunFilter) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed int64
	for id, run := range s.runs {
		if run.Status.IsLive() || !s.matchesFilterLocked(run, filter) {
			continue
		}
		delete(s.runs, id)
		removed++
	}
	return removed, nil
}

// PruneRuns 只实现「每任务留最近 N 次**终态**运行」与「**活动态**和**排队中**永不被选中」。
// 按时长裁剪与级联删除的选中集合由 SQL 回答，契约用例不在这里重复一遍近似实现。
func (s *memStore) PruneRuns(_ context.Context, policy RetentionPolicy) (PruneResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if policy.RunsPerTask <= 0 {
		return PruneResult{}, nil
	}
	byTask := make(map[int64][]Run)
	for _, run := range s.runs {
		if run.Status.IsLive() {
			continue
		}
		byTask[run.TaskID] = append(byTask[run.TaskID], run)
	}
	var removed int64
	for _, runs := range byTask {
		if len(runs) <= policy.RunsPerTask {
			continue
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i].Sequence > runs[j].Sequence })
		for _, run := range runs[policy.RunsPerTask:] {
			delete(s.runs, run.ID)
			removed++
		}
	}
	return PruneResult{Runs: removed}, nil
}

func mergeInto(target map[int64]map[string]string, runID int64, values map[string]string) {
	if target[runID] == nil {
		target[runID] = make(map[string]string, len(values))
	}
	for key, value := range values {
		target[runID][key] = value
	}
}

// matchesFilterLocked 判一条运行是否满足谓词。调用方持锁——身份那几项要回头查任务表。
func (s *memStore) matchesFilterLocked(run Run, filter RunFilter) bool {
	if filter.TaskID != 0 && run.TaskID != filter.TaskID {
		return false
	}
	if filter.Query != "" {
		haystack := strings.ToLower(run.ScopeName + " " + run.MessageCode + " " + run.Error)
		if !strings.Contains(haystack, strings.ToLower(filter.Query)) {
			return false
		}
	}
	if len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, run.Status) {
		return false
	}
	return s.matchesIdentityLocked(run.TaskID, filter)
}

func (s *memStore) matchesIdentityLocked(taskID int64, filter RunFilter) bool {
	if len(filter.Types) == 0 && filter.Scope == "" && filter.ScopeID == nil {
		return true
	}
	for id, owner := range s.tasks {
		if owner.ID != taskID {
			continue
		}
		if len(filter.Types) > 0 && !slices.Contains(filter.Types, id.Type) {
			return false
		}
		if filter.Scope != "" && id.Scope != filter.Scope {
			return false
		}
		if filter.ScopeID != nil && id.ScopeID != *filter.ScopeID {
			return false
		}
		return true
	}
	return false
}

// argsOf / labelsOf / metricsOf 取侧表内容，供断言「声明整份原子落地」的用例使用。
func (s *memStore) argsOf(runID int64) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStrings(s.args[runID])
}

func (s *memStore) labelsOf(runID int64) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStrings(s.labels[runID])
}

func (s *memStore) metricsOf(runID int64) map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := make(map[string]int64, len(s.metrics[runID]))
	for key, value := range s.metrics[runID] {
		copied[key] = value
	}
	return copied
}

func (s *memStore) limitsOf(runID int64) Limits {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limits[runID]
}
