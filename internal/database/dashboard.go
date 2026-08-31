// 本文件由 store.go 拆分而来，属于 SQLite 数据访问层的「仪表盘统计」子域。
// 统计分「结构性」（随扫描变化，代价高）与「易变」（随阅读进度变化，走索引）两层，供上层分别缓存。

package database

import (
	"context"
)

type DashboardStats struct {
	TotalSeries  int           `json:"total_series"`
	TotalBooks   int           `json:"total_books"`
	ReadBooks    int           `json:"read_books"`
	TotalPages   int           `json:"total_pages"`
	ActiveDays7  int           `json:"active_days_7"` // 最近7天有阅读行为的天数
	LibrarySizes []LibrarySize `json:"library_sizes"`
}

// DashboardStructuralStats 是仅在扫描增删书/库时才变化的结构性统计。
// 其中 TotalBooks/TotalPages 是对 books 表的全表扫描（70w 行级别），代价高，
// 故与高频变化的阅读类统计分开缓存，避免阅读进度更新触发全表重算。
type DashboardStructuralStats struct {
	TotalSeries  int
	TotalBooks   int
	TotalPages   int
	LibrarySizes []LibrarySize
}

// DashboardVolatileStats 是随阅读进度高频变化的统计，查询均走索引/时间窗口，代价低。
// 两个字段都是**全局**值，只作为未启用多用户时的口径；多用户下调用方须按当前用户改写
// （GetUserReadBooksCount / GetUserActiveDays），否则会把别人的阅读情况显示给当前用户。
type DashboardVolatileStats struct {
	ReadBooks   int
	ActiveDays7 int
}

// GetDashboardStats 一次性拿到全局统计看板所需的聚合数据（组合结构性+阅读类统计）。
// 其中阅读类两项（ReadBooks/ActiveDays7）是全局值，多用户下须由调用方按当前用户改写。
func (s *SqlStore) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	structural, err := s.GetDashboardStructuralStats(ctx)
	if err != nil {
		return nil, err
	}
	volatile, err := s.GetDashboardVolatileStats(ctx)
	if err != nil {
		return nil, err
	}
	return &DashboardStats{
		TotalSeries:  structural.TotalSeries,
		TotalBooks:   structural.TotalBooks,
		TotalPages:   structural.TotalPages,
		LibrarySizes: structural.LibrarySizes,
		ReadBooks:    volatile.ReadBooks,
		ActiveDays7:  volatile.ActiveDays7,
	}, nil
}

// GetDashboardStructuralStats 计算仅随扫描增删书/库变化的结构性统计。
// total_books / total_pages 是对 books 表的全表扫描，代价高，调用方应缓存且仅在扫描后失效。
func (s *SqlStore) GetDashboardStructuralStats(ctx context.Context) (*DashboardStructuralStats, error) {
	var stats DashboardStructuralStats
	var totalPages any
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM series) AS total_series,
			(SELECT COUNT(*) FROM books) AS total_books,
			(SELECT COALESCE(SUM(page_count), 0) FROM books) AS total_pages
	`).Scan(&stats.TotalSeries, &stats.TotalBooks, &totalPages)
	if err != nil {
		return nil, err
	}
	switch v := totalPages.(type) {
	case int64:
		stats.TotalPages = int(v)
	case float64:
		stats.TotalPages = int(v)
	}

	sizeRows, err := s.Queries.ListLibrarySizes(ctx)
	if err == nil {
		sizes := make([]LibrarySize, 0, len(sizeRows))
		for _, r := range sizeRows {
			sizes = append(sizes, LibrarySize{
				LibraryID:   r.LibraryID,
				LibraryName: r.LibraryName,
				TotalSize:   int64(r.TotalSize),
			})
		}
		stats.LibrarySizes = sizes
	}

	return &stats, nil
}

// GetDashboardVolatileStats 计算随阅读进度高频变化的全局统计，均走索引/时间窗口，代价低。
// 近 7 天窗口的下界取 DayKeyDaysAgo（服务器本地日历日），与每用户版 GetUserActiveDays 同口径。
func (s *SqlStore) GetDashboardVolatileStats(ctx context.Context) (*DashboardVolatileStats, error) {
	var stats DashboardVolatileStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM books WHERE last_read_page > 0) AS read_books,
			(SELECT COUNT(DISTINCT date) FROM reading_activity WHERE date >= ?) AS active_days_7
	`, DayKeyDaysAgo(7)).Scan(&stats.ReadBooks, &stats.ActiveDays7)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// ActivityDay 代表某一天的阅读活跃数据
type ActivityDay struct {
	Date      string `json:"date"`       // YYYY-MM-DD
	PageCount int    `json:"page_count"` // 当天阅读的总页数
}
