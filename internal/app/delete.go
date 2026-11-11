package app

import (
	"database/sql"
	"errors"
	"net/http"
)

var errBookmarkMissing = errors.New("部分书签已不存在，请刷新后重试")

func (a *App) batchDeleteBookmarks(w http.ResponseWriter, r *http.Request) {
	var v struct {
		IDs []string `json:"ids"`
	}
	if !check(w, decode(r, &v)) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	result, err := a.deleteBookmarks(v.IDs)
	if !check(w, err) {
		return
	}
	send(w, result)
}

type bookmarkDeletion struct {
	BookmarkIDs []string `json:"bookmark_ids"`
	CategoryIDs []string `json:"category_ids"`
}

// Only prune folders affected by this deletion, leaving independently created
// empty folders untouched. Bookmark deletion and ancestor pruning are atomic.
// Icon files retain the existing single-delete behavior; they may be shared.
func (a *App) deleteBookmarks(ids []string) (bookmarkDeletion, error) {
	result := bookmarkDeletion{BookmarkIDs: ids, CategoryIDs: []string{}}
	if len(ids) == 0 || len(ids) > 10000 {
		return bookmarkDeletion{}, errors.New("请选择 1 至 10000 个书签")
	}
	tx, err := a.db.Begin()
	if err != nil {
		return bookmarkDeletion{}, err
	}
	defer tx.Rollback()
	seen := make(map[string]bool)
	folders := make(map[string]bool)
	for _, id := range ids {
		if id == "" || seen[id] {
			return bookmarkDeletion{}, errors.New("书签列表无效")
		}
		seen[id] = true
		var folder string
		if err := tx.QueryRow("SELECT category_id FROM bookmarks WHERE id=?", id).Scan(&folder); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return bookmarkDeletion{}, errBookmarkMissing
			}
			return bookmarkDeletion{}, err
		}
		folders[folder] = true
		if _, err := tx.Exec("DELETE FROM bookmarks WHERE id=?", id); err != nil {
			return bookmarkDeletion{}, err
		}
	}
	for len(folders) > 0 {
		parents := make(map[string]bool)
		for folder := range folders {
			var parent string
			if err := tx.QueryRow("SELECT COALESCE(parent_id,'') FROM categories WHERE id=?", folder).Scan(&parent); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return bookmarkDeletion{}, err
			}
			deleted, err := tx.Exec(`DELETE FROM categories WHERE id=?
    AND NOT EXISTS (SELECT 1 FROM bookmarks WHERE category_id=?)
    AND NOT EXISTS (SELECT 1 FROM categories WHERE parent_id=?)`, folder, folder, folder)
			if err != nil {
				return bookmarkDeletion{}, err
			}
			n, err := deleted.RowsAffected()
			if err != nil {
				return bookmarkDeletion{}, err
			}
			if n > 0 {
				result.CategoryIDs = append(result.CategoryIDs, folder)
				if parent != "" {
					parents[parent] = true
				}
			}
		}
		folders = parents
	}
	if err := tx.Commit(); err != nil {
		return bookmarkDeletion{}, err
	}
	return result, nil
}
