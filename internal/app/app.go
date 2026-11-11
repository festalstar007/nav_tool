package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	DataDir      string
	WebDir       string
	Origin       string
	SecureCookie bool
}
type App struct {
	db       *sql.DB
	root     string
	cfg      Config
	mu       sync.Mutex
	backupMu sync.Mutex
	token    string
	store    BackupStore
	iconSem  chan struct{}
	loginMu  sync.Mutex
	failures map[string]attempt
}
type attempt struct {
	Count int
	Until time.Time
}

func New(cfg Config) (*App, error) {
	root, e := filepath.Abs(cfg.DataDir)
	if e != nil {
		return nil, e
	}
	if e = ensureData(root); e != nil {
		return nil, e
	}
	db, e := openDB(filepath.Join(root, "nav.db"))
	if e != nil {
		return nil, e
	}
	a := &App{db: db, root: root, cfg: cfg, iconSem: make(chan struct{}, 2), failures: map[string]attempt{}, store: LocalStore{Root: filepath.Join(root, "backups")}}
	if !a.initialized() {
		p := filepath.Join(root, "setup-token")
		b, err := os.ReadFile(p)
		if err == nil {
			a.token = strings.TrimSpace(string(b))
		} else {
			a.token = id() + id()
			if err = os.WriteFile(p, []byte(a.token+"\n"), 0600); err != nil {
				db.Close()
				return nil, err
			}
		}
	}
	return a, nil
}
func (a *App) Close() error { return a.db.Close() }
func (a *App) initialized() bool {
	var n int
	return a.db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&n) == nil && n > 0
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/status", a.status)
	mux.HandleFunc("POST /api/auth/setup", a.setup)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.protect(a.logout))
	mux.HandleFunc("POST /api/auth/password", a.protect(a.password))
	mux.HandleFunc("GET /api/data", a.protect(a.getData))
	mux.HandleFunc("POST /api/categories", a.protect(a.category))
	mux.HandleFunc("PUT /api/categories/{id}", a.protect(a.category))
	mux.HandleFunc("DELETE /api/categories/{id}", a.protect(a.category))
	mux.HandleFunc("POST /api/bookmarks", a.protect(a.bookmark))
	mux.HandleFunc("POST /api/bookmarks/batch-delete", a.protect(a.batchDeleteBookmarks))
	mux.HandleFunc("PUT /api/bookmarks/{id}", a.protect(a.bookmark))
	mux.HandleFunc("DELETE /api/bookmarks/{id}", a.protect(a.bookmark))
	mux.HandleFunc("POST /api/bookmarks/{id}/icon", a.protect(a.icon))
	mux.HandleFunc("POST /api/bookmarks/{id}/move", a.protect(a.moveBookmark))
	mux.HandleFunc("POST /api/reorder", a.protect(a.sort))
	mux.HandleFunc("POST /api/import/{action}", a.protect(a.importHTML))
	mux.HandleFunc("GET /api/backups", a.protect(a.backupStatus))
	mux.HandleFunc("POST /api/backups", a.protect(a.backupNow))
	mux.HandleFunc("PUT /api/backups/settings", a.protect(a.backupSettings))
	mux.HandleFunc("GET /api/backups/{name}", a.protect(a.downloadBackup))
	mux.HandleFunc("GET /api/icons/{name}", a.protect(a.serveIcon))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "接口不存在") })
	mux.HandleFunc("/", a.frontend)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			expected := a.cfg.Origin
			if expected == "" {
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				expected = scheme + "://" + r.Host
			}
			if r.Header.Get("X-Nav-Request") != "1" || (origin != "" && origin != expected) {
				fail(w, 403, "请求来源无效")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		}
		mux.ServeHTTP(w, r)
	})
}
func send(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("请求内容无效")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("请求内容无效")
	}
	return nil
}
func check(w http.ResponseWriter, e error) bool {
	if e != nil {
		fail(w, 400, e.Error())
		return false
	}
	return true
}
func (a *App) authenticated(r *http.Request) bool {
	c, e := r.Cookie("nav_session")
	if e != nil {
		return false
	}
	h := sha256.Sum256([]byte(c.Value))
	var n int
	return a.db.QueryRow("SELECT 1 FROM sessions WHERE token=? AND expires>?", hex.EncodeToString(h[:]), time.Now().Unix()).Scan(&n) == nil
}
func (a *App) protect(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			fail(w, 401, "请先登录")
			return
		}
		h(w, r)
	}
}
func (a *App) status(w http.ResponseWriter, r *http.Request) {
	send(w, map[string]bool{"initialized": a.initialized(), "authenticated": a.authenticated(r)})
}
func (a *App) rateOK(r *http.Request) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	now := time.Now()
	for k, v := range a.failures {
		if now.After(v.Until) {
			delete(a.failures, k)
		}
	}
	v := a.failures[ip]
	if v.Count >= 10 {
		return false
	}
	v.Count++
	v.Until = now.Add(15 * time.Minute)
	a.failures[ip] = v
	return true
}
func (a *App) newSession(w http.ResponseWriter) error {
	token := id() + id()
	h := sha256.Sum256([]byte(token))
	_, err := a.db.Exec("INSERT INTO sessions VALUES(?,?)", hex.EncodeToString(h[:]), time.Now().Add(7*24*time.Hour).Unix())
	if err != nil {
		return err
	}
	a.db.Exec("DELETE FROM sessions WHERE expires<=?", time.Now().Unix())
	http.SetCookie(w, &http.Cookie{Name: "nav_session", Value: token, Path: "/", HttpOnly: true, Secure: a.cfg.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 7 * 86400})
	return nil
}
func (a *App) setup(w http.ResponseWriter, r *http.Request) {
	if !a.rateOK(r) {
		fail(w, 429, "尝试过多，请稍后再试")
		return
	}
	var v struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Token    string `json:"token"`
	}
	if !check(w, decode(r, &v)) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initialized() {
		fail(w, 409, "已完成初始化")
		return
	}
	if a.token == "" || subtle.ConstantTimeCompare([]byte(v.Token), []byte(a.token)) != 1 {
		fail(w, 403, "初始化凭据无效")
		return
	}
	if len(strings.TrimSpace(v.Username)) < 1 || len(v.Username) > 80 || len(v.Password) < 10 || len(v.Password) > 72 {
		fail(w, 400, "用户名不能为空，密码需为 10–72 字节")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(v.Password), bcrypt.DefaultCost)
	if !check(w, e) {
		return
	}
	_, e = a.db.Exec("INSERT INTO admins VALUES(1,?,?)", strings.TrimSpace(v.Username), hash)
	if !check(w, e) {
		return
	}
	os.Remove(filepath.Join(a.root, "setup-token"))
	a.token = ""
	if !check(w, a.newSession(w)) {
		return
	}
	send(w, map[string]bool{"ok": true})
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !a.rateOK(r) {
		fail(w, 429, "尝试过多，请稍后再试")
		return
	}
	var v struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !check(w, decode(r, &v)) {
		return
	}
	var username string
	var hash []byte
	e := a.db.QueryRow("SELECT username,password FROM admins WHERE id=1").Scan(&username, &hash)
	if e != nil || username != v.Username || bcrypt.CompareHashAndPassword(hash, []byte(v.Password)) != nil {
		fail(w, 401, "用户名或密码错误")
		return
	}
	if !check(w, a.newSession(w)) {
		return
	}
	send(w, map[string]bool{"ok": true})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie("nav_session")
	h := sha256.Sum256([]byte(c.Value))
	a.db.Exec("DELETE FROM sessions WHERE token=?", hex.EncodeToString(h[:]))
	http.SetCookie(w, &http.Cookie{Name: "nav_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.SecureCookie, SameSite: http.SameSiteStrictMode})
	send(w, map[string]bool{"ok": true})
}
func (a *App) password(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !check(w, decode(r, &v)) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var hash []byte
	a.db.QueryRow("SELECT password FROM admins WHERE id=1").Scan(&hash)
	if bcrypt.CompareHashAndPassword(hash, []byte(v.Current)) != nil {
		fail(w, 400, "当前密码错误")
		return
	}
	if len(v.Password) < 10 || len(v.Password) > 72 {
		fail(w, 400, "密码需为 10–72 字节")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(v.Password), bcrypt.DefaultCost)
	if !check(w, e) {
		return
	}
	_, e = a.db.Exec("UPDATE admins SET password=? WHERE id=1", hash)
	if !check(w, e) {
		return
	}
	a.db.Exec("DELETE FROM sessions")
	if !check(w, a.newSession(w)) {
		return
	}
	send(w, map[string]bool{"ok": true})
}
func (a *App) getData(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, e := a.data()
	if e != nil {
		fail(w, 500, "读取失败")
		return
	}
	send(w, d)
}
func (a *App) category(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cid := r.PathValue("id")
	if r.Method == "DELETE" {
		var n int
		a.db.QueryRow("SELECT (SELECT COUNT(*) FROM categories WHERE parent_id=?)+(SELECT COUNT(*) FROM bookmarks WHERE category_id=?)", cid, cid).Scan(&n)
		if n > 0 {
			fail(w, 409, "请先移走子分类和书签")
			return
		}
		res, e := a.db.Exec("DELETE FROM categories WHERE id=?", cid)
		if !check(w, e) {
			return
		}
		n64, _ := res.RowsAffected()
		if n64 == 0 {
			fail(w, 404, "分类不存在")
			return
		}
		send(w, map[string]bool{"ok": true})
		return
	}
	var c Category
	if !check(w, decode(r, &c)) {
		return
	}
	c.ID = cid
	if cid == "" {
		c.ID = id()
	}
	if !check(w, a.saveCategory(c, cid != "")) {
		return
	}
	send(w, c)
}
func (a *App) bookmark(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	bid := r.PathValue("id")
	if r.Method == "DELETE" {
		result, err := a.deleteBookmarks([]string{bid})
		if errors.Is(err, errBookmarkMissing) {
			fail(w, 404, "书签不存在")
			return
		}
		if !check(w, err) {
			return
		}
		send(w, result)
		return
	}
	var b Bookmark
	if !check(w, decode(r, &b)) {
		return
	}
	b.ID = bid
	b.Icon = ""
	if bid == "" {
		b.ID = id()
	}
	if !check(w, a.saveBookmark(b, bid != "")) {
		return
	}
	send(w, b)
}
func (a *App) moveBookmark(w http.ResponseWriter, r *http.Request) {
	var v struct {
		CategoryID string `json:"category_id"`
		BeforeID   string `json:"before_id"`
	}
	if !check(w, decode(r, &v)) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !check(w, a.placeBookmark(r.PathValue("id"), v.CategoryID, v.BeforeID)) {
		return
	}
	d, err := a.data()
	if err != nil {
		fail(w, 500, "读取失败，请刷新页面")
		return
	}
	send(w, d)
}
func (a *App) sort(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Kind   string   `json:"kind"`
		Parent string   `json:"parent"`
		IDs    []string `json:"ids"`
	}
	if !check(w, decode(r, &v)) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !check(w, a.reorder(v.Kind, v.Parent, v.IDs)) {
		return
	}
	send(w, map[string]bool{"ok": true})
}
func (a *App) importHTML(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action != "preview" && action != "apply" {
		fail(w, 404, "接口不存在")
		return
	}
	if e := r.ParseMultipartForm(1 << 20); e != nil {
		fail(w, 400, "文件过大或格式无效（最多 16 MB）")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, e := r.FormFile("file")
	if !check(w, e) {
		return
	}
	defer file.Close()
	p, e := ParseImport(file)
	if !check(w, e) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !check(w, a.applyImport(&p, action == "apply")) {
		return
	}
	send(w, p)
}
func (a *App) frontend(w http.ResponseWriter, r *http.Request) {
	path := filepath.Clean(r.URL.Path)
	if path == "/" {
		path = "/index.html"
	}
	if path != "/index.html" && path != "/favicon.ico" && !strings.HasPrefix(path, "/assets/") {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(a.cfg.WebDir, path)
	if _, e := os.Stat(full); e != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFile(w, r, full)
}
func (a *App) serveIcon(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !safeName(name) {
		http.NotFound(w, r)
		return
	}
	b, e := os.ReadFile(filepath.Join(a.root, "icons", name))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	if _, ok := iconType(b); !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(b))
	w.Write(b)
}
func safeName(s string) bool {
	return s != "" && s != "." && s != ".." && filepath.Base(s) == s && !strings.ContainsAny(s, "/\\\x00")
}
func (a *App) Scheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			cfg := a.backupConfig()
			if !cfg.Enabled {
				continue
			}
			loc, e := time.LoadLocation(cfg.Timezone)
			if e != nil {
				continue
			}
			local := now.In(loc)
			today := local.Format("2006-01-02")
			if local.Format("15:04") < cfg.Time || a.setting("backup_attempt", "") == today {
				continue
			}
			a.mu.Lock()
			a.setSetting("backup_attempt", today)
			a.mu.Unlock()
			if _, e = a.Backup(); e != nil {
				log.Print("scheduled backup failed; inspect backup status")
			}
		}
	}
}
func ParseOrigin(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	u, e := url.Parse(s)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("invalid NAV_ORIGIN")
	}
	return u.Scheme + "://" + u.Host, nil
}
