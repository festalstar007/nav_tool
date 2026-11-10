import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  DndContext,
  DragOverlay,
  closestCenter,
  useDroppable,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  arrayMove,
  rectSortingStrategy,
  sortableKeyboardCoordinates,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  Search,
  Plus,
  Settings,
  LogOut,
  Folder,
  ChevronRight,
  ChevronDown,
  X,
  GripVertical,
  Pencil,
  Trash2,
  ArrowUp,
  ArrowDown,
  Upload,
  Download,
  Check,
  LayoutGrid,
  Menu,
  Link,
  RefreshCw,
  LoaderCircle,
  Bookmark as BookmarkIcon,
  ArrowUpRight,
  ShieldCheck,
} from "lucide-react";
import "./style.css";
import { bookmarkCollision, insertAfter } from "./bookmark-drag";

type Category = {
  id: string;
  parent_id: string;
  name: string;
  sort_order: number;
};
type Bookmark = {
  id: string;
  category_id: string;
  name: string;
  url: string;
  note: string;
  icon: string;
  sort_order: number;
};
type Data = { categories: Category[]; bookmarks: Bookmark[] };
type Auth = { initialized: boolean; authenticated: boolean };
type BackupConfig = {
  enabled: boolean;
  time: string;
  timezone: string;
  keep: number;
};
type BackupState = {
  config: BackupConfig;
  last: null | { time: string; success: boolean; error?: string };
  files: { name: string; size: number; time: string }[];
};
type Plan = {
  total: number;
  added: number;
  duplicates: number;
  skipped: number;
  folders: number;
  paths: string[];
  warnings: string[];
};
type ModalState =
  | { kind: "bookmark"; value?: Bookmark; category?: string }
  | { kind: "category"; value?: Category; parent?: string }
  | { kind: "import" }
  | { kind: "settings" }
  | { kind: "password" }
  | null;
async function api<T = unknown>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  const form = body instanceof FormData;
  const res = await fetch("/api" + path, {
    method,
    headers: {
      "X-Nav-Request": "1",
      ...(!form && body !== undefined
        ? { "Content-Type": "application/json" }
        : {}),
    },
    body: body === undefined ? undefined : form ? body : JSON.stringify(body),
  });
  if (!res.ok) {
    let message = "操作失败，请重试";
    try {
      message = (await res.json()).error || message;
    } catch {}
    if (res.status === 401 && !path.startsWith("/auth/"))
      window.dispatchEvent(new Event("nav-unauthorized"));
    throw new Error(message);
  }
  return res.json();
}
const ordered = <T extends { sort_order: number; id: string }>(items: T[]) =>
  [...items].sort(
    (a, b) => a.sort_order - b.sort_order || a.id.localeCompare(b.id),
  );
