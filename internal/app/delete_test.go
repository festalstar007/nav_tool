package app

import "testing"

func TestBatchDeletePrunesOnlyAffectedEmptyAncestors(t *testing.T) {
	a := newTestApp(t)
	cookie := admin(t, a)
	for _, c := range []Category{
		{ID: "root", Name: "root"}, {ID: "branch", Name: "branch", ParentID: "root"},
		{ID: "leaf", Name: "leaf", ParentID: "branch"}, {ID: "keep", Name: "keep", ParentID: "root"},
		{ID: "empty", Name: "previously empty"},
	} {
		if err := a.saveCategory(c, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range []Bookmark{
		{ID: "a", CategoryID: "leaf", Name: "a", URL: "https://example.com/a"},
		{ID: "b", CategoryID: "branch", Name: "b", URL: "https://example.com/b"},
		{ID: "c", CategoryID: "keep", Name: "c", URL: "https://example.com/c"},
		{ID: "d", CategoryID: "keep", Name: "d", URL: "https://example.com/d"},
	} {
		if err := a.saveBookmark(b, false); err != nil {
			t.Fatal(err)
		}
	}
	body := map[string]any{"ids": []string{"a", "b", "c"}}
	if w := call(t, a, "POST", "/api/bookmarks/batch-delete", body, nil, true); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call(t, a, "POST", "/api/bookmarks/batch-delete", body, cookie, false); w.Code != 403 {
		t.Fatal(w.Code)
	}
	// A stale or malformed selection must not partially remove valid bookmarks.
	for _, ids := range [][]string{nil, {"a", "missing"}, {"a", "a"}, {""}} {
		w := call(t, a, "POST", "/api/bookmarks/batch-delete", map[string]any{"ids": ids}, cookie, true)
		if w.Code != 400 {
			t.Fatalf("invalid selection: %d %s", w.Code, w.Body.String())
		}
		d, err := a.data()
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Bookmarks) != 4 || len(d.Categories) != 5 {
			t.Fatal("partial deletion", d)
		}
	}
	w := call(t, a, "POST", "/api/bookmarks/batch-delete", body, cookie, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	d, err := a.data()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Bookmarks) != 1 || d.Bookmarks[0].ID != "d" {
		t.Fatal(d.Bookmarks)
	}
	if len(d.Categories) != 3 {
		t.Fatal(d.Categories)
	}
	for _, c := range d.Categories {
		if c.ID == "leaf" || c.ID == "branch" {
			t.Fatal(c)
		}
	}
	// Single deletion uses the same pruning rule and persists across reads.
	w = call(t, a, "DELETE", "/api/bookmarks/d", nil, cookie, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	d, err = a.data()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Bookmarks) != 0 || len(d.Categories) != 1 || d.Categories[0].ID != "empty" {
		t.Fatal(d)
	}
}

func TestBatchDeleteRollsBackPruningFailure(t *testing.T) {
	a := newTestApp(t)
	if err := a.saveCategory(Category{ID: "folder", Name: "folder"}, false); err != nil {
		t.Fatal(err)
	}
	if err := a.saveBookmark(Bookmark{ID: "a", Name: "a", CategoryID: "folder", URL: "https://example.com"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_prune BEFORE DELETE ON categories BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := a.deleteBookmarks([]string{"a"}); err == nil {
		t.Fatal("expected failure")
	}
	d, err := a.data()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Bookmarks) != 1 || len(d.Categories) != 1 {
		t.Fatal("deletion was not atomic", d)
	}
}
