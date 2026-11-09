package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BackupFile struct {
	Name string    `json:"name"`
	Size int64     `json:"size"`
	Time time.Time `json:"time"`
}

// BackupStore separates archive creation from local or future remote storage.
type BackupStore interface {
	Save(context.Context, string, io.Reader) error
	List(context.Context) ([]BackupFile, error)
	Delete(context.Context, string) error
}
type LocalStore struct{ Root string }

func (s LocalStore) Save(ctx context.Context, name string, r io.Reader) error {
	if !safeName(name) {
		return errors.New("invalid archive name")
	}
	f, e := os.CreateTemp(s.Root, ".pending-")
	if e != nil {
		return e
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, e = io.Copy(f, r); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = os.Rename(temp, filepath.Join(s.Root, name)); e != nil {
		return e
	}
	dir, e := os.Open(s.Root)
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func (s LocalStore) List(ctx context.Context) ([]BackupFile, error) {
	entries, e := os.ReadDir(s.Root)
	if e != nil {
		return nil, e
	}
	files := []BackupFile{}
	for _, v := range entries {
		if !v.Type().IsRegular() || !strings.HasPrefix(v.Name(), "nav-") || !strings.HasSuffix(v.Name(), ".tar.gz") {
			continue
		}
		i, e := v.Info()
		if e != nil {
			return nil, e
		}
		files = append(files, BackupFile{v.Name(), i.Size(), i.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name > files[j].Name })
	return files, nil
}
func (s LocalStore) Delete(ctx context.Context, name string) error {
	if !safeName(name) || !strings.HasPrefix(name, "nav-") || !strings.HasSuffix(name, ".tar.gz") {
		return errors.New("invalid archive name")
	}
	return os.Remove(filepath.Join(s.Root, name))
}

type BackupConfig struct {
	Enabled  bool   `json:"enabled"`
	Time     string `json:"time"`
	Timezone string `json:"timezone"`
	Keep     int    `json:"keep"`
}
type BackupResult struct {
	Time    time.Time `json:"time"`
	Success bool      `json:"success"`
	Name    string    `json:"name"`
	Error   string    `json:"error,omitempty"`
}

func (a *App) backupConfig() BackupConfig {
	c := BackupConfig{false, "03:00", "Asia/Shanghai", 7}
	json.Unmarshal([]byte(a.setting("backup_config", "")), &c)
	return c
}
func (a *App) backupSettings(w http.ResponseWriter, r *http.Request) {
	var c BackupConfig
	if !check(w, decode(r, &c)) {
		return
	}
	if _, e := time.Parse("15:04", c.Time); e != nil {
		fail(w, 400, "请选择有效时间")
		return
	}
	if _, e := time.LoadLocation(c.Timezone); e != nil || c.Keep < 2 || c.Keep > 365 {
		fail(w, 400, "时区无效或保留份数不在 2–365 之间")
		return
	}
	b, _ := json.Marshal(c)
	a.mu.Lock()
	e := a.setSetting("backup_config", string(b))
	a.mu.Unlock()
	if !check(w, e) {
		return
	}
	send(w, c)
}
func (a *App) backupStatus(w http.ResponseWriter, r *http.Request) {
	files, e := a.store.List(r.Context())
	if e != nil {
		fail(w, 500, "无法读取备份目录")
		return
	}
	var result *BackupResult
	json.Unmarshal([]byte(a.setting("backup_result", "null")), &result)
	send(w, map[string]any{"config": a.backupConfig(), "last": result, "files": files})
}
func (a *App) backupNow(w http.ResponseWriter, r *http.Request) {
	name, e := a.Backup()
	if e != nil {
		fail(w, 500, "备份失败，请检查磁盘空间和目录权限")
		return
	}
	send(w, map[string]string{"name": name})
}
func (a *App) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	files, e := a.store.List(r.Context())
	if e != nil {
		fail(w, 500, "读取失败")
		return
	}
	for _, f := range files {
		if name == f.Name {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
			w.Header().Set("Content-Type", "application/gzip")
			http.ServeFile(w, r, filepath.Join(a.root, "backups", name))
			return
		}
	}
	http.NotFound(w, r)
}
func (a *App) Backup() (name string, err error) {
	a.backupMu.Lock()
	defer a.backupMu.Unlock()
	defer func() {
		result := BackupResult{Time: time.Now(), Success: err == nil, Name: name}
		if err != nil {
			result.Error = "备份失败，请检查磁盘空间和目录权限"
		}
		b, _ := json.Marshal(result)
		a.mu.Lock()
		a.setSetting("backup_result", string(b))
		a.mu.Unlock()
	}()
	temp, err := os.MkdirTemp(a.root, ".backup-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	// Keep the database and icon files at the same logical point in time.
	err = func() error {
		a.mu.Lock()
		defer a.mu.Unlock()
		snap := filepath.Join(temp, "nav.db")
		if _, e := a.db.Exec("VACUUM INTO ?", snap); e != nil {
			return e
		}
		if e := integrity(snap); e != nil {
			return e
		}
		if e := copyIcons(filepath.Join(a.root, "icons"), filepath.Join(temp, "icons")); e != nil {
			return e
		}
		meta, _ := json.Marshal(map[string]any{"application": "nav_tool", "version": "0.1.0", "schema_version": 1, "created_at": time.Now().UTC()})
		return os.WriteFile(filepath.Join(temp, "manifest.json"), meta, 0600)
	}()
	if err != nil {
		return "", err
	}
	archive := filepath.Join(temp, "archive.tar.gz")
	if err = pack(temp, archive); err != nil {
		return "", err
	}
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	name = "nav-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".tar.gz"
	ctx := context.Background()
	if err = a.store.Save(ctx, name, f); err != nil {
		return name, err
	}
	files, err := a.store.List(ctx)
	if err != nil {
		return name, err
	}
	keep := a.backupConfig().Keep
	for i := keep; i < len(files); i++ {
		if err = a.store.Delete(ctx, files[i].Name); err != nil {
			return name, err
		}
	}
	return name, nil
}
func integrity(path string) error {
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if e != nil {
		return e
	}
	defer db.Close()
	var result string
	if e = db.QueryRow("PRAGMA integrity_check").Scan(&result); e != nil {
		return e
	}
	if result != "ok" {
		return errors.New("database integrity check failed")
	}
	return nil
}
func copyIcons(src, dst string) error {
	if e := privateDir(dst); e != nil {
		return e
	}
	entries, e := os.ReadDir(src)
	if e != nil {
		return e
	}
	for _, v := range entries {
		if !v.Type().IsRegular() {
			continue
		}
		b, e := os.ReadFile(filepath.Join(src, v.Name()))
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(dst, v.Name()), b, 0600); e != nil {
			return e
		}
	}
	return nil
}
func pack(root, path string) (err error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer func() {
		if e := f.Close(); err == nil {
			err = e
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, base := range []string{"nav.db", "manifest.json", "icons"} {
		err = filepath.WalkDir(filepath.Join(root, base), func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.Type().IsRegular() && !d.IsDir() {
				return errors.New("invalid archive entry")
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			h, e := tar.FileInfoHeader(info, "")
			if e != nil {
				return e
			}
			h.Name, _ = filepath.Rel(root, p)
			h.Mode = 0600
			if d.IsDir() {
				h.Mode = 0700
			}
			if e = tw.WriteHeader(h); e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			in, e := os.Open(p)
			if e != nil {
				return e
			}
			_, e = io.Copy(tw, in)
			in.Close()
			return e
		})
		if err != nil {
			tw.Close()
			gz.Close()
			return err
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	return f.Sync()
}

// Restore only creates a NEW data directory. It cannot replace a running installation.
func Restore(archive, target string) (err error) {
	if _, e := os.Stat(target); !os.IsNotExist(e) {
		return errors.New("恢复目标必须是不存在的新目录")
	}
	parent := filepath.Dir(target)
	if e := os.MkdirAll(parent, 0700); e != nil {
		return e
	}
	stage, e := os.MkdirTemp(parent, ".nav-restore-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	f, e := os.Open(archive)
	if e != nil {
		return e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var total int64
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		name := h.Name
		if seen[name] {
			return errors.New("重复的归档条目")
		}
		seen[name] = true
		allowed := name == "nav.db" || name == "manifest.json" || name == "icons" || (strings.HasPrefix(name, "icons/") && safeName(strings.TrimPrefix(name, "icons/")))
		if !allowed {
			return errors.New("归档路径无效")
		}
		if h.Size < 0 || h.Size > 256<<20 {
			return errors.New("归档文件过大")
		}
		total += h.Size
		if total > 1<<30 {
			return errors.New("归档总大小超过限制")
		}
		out := filepath.Join(stage, name)
		if h.Typeflag == tar.TypeDir {
			if name != "icons" {
				return errors.New("目录无效")
			}
			if e = privateDir(out); e != nil {
				return e
			}
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return errors.New("归档不支持链接或特殊文件")
		}
		if e = os.MkdirAll(filepath.Dir(out), 0700); e != nil {
			return e
		}
		dest, e := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(dest, tr)
		ce := dest.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	if _, e = io.Copy(io.Discard, gz); e != nil {
		return e
	}
	meta, e := os.ReadFile(filepath.Join(stage, "manifest.json"))
	if e != nil {
		return e
	}
	var m struct {
		Application string `json:"application"`
		Schema      int    `json:"schema_version"`
	}
	if e = json.Unmarshal(meta, &m); e != nil || m.Application != "nav_tool" || m.Schema != 1 {
		return errors.New("备份版本无效")
	}
	dbPath := filepath.Join(stage, "nav.db")
	if e = integrity(dbPath); e != nil {
		return e
	}
	db, e := openDB(dbPath)
	if e != nil {
		return e
	}
	rows, e := db.Query("PRAGMA foreign_key_check")
	if e != nil {
		db.Close()
		return e
	}
	bad := rows.Next()
	rows.Close()
	if bad {
		db.Close()
		return errors.New("备份关系检查失败")
	}
	rows, e = db.Query("SELECT icon FROM bookmarks WHERE icon<>''")
	if e != nil {
		db.Close()
		return e
	}
	for rows.Next() {
		var icon string
		rows.Scan(&icon)
		if !safeName(icon) {
			rows.Close()
			db.Close()
			return errors.New("图标引用无效")
		}
		if _, e = os.Stat(filepath.Join(stage, "icons", icon)); e != nil {
			rows.Close()
			db.Close()
			return errors.New("备份缺少图标")
		}
	}
	rows.Close()
	if _, e = db.Exec("DELETE FROM sessions"); e != nil {
		db.Close()
		return e
	}
	db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if e = db.Close(); e != nil {
		return e
	}
	if e = ensureData(stage); e != nil {
		return e
	}
	os.Remove(filepath.Join(stage, "manifest.json"))
	return os.Rename(stage, target)
}
