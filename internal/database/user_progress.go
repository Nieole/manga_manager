// 业务说明：本文件是「每用户阅读进度」的数据访问层（多用户阶段2）。它把旧的全局
// books.last_read_page/last_read_at 拆成按用户的 user_book_progress，并派生每用户 × 每系列的
// user_series_progress 聚合（进度条/已读·完成计数/最近阅读），供列表、看板、继续阅读按当前用户取数。
// 维护要点：写入进度后必须按 (user, series) 增量刷新 user_series_progress；旧全局进度在首个管理员
// 创建时经 MigrateGlobalProgressToUser 迁移过来（幂等）。books 表的旧进度列保留为迁移来源，不再读写。

package database

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// UserBookProgress 是某用户对某本书的进度快照，用于叠加到书目响应。
type UserBookProgress struct {
	LastReadPage sql.NullInt64
	LastReadAt   sql.NullTime
}

// refreshUserSeriesProgressForSeriesStmt 增量刷新单个 (user, series) 的派生进度。占位符顺序见下方调用。
const refreshUserSeriesProgressForSeriesStmt = `
	INSERT INTO user_series_progress (
		user_id, series_id, read_pages, read_book_count, completed_book_count, last_read_at, last_read_book_id, updated_at
	)
	SELECT ?, ?,
		COALESCE((SELECT SUM(CASE
			WHEN ubp.last_read_page IS NULL OR ubp.last_read_page <= 0 THEN 0
			WHEN b.page_count > 0 AND ubp.last_read_page > b.page_count THEN b.page_count
			ELSE ubp.last_read_page END)
			FROM user_book_progress ubp JOIN books b ON b.id = ubp.book_id
			WHERE ubp.user_id = ? AND b.series_id = ?), 0),
		COALESCE((SELECT COUNT(*) FROM user_book_progress ubp JOIN books b ON b.id = ubp.book_id
			WHERE ubp.user_id = ? AND b.series_id = ? AND ubp.last_read_page IS NOT NULL AND ubp.last_read_page > 0), 0),
		COALESCE((SELECT COUNT(*) FROM user_book_progress ubp JOIN books b ON b.id = ubp.book_id
			WHERE ubp.user_id = ? AND b.series_id = ? AND b.page_count > 0 AND ubp.last_read_page >= b.page_count), 0),
		(SELECT ubp.last_read_at FROM user_book_progress ubp JOIN books b ON b.id = ubp.book_id
			WHERE ubp.user_id = ? AND b.series_id = ? AND ubp.last_read_at IS NOT NULL
			ORDER BY ubp.last_read_at DESC, ubp.book_id DESC LIMIT 1),
		COALESCE((SELECT ubp.book_id FROM user_book_progress ubp JOIN books b ON b.id = ubp.book_id
			WHERE ubp.user_id = ? AND b.series_id = ? AND ubp.last_read_at IS NOT NULL
			ORDER BY ubp.last_read_at DESC, ubp.book_id DESC LIMIT 1), 0),
		CURRENT_TIMESTAMP
	ON CONFLICT(user_id, series_id) DO UPDATE SET
		read_pages = excluded.read_pages,
		read_book_count = excluded.read_book_count,
		completed_book_count = excluded.completed_book_count,
		last_read_at = excluded.last_read_at,
		last_read_book_id = excluded.last_read_book_id,
		updated_at = CURRENT_TIMESTAMP`

