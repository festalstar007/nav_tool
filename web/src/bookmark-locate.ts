import { useEffect, useRef, useState } from "react";

type Bookmark = { id: string; name: string; url: string };

function matches(bookmark: Bookmark, prefix: string) {
  if (bookmark.name.toLocaleLowerCase().startsWith(prefix)) return true;
  try {
    return new URL(bookmark.url).hostname
      .replace(/^www\./i, "")
      .toLocaleLowerCase()
      .startsWith(prefix);
  } catch {
    return false;
  }
}

function editing(target: EventTarget | null) {
  return (
    target instanceof HTMLElement &&
    (target.isContentEditable ||
      !!target.closest("input, textarea, select, [role='textbox']"))
  );
}

export function useBookmarkLocate(
  bookmarks: Bookmark[],
  enabled: boolean,
  scope: string,
  open: (id: string) => void,
) {
  const [prefix, setPrefix] = useState("");
  const [index, setIndex] = useState(0);
  const lastTyped = useRef(0);
  const composing = useRef(false);
  const results = enabled
    ? bookmarks.filter((bookmark) => prefix && matches(bookmark, prefix))
    : [];
  const activeId = results[index % (results.length || 1)]?.id;

  useEffect(() => {
    setPrefix("");
    setIndex(0);
    lastTyped.current = 0;
  }, [enabled, scope]);

  useEffect(() => {
    function clear() {
      setPrefix("");
      setIndex(0);
      lastTyped.current = 0;
    }
    function startComposition() {
      composing.current = true;
    }
    function endComposition() {
      composing.current = false;
    }
    function blur() {
      composing.current = false;
      clear();
    }
    function focus(event: FocusEvent) {
      if (editing(event.target)) clear();
    }
    function key(event: KeyboardEvent) {
      if (
        !enabled ||
        event.defaultPrevented ||
        event.isComposing ||
        composing.current ||
        event.keyCode === 229 ||
        event.ctrlKey ||
        event.metaKey ||
        event.altKey ||
        editing(event.target)
      )
        return;
      if (event.key === "Escape") {
        if (prefix) event.preventDefault();
        clear();
      } else if (prefix && event.key === "Enter") {
        event.preventDefault();
        if (!event.repeat && activeId) open(activeId);
      } else if (
        prefix &&
        (event.key === "ArrowDown" || event.key === "ArrowUp")
      ) {
        event.preventDefault();
        const count = results.length;
        if (count)
          setIndex(
            (index) =>
              (index + (event.key === "ArrowDown" ? 1 : -1) + count) % count,
          );
      } else if (prefix && event.key === "Backspace") {
        event.preventDefault();
        setPrefix((value) => value.slice(0, -1));
        setIndex(0);
        lastTyped.current = performance.now();
      } else if (event.key.length === 1 && (prefix || /\S/u.test(event.key))) {
        event.preventDefault();
        if (event.repeat) return;
        const now = performance.now();
        const reset = now - lastTyped.current > 800;
        setPrefix(
          (value) => (reset ? "" : value) + event.key.toLocaleLowerCase(),
        );
        lastTyped.current = now;
        setIndex(0);
      } else if (event.key === "Tab") {
        clear();
      }
    }
    window.addEventListener("keydown", key);
    window.addEventListener("compositionstart", startComposition);
    window.addEventListener("compositionend", endComposition);
    window.addEventListener("focusin", focus);
    window.addEventListener("pointerdown", clear);
    window.addEventListener("blur", blur);
    return () => {
      window.removeEventListener("keydown", key);
      window.removeEventListener("compositionstart", startComposition);
      window.removeEventListener("compositionend", endComposition);
      window.removeEventListener("focusin", focus);
      window.removeEventListener("pointerdown", clear);
      window.removeEventListener("blur", blur);
    };
  }, [enabled, bookmarks, prefix, results.length, activeId, open]);

  return { prefix: enabled ? prefix : "", count: results.length, activeId };
}
