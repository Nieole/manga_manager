package storageio

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

type WorkKind string

const (
	WorkKindReader       WorkKind = "reader"
	WorkKindScanFast     WorkKind = "scan_fast"
	WorkKindMetadataScan WorkKind = "metadata_scan"
	WorkKindCoverBuild   WorkKind = "cover_build"
	WorkKindCacheWrite   WorkKind = "cache_write"
	WorkKindIdentityHash WorkKind = "identity_hash"

	defaultReaderIdleDuration = 30 * time.Second
)

type Request struct {
	VolumeKey          string
	Limit              int
	Kind               WorkKind
	PauseWhenReading   bool
	IdleOnly           bool
	ReaderIdleDuration time.Duration
}

type Lease struct {
	Wait       time.Duration
	PausedWait time.Duration
	done       func()
}

func (l Lease) Release() {
	if l.done != nil {
		l.done()
	}
}

type Scheduler struct {
	mu               sync.Mutex
	limiters         map[string]*limiter
	readers          map[string]*readerState
	backgroundWaits  map[string]int
	pauseableActive  map[string]int
	backgroundPaused bool
	// broadcast 是「广播 channel」：等待者在阻塞时捕获它并 select 于其上；释放槽位 / 恢复后台 等会关闭它
	// 再换一个新 channel，从而一次性唤醒所有等待者立即重判条件，而不是固定周期轮询——
	// 后者会让被限流阻塞的读页请求多等最多一个轮询周期，且周期性造成锁风暴。
	broadcast chan struct{}
}

type limiter struct {
	used  int
	limit int
}

type readerState struct {
	active  int
	waiting int
	lastEnd time.Time
}

type VolumeSnapshot struct {
	VolumeKey         string
	Active            int
	Limit             int
	ReaderActive      int
	ReaderWaiting     int
	BackgroundWaiting int
	BackgroundPaused  bool
	PauseReason       string
}

var Default = NewScheduler()

func NewScheduler() *Scheduler {
	s := &Scheduler{
		limiters:        make(map[string]*limiter),
		readers:         make(map[string]*readerState),
		backgroundWaits: make(map[string]int),
		pauseableActive: make(map[string]int),
		broadcast:       make(chan struct{}),
	}
	return s
}

// notifyLocked 唤醒所有阻塞中的等待者：关闭当前广播 channel 再换新的。调用方须持有 s.mu。
func (s *Scheduler) notifyLocked() {
	close(s.broadcast)
	s.broadcast = make(chan struct{})
}

// idleWindowRemainingLocked 返回某卷「读者空闲窗口」距到期还剩多久（供被 reader_idle_window 阻塞的
// 后台任务设置精确定时器，在窗口到期而非每 20ms 轮询时被唤醒）。调用方须持有 s.mu。
func (s *Scheduler) idleWindowRemainingLocked(volumeKey string, idleDuration time.Duration) time.Duration {
	state := s.readers[volumeKey]
	if state == nil || state.lastEnd.IsZero() {
		return 0
	}
	return idleDuration - time.Since(state.lastEnd)
}