// refreshUserSeriesProgressAllStmt 全量重算某用户所有系列的派生进度（迁移/回填用）。
const refreshUserSeriesProgressAllStmt = `
	INSERT INTO user_series_progress (
		user_id, series_id, read_pages, read_book_count, completed_book_count, last_read_at, last_read_book_id, updated_at
	)
	SELECT ubp.user_id, b.series_id,
		SUM(CASE
			WHEN ubp.last_read_page IS NULL OR ubp.last_read_page <= 0 THEN 0
			WHEN b.page_count > 0 AND ubp.last_read_page > b.page_count THEN b.page_count
			ELSE ubp.last_read_page END),
		SUM(CASE WHEN ubp.last_read_page IS NOT NULL AND ubp.last_read_page > 0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN b.page_count > 0 AND ubp.last_read_page >= b.page_count THEN 1 ELSE 0 END),
		MAX(ubp.last_read_at),
		COALESCE((SELECT ubp2.book_id FROM user_book_progress ubp2 JOIN books b2 ON b2.id = ubp2.book_id
			WHERE ubp2.user_id = ubp.user_id AND b2.series_id = b.series_id AND ubp2.last_read_at IS NOT NULL
			ORDER BY ubp2.last_read_at DESC, ubp2.book_id DESC LIMIT 1), 0),
		CURRENT_TIMESTAMP
	FROM user_book_progress ubp JOIN books b ON b.id = ubp.book_id
	WHERE ubp.user_id = ?
	GROUP BY ubp.user_id, b.series_id
	ON CONFLICT(user_id, series_id) DO UPDATE SET
		read_pages = excluded.read_pages,
		read_book_count = excluded.read_book_count,
		completed_book_count = excluded.completed_book_count,
		last_read_at = excluded.last_read_at,
		last_read_book_id = excluded.last_read_book_id,
		updated_at = CURRENT_TIMESTAMP`

// refreshUserSeriesProgressTx 在事务内刷新单个 (user, series) 聚合。
func refreshUserSeriesProgressTx(ctx context.Context, q *Queries, userID, seriesID int64) error {
	_, err := q.db.ExecContext(ctx, refreshUserSeriesProgressForSeriesStmt,
		userID, seriesID,
		userID, seriesID,
		userID, seriesID,
		userID, seriesID,
		userID, seriesID,
		userID, seriesID)
	return err
}

