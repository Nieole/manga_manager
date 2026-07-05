// 业务说明：本文件属于后端 HTTP API 层，负责“重复文件”检测与安全移除工作流。
// 它按 file_hash 分组列出内容相同的书籍；移除仅“从库中删除记录”，可选把原文件移入回收站目录，
// 绝不硬删源文件（安全策略）。维护时应关注：移动失败即不删记录以避免孤儿、跨盘移动回退为复制+删除。

package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"manga-manager/internal/database"
)

// getDuplicateBooks 返回按 file_hash 分组的重复书籍。
func (c *Controller) getDuplicateBooks(w http.ResponseWriter, r *http.Request) {
	rows, err := c.store.FindDuplicateBooks(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to find duplicates")
		return
	}
	type dupGroup struct {
		FileHash string                      `json:"file_hash"`
		Books    []database.DuplicateBookRow `json:"books"`
	}
	groupIndex := make(map[string]int)
	groups := make([]dupGroup, 0)
	for _, row := range rows {
		idx, ok := groupIndex[row.FileHash]
		if !ok {
			idx = len(groups)
			groupIndex[row.FileHash] = idx
			groups = append(groups, dupGroup{FileHash: row.FileHash})
		}
		groups[idx].Books = append(groups[idx].Books, row)
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"groups": groups})
}

// removeBooks 从库中移除选中书籍（删除数据库记录）。move_to_trash 为真时先把原文件移入回收站目录。
func (c *Controller) removeBooks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookIDs     []int64 `json:"book_ids"`
		MoveToTrash bool    `json:"move_to_trash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.BookIDs) == 0 {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"removed": 0})
		return
	}

	var trashDir string
	if req.MoveToTrash {
		trashDir = filepath.Join(filepath.Dir(c.currentConfig().Database.Path), "trash")
		if err := os.MkdirAll(trashDir, 0o755); err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to prepare trash directory")
			return
		}
	}

	removed, trashed, failed := 0, 0, 0
	for _, id := range req.BookIDs {
		book, err := c.store.GetBook(r.Context(), id)
		if err != nil {
			failed++
			continue
		}
		if req.MoveToTrash && trashDir != "" {
			if err := moveFileToTrash(book.Path, trashDir, id); err != nil {
				// 移动失败即不删记录，避免留下孤儿文件与不一致状态。
				slog.Warn("move book file to trash failed", "book_id", id, "path", book.Path, "error", err)
				failed++
				continue
			}
			trashed++
		}
		if err := c.store.DeleteBook(r.Context(), id); err != nil {
			slog.Error("delete book record failed", "book_id", id, "error", err)
			failed++
			continue
		}
		removed++
	}

	c.invalidateDashboardStatsCache("dedup_remove")
	jsonResponse(w, http.StatusOK, map[string]interface{}{"removed": removed, "trashed": trashed, "failed": failed})
}

// moveFileToTrash 把文件移入回收站目录（文件名前缀 book ID 避免碰撞）；跨盘 rename 失败时回退复制+删除。
func moveFileToTrash(srcPath, trashDir string, bookID int64) error {
	dest := filepath.Join(trashDir, strconv.FormatInt(bookID, 10)+"-"+filepath.Base(srcPath))
	if err := os.Rename(srcPath, dest); err == nil {
		return nil
	}
	// 跨设备等 rename 失败：复制后删除源。
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dest)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dest)
		return err
	}
	in.Close()
	return os.Remove(srcPath)
}
