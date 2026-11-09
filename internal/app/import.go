package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/net/html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type ImportItem struct {
	Path []string `json:"path"`
	Name string   `json:"name"`
	URL  string   `json:"url"`
	Icon string   `json:"-"`
}

// ImportFile seeds data without exposing an unauthenticated HTTP import endpoint.
// Run this command only while the application service is stopped.
func (a *App) ImportFile(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	p, err := ParseImport(f)
	if err != nil {
		return 0, 0, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err = a.applyImport(&p, true); err != nil {
		return 0, 0, err
	}
	return p.Added, p.Duplicates, nil
}

type ImportPlan struct {
	Items       []ImportItem `json:"-"`
	Total       int          `json:"total"`
	Skipped     int          `json:"skipped"`
	Duplicates  int          `json:"duplicates"`
	Added       int          `json:"added"`
	Folders     int          `json:"folders"`
	Paths       []string     `json:"paths"`
	Warnings    []string     `json:"warnings"`
	folderPaths [][]string
}

func validURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && len(s) <= 8192
}
func canonical(s string) string {
	u, e := url.Parse(strings.TrimSpace(s))
	if e != nil {
		return s
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}

// Netscape exports use a DL for each folder; DT tags are commonly left unclosed.
func ParseImport(r io.Reader) (ImportPlan, error) {
	p := ImportPlan{Items: []ImportItem{}, Paths: []string{}, Warnings: []string{}}
	z := html.NewTokenizer(io.LimitReader(r, 16<<20))
	stack := [][]string{}
	path := []string{}
	pending := ""
	capture := ""
	text := ""
	href := ""
	icon := ""
	seen := map[string]bool{}
	addFolder := func(parts []string) {
		key := strings.Join(parts, "\x00")
		if !seen[key] {
			seen[key] = true
			p.folderPaths = append(p.folderPaths, append([]string{}, parts...))
			p.Paths = append(p.Paths, strings.Join(parts, " / "))
		}
	}
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				return p, z.Err()
			}
			break
		}
		switch tt {
		case html.StartTagToken:
			t := z.Token()
			switch t.Data {
			case "h3":
				capture = "folder"
				text = ""
			case "a":
				capture = "link"
				text = ""
				href = ""
				icon = ""
				for _, a := range t.Attr {
					if a.Key == "href" {
						href = a.Val
					}
					if a.Key == "icon" {
						icon = a.Val
					}
				}
			case "dl":
				stack = append(stack, append([]string{}, path...))
				if pending != "" {
					if len(path) >= 64 {
						return p, errors.New("书签层级超过 64 层")
					}
					path = append(append([]string{}, path...), pending)
					addFolder(path)
					pending = ""
				}
			}
		case html.TextToken:
			if capture != "" {
				text += string(z.Text())
				if len(text) > 10000 {
					return p, errors.New("书签名称过长")
				}
			}
		case html.EndTagToken:
			t := z.Token()
			switch t.Data {
			case "h3":
				pending = strings.TrimSpace(text)
				if len(pending) > 240 {
					return p, errors.New("分类名称过长")
				}
				capture = ""
			case "a":
				p.Total++
				if p.Total > 20000 {
					return p, errors.New("单次最多导入 20000 条书签")
				}
				name := strings.TrimSpace(text)
				if !validURL(href) || len(name) > 1000 {
					p.Skipped++
				} else {
					if name == "" {
						name = href
					}
					parts := append([]string{}, path...)
					if len(parts) == 0 {
						parts = []string{"未分类"}
						addFolder(parts)
					}
					p.Items = append(p.Items, ImportItem{parts, name, href, icon})
				}
				capture = ""
			case "dl":
				if len(stack) > 0 {
					path = stack[len(stack)-1]
					stack = stack[:len(stack)-1]
				}
				pending = ""
			}
		}
	}
	p.Folders = len(p.folderPaths)
	if p.Total == 0 {
		return p, errors.New("没有找到书签，请选择浏览器导出的 HTML 文件")
	}
	if p.Skipped > 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%d 条无效或不支持的链接将跳过", p.Skipped))
	}
	return p, nil
}
func (a *App) applyImport(p *ImportPlan, commit bool) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids := map[string]string{}
	nextC := map[string]int{}
	nextB := map[string]int{}
	existing := map[string]bool{}
	rows, err := tx.Query("SELECT id,COALESCE(parent_id,''),name,sort_order FROM categories ORDER BY sort_order,id")
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, parent, name string
		var order int
		if err = rows.Scan(&cid, &parent, &name, &order); err != nil {
			rows.Close()
			return err
		}
		key := parent + "\x00" + name
		if ids[key] == "" {
			ids[key] = cid
		}
		if order >= nextC[parent] {
			nextC[parent] = order + 1
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query("SELECT category_id,url,sort_order FROM bookmarks")
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, u string
		var order int
		if err = rows.Scan(&cid, &u, &order); err != nil {
			rows.Close()
			return err
		}
		existing[cid+"\x00"+canonical(u)] = true
		if order >= nextB[cid] {
			nextB[cid] = order + 1
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	resolve := func(parts []string) (string, error) {
		parent := ""
		for _, name := range parts {
			key := parent + "\x00" + name
			cid := ids[key]
			if cid == "" {
				cid = id()
				if commit {
					if _, err := tx.Exec("INSERT INTO categories VALUES(?,NULLIF(?,''),?,?,?)", cid, parent, name, nextC[parent], stamp()); err != nil {
						return "", err
					}
				}
				nextC[parent]++
				ids[key] = cid
			}
			parent = cid
		}
		return parent, nil
	}
	for _, parts := range p.folderPaths {
		if _, err = resolve(parts); err != nil {
			return err
		}
	}
	for _, item := range p.Items {
		cid, e := resolve(item.Path)
		if e != nil {
			return e
		}
		key := cid + "\x00" + canonical(item.URL)
		if existing[key] {
			p.Duplicates++
			continue
		}
		existing[key] = true
		p.Added++
		if commit {
			bid := id()
			icon := ""
			if data, ext, ok := decodeIcon(item.Icon); ok {
				icon = bid + ext
				if err = os.WriteFile(filepath.Join(a.root, "icons", icon), data, 0600); err != nil {
					return err
				}
			}
			_, err = tx.Exec("INSERT INTO bookmarks VALUES(?,?,?,?,?,?,?,?)", bid, cid, item.Name, item.URL, "", icon, nextB[cid], stamp())
			if err != nil {
				return err
			}
			nextB[cid]++
		}
	}
	if commit {
		return tx.Commit()
	}
	return nil
}
func decodeIcon(s string) ([]byte, string, bool) {
	if len(s) > 400000 || !strings.HasPrefix(s, "data:image/") {
		return nil, "", false
	}
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
		return nil, "", false
	}
	b, e := base64.StdEncoding.DecodeString(parts[1])
	if e != nil {
		return nil, "", false
	}
	ext, ok := iconType(b)
	return b, ext, ok
}
func iconType(b []byte) (string, bool) {
	if len(b) == 0 || len(b) > 256<<10 {
		return "", false
	}
	switch http.DetectContentType(b) {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	case "image/x-icon", "image/vnd.microsoft.icon":
		return ".ico", true
	}
	if len(b) > 4 && bytes.Equal(b[:4], []byte{0, 0, 1, 0}) {
		return ".ico", true
	}
	return "", false
}
