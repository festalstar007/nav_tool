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
	if !check(w, a.deleteBookmarks(v.IDs)) {
		return
	}
	send(w, map[string]bool{"ok": true})
}

// Only prune folders affected by this deletion, leaving independently created
// empty folders untouched. Bookmark deletion and ancestor pruning are atomic.
// Icon files retain the existing single-delete behavior; they may be shared.
func (a *App) deleteBookmarks(ids []string) error {
	if len(ids) == 0 || len(ids) > 10000 {
		return errors.New("请选择 1 至 10000 个书签")
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := make(map[string]bool)
	folders := make(map[string]bool)
	for _, id := range ids {
		if id == "" || seen[id] {
			return errors.New("书签列表无效")
		}
		seen[id] = true
		var folder string
		if err := tx.QueryRow("SELECT category_id FROM bookmarks WHERE id=?", id).Scan(&folder); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errBookmarkMissing
			}
			return err
		}
		folders[folder] = true
		if _, err := tx.Exec("DELETE FROM bookmarks WHERE id=?", id); err != nil {
			return err
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
				return err
			}
			result, err := tx.Exec(`DELETE FROM categories WHERE id=?
    AND NOT EXISTS (SELECT 1 FROM bookmarks WHERE category_id=?)
    AND NOT EXISTS (SELECT 1 FROM categories WHERE parent_id=?)`, folder, folder, folder)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n > 0 && parent != "" {
				parents[parent] = true
			}
		}
		folders = parents
	}
	return tx.Commit()
}
