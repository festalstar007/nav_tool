package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Category struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
	Sort     int    `json:"sort_order"`
}
type Bookmark struct {
	ID         string `json:"id"`
	CategoryID string `json:"category_id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Note       string `json:"note"`
	Icon       string `json:"icon"`
	Sort       int    `json:"sort_order"`
}
type Data struct {
	Categories []Category `json:"categories"`
	Bookmarks  []Bookmark `json:"bookmarks"`
}

func id() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`, `INSERT INTO schema_version SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM schema_version)`,
		`CREATE TABLE IF NOT EXISTS categories(id TEXT PRIMARY KEY,parent_id TEXT REFERENCES categories(id),name TEXT NOT NULL,sort_order INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS bookmarks(id TEXT PRIMARY KEY,category_id TEXT NOT NULL REFERENCES categories(id),name TEXT NOT NULL,url TEXT NOT NULL,note TEXT NOT NULL DEFAULT '',icon TEXT NOT NULL DEFAULT '',sort_order INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS bookmark_category ON bookmarks(category_id,sort_order)`,
		`CREATE TABLE IF NOT EXISTS admins(id INTEGER PRIMARY KEY CHECK(id=1),username TEXT NOT NULL,password BLOB NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY,expires INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL)`} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	var version int
	if err = db.QueryRow("SELECT version FROM schema_version").Scan(&version); err != nil || version != 1 {
		db.Close()
		return nil, errors.New("unsupported database version")
	}
	return db, nil
}
func stamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func (a *App) data() (Data, error) {
	d := Data{Categories: []Category{}, Bookmarks: []Bookmark{}}
	rows, err := a.db.Query("SELECT id,COALESCE(parent_id,''),name,sort_order FROM categories ORDER BY sort_order,id")
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var c Category
		if err = rows.Scan(&c.ID, &c.ParentID, &c.Name, &c.Sort); err != nil {
			rows.Close()
			return d, err
		}
		d.Categories = append(d.Categories, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	rows, err = a.db.Query("SELECT id,category_id,name,url,note,icon,sort_order FROM bookmarks ORDER BY sort_order,id")
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var b Bookmark
		if err = rows.Scan(&b.ID, &b.CategoryID, &b.Name, &b.URL, &b.Note, &b.Icon, &b.Sort); err != nil {
			return d, err
		}
		d.Bookmarks = append(d.Bookmarks, b)
	}
	return d, rows.Err()
}
func (a *App) setting(key, fallback string) string {
	var s string
	if a.db.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&s) != nil {
		return fallback
	}
	return s
}
func (a *App) setSetting(key, value string) error {
	_, err := a.db.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return err
}
func (a *App) categoryExists(id string) bool {
	var n int
	return id != "" && a.db.QueryRow("SELECT 1 FROM categories WHERE id=?", id).Scan(&n) == nil
}
func (a *App) saveCategory(c Category, update bool) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 240 {
		return errors.New("请填写分类名（最多 240 字节）")
	}
	if c.ParentID != "" && !a.categoryExists(c.ParentID) {
		return errors.New("上级分类不存在")
	}
	if update {
		if !a.categoryExists(c.ID) {
			return errors.New("分类不存在")
		}
		parent := c.ParentID
		for depth := 0; parent != ""; depth++ {
			if parent == c.ID || depth >= 64 {
				return errors.New("分类不能移动到自身或子分类中")
			}
			if err := a.db.QueryRow("SELECT COALESCE(parent_id,'') FROM categories WHERE id=?", parent).Scan(&parent); err != nil {
				return err
			}
		}
		var previous string
		a.db.QueryRow("SELECT COALESCE(parent_id,'') FROM categories WHERE id=?", c.ID).Scan(&previous)
		if previous != c.ParentID {
			a.db.QueryRow("SELECT COALESCE(MAX(sort_order),-1)+1 FROM categories WHERE COALESCE(parent_id,'')=?", c.ParentID).Scan(&c.Sort)
		} else {
			a.db.QueryRow("SELECT sort_order FROM categories WHERE id=?", c.ID).Scan(&c.Sort)
		}
		_, err := a.db.Exec("UPDATE categories SET parent_id=NULLIF(?,''),name=?,sort_order=?,updated_at=? WHERE id=?", c.ParentID, c.Name, c.Sort, stamp(), c.ID)
		return err
	}
	a.db.QueryRow("SELECT COALESCE(MAX(sort_order),-1)+1 FROM categories WHERE COALESCE(parent_id,'')=?", c.ParentID).Scan(&c.Sort)
	_, err := a.db.Exec("INSERT INTO categories VALUES(?,NULLIF(?,''),?,?,?)", c.ID, c.ParentID, c.Name, c.Sort, stamp())
	return err
}
func (a *App) saveBookmark(b Bookmark, update bool) error {
	b.Name = strings.TrimSpace(b.Name)
	b.URL = strings.TrimSpace(b.URL)
	if b.Name == "" || len(b.Name) > 1000 || len(b.Note) > 10000 || len(b.URL) > 8192 {
		return errors.New("请检查书签名称、网址或备注长度")
	}
	if !validURL(b.URL) {
		return errors.New("网址须使用 http 或 https")
	}
	if !a.categoryExists(b.CategoryID) {
		return errors.New("请选择有效分类")
	}
	if update {
		var oldCategory string
		if err := a.db.QueryRow("SELECT category_id,icon,sort_order FROM bookmarks WHERE id=?", b.ID).Scan(&oldCategory, &b.Icon, &b.Sort); err != nil {
			return errors.New("书签不存在")
		}
		if oldCategory != b.CategoryID {
			a.db.QueryRow("SELECT COALESCE(MAX(sort_order),-1)+1 FROM bookmarks WHERE category_id=?", b.CategoryID).Scan(&b.Sort)
		}
		_, err := a.db.Exec("UPDATE bookmarks SET category_id=?,name=?,url=?,note=?,sort_order=?,updated_at=? WHERE id=?", b.CategoryID, b.Name, b.URL, b.Note, b.Sort, stamp(), b.ID)
		return err
	}
	a.db.QueryRow("SELECT COALESCE(MAX(sort_order),-1)+1 FROM bookmarks WHERE category_id=?", b.CategoryID).Scan(&b.Sort)
	_, err := a.db.Exec("INSERT INTO bookmarks VALUES(?,?,?,?,?,?,?,?)", b.ID, b.CategoryID, b.Name, b.URL, b.Note, b.Icon, b.Sort, stamp())
	return err
}

// Reorder validates the complete sibling set before writing any positions.
func (a *App) reorder(kind, parent string, ids []string) error {
	table, field := "bookmarks", "category_id"
	if kind == "categories" {
		table, field = "categories", "COALESCE(parent_id,'')"
	} else if kind != "bookmarks" {
		return errors.New("排序类型无效")
	}
	rows, err := a.db.Query("SELECT id FROM "+table+" WHERE "+field+"=?", parent)
	if err != nil {
		return err
	}
	set := map[string]bool{}
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			rows.Close()
			return err
		}
		set[s] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) != len(set) {
		return errors.New("列表已变化，请刷新后重试")
	}
	for _, s := range ids {
		if !set[s] {
			return errors.New("排序包含重复或无效条目")
		}
		delete(set, s)
	}
	tx, err := a.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for n, s := range ids {
		if _, err = tx.Exec("UPDATE "+table+" SET sort_order=?,updated_at=? WHERE id=?", n, stamp(), s); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func privateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}
func ensureData(root string) error {
	for _, d := range []string{root, filepath.Join(root, "icons"), filepath.Join(root, "backups")} {
		if err := privateDir(d); err != nil {
			return fmt.Errorf("data directory: %w", err)
		}
	}
	return nil
}