func (s *Scheduler) Acquire(ctx context.Context, req Request) (Lease, error) {
	// 卷键的归一只发生在这一处：req 是值拷贝，改写后限流桶、读者、排队与快照共用同一个键，
	// 快照归拢出的行因此必然对得上调用方传进来的那块盘。
	req.VolumeKey = normalizeVolumeKey(req.VolumeKey)
	if req.Limit <= 0 || req.VolumeKey == "" {
		return Lease{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	idleDuration := req.ReaderIdleDuration
	if idleDuration <= 0 {
		idleDuration = defaultReaderIdleDuration
	}

	key := limiterKey(req.VolumeKey, req.Limit)
	started := time.Now()
	lastChecked := started
	lastBlockedByPause := false
	var pausedWait time.Duration

	s.mu.Lock()
	if req.Kind == WorkKindReader {
		state := s.readerStateLocked(req.VolumeKey)
		state.waiting++
	} else {
		s.backgroundWaits[req.VolumeKey]++
	}

	for {
		now := time.Now()
		if lastBlockedByPause {
			pausedWait += now.Sub(lastChecked)
		}
		lastChecked = now

		if err := ctx.Err(); err != nil {
			s.decrementWaitingLocked(req)
			s.mu.Unlock()
			return Lease{Wait: time.Since(started), PausedWait: pausedWait}, err
		}

		l := s.limiterLocked(key, req.Limit)
		pauseReason := s.pauseReasonLocked(req, idleDuration)
		lastBlockedByPause = pauseReason != ""
		if (l.used < l.limit || s.readerMayBypassBackgroundLocked(req, l)) && pauseReason == "" {
			l.used++
			if req.Kind == WorkKindReader {
				s.readerStateLocked(req.VolumeKey).active++
			} else if req.PauseWhenReading || req.IdleOnly {
				s.pauseableActive[req.VolumeKey]++
			}
			s.decrementWaitingLocked(req)
			s.mu.Unlock()
			releaseOnce := sync.Once{}
			return Lease{
				Wait:       time.Since(started),
				PausedWait: pausedWait,
				done: func() {
					releaseOnce.Do(func() {
						s.release(req, key)
					})
				},
			}, nil
		}

		// 阻塞：捕获当前广播 channel，等待「槽位释放 / 后台恢复」等事件将其关闭而被唤醒。仅当阻塞原因是
		// 时间型的 reader_idle_window 时才另设一个精确定时器，在窗口到期（而非固定 20ms）时再判一次。
		waitCh := s.broadcast
		var timerC <-chan time.Time
		var timer *time.Timer
		if pauseReason == "reader_idle_window" {
			remaining := s.idleWindowRemainingLocked(req.VolumeKey, idleDuration)
			if remaining <= 0 {
				remaining = time.Millisecond
			}
			timer = time.NewTimer(remaining)
			timerC = timer.C
		}
		s.mu.Unlock()
		select {
		case <-waitCh:
		case <-timerC:
		case <-ctx.Done():
		}
		if timer != nil {
			timer.Stop()
		}
		s.mu.Lock()
	}
}

func (s *Scheduler) PauseBackground() {
	s.mu.Lock()
	s.backgroundPaused = true
	s.mu.Unlock()
}

func (s *Scheduler) ResumeBackground() {
	s.mu.Lock()
	s.backgroundPaused = false
	s.notifyLocked()
	s.mu.Unlock()
}

func (s *Scheduler) BackgroundPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backgroundPaused
}

func (s *Scheduler) Snapshot() []VolumeSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]int)
	snapshots := make([]VolumeSnapshot, 0, len(s.limiters))
	for key, limiter := range s.limiters {
		volumeKey := volumeFromLimiterKey(key)
		index, ok := seen[volumeKey]
		if !ok {
			state := s.readers[volumeKey]
			pauseReason := s.snapshotPauseReasonLocked(volumeKey)
			snapshot := VolumeSnapshot{
				VolumeKey:         volumeKey,
				BackgroundPaused:  s.backgroundPaused,
				BackgroundWaiting: s.backgroundWaits[volumeKey],
				PauseReason:       pauseReason,
			}
			if state != nil {
				snapshot.ReaderActive = state.active
				snapshot.ReaderWaiting = state.waiting
			}
			seen[volumeKey] = len(snapshots)
			snapshots = append(snapshots, snapshot)
			index = len(snapshots) - 1
		}
		snapshots[index].Active += limiter.used
		if snapshots[index].Limit == 0 || limiter.limit < snapshots[index].Limit {
			snapshots[index].Limit = limiter.limit
		}
	}
	for volumeKey, state := range s.readers {
		if _, ok := seen[volumeKey]; ok {
			continue
		}
		snapshots = append(snapshots, VolumeSnapshot{
			VolumeKey:         volumeKey,
			ReaderActive:      state.active,
			ReaderWaiting:     state.waiting,
			BackgroundWaiting: s.backgroundWaits[volumeKey],
			BackgroundPaused:  s.backgroundPaused,
			PauseReason:       s.snapshotPauseReasonLocked(volumeKey),
		})
	}
	return snapshots
}