function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = ref.current;
    const previous = document.activeElement as HTMLElement;
    element?.showModal();
    return () => {
      element?.close();
      previous?.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className="modal"
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          const b = e.currentTarget.getBoundingClientRect();
          if (
            e.clientX < b.left ||
            e.clientX > b.right ||
            e.clientY < b.top ||
            e.clientY > b.bottom
          )
            onClose();
        }
      }}
    >
      <div className="modal-head">
        <h2>{title}</h2>
        <button className="icon-btn" aria-label="关闭" onClick={onClose}>
          <X size={19} />
        </button>
      </div>
      {children}
    </dialog>
  );
}
type SortableProps = {
  id: string;
  disabled: boolean;
  inactive?: boolean;
  inactiveReason?: string;
  label?: string;
  children: (handle: React.ReactNode) => React.ReactNode;
};
function Sortable(props: SortableProps) {
  return props.disabled ? (
    <div className="sortable">{props.children(null)}</div>
  ) : (
    <ActiveSortable {...props} />
  );
}
function ActiveSortable({
  id,
  disabled,
  inactive = false,
  inactiveReason,
  label,
  children,
}: SortableProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
    isOver,
    active,
    over,
  } = useSortable({ id, disabled: disabled || inactive, data: { label } });
  const after =
    active?.rect.current.translated && over?.rect
      ? insertAfter(active.rect.current.translated, over.rect)
      : false;
  return (
    <div
      ref={setNodeRef}
      style={{
        transform:
          isDragging && label ? undefined : CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.45 : 1,
        zIndex: isDragging ? 5 : undefined,
      }}
      className={
        "sortable " +
        (isOver ? "drop-target " + (after ? "drop-after" : "drop-before") : "")
      }
    >
      {children(
        <button
          className="drag-handle"
          aria-label="拖动排序"
          disabled={inactive}
          title={inactiveReason || "拖动排序"}
          {...attributes}
          {...(inactive ? {} : listeners)}
        >
          <GripVertical size={15} />
        </button>,
      )}
    </div>
  );
}
type SortGroupProps = {
  items: string[];
  disabled: boolean;
  onSort: (ids: string[]) => void;
  children: React.ReactNode;
};
function SortGroup(props: SortGroupProps) {
  return props.disabled ? (
    <>{props.children}</>
  ) : (
    <ActiveSortGroup {...props} />
  );
}
function ActiveSortGroup({
  items,
  disabled,
  onSort,
  children,
}: {
  items: string[];
  disabled: boolean;
  onSort: (ids: string[]) => void;
  children: React.ReactNode;
}) {
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );
  function end(e: DragEndEvent) {
    if (e.over && e.active.id !== e.over.id) {
      const a = items.indexOf(String(e.active.id)),
        b = items.indexOf(String(e.over.id));
      if (a >= 0 && b >= 0) onSort(arrayMove(items, a, b));
    }
  }
  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragEnd={disabled ? undefined : end}
    >
      <SortableContext items={items} strategy={rectSortingStrategy}>
        {children}
      </SortableContext>
    </DndContext>
  );
}
function BookmarkBoard({
  disabled,
  onDrop,
  children,
}: {
  disabled: boolean;
  onDrop: (event: DragEndEvent) => void;
  children: React.ReactNode;
}) {
  return disabled ? (
    <>{children}</>
  ) : (
    <ActiveBookmarkBoard onDrop={onDrop}>{children}</ActiveBookmarkBoard>
  );
}
function ActiveBookmarkBoard({
  onDrop,
  children,
}: {
  onDrop: (event: DragEndEvent) => void;
  children: React.ReactNode;
}) {
  const [preview, setPreview] = useState<{
    name: string;
    width: number;
  } | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );
  return (
    <DndContext
      sensors={sensors}
      collisionDetection={bookmarkCollision}
      onDragStart={({ active }) =>
        setPreview({
          name: active.data.current?.label || "",
          width: active.rect.current.initial?.width || 168,
        })
      }
      onDragCancel={() => setPreview(null)}
      onDragEnd={(event) => {
        setPreview(null);
        onDrop(event);
      }}
    >
      {children}
      <DragOverlay dropAnimation={null}>
        {preview && (
          <div
            className="bookmark-drag-preview"
            style={{ width: preview.width }}
          >
            <GripVertical size={15} />
            <span>{preview.name}</span>
          </div>
        )}
      </DragOverlay>
    </DndContext>
  );
}
function BookmarkSection({
  category,
  disabled,
  empty,
  children,
}: {
  category: Category;
  disabled: boolean;
  empty: boolean;
  children: React.ReactNode;
}) {
  const { setNodeRef, isOver } = useDroppable({
    id: "bookmark-section-" + category.id,
    data: { categoryId: category.id, section: true, empty },
    disabled,
  });
  return (
    <section
      ref={setNodeRef}
      className={"bookmark-section " + (isOver ? "over" : "")}
    >
      {children}
    </section>
  );
}
function Icon({ b }: { b: Bookmark }) {
  const [broken, setBroken] = useState(false);
  useEffect(() => setBroken(false), [b.icon]);
  return b.icon && !broken ? (
    <img
      className="site-icon"
      src={"/api/icons/" + encodeURIComponent(b.icon)}
      alt=""
      onError={() => setBroken(true)}
      loading="lazy"
      decoding="async"
      width={20}
      height={20}
    />
  ) : (
    <span className="site-fallback" aria-hidden="true">
      {b.name.slice(0, 1).toUpperCase()}
    </span>
  );
}
function App() {
  const [auth, setAuth] = useState<Auth | null>(null);
  const [data, setData] = useState<Data>({ categories: [], bookmarks: [] });
  const [selected, setSelected] = useState("");
  const [search, setSearch] = useState("");
  const [edit, setEdit] = useState(false);
  const [modal, setModal] = useState<ModalState>(null);
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [sideOpen, setSideOpen] = useState(false);
  const [folded, setFolded] = useState<Set<string>>(new Set());
  const searchRef = useRef<HTMLInputElement>(null);
  const [renderLimit, setRenderLimit] = useState(80);
  const moreRef = useRef<HTMLButtonElement>(null);
  // Count each bookmark once along its ancestor chain, rather than rebuilding
  // every category subtree separately for every bookmark on every render.
  const categoryCounts = useMemo(() => {
    const byId = new Map(data.categories.map((c) => [c.id, c]));
    const counts = new Map<string, number>();
    for (const b of data.bookmarks) {
      let id = b.category_id;
      const seen = new Set<string>();
      while (id && !seen.has(id)) {
        seen.add(id);
        counts.set(id, (counts.get(id) || 0) + 1);
        id = byId.get(id)?.parent_id || "";
      }
    }
    return counts;
  }, [data.categories, data.bookmarks]);
  const message = (e: unknown) =>
    setNotice(e instanceof Error ? e.message : String(e));
  async function load() {
    const d = await api<Data>("/data");
    setData(d);
    return d;
  }
  useEffect(() => {
    api<Auth>("/auth/status").then(setAuth).catch(message);
    const unauth = () => {
      setAuth((a) => (a ? { ...a, authenticated: false } : null));
      setData({ categories: [], bookmarks: [] });
      setModal(null);
    };
    window.addEventListener("nav-unauthorized", unauth);
    return () => window.removeEventListener("nav-unauthorized", unauth);
  }, []);
  useEffect(() => {
    if (auth?.authenticated) {
      setLoading(true);
      load()
        .catch(message)
        .finally(() => setLoading(false));
    }
  }, [auth?.authenticated]);
  useEffect(() => {
    if (!notice) return;
    const timer = setTimeout(() => setNotice(""), 5000);
    return () => clearTimeout(timer);
  }, [notice]);
  useEffect(() => {
    function key(e: KeyboardEvent) {
      if ((e.ctrlKey || e.metaKey) && e.key === "k") {
        e.preventDefault();
        searchRef.current?.focus();
      }
    }
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, []);
  async function perform(fn: () => Promise<unknown>, close = true) {
    if (busy) return;
    setBusy(true);
    try {
      await fn();
      await load();
      if (close) setModal(null);
    } catch (e) {
      message(e);
    } finally {
      setBusy(false);
    }
  }
  async function reorder(
    kind: "categories" | "bookmarks",
    parent: string,
    ids: string[],
  ) {
    if (busy) return;
    setBusy(true);
    const previous = data;
    setData((d) => ({
      ...d,
      [kind]: d[kind].map((item) => {
        const index = ids.indexOf(item.id);
        return index < 0 ? item : { ...item, sort_order: index };
      }),
    }));
    try {
      await api("/reorder", "POST", { kind, parent, ids });
    } catch (e) {
      setData(previous);
      message(e);
    } finally {
      setBusy(false);
    }
  }
  const breadcrumb = (id: string): string => {
    const names: string[] = [];
    let c = data.categories.find((v) => v.id === id);
    for (let n = 0; c && n < 64; n++) {
      names.unshift(c.name);
      c = data.categories.find((v) => v.id === c!.parent_id);
    }
    return names.join(" / ");
  };
  const descendants = (id: string) => {
    const set = new Set([id]);
    for (let i = 0; i < 64; i++) {
      const size = set.size;
      data.categories.forEach((c) => {
        if (set.has(c.parent_id)) set.add(c.id);
      });
      if (size === set.size) break;
    }
    return set;
  };
  const scope = selected ? descendants(selected) : null;
  const query = search.trim().toLocaleLowerCase();
  const visible = data.bookmarks.filter(
    (b) =>
      (!scope || !!query || scope.has(b.category_id)) &&
      (!query ||
        `${b.name} ${b.url} ${b.note}`.toLocaleLowerCase().includes(query)),
  );
  const flatCategories: Category[] = [];
  function flatten(parent = "") {
    ordered(data.categories.filter((c) => c.parent_id === parent)).forEach(
      (c) => {
        flatCategories.push(c);
        flatten(c.id);
      },
    );
  }
  flatten();
  const orderedVisible = flatCategories.flatMap((c) =>
    ordered(visible.filter((b) => b.category_id === c.id)),
  );
  const displayed = edit
    ? orderedVisible
    : orderedVisible.slice(0, renderLimit);
  const hasMore = !edit && displayed.length < orderedVisible.length;
  useEffect(() => setRenderLimit(80), [selected, query]);
  useEffect(() => {
    const target = moreRef.current;
    if (!target || !hasMore || !("IntersectionObserver" in window)) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          observer.disconnect();
          setRenderLimit((limit) => limit + 80);
        }
      },
      { rootMargin: "200px" },
    );
    observer.observe(target);
    return () => observer.disconnect();
  }, [hasMore, renderLimit, selected, query, loading, edit]);
  const choose = (id: string) => {
    setSelected(id);
    setSearch("");
    setSideOpen(false);
  };
  const toggle = (id: string) =>
    setFolded((s) => {
      const n = new Set(s);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });
  function categoryTree(parent = "", depth = 0): React.ReactNode {
    const items = ordered(
      data.categories.filter((c) => c.parent_id === parent),
    );
    return (
      <SortGroup
        items={items.map((c) => c.id)}
        disabled={!edit}
        onSort={(ids) => void reorder("categories", parent, ids)}
      >
        {items.map((c) => {
          const hasChild = data.categories.some((v) => v.parent_id === c.id);
          const count = categoryCounts.get(c.id) || 0;
          return (
            <React.Fragment key={c.id}>
              <Sortable id={c.id} disabled={!edit}>
                {(handle) => (
                  <div
                    className={
                      "category-row " + (selected === c.id ? "active" : "")
                    }
                    style={{ paddingLeft: 12 + depth * 14 }}
                  >
                    {handle}
                    <button
                      className="fold-btn"
                      aria-label={
                        folded.has(c.id) ? "展开 " + c.name : "折叠 " + c.name
                      }
                      onClick={() => toggle(c.id)}
                      disabled={!hasChild}
                    >
                      {hasChild ? (
                        folded.has(c.id) ? (
                          <ChevronRight size={13} />
                        ) : (
                          <ChevronDown size={13} />
                        )
                      ) : (
                        <Folder size={14} />
                      )}
                    </button>
                    <button
                      className="category-label"
                      title={c.name}
                      onClick={() => choose(c.id)}
                    >
                      {c.name}
                    </button>
                    {edit ? (
                      <button
                        className="icon-btn small"
                        aria-label={"编辑分类 " + c.name}
                        onClick={() => setModal({ kind: "category", value: c })}
                      >
                        <Pencil size={13} />
                      </button>
                    ) : (
                      <span className="count">{count || ""}</span>
                    )}
                  </div>
                )}
              </Sortable>
              {hasChild && !folded.has(c.id) && categoryTree(c.id, depth + 1)}
            </React.Fragment>
          );
        })}
      </SortGroup>
    );
  }
  function moveBookmark(b: Bookmark, direction: number) {
    const group = ordered(
      data.bookmarks.filter((v) => v.category_id === b.category_id),
    );
    const i = group.findIndex((v) => v.id === b.id);
    if (i + direction < 0 || i + direction >= group.length) return;
    void reorder(
      "bookmarks",
      b.category_id,
      arrayMove(group, i, i + direction).map((v) => v.id),
    );
  }
  async function dropBookmark(event: DragEndEvent) {
    if (busy || !event.over || event.active.id === event.over.id) return;
    const moved = data.bookmarks.find((b) => b.id === event.active.id);
    const target = data.bookmarks.find((b) => b.id === event.over!.id);
    if (!moved) return;
    const category = target?.category_id || event.over.data.current?.categoryId;
    if (!category) return;
    if (!target && category === moved.category_id) return;
    const group = ordered(
      data.bookmarks.filter((b) => b.category_id === category),
    );
    if (category === moved.category_id && target) {
      void reorder(
        "bookmarks",
        category,
        arrayMove(
          group,
          group.findIndex((b) => b.id === moved.id),
          group.findIndex((b) => b.id === target.id),
        ).map((b) => b.id),
      );
      return;
    }
    let index = target
      ? group.findIndex((b) => b.id === target.id)
      : group.length;
    const rect = event.active.rect.current.translated;
    if (target && rect) {
      if (insertAfter(rect, event.over.rect)) index++;
    }
    setBusy(true);
    try {
      const updated = await api<Data>(
        "/bookmarks/" + moved.id + "/move",
        "POST",
        {
          category_id: category,
          before_id: group[index]?.id || "",
        },
      );
      setData(updated);
      setNotice("已移入 " + breadcrumb(category));
    } catch (e) {
      message(e);
    } finally {
      setBusy(false);
    }
  }
  async function fetchIcon(bid: string) {
    try {
      await api("/bookmarks/" + bid + "/icon", "POST");
      await load();
      setNotice("图标已更新");
    } catch (e) {
      message(e);
    }
  }
  const toast = notice && (
    <div className="toast" role="status">
      {notice}
      <button aria-label="关闭提示" onClick={() => setNotice("")}>
        <X size={15} />
      </button>
    </div>
  );
  if (!auth)
    return (
      <main className="auth-page">
        <LoaderCircle className="spin" size={24} />
        {toast}
      </main>
    );
  if (!auth.authenticated)
    return (
      <main className="auth-page">
        <section className="auth-card">
          <Brand />
          <div className="auth-intro">
            <h1>{auth.initialized ? "登录" : "创建管理员"}</h1>
          </div>
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              const f = new FormData(e.currentTarget);
              setBusy(true);
              try {
                await api(
                  "/auth/" + (auth.initialized ? "login" : "setup"),
                  "POST",
                  {
                    username: f.get("username"),
                    password: f.get("password"),
                    ...(!auth.initialized ? { token: f.get("token") } : {}),
                  },
                );
                setAuth({ initialized: true, authenticated: true });
              } catch (e) {
                message(e);
              } finally {
                setBusy(false);
              }
            }}
          >
            {!auth.initialized && (
              <label>
                初始化凭据
                <input
                  name="token"
                  type="password"
                  required
                  autoComplete="off"
                />
              </label>
            )}
            <label>
              用户名
              <input
                name="username"
                autoComplete="username"
                required
                defaultValue="admin"
              />
            </label>
            <label>
              密码
              <input
                name="password"
                type="password"
                required
                minLength={auth.initialized ? 1 : 10}
                maxLength={72}
                autoComplete={
                  auth.initialized ? "current-password" : "new-password"
                }
              />
            </label>
            <button className="primary full" disabled={busy}>
              {busy ? <LoaderCircle className="spin" size={16} /> : null}
              {auth.initialized ? "登录" : "创建导航"}
              <ArrowUpRight size={16} />
            </button>
          </form>
        </section>
        {toast}
      </main>
    );
  return (
    <div className="app-shell">
      <aside className={"sidebar " + (sideOpen ? "open" : "")}>
        <div className="sidebar-brand">
          <Brand />
          <button
            className="icon-btn mobile-only"
            aria-label="收起分类"
            onClick={() => setSideOpen(false)}
          >
            <X size={18} />
          </button>
        </div>
        <div className="sidebar-top">
          <button
            className={"all-category " + (!selected ? "active" : "")}
            onClick={() => choose("")}
          >
            <LayoutGrid size={17} />
            <span>全部书签</span>
            <span className="count">{data.bookmarks.length}</span>
          </button>
        </div>
        <div className="section-label">
          <span>我的分类</span>
          <button
            className="icon-btn small"
            aria-label="添加分类"
            onClick={() => setModal({ kind: "category" })}
          >
            <Plus size={16} />
          </button>
        </div>
        <nav className="category-tree" aria-label="书签分类">
          {categoryTree()}
          {data.categories.length === 0 && !loading && (
            <button
              className="empty-category"
              onClick={() => setModal({ kind: "category" })}
            >
              创建第一个分类 <Plus size={14} />
            </button>
          )}
        </nav>
        <div className="sidebar-bottom">
          <button onClick={() => setModal({ kind: "import" })}>
            <Upload size={16} />
            导入书签
          </button>
          <button onClick={() => setModal({ kind: "settings" })}>
            <Settings size={16} />
            设置与备份
          </button>
          <button
            onClick={() => {
              api("/auth/logout", "POST")
                .then(() => {
                  setAuth({ initialized: true, authenticated: false });
                  setData({ categories: [], bookmarks: [] });
                  setSelected("");
                  setEdit(false);
                })
                .catch(message);
            }}
          >
            <LogOut size={16} />
            退出登录
          </button>
        </div>
      </aside>
      {sideOpen && (
        <button
          className="side-scrim"
          aria-label="关闭分类"
          onClick={() => setSideOpen(false)}
        />
      )}
      <main className="workspace">
        <header className="topbar">
          <button
            className="icon-btn mobile-only"
            aria-label="打开分类"
            onClick={() => setSideOpen(true)}
          >
            <Menu size={21} />
          </button>
          <div className="search-box">
            <Search size={18} />
            <input
              ref={searchRef}
              aria-label="搜索书签"
              placeholder="搜索书签"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {search && (
              <button
                className="icon-btn small"
                aria-label="清空搜索"
                onClick={() => setSearch("")}
              >
                <X size={14} />
              </button>
            )}
          </div>
          <div className="topbar-actions">
            <button
              className={"secondary " + (edit ? "selected" : "")}
              aria-label={edit ? "完成编辑" : "编辑"}
              onClick={() => {
                if (!edit) setSearch("");
                setEdit((v) => !v);
              }}
            >
              {edit ? <Check size={15} /> : <Pencil size={15} />}
              <span>{edit ? "完成编辑" : "编辑"}</span>
            </button>
            <button
              className="primary"
              aria-label="添加书签"
              onClick={() => {
                if (!data.categories.length) {
                  setModal({ kind: "category" });
                  return;
                }
                setModal({
                  kind: "bookmark",
                  category: selected || flatCategories[0]?.id,
                });
              }}
            >
              <Plus size={16} />
              <span>添加书签</span>
            </button>
          </div>
        </header>
        <div className="content">
          {(query || selected) && (
            <div className="page-heading">
              <div>
                <h1>
                  {query
                    ? "搜索结果"
                    : data.categories.find((c) => c.id === selected)?.name}
                  <span>{visible.length}</span>
                </h1>
                {selected && !query && (
                  <p className="breadcrumb">{breadcrumb(selected)}</p>
                )}
              </div>
            </div>
          )}
          {loading ? (
            <div className="empty-state">
              <LoaderCircle className="spin" size={24} />
              <p>正在加载</p>
            </div>
          ) : visible.length === 0 ? (
            <div className="empty-state">
              <span className="empty-symbol">
                {query ? <Search size={28} /> : <BookmarkIcon size={28} />}
              </span>
              <h2>{query ? "没有找到书签" : "暂无书签"}</h2>
              {query ? (
                <button className="secondary" onClick={() => setSearch("")}>
                  清空搜索
                </button>
              ) : (
                <button
                  className="primary"
                  onClick={() => setModal({ kind: "import" })}
                >
                  <Upload size={16} />
                  导入书签
                </button>
              )}
            </div>
          ) : (
            <BookmarkBoard
              disabled={!edit}
              onDrop={(event) => void dropBookmark(event)}
            >
              {flatCategories
                .filter(
                  (c) =>
                    displayed.some((b) => b.category_id === c.id) ||
                    (edit && !query && (!scope || scope.has(c.id))),
                )
                .map((c) => {
                  const items = ordered(
                    displayed.filter((b) => b.category_id === c.id),
                  );
                  return (
                    <BookmarkSection
                      category={c}
                      disabled={!edit || !!query || busy}
                      empty={items.length === 0}
                      key={c.id}
                    >
                      <div className="group-heading">
                        <h2>
                          <Folder size={15} />
                          {selected === c.id ? c.name : breadcrumb(c.id)}
                          <span>
                            {
                              visible.filter((b) => b.category_id === c.id)
                                .length
                            }
                          </span>
                        </h2>
                        {edit && (
                          <button
                            className="icon-btn small"
                            aria-label={"向 " + c.name + " 添加书签"}
                            onClick={() =>
                              setModal({ kind: "bookmark", category: c.id })
                            }
                          >
                            <Plus size={15} />
                          </button>
                        )}
                      </div>
                      <SortableContext
                        items={items.map((b) => b.id)}
                        strategy={rectSortingStrategy}
                      >
                        <div
                          className={"bookmark-grid " + (edit ? "editing" : "")}
                        >
                          {edit && !query && items.length === 0 && (
                            <div
                              className="empty-bookmark-drop"
                              aria-label={"移入 " + c.name}
                            >
                              拖到这里
                            </div>
                          )}
                          {items.map((b) => (
                            <Sortable
                              key={b.id}
                              id={b.id}
                              label={b.name}
                              disabled={!edit}
                              inactive={!!query || busy}
                              inactiveReason={
                                query
                                  ? "清空搜索后可拖动"
                                  : busy
                                    ? "正在保存"
                                    : undefined
                              }
                            >
                              {(handle) => (
                                <div className="bookmark-tile">
                                  {handle}
                                  <a
                                    className="bookmark-link"
                                    href={b.url}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    title={
                                      b.name +
                                      "\n" +
                                      b.url +
                                      (b.note ? "\n" + b.note : "")
                                    }
                                    onClick={(e) => {
                                      if (edit) {
                                        e.preventDefault();
                                        setModal({
                                          kind: "bookmark",
                                          value: b,
                                        });
                                      }
                                    }}
                                  >
                                    <Icon b={b} />
                                    <span>{b.name}</span>
                                    {!edit && (
                                      <ArrowUpRight
                                        className="link-arrow"
                                        size={12}
                                      />
                                    )}
                                  </a>
                                  {edit && (
                                    <div className="tile-actions">
                                      <button
                                        aria-label={"前移 " + b.name}
                                        onClick={() => moveBookmark(b, -1)}
                                        disabled={busy}
                                      >
                                        <ArrowUp size={11} />
                                      </button>
                                      <button
                                        aria-label={"后移 " + b.name}
                                        onClick={() => moveBookmark(b, 1)}
                                        disabled={busy}
                                      >
                                        <ArrowDown size={11} />
                                      </button>
                                    </div>
                                  )}
                                </div>
                              )}
                            </Sortable>
                          ))}
                        </div>
                      </SortableContext>
                    </BookmarkSection>
                  );
                })}
            </BookmarkBoard>
          )}
          {!loading && hasMore && (
            <button
              ref={moreRef}
              className="secondary load-more"
              onClick={() => setRenderLimit((limit) => limit + 80)}
            >
              显示更多（剩余 {orderedVisible.length - displayed.length}）
            </button>
          )}
        </div>
      </main>
      {modal?.kind === "category" && (
        <CategoryEditor
          value={modal.value}
          parent={modal.parent}
          categories={flatCategories}
          breadcrumb={breadcrumb}
          excluded={modal.value ? descendants(modal.value.id) : new Set()}
          busy={busy}
          onClose={() => setModal(null)}
          onSave={(c) =>
            perform(() =>
              api(
                "/categories" + (modal.value ? "/" + modal.value.id : ""),
                modal.value ? "PUT" : "POST",
                c,
              ),
            )
          }
          onDelete={
            modal.value
              ? () => {
                  if (confirm("删除这个分类？"))
                    void perform(() =>
                      api("/categories/" + modal.value!.id, "DELETE"),
                    );
                }
              : undefined
          }
        />
      )}
      {modal?.kind === "bookmark" && (
        <BookmarkEditor
          value={modal.value}
          category={modal.category}
          categories={flatCategories}
          breadcrumb={breadcrumb}
          busy={busy}
          onClose={() => setModal(null)}
          onIcon={modal.value ? () => fetchIcon(modal.value!.id) : undefined}
          onSave={(b) =>
            perform(() =>
              api(
                "/bookmarks" + (modal.value ? "/" + modal.value.id : ""),
                modal.value ? "PUT" : "POST",
                b,
              ),
            )
          }
          onDelete={
            modal.value
              ? () => {
                  if (confirm("删除这个书签？"))
                    void perform(() =>
                      api("/bookmarks/" + modal.value!.id, "DELETE"),
                    );
                }
              : undefined
          }
        />
      )}
      {modal?.kind === "import" && (
        <ImportDialog
          onClose={() => setModal(null)}
          onDone={async () => {
            await load();
            setModal(null);
          }}
          message={message}
        />
      )}
      {modal?.kind === "settings" && (
        <SettingsDialog
          onClose={() => setModal(null)}
          message={message}
          onPassword={() => setModal({ kind: "password" })}
        />
      )}
      {modal?.kind === "password" && (
        <Modal title="修改密码" onClose={() => setModal(null)}>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const f = new FormData(e.currentTarget);
              void perform(() =>
                api("/auth/password", "POST", {
                  current: f.get("current"),
                  password: f.get("password"),
                }),
              );
            }}
          >
            <label>
              当前密码
              <input
                name="current"
                type="password"
                required
                autoComplete="current-password"
              />
            </label>
            <label>
              新密码
              <input
                name="password"
                type="password"
                minLength={10}
                maxLength={72}
                required
                autoComplete="new-password"
              />
            </label>
            <div className="modal-actions">
              <button className="primary" disabled={busy}>
                保存
              </button>
            </div>
          </form>
        </Modal>
      )}
      {toast}
    </div>
  );
}
function Brand() {
  return (
    <div className="brand">
      <span className="brand-mark">
        <LayoutGrid size={20} />
      </span>
      <span>
        Nav<span className="brand-light">Tool</span>
      </span>
    </div>
  );
}
function CategoryEditor({
  value,
  parent,
  categories,
  breadcrumb,
  excluded,
  busy,
  onClose,
  onSave,
  onDelete,
}: {
  value?: Category;
  parent?: string;
  categories: Category[];
  breadcrumb: (id: string) => string;
  excluded: Set<string>;
  busy: boolean;
  onClose: () => void;
  onSave: (c: Partial<Category>) => void;
  onDelete?: () => void;
}) {
  return (
    <Modal title={value ? "编辑分类" : "新建分类"} onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const f = new FormData(e.currentTarget);
          onSave({
            name: String(f.get("name")),
            parent_id: String(f.get("parent_id")),
          });
        }}
      >
        <label>
          名称
          <input
            name="name"
            required
            autoFocus
            maxLength={80}
            defaultValue={value?.name}
          />
        </label>
        <label>
          上级分类
          <select
            name="parent_id"
            defaultValue={value?.parent_id || parent || ""}
          >
            <option value="">顶级分类</option>
            {categories
              .filter((c) => !excluded.has(c.id))
              .map((c) => (
                <option key={c.id} value={c.id}>
                  {breadcrumb(c.id)}
                </option>
              ))}
          </select>
        </label>
        <div className="modal-actions">
          {onDelete && (
            <button
              type="button"
              className="danger-text"
              disabled={busy}
              onClick={onDelete}
            >
              <Trash2 size={15} />
              删除
            </button>
          )}
          <span />
          <button type="button" className="secondary" onClick={onClose}>
            取消
          </button>
          <button className="primary" disabled={busy}>
            {busy ? "保存中…" : "保存"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
function BookmarkEditor({
  value,
  category,
  categories,
  breadcrumb,
  busy,
  onClose,
  onSave,
  onDelete,
  onIcon,
}: {
  value?: Bookmark;
  category?: string;
  categories: Category[];
  breadcrumb: (id: string) => string;
  busy: boolean;
  onClose: () => void;
  onSave: (b: Partial<Bookmark>) => void;
  onDelete?: () => void;
  onIcon?: () => Promise<void>;
}) {
  const [iconBusy, setIconBusy] = useState(false);
  return (
    <Modal title={value ? "编辑书签" : "添加书签"} onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const f = new FormData(e.currentTarget);
          onSave({
            name: String(f.get("name")),
            url: String(f.get("url")),
            category_id: String(f.get("category_id")),
            note: String(f.get("note")),
          });
        }}
      >
        <label>
          名称
          <input
            name="name"
            required
            autoFocus
            defaultValue={value?.name}
            maxLength={300}
          />
        </label>
        <label>
          网址
          <input
            name="url"
            type="url"
            required
            placeholder="https://"
            defaultValue={value?.url}
          />
        </label>
        <label>
          分类
          <select
            name="category_id"
            defaultValue={value?.category_id || category || categories[0]?.id}
            required
          >
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {breadcrumb(c.id)}
              </option>
            ))}
          </select>
        </label>
        <label>
          备注 <span className="optional">可选</span>
          <textarea
            name="note"
            rows={2}
            defaultValue={value?.note}
            maxLength={3000}
          />
        </label>
        {onIcon && (
          <button
            className="text-button"
            type="button"
            disabled={iconBusy}
            onClick={async () => {
              setIconBusy(true);
              try {
                await onIcon();
              } finally {
                setIconBusy(false);
              }
            }}
          >
            <RefreshCw size={14} className={iconBusy ? "spin" : ""} />
            {iconBusy ? "获取中…" : "更新网站图标"}
          </button>
        )}
        <div className="modal-actions">
          {onDelete && (
            <button
              type="button"
              className="danger-text"
              disabled={busy}
              onClick={onDelete}
            >
              <Trash2 size={15} />
              删除
            </button>
          )}
          <span />
          <button type="button" className="secondary" onClick={onClose}>
            取消
          </button>
          <button className="primary" disabled={busy}>
            {busy ? "保存中…" : "保存"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
function ImportDialog({
  onClose,
  onDone,
  message,
}: {
  onClose: () => void;
  onDone: () => Promise<void>;
  message: (e: unknown) => void;
}) {
  const [file, setFile] = useState<File | null>(null);
  const [plan, setPlan] = useState<Plan | null>(null);
  const [busy, setBusy] = useState(false);
  async function preview(f: File) {
    setFile(f);
    setPlan(null);
    setBusy(true);
    try {
      const form = new FormData();
      form.set("file", f);
      setPlan(await api<Plan>("/import/preview", "POST", form));
    } catch (e) {
      message(e);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="导入书签" onClose={busy ? () => {} : onClose}>
      <label className="file-drop">
        <Upload size={25} />
        <strong>{file?.name || "选择书签 HTML 文件"}</strong>
        <span>Edge / Chrome · 最大 16 MB</span>
        <input
          type="file"
          aria-label="选择书签文件"
          accept=".html,.htm"
          disabled={busy}
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) void preview(f);
          }}
        />
      </label>
      {busy && (
        <p className="inline-status">
          <LoaderCircle size={16} className="spin" />
          处理中…
        </p>
      )}
      {plan && (
        <>
          <div className="import-stats">
            <div>
              <strong>{plan.added}</strong>
              <span>新增</span>
            </div>
            <div>
              <strong>{plan.duplicates}</strong>
              <span>重复跳过</span>
            </div>
            <div>
              <strong>{plan.folders}</strong>
              <span>分类路径</span>
            </div>
          </div>
          {plan.skipped > 0 && (
            <p className="muted">跳过 {plan.skipped} 条无效或不支持的链接</p>
          )}
          <div className="import-paths">
            {plan.paths.slice(0, 8).map((p) => (
              <div key={p}>
                <Folder size={13} />
                {p}
              </div>
            ))}
            {plan.paths.length > 8 && (
              <div className="muted">还有 {plan.paths.length - 8} 个分类</div>
            )}
          </div>
          <p className="muted small-note">
            同一分类内重复网址跳过，已有书签保持不变。
          </p>
        </>
      )}
      <div className="modal-actions">
        <span />
        <button className="secondary" disabled={busy} onClick={onClose}>
          取消
        </button>
        <button
          className="primary"
          disabled={!plan || busy}
          onClick={async () => {
            if (!file) return;
            setBusy(true);
            try {
              const f = new FormData();
              f.set("file", file);
              const result = await api<Plan>("/import/apply", "POST", f);
              await onDone();
              message(
                `新增 ${result.added} 条，重复跳过 ${result.duplicates} 条，无效或不支持 ${result.skipped} 条`,
              );
            } catch (e) {
              message(e);
            } finally {
              setBusy(false);
            }
          }}
        >
          确认导入
        </button>
      </div>
    </Modal>
  );
}
function SettingsDialog({
  onClose,
  message,
  onPassword,
}: {
  onClose: () => void;
  message: (e: unknown) => void;
  onPassword: () => void;
}) {
  const [state, setState] = useState<BackupState | null>(null);
  const [busy, setBusy] = useState(false);
  async function refresh() {
    setState(await api<BackupState>("/backups"));
  }
  useEffect(() => {
    refresh().catch(message);
  }, []);
  return (
    <Modal title="设置与备份" onClose={busy ? () => {} : onClose}>
      {!state ? (
        <p className="inline-status">正在加载…</p>
      ) : (
        <>
          <div className="settings-section-title">
            <h3>本地备份</h3>
            <button
              className="secondary"
              disabled={busy}
              onClick={async () => {
                setBusy(true);
                try {
                  await api("/backups", "POST");
                  await refresh();
                  message("备份已生成");
                } catch (e) {
                  message(e);
                } finally {
                  setBusy(false);
                }
              }}
            >
              {busy ? (
                <LoaderCircle size={14} className="spin" />
              ) : (
                <Download size={14} />
              )}
              立即备份
            </button>
          </div>
          <div className="backup-status">
            <ShieldCheck size={18} />
            <span>
              {state.last
                ? state.last.success
                  ? "最近备份成功"
                  : "最近备份失败"
                : "暂无备份"}
              <small>
                {state.last ? new Date(state.last.time).toLocaleString() : ""}
              </small>
            </span>
          </div>
          {state.last?.error && (
            <p className="error-text">{state.last.error}</p>
          )}
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              try {
                await api("/backups/settings", "PUT", state.config);
                await refresh();
                message("设置已保存");
              } catch (e) {
                message(e);
              } finally {
                setBusy(false);
              }
            }}
          >
            <label className="switch-row">
              <span>每天自动备份</span>
              <input
                type="checkbox"
                checked={state.config.enabled}
                onChange={(e) =>
                  setState({
                    ...state,
                    config: { ...state.config, enabled: e.target.checked },
                  })
                }
              />
            </label>
            <div className="form-columns">
              <label>
                执行时间
                <input
                  type="time"
                  value={state.config.time}
                  onChange={(e) =>
                    setState({
                      ...state,
                      config: { ...state.config, time: e.target.value },
                    })
                  }
                  required
                />
              </label>
              <label>
                保留份数
                <input
                  type="number"
                  min={2}
                  max={365}
                  value={state.config.keep}
                  onChange={(e) =>
                    setState({
                      ...state,
                      config: { ...state.config, keep: Number(e.target.value) },
                    })
                  }
                  required
                />
              </label>
            </div>
            <label>
              时区
              <select
                value={state.config.timezone}
                onChange={(e) =>
                  setState({
                    ...state,
                    config: { ...state.config, timezone: e.target.value },
                  })
                }
              >
                {[
                  ...new Set([
                    "Asia/Shanghai",
                    "UTC",
                    Intl.DateTimeFormat().resolvedOptions().timeZone,
                    state.config.timezone,
                  ]),
                ].map((z) => (
                  <option key={z}>{z}</option>
                ))}
              </select>
            </label>
            <div className="align-right">
              <button className="primary" disabled={busy}>
                保存设置
              </button>
            </div>
          </form>
          <h3 className="history-title">
            历史备份 <span>{state.files.length}</span>
          </h3>
          <div className="backup-list">
            {state.files.length === 0 ? (
              <p className="muted">暂无记录</p>
            ) : (
              state.files.map((f) => (
                <a
                  href={"/api/backups/" + encodeURIComponent(f.name)}
                  key={f.name}
                >
                  <span>
                    {new Date(f.time).toLocaleString()}
                    <small>{Math.max(1, Math.round(f.size / 1024))} KB</small>
                  </span>
                  <Download size={15} />
                </a>
              ))
            )}
          </div>
          <div className="settings-account">
            <span>账号</span>
            <button className="text-button" onClick={onPassword}>
              修改密码 <ChevronRight size={14} />
            </button>
          </div>
        </>
      )}
    </Modal>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
