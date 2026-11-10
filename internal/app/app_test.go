package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	a, e := New(Config{DataDir: t.TempDir(), WebDir: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Close() })
	return a
}
func call(t *testing.T, a *App, method, path string, body any, cookie *http.Cookie, header bool) *httptest.ResponseRecorder {
	t.Helper()
	var in io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		in = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, in)
	r.Header.Set("Content-Type", "application/json")
	if header {
		r.Header.Set("X-Nav-Request", "1")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func admin(t *testing.T, a *App) *http.Cookie {
	t.Helper()
	w := call(t, a, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "test-password-123", "token": a.token}, nil, true)
	if w.Code != 200 {
		t.Fatalf("setup %d: %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}
func TestAuthAndRequestProtection(t *testing.T) {
	a := newTestApp(t)
	if got := call(t, a, "GET", "/api/data", nil, nil, false).Code; got != 401 {
		t.Fatal(got)
	}
	w := call(t, a, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "test-password-123", "token": "wrong"}, nil, true)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	c := admin(t, a)
	if call(t, a, "GET", "/api/data", nil, c, false).Code != 200 {
		t.Fatal("authenticated read failed")
	}
	if call(t, a, "POST", "/api/categories", map[string]string{"name": "test"}, c, false).Code != 403 {
		t.Fatal("missing CSRF header accepted")
	}
	r := httptest.NewRequest("POST", "/api/categories", strings.NewReader(`{"name":"test"}`))
	r.Header.Set("Origin", "https://attacker.example")
	r.Header.Set("X-Nav-Request", "1")
	r.AddCookie(c)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
	call(t, a, "POST", "/api/auth/logout", nil, c, true)
	if call(t, a, "GET", "/api/data", nil, c, false).Code != 401 {
		t.Fatal("session remained active")
	}
}
func TestCategoriesAndTransactionalOrder(t *testing.T) {
	a := newTestApp(t)
	root := Category{ID: id(), Name: "技术"}
	child := Category{ID: id(), ParentID: root.ID, Name: "Go"}
	if e := a.saveCategory(root, false); e != nil {
		t.Fatal(e)
	}
	if e := a.saveCategory(child, false); e != nil {
		t.Fatal(e)
	}
	root.ParentID = child.ID
	if e := a.saveCategory(root, true); e == nil {
		t.Fatal("cycle accepted")
	}
	var ids []string
	for _, name := range []string{"A", "B", "C"} {
		b := Bookmark{ID: id(), Name: name, CategoryID: child.ID, URL: "https://example.com/" + name}
		if e := a.saveBookmark(b, false); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, b.ID)
	}
	if e := a.reorder("bookmarks", child.ID, []string{ids[2], ids[1], ids[0]}); e != nil {
		t.Fatal(e)
	}
	if e := a.reorder("bookmarks", child.ID, []string{ids[0], ids[0], ids[2]}); e == nil {
		t.Fatal("duplicate accepted")
	}
	if e := a.reorder("bookmarks", root.ID, ids); e == nil {
		t.Fatal("cross-category order accepted")
	}
	d, e := a.data()
	if e != nil || d.Bookmarks[0].ID != ids[2] {
		t.Fatal("failed order changed state")
	}
	c := admin(t, a)
	if call(t, a, "DELETE", "/api/categories/"+child.ID, nil, c, true).Code != 409 {
		t.Fatal("non-empty category deleted")
	}
}
func TestMoveBookmarkAcrossCategories(t *testing.T) {
	a := newTestApp(t)
	cookie := admin(t, a)
	for _, c := range []Category{{ID: "source", Name: "来源"}, {ID: "target", Name: "目标"}, {ID: "empty", Name: "空分类", ParentID: "target"}} {
		if err := a.saveCategory(c, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range []Bookmark{
		{ID: "a", CategoryID: "source", Name: "A", URL: "https://example.com/a", Note: "保留备注", Icon: "a.png"},
		{ID: "b", CategoryID: "source", Name: "B", URL: "https://example.com/b"},
		{ID: "c", CategoryID: "target", Name: "C", URL: "https://example.com/c"},
		{ID: "d", CategoryID: "target", Name: "D", URL: "https://example.com/d"},
	} {
		if err := a.saveBookmark(b, false); err != nil {
			t.Fatal(err)
		}
	}
	assertOrder := func(cid string, want string) {
		t.Helper()
		rows, err := a.db.Query("SELECT id,sort_order FROM bookmarks WHERE category_id=? ORDER BY sort_order,id", cid)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var id string
			var order int
			if err := rows.Scan(&id, &order); err != nil {
				t.Fatal(err)
			}
			if order != len(got) {
				t.Fatal("non-contiguous positions", cid, order)
			}
			got = append(got, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, ",") != want {
			t.Fatalf("%s: got %v, want %s", cid, got, want)
		}
	}
	move := map[string]string{"category_id": "target", "before_id": "d"}
	if w := call(t, a, "POST", "/api/bookmarks/a/move", move, nil, true); w.Code != 401 {
		t.Fatal("unauthenticated move accepted")
	}
	if w := call(t, a, "POST", "/api/bookmarks/a/move", move, cookie, false); w.Code != 403 {
		t.Fatal("move without request protection accepted")
	}
	if w := call(t, a, "POST", "/api/bookmarks/a/move", move, cookie, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	assertOrder("source", "b")
	assertOrder("target", "c,a,d")
	var name, href, note, icon string
	if err := a.db.QueryRow("SELECT name,url,note,icon FROM bookmarks WHERE id='a'").Scan(&name, &href, &note, &icon); err != nil {
		t.Fatal(err)
	}
	if name != "A" || href != "https://example.com/a" || note != "保留备注" || icon != "a.png" {
		t.Fatal("move changed bookmark content")
	}
	for _, invalid := range []map[string]string{
		{"category_id": "source", "before_id": "d"},
		{"category_id": "missing"},
		{"category_id": "target", "before_id": "missing"},
		{"category_id": "target", "before_id": "a"},
	} {
		if w := call(t, a, "POST", "/api/bookmarks/a/move", invalid, cookie, true); w.Code != 400 {
			t.Fatal("invalid destination accepted", w.Code)
		}
		assertOrder("source", "b")
		assertOrder("target", "c,a,d")
	}
	if err := a.placeBookmark("missing", "source", ""); err == nil {
		t.Fatal("missing bookmark accepted")
	}
	if err := a.placeBookmark("a", "target", "c"); err != nil {
		t.Fatal(err)
	}
	assertOrder("target", "a,c,d")
	if err := a.placeBookmark("a", "empty", ""); err != nil {
		t.Fatal(err)
	}
	assertOrder("target", "c,d")
	assertOrder("empty", "a")
	if err := a.placeBookmark("a", "target", ""); err != nil {
		t.Fatal(err)
	}
	assertOrder("empty", "")
	assertOrder("target", "c,d,a")
}

func TestImportNestedFoldersAndIdempotence(t *testing.T) {
	a := newTestApp(t)
	source := `<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><p><DT><H3>技术</H3><DL><p><DT><H3>Go &amp; SQL</H3><DL><p><DT><A HREF="https://EXAMPLE.com/">例子</A><DT><A HREF="https://example.com">重复</A><DT><A HREF="javascript:alert(1)">无效</A></DL><p><DT><A HREF="https://example.org/">父目录链接</A></DL><p><DT><H3>生活</H3><DL><p><DT><A HREF="https://example.com">跨分类允许</A></DL></DL>`
	p, e := ParseImport(strings.NewReader(source))
	if e != nil {
		t.Fatal(e)
	}
	if p.Total != 5 || p.Skipped != 1 || strings.Join(p.Items[0].Path, "/") != "技术/Go & SQL" || strings.Join(p.Items[2].Path, "/") != "技术" {
		t.Fatalf("wrong import plan counts/paths")
	}
	if e = a.applyImport(&p, false); e != nil {
		t.Fatal(e)
	}
	if p.Added != 3 || p.Duplicates != 1 {
		t.Fatalf("preview counts %d %d", p.Added, p.Duplicates)
	}
	d, _ := a.data()
	if len(d.Categories) > 0 {
		t.Fatal("preview wrote data")
	}
	p, _ = ParseImport(strings.NewReader(source))
	if e = a.applyImport(&p, true); e != nil {
		t.Fatal(e)
	}
	p, _ = ParseImport(strings.NewReader(source))
	if e = a.applyImport(&p, true); e != nil {
		t.Fatal(e)
	}
	d, _ = a.data()
	if p.Added != 0 || p.Duplicates != 4 || len(d.Bookmarks) != 3 || len(d.Categories) != 3 {
		t.Fatalf("duplicate import changed counts")
	}
}
func TestRealEdgeExport(t *testing.T) {
	path := "../../docs/favorites_2026_11_9.html"
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		t.Skip("private Edge fixture not present")
	}
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	p, e := ParseImport(f)
	if e != nil {
		t.Fatal(e)
	}
	if p.Total != 587 || p.Folders != 56 || p.Skipped != 0 {
		t.Fatalf("unexpected real fixture counts: %d %d %d", p.Total, p.Folders, p.Skipped)
	}
	a := newTestApp(t)
	if e = a.applyImport(&p, true); e != nil {
		t.Fatal(e)
	}
	d, e := a.data()
	if e != nil || len(d.Bookmarks)+p.Duplicates != 587 || len(d.Categories) != 56 {
		t.Fatal("real fixture import mismatch")
	}
	for _, b := range d.Bookmarks {
		if b.Icon != "" {
			if _, e = os.Stat(filepath.Join(a.root, "icons", b.Icon)); e != nil {
				t.Fatal("missing imported icon")
			}
		}
	}
	t.Logf("real Edge import verified: %d bookmarks, %d categories, %d duplicates", len(d.Bookmarks), len(d.Categories), p.Duplicates)
}
func TestBackupRestoreRetention(t *testing.T) {
	a := newTestApp(t)
	c := admin(t, a)
	root := Category{ID: id(), Name: "恢复验证"}
	if e := a.saveCategory(root, false); e != nil {
		t.Fatal(e)
	}
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aN1sAAAAASUVORK5CYII=")
	os.WriteFile(filepath.Join(a.root, "icons", "test.png"), png, 0600)
	b := Bookmark{ID: id(), CategoryID: root.ID, Name: "测试", URL: "https://example.com", Icon: "test.png"}
	if e := a.saveBookmark(b, false); e != nil {
		t.Fatal(e)
	}
	a.setSetting("backup_config", `{"enabled":false,"time":"03:00","timezone":"Asia/Shanghai","keep":2}`)
	var name string
	for i := 0; i < 3; i++ {
		var e error
		name, e = a.Backup()
		if e != nil {
			t.Fatal(e)
		}
	}
	files, e := a.store.List(context.Background())
	if e != nil || len(files) != 2 {
		t.Fatal("retention failed")
	}
	if call(t, a, "GET", "/api/backups/"+name, nil, nil, false).Code != 401 {
		t.Fatal("anonymous backup allowed")
	}
	w := call(t, a, "GET", "/api/backups/"+name, nil, c, false)
	if w.Code != 200 || w.Body.Len() == 0 {
		t.Fatal("download failed")
	}
	target := filepath.Join(t.TempDir(), "restored")
	if e = Restore(filepath.Join(a.root, "backups", name), target); e != nil {
		t.Fatal(e)
	}
	restored, e := New(Config{DataDir: target})
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	d, e := restored.data()
	if e != nil || len(d.Bookmarks) != 1 || d.Bookmarks[0].Name != b.Name || len(d.Categories) != 1 {
		t.Fatal("restore data mismatch")
	}
	if _, e = os.Stat(filepath.Join(target, "icons", "test.png")); e != nil {
		t.Fatal(e)
	}
	if restored.authenticated(httptest.NewRequest("GET", "/", nil)) {
		t.Fatal("restore authentication")
	}
	var n int
	restored.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n)
	if n != 0 {
		t.Fatal("sessions were restored")
	}
	if Restore(filepath.Join(a.root, "backups", name), target) == nil {
		t.Fatal("existing target replaced")
	}
}
func TestRestoreRejectsTraversal(t *testing.T) {
	file := filepath.Join(t.TempDir(), "malicious.tar.gz")
	f, _ := os.Create(file)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0600, Size: 1, Typeflag: tar.TypeReg})
	tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	f.Close()
	if e := Restore(file, filepath.Join(t.TempDir(), "out")); e == nil {
		t.Fatal("traversal accepted")
	}
}
func TestIconAddressRestrictions(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "169.254.169.254", "100.100.100.200", "::1", "fc00::1", "0.0.0.0"} {
		if publicIP(net.ParseIP(s)) {
			t.Fatalf("private IP accepted: %s", s)
		}
	}
	if !publicIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public IP blocked")
	}
	for _, s := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://user:pass@example.com"} {
		if validURL(s) {
			t.Fatal("unsafe URL accepted")
		}
	}
}
