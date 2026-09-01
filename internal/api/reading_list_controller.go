package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"manga-manager/internal/database"
)

type CreateReadingListRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type AddReadingListItemRequest struct {
	SeriesID int64  `json:"series_id"`
	Note     string `json:"note"`
}

type ReorderReadingListItemsRequest struct {
	ItemIDs []int64 `json:"item_ids"`
}

// 事务内的「目标不在场」信号，出了 ExecTx 才映射成 404——事务里直接写响应会让回滚与
// 已写出的状态码脱节。
var (
	errReadingListMissing       = errors.New("reading list not found")
	errReadingListSeriesMissing = errors.New("series not found")
	errReadingListItemMissing   = errors.New("reading list item not found")
)

func (c *Controller) listReadingLists(w http.ResponseWriter, r *http.Request) {
	items, err := c.store.ListReadingLists(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list reading lists")
		return
	}
	if items == nil {
		items = []database.ListReadingListsRow{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) createReadingList(w http.ResponseWriter, r *http.Request) {
	var req CreateReadingListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}
	item, err := c.store.CreateReadingList(r.Context(), database.CreateReadingListParams{
		Name:        name,
		Description: strings.TrimSpace(req.Description),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to create reading list")
		return
	}
	jsonResponse(w, http.StatusCreated, item)
}

func (c *Controller) updateReadingList(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	var req CreateReadingListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}
	item, err := c.store.UpdateReadingList(r.Context(), database.UpdateReadingListParams{
		ID:          listID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Reading list not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to update reading list")
		return
	}
	jsonResponse(w, http.StatusOK, item)
}

func (c *Controller) deleteReadingList(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	affected, err := c.store.DeleteReadingList(r.Context(), listID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete reading list")
		return
	}
	if affected == 0 {
		jsonError(w, http.StatusNotFound, "Reading list not found")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (c *Controller) listReadingListItems(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	if _, err := c.store.GetReadingList(r.Context(), listID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Reading list not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load reading list")
		return
	}
	// next_book_id（「继续阅读」落点）必须按当前用户算：sqlc 版读的是全局
	// books.last_read_page，而多用户改造后该列已停写，导致按钮永远指回第一卷。
	var items []database.ListReadingListItemsRow
	if uid := c.currentUserID(r); uid > 0 {
		items, err = c.store.ListUserReadingListItems(r.Context(), uid, listID)
	} else {
		items, err = c.store.ListReadingListItems(r.Context(), listID)
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to list reading list items")
		return
	}
	if items == nil {
		items = []database.ListReadingListItemsRow{}
	}
	progress, err := c.store.GetReadingListItemProgress(r.Context(), listID, c.currentUserID(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load reading list progress")
		return
	}
	enriched := make([]readingListItemResponse, 0, len(items))
	for _, item := range items {
		row := readingListItemResponse{ListReadingListItemsRow: item}
		if p, ok := progress[item.SeriesID]; ok {
			row.ReadBooks = p.ReadBooks
			row.CompletedBooks = p.CompletedBooks
			row.TotalBooks = p.TotalBooks
		} else {
			row.TotalBooks = item.BookCount
		}
		enriched = append(enriched, row)
	}
	jsonResponse(w, http.StatusOK, enriched)
}

type readingListItemResponse struct {
	database.ListReadingListItemsRow
	ReadBooks      int64 `json:"read_books"`
	CompletedBooks int64 `json:"completed_books"`
	TotalBooks     int64 `json:"total_books"`
}

func (c *Controller) addReadingListItem(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	var req AddReadingListItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SeriesID <= 0 {
		jsonError(w, http.StatusBadRequest, "series_id is required")
		return
	}
	// 清单与系列都先查在场：不查清单时不存在的 listId 会撞 reading_list_items 的外键，
	// 兜底成 500，同一个请求里两个错 id 一个报 404 一个报服务端故障。
	// 两次校验与插入同处一个 ExecTx：store 的 DSN 带 _txlock=immediate，BeginTx 即取写锁，
	// 因此「查到在场」与「插入」之间没有别人删掉清单或系列的窗口。下面的外键兜底 500 仍留着，
	// 兜的是这条串行化之外的真正异常。
	var item database.ReadingListItem
	err = c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		if _, err := q.GetReadingList(r.Context(), listID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errReadingListMissing
			}
			return err
		}
		if _, err := q.GetSeries(r.Context(), req.SeriesID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errReadingListSeriesMissing
			}
			return err
		}
		created, err := q.AddReadingListItem(r.Context(), database.AddReadingListItemParams{
			ReadingListID: listID,
			SeriesID:      req.SeriesID,
			Note:          strings.TrimSpace(req.Note),
		})
		if err != nil {
			return err
		}
		item = created
		return nil
	})
	switch {
	case errors.Is(err, errReadingListMissing):
		jsonError(w, http.StatusNotFound, "Reading list not found")
		return
	case errors.Is(err, errReadingListSeriesMissing):
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	case err != nil:
		jsonError(w, http.StatusInternalServerError, "Failed to add series to reading list")
		return
	}
	jsonResponse(w, http.StatusOK, item)
}

func (c *Controller) removeReadingListItem(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	itemID, err := parseID(r, "itemId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list item ID")
		return
	}
	// 检查影响行数，与删合集同口径：删了 0 行既可能是条目早就没了，也可能是这个 itemId
	// 属于别的清单——两者在这条 DELETE 里无法区分，回 200 就等于对后一种情况谎报删除成功，
	// 前端把它从列表里划掉，刷新后它又回来了。
	affected, err := c.store.RemoveReadingListItem(r.Context(), database.RemoveReadingListItemParams{
		ReadingListID: listID,
		ID:            itemID,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to remove reading list item")
		return
	}
	if affected == 0 {
		jsonError(w, http.StatusNotFound, "Reading list item not found")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (c *Controller) reorderReadingListItems(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	var req ReorderReadingListItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.ItemIDs) == 0 {
		jsonError(w, http.StatusBadRequest, "item_ids is required")
		return
	}
	if len(req.ItemIDs) > maxCollectionBatchSize {
		jsonError(w, http.StatusBadRequest, "Too many items in one request")
		return
	}
	// 先确认清单存在：不校验时对着一个不存在的 listId 重排会静默返回 200，
	// 前端以为排序已保存，刷新后发现毫无变化。
	if _, err := c.store.GetReadingList(r.Context(), listID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Reading list not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load reading list")
		return
	}
	// 每条 UPDATE 都查影响行数：不属于本清单的 itemId 会静默 no-op，整批仍回 200，
	// 于是清单对了、条目错了，前端以为顺序已保存，刷新后发现毫无变化——正是上面那条校验
	// 在清单层挡下的毛病，只是下沉了一层。不覆盖全部条目是合法的（并发新增会让前端手里的
	// 快照少一条），没送到的条目保留原 sort_order；错 id 则说明前端状态已脏，整批放弃。
	// 复用 ExecTx 已有的回滚边界：重排是一次用户手势，只落一半会排出既非原序也非新序的顺序。
	if err := c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		for index, itemID := range req.ItemIDs {
			affected, err := q.UpdateReadingListItemSortOrder(r.Context(), database.UpdateReadingListItemSortOrderParams{
				ReadingListID: listID,
				ID:            itemID,
				SortOrder:     int64((index + 1) * 10),
			})
			if err != nil {
				return err
			}
			if affected == 0 {
				return errReadingListItemMissing
			}
		}
		return nil
	}); err != nil {
		if errors.Is(err, errReadingListItemMissing) {
			jsonError(w, http.StatusNotFound, "Reading list item not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to reorder reading list items")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "reordered"})
}