// SetUserBookProgress 记录某用户对某本书的进度（page/at），并刷新其所在系列的派生聚合。
func (s *SqlStore) SetUserBookProgress(ctx context.Context, userID, bookID, page int64, at time.Time) error {
	return s.ExecTx(ctx, func(q *Queries) error {
		if _, err := q.db.ExecContext(ctx,
			`INSERT INTO user_book_progress (user_id, book_id, last_read_page, last_read_at, updated_at)
			 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(user_id, book_id) DO UPDATE SET
			   last_read_page = excluded.last_read_page, last_read_at = excluded.last_read_at, updated_at = CURRENT_TIMESTAMP`,
			userID, bookID, page, at); err != nil {
			return err
		}
		seriesID, err := q.GetSeriesIDByBookID(ctx, bookID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		return refreshUserSeriesProgressTx(ctx, q, userID, seriesID)
	})
}

// ClearUserBookProgress 清除某用户对某本书的进度（标记未读），并刷新其系列聚合。
func (s *SqlStore) ClearUserBookProgress(ctx context.Context, userID, bookID int64) error {
	return s.ExecTx(ctx, func(q *Queries) error {
		seriesID, err := q.GetSeriesIDByBookID(ctx, bookID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := q.db.ExecContext(ctx,
			`DELETE FROM user_book_progress WHERE user_id = ? AND book_id = ?`, userID, bookID); err != nil {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return refreshUserSeriesProgressTx(ctx, q, userID, seriesID)
	})
}

// SetUserBooksReadState 批量把若干书标记为已读（进度=页数）或未读（清除），一次事务内刷新受影响系列。
func (s *SqlStore) SetUserBooksReadState(ctx context.Context, userID int64, bookIDs []int64, isRead bool, at time.Time) error {
	if len(bookIDs) == 0 {
		return nil
	}
	return s.ExecTx(ctx, func(q *Queries) error {
		affected := make(map[int64]struct{})
		for _, bookID := range bookIDs {
			seriesID, err := q.GetSeriesIDByBookID(ctx, bookID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			if isRead {
				// 已读：进度设为该书页数（page_count 为 0 时用大哨兵值，语义同旧 UpdateBookProgress）。
				if _, err := q.db.ExecContext(ctx,
					`INSERT INTO user_book_progress (user_id, book_id, last_read_page, last_read_at, updated_at)
					 SELECT ?, id, CASE WHEN page_count > 0 THEN page_count ELSE 99999 END, ?, CURRENT_TIMESTAMP
					 FROM books WHERE id = ?
					 ON CONFLICT(user_id, book_id) DO UPDATE SET
					   last_read_page = excluded.last_read_page, last_read_at = excluded.last_read_at, updated_at = CURRENT_TIMESTAMP`,
					userID, at, bookID); err != nil {
					return err
				}
			} else {
				if _, err := q.db.ExecContext(ctx,
					`DELETE FROM user_book_progress WHERE user_id = ? AND book_id = ?`, userID, bookID); err != nil {
					return err
				}
			}
			affected[seriesID] = struct{}{}
		}
		for seriesID := range affected {
			if err := refreshUserSeriesProgressTx(ctx, q, userID, seriesID); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetUserBookProgress 返回某用户对某本书的进度；无记录时 found=false。
func (s *SqlStore) GetUserBookProgress(ctx context.Context, userID, bookID int64) (UserBookProgress, bool, error) {
	var p UserBookProgress
	err := s.db.QueryRowContext(ctx,
		`SELECT last_read_page, last_read_at FROM user_book_progress WHERE user_id = ? AND book_id = ?`,
		userID, bookID).Scan(&p.LastReadPage, &p.LastReadAt)
	if errors.Is(err, sql.ErrNoRows) {
		return UserBookProgress{}, false, nil
	}
	if err != nil {
		return UserBookProgress{}, false, err
	}
	return p, true, nil
}

// GetUserBookProgressMap 批量取某用户对一组书的进度，供响应叠加。返回的 map 只含有进度记录的书。
func (s *SqlStore) GetUserBookProgressMap(ctx context.Context, userID int64, bookIDs []int64) (map[int64]UserBookProgress, error) {
	out := make(map[int64]UserBookProgress)
	if len(bookIDs) == 0 {
		return out, nil
	}
	inClause, args := sqlInClause(bookIDs)
	query := `SELECT book_id, last_read_page, last_read_at FROM user_book_progress WHERE user_id = ? AND book_id IN (` + inClause + `)`
	fullArgs := append([]interface{}{userID}, args...)
	rows, err := s.db.QueryContext(ctx, query, fullArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var bookID int64
		var p UserBookProgress
		if err := rows.Scan(&bookID, &p.LastReadPage, &p.LastReadAt); err != nil {
			return nil, err
		}
		out[bookID] = p
	}
	return out, rows.Err()
}

// GetKOReaderAccountUserID 返回某 KOReader 账户绑定的站点用户 id；0 表示未关联（进度回落到全局）。
// 用单列查询避免改动 KOReaderAccount 结构与其众多扫描点。
func (s *SqlStore) GetKOReaderAccountUserID(ctx context.Context, username string) (int64, error) {
	var uid int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM koreader_accounts WHERE username = ?`, username).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return uid, err
}

// SetKOReaderAccountUser 把某 KOReader 账户绑定到站点用户（创建账户时归属创建者）。
func (s *SqlStore) SetKOReaderAccountUser(ctx context.Context, accountID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE koreader_accounts SET user_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, userID, accountID)
	return err
}

// AssignOrphanKOReaderAccountsToUser 把所有未关联（user_id=0）的 KOReader 账户归属某用户。
// 首个管理员创建时调用，实现「现有 KOReader 账户并入第一个管理员」。
func (s *SqlStore) AssignOrphanKOReaderAccountsToUser(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE koreader_accounts SET user_id = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = 0`, userID)
	return err
}

// GetUserRecentReadAll 是 GetRecentReadAll 的每用户版本：从 user_series_progress + user_book_progress 取
// 该用户最近阅读的书目（跨库，用于看板「继续阅读」）。返回行结构与全局版一致。
func (s *SqlStore) GetUserRecentReadAll(ctx context.Context, userID, limit int64) ([]GetRecentReadAllRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.name, s.id, b.id, b.name, b.title, usp.last_read_at, ubp.last_read_page, b.page_count, COALESCE(sc.cover_path, ''), b.path
		FROM user_series_progress usp
		JOIN series s ON s.id = usp.series_id
		JOIN books b ON b.id = usp.last_read_book_id
		LEFT JOIN user_book_progress ubp ON ubp.user_id = usp.user_id AND ubp.book_id = usp.last_read_book_id
		LEFT JOIN series_stats sc ON sc.series_id = s.id
		WHERE usp.user_id = ? AND usp.last_read_at IS NOT NULL AND usp.last_read_book_id > 0
		  AND ubp.last_read_page IS NOT NULL AND ubp.last_read_page > 0
		ORDER BY usp.last_read_at DESC, s.name ASC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetRecentReadAllRow
	for rows.Next() {
		var i GetRecentReadAllRow
		if err := rows.Scan(&i.SeriesName, &i.SeriesID, &i.BookID, &i.BookName, &i.BookTitle,
			&i.LastReadAt, &i.LastReadPage, &i.PageCount, &i.CoverPath, &i.BookPath); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// GetUserRecentReadSeries 是 GetRecentReadSeries 的每用户版本（某库内按最近阅读排序的系列）。
func (s *SqlStore) GetUserRecentReadSeries(ctx context.Context, userID, libraryID, limit int64) ([]GetRecentReadSeriesRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH RankedBooks AS (
			SELECT b.series_id, b.id AS book_id, ubp.last_read_at, ubp.last_read_page,
				ROW_NUMBER() OVER(PARTITION BY b.series_id ORDER BY ubp.last_read_at DESC) as rn
			FROM user_book_progress ubp JOIN books b ON b.id = ubp.book_id
			WHERE ubp.user_id = ? AND ubp.last_read_at IS NOT NULL AND b.library_id = ?
		)
		SELECT s.id, s.library_id, s.name, s.title, s.summary, s.publisher, s.status, s.rating, s.language, s.locked_fields, s.name_initial, s.path, s.created_at, s.updated_at, s.is_favorite, s.volume_count, s.book_count, s.total_pages,
			rb.book_id, rb.last_read_at, rb.last_read_page,
			(SELECT b.cover_path FROM books b WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != '' ORDER BY b.sort_number, b.name LIMIT 1)
		FROM series s
		JOIN RankedBooks rb ON s.id = rb.series_id AND rb.rn = 1
		WHERE s.library_id = ?
		ORDER BY rb.last_read_at DESC
		LIMIT ?`, userID, libraryID, libraryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetRecentReadSeriesRow
	for rows.Next() {
		var i GetRecentReadSeriesRow
		if err := rows.Scan(&i.ID, &i.LibraryID, &i.Name, &i.Title, &i.Summary, &i.Publisher, &i.Status, &i.Rating,
			&i.Language, &i.LockedFields, &i.NameInitial, &i.Path, &i.CreatedAt, &i.UpdatedAt, &i.IsFavorite,
			&i.VolumeCount, &i.BookCount, &i.TotalPages, &i.RecentBookID, &i.LastReadAt, &i.LastReadPage, &i.CoverPath); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// GetUserReadBooksCount 返回某用户已开始阅读的书本数（看板统计的每用户版）。
func (s *SqlStore) GetUserReadBooksCount(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_book_progress WHERE user_id = ? AND last_read_page IS NOT NULL AND last_read_page > 0`,
		userID).Scan(&n)
	return n, err
}

// MigrateGlobalProgressToUser 把旧的全局 books 进度一次性迁移到某用户名下（幂等），并回填其系列聚合。
// 在首个管理员创建时调用，实现「现有进度迁移到第一个管理员」。
func (s *SqlStore) MigrateGlobalProgressToUser(ctx context.Context, userID int64) error {
	return s.ExecTx(ctx, func(q *Queries) error {
		if _, err := q.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO user_book_progress (user_id, book_id, last_read_page, last_read_at)
			 SELECT ?, id, last_read_page, last_read_at FROM books WHERE last_read_page IS NOT NULL AND last_read_page > 0`,
			userID); err != nil {
			return err
		}
		_, err := q.db.ExecContext(ctx, refreshUserSeriesProgressAllStmt, userID)
		return err
	})
}

// ListUserReadingListItems 是 ListReadingListItems 的每用户版本。
//
// sqlc 生成的那版把 next_book_id（「继续阅读」按钮的落点）算在全局 books.last_read_page 上，
// 而多用户改造后该列已停写——于是不论读到哪一卷，按钮永远指回第一卷。
// user_book_progress 位于 schema_handwritten.sql（刻意避开 sqlc 以防模型重名），
// 无法在 sql/query.sql 里 JOIN，故这条查询手写在此。
//
// userID<=0 时不应调用本方法（走 sqlc 版即可）；即便调用，LEFT JOIN 无匹配行会让
// COALESCE 回落到全局列，行为与旧版一致。
func (s *SqlStore) ListUserReadingListItems(ctx context.Context, userID, readingListID int64) ([]ListReadingListItemsRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			rli.id,
			rli.reading_list_id,
			rli.series_id,
			rli.sort_order,
			rli.note,
			rli.created_at,
			rli.updated_at,
			s.name AS series_name,
			COALESCE(s.title, '') AS series_title,
			s.book_count,
			CAST(COALESCE((
				SELECT b.cover_path
				FROM books b
				WHERE b.series_id = s.id AND b.cover_path IS NOT NULL AND b.cover_path != ''
				ORDER BY b.sort_number, b.name
				LIMIT 1
			), '') AS TEXT) AS cover_path,
			CAST(COALESCE((
				SELECT b.id
				FROM books b
				LEFT JOIN user_book_progress ubp ON ubp.book_id = b.id AND ubp.user_id = ?
				WHERE b.series_id = s.id
				ORDER BY
					CASE
						WHEN b.page_count = 0 THEN 0
						WHEN COALESCE(ubp.last_read_page, b.last_read_page) IS NULL THEN 0
						WHEN COALESCE(ubp.last_read_page, b.last_read_page) < b.page_count THEN 0
						ELSE 1
					END,
					b.sort_number,
					b.name
				LIMIT 1
			), 0) AS INTEGER) AS next_book_id
		FROM reading_list_items rli
		JOIN series s ON s.id = rli.series_id
		WHERE rli.reading_list_id = ?
		ORDER BY rli.sort_order, s.name`, userID, readingListID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ListReadingListItemsRow
	for rows.Next() {
		var i ListReadingListItemsRow
		if err := rows.Scan(
			&i.ID, &i.ReadingListID, &i.SeriesID, &i.SortOrder, &i.Note,
			&i.CreatedAt, &i.UpdatedAt, &i.SeriesName, &i.SeriesTitle,
			&i.BookCount, &i.CoverPath, &i.NextBookID,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// RefreshUserSeriesProgressForAllUsers 重算某系列在所有用户下的派生进度聚合。
//
// 用于「书被删除」这类会改变系列组成的场景：删除 books 行会经 CASCADE 带走
// user_book_progress，但 user_series_progress 是派生快照，不会自动跟着变——
// 于是已读页数/完成卷数会长期停留在删除前的值。
// 只遍历确实在该系列留有进度的用户，无进度的用户不需要（也不应）建行。
func (s *SqlStore) RefreshUserSeriesProgressForAllUsers(ctx context.Context, seriesID int64) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT user_id FROM user_series_progress WHERE series_id = ?`, seriesID)
	if err != nil {
		return err
	}
	var userIDs []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return err
		}
		userIDs = append(userIDs, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, uid := range userIDs {
		if _, err := s.db.ExecContext(ctx, refreshUserSeriesProgressForSeriesStmt,
			uid, seriesID, uid, seriesID, uid, seriesID, uid, seriesID, uid, seriesID, uid, seriesID); err != nil {
			return err
		}
	}
	return nil
}

// RefreshSeriesDerivedData 重算某系列的全部派生数据：series 行上的冗余统计、
// series_stats 缓存表，以及所有用户的 user_series_progress 聚合。
//
// 存在的意义是让「系列组成变了」的调用方只需要记住一件事。此前 removeBooks 只刷了
// series_stats，漏掉 series.book_count/total_pages 与每用户聚合，导致进度条与完成态长期偏差。
func (s *SqlStore) RefreshSeriesDerivedData(ctx context.Context, seriesID int64) error {
	if seriesID <= 0 {
		return nil
	}
	if err := s.Queries.UpdateSeriesStatistics(ctx, UpdateSeriesStatisticsParams{
		SeriesID:   seriesID,
		SeriesID_2: seriesID,
		SeriesID_3: seriesID,
		ID:         seriesID,
	}); err != nil {
		return err
	}
	if err := s.RefreshSeriesStats(ctx, seriesID); err != nil {
		return err
	}
	return s.RefreshUserSeriesProgressForAllUsers(ctx, seriesID)
}
