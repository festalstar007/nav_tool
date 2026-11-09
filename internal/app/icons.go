package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !net.ParseIP("100.64.0.0").Equal(ip) && !carrierNAT.Contains(ip)
}

var _, carrierNAT, _ = net.ParseCIDR("100.64.0.0/10")

func safeClient() *http.Client {
	d := net.Dialer{Timeout: 4 * time.Second}
	return &http.Client{Timeout: 7 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("redirect limit")
		}
		if !validURL(r.URL.String()) {
			return errors.New("invalid URL")
		}
		return nil
	}, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		if port != "80" && port != "443" {
			return nil, errors.New("port blocked")
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("DNS failed")
		}
		for _, v := range ips {
			if !publicIP(v.IP) {
				return nil, errors.New("private address blocked")
			}
		}
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}}
}
func (a *App) icon(w http.ResponseWriter, r *http.Request) {
	select {
	case a.iconSem <- struct{}{}:
		defer func() { <-a.iconSem }()
	default:
		fail(w, 429, "图标处理中，请稍后重试")
		return
	}
	bid := r.PathValue("id")
	var raw string
	if e := a.db.QueryRow("SELECT url FROM bookmarks WHERE id=?", bid).Scan(&raw); e != nil {
		fail(w, 404, "书签不存在")
		return
	}
	u, e := url.Parse(raw)
	if e != nil {
		fail(w, 400, "网址无效")
		return
	}
	u.Path = "/favicon.ico"
	u.RawQuery = ""
	u.Fragment = ""
	req, e := http.NewRequestWithContext(r.Context(), "GET", u.String(), nil)
	if e != nil {
		fail(w, 400, "网址无效")
		return
	}
	res, e := safeClient().Do(req)
	if e != nil {
		fail(w, 400, "未能获取图标")
		return
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, (256<<10)+1))
	ext, ok := iconType(b)
	if e != nil || res.StatusCode != 200 || !ok {
		fail(w, 400, "未找到可用图标")
		return
	}
	name := id() + ext
	a.mu.Lock()
	defer a.mu.Unlock()
	var current string
	if a.db.QueryRow("SELECT url FROM bookmarks WHERE id=?", bid).Scan(&current) != nil || current != raw {
		fail(w, 409, "书签已变化，请重试")
		return
	}
	if e = os.WriteFile(filepath.Join(a.root, "icons", name), b, 0600); !check(w, e) {
		return
	}
	_, e = a.db.Exec("UPDATE bookmarks SET icon=?,updated_at=? WHERE id=?", name, stamp(), bid)
	if !check(w, e) {
		os.Remove(filepath.Join(a.root, "icons", name))
		return
	}
	send(w, map[string]string{"icon": name})
}