func (s *Scheduler) decrementWaitingLocked(req Request) {
	if req.Kind == WorkKindReader {
		state := s.readerStateLocked(req.VolumeKey)
		if state.waiting > 0 {
			state.waiting--
		}
		return
	}
	if s.backgroundWaits[req.VolumeKey] > 0 {
		s.backgroundWaits[req.VolumeKey]--
	}
}

func (s *Scheduler) release(req Request, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l := s.limiters[key]; l != nil && l.used > 0 {
		l.used--
	}
	if req.Kind == WorkKindReader {
		state := s.readerStateLocked(req.VolumeKey)
		if state.active > 0 {
			state.active--
		}
		state.lastEnd = time.Now()
	} else if req.PauseWhenReading || req.IdleOnly {
		if s.pauseableActive[req.VolumeKey] > 0 {
			s.pauseableActive[req.VolumeKey]--
		}
	}
	// 释放槽位 / 结束读者：唤醒等待者立即抢占空出的并发额度（读者优先仍由 reader.waiting 守卫保证）。
	s.notifyLocked()
}

func (s *Scheduler) readerMayBypassBackgroundLocked(req Request, l *limiter) bool {
	if req.Kind != WorkKindReader || l == nil || l.limit <= 0 {
		return false
	}
	if s.pauseableActive[req.VolumeKey] <= 0 {
		return false
	}
	state := s.readerStateLocked(req.VolumeKey)
	return state.active < l.limit
}

func (s *Scheduler) pauseReasonLocked(req Request, idleDuration time.Duration) string {
	if req.Kind == WorkKindReader {
		return ""
	}
	if s.backgroundPaused {
		return "manual_pause"
	}
	if req.IdleOnly && s.volumeActiveLocked(req.VolumeKey) {
		return "volume_busy"
	}
	state := s.readers[req.VolumeKey]
	if state == nil {
		return ""
	}
	if req.PauseWhenReading && (state.waiting > 0 || state.active > 0) {
		return "reader_active"
	}
	if (req.PauseWhenReading || req.IdleOnly) && !state.lastEnd.IsZero() && time.Since(state.lastEnd) < idleDuration {
		return "reader_idle_window"
	}
	return ""
}

func (s *Scheduler) snapshotPauseReasonLocked(volumeKey string) string {
	if s.backgroundPaused {
		return "manual_pause"
	}
	state := s.readers[volumeKey]
	if state != nil && (state.waiting > 0 || state.active > 0) {
		return "reader_active"
	}
	if s.backgroundWaits[volumeKey] > 0 && s.volumeActiveLocked(volumeKey) {
		return "volume_busy"
	}
	return ""
}

func (s *Scheduler) volumeActiveLocked(volumeKey string) bool {
	prefix := volumeKey + "|"
	for key, limiter := range s.limiters {
		if strings.HasPrefix(key, prefix) && limiter.used > 0 {
			return true
		}
	}
	return false
}

func (s *Scheduler) limiterLocked(key string, limit int) *limiter {
	l := s.limiters[key]
	if l == nil {
		l = &limiter{limit: limit}
		s.limiters[key] = l
	}
	return l
}

func (s *Scheduler) readerStateLocked(volumeKey string) *readerState {
	state := s.readers[volumeKey]
	if state == nil {
		state = &readerState{}
		s.readers[volumeKey] = state
	}
	return state
}

// normalizeVolumeKey 是卷键进入本包的唯一入口加工：只去掉首尾空白。卷键在这里是一个不透明的身份，
// 大小写归一属于推导它的 config.VolumeKey——只有那一层知道平台文件系统区不区分大小写；本包再折叠一次
// 会让快照报出的卷键与用户配置里的那个对不上。
func normalizeVolumeKey(volumeKey string) string {
	return strings.TrimSpace(volumeKey)
}

// limiterKey 把已归一的卷键与并发上限拼成限流桶键：同卷不同上限是各自独立的桶。
func limiterKey(volumeKey string, limit int) string {
	return volumeKey + "|" + strconv.Itoa(limit)
}

func volumeFromLimiterKey(key string) string {
	if idx := strings.LastIndex(key, "|"); idx > -1 {
		return key[:idx]
	}
	return key
}
