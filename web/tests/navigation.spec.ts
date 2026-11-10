import { test, expect, request } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
let state: Awaited<
  ReturnType<Awaited<ReturnType<typeof request.newContext>>["storageState"]>
>;
const names = [
  "GitHub",
  "Go 文档",
  "React",
  "TypeScript",
  "MDN",
  "SQLite",
  "Caddy",
  "Docker",
  "Vite",
  "Linux",
  "Figma",
  "CodePen",
  "Can I Use",
  "npm",
  "Stack Overflow",
  "CSS Tricks",
  "Excalidraw",
  "开发工具",
];
const fixture =
  "<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><p><DT><H3>开发与设计</H3><DL><p>" +
  names
    .map((name, i) => `<DT><A HREF="https://example.com/${i}">${name}</A>`)
    .join("") +
  '<DT><H3>参考资料</H3><DL><p><DT><A HREF="https://example.org/guide">指南</A></DL></DL><DT><H3>日常</H3><DL><p><DT><A HREF="https://example.net/">阅读</A></DL></DL>';
test.describe.serial("navigation workflow", () => {
  test.beforeAll(async () => {
    const api = await request.newContext({
      baseURL: "http://127.0.0.1:31516",
      extraHTTPHeaders: { "X-Nav-Request": "1" },
    });
    const token = readFileSync(
      join(process.env.NAV_E2E_DATA_DIR!, "setup-token"),
      "utf8",
    ).trim();
    const res = await api.post("/api/auth/setup", {
      data: { username: "admin", password: "test-password-123", token },
    });
    expect(res.ok()).toBeTruthy();
    state = await api.storageState();
    await api.dispose();
  });
  test.beforeEach(async ({ context }) => {
    await context.addCookies(state.cookies);
  });
  test("import, search, edit, sort and backup", async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto("/");
    await expect(page.getByRole("button", { name: "添加书签" })).toBeVisible();
    await expect(page.locator(".page-heading")).toHaveCount(0);
    await page
      .getByRole("button", { name: "导入书签", exact: true })
      .first()
      .click();
    await page.getByLabel("选择书签文件").setInputFiles({
      name: "favorites.html",
      mimeType: "text/html",
      buffer: Buffer.from(fixture),
    });
    await expect(page.getByRole("button", { name: "确认导入" })).toBeEnabled();
    await page.getByRole("button", { name: "确认导入" }).click();
    await expect(page.locator(".bookmark-link")).toHaveCount(20);
    await page
      .locator(".category-label")
      .filter({ hasText: "开发与设计" })
      .click();
    await page.getByRole("textbox", { name: "搜索书签" }).fill("阅读");
    await expect(page.locator(".bookmark-link")).toHaveCount(1);
    await expect(page.locator(".bookmark-link")).toContainText("阅读");
    await page.getByRole("button", { name: "清空搜索" }).click();
    await page.getByRole("button", { name: "全部书签" }).click();
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    await page.getByRole("link", { name: "GitHub", exact: true }).click();
    await page
      .getByRole("textbox", { name: "名称", exact: true })
      .fill("GitHub 收藏");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(
      page.getByRole("link", { name: "GitHub 收藏", exact: true }),
    ).toBeVisible();
    const first = page.locator(".bookmark-section").first();
    await first
      .getByRole("button", { name: "后移 GitHub 收藏", exact: true })
      .click();
    await expect(first.locator(".bookmark-link").first()).toContainText(
      "Go 文档",
    );
    const handle = first.getByRole("button", { name: "拖动排序" }).nth(1);
    await handle.focus();
    await page.keyboard.press("Space");
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("Space");
    await expect(first.locator(".bookmark-link").nth(2)).toContainText(
      "GitHub 收藏",
    );
    await page.getByRole("button", { name: "完成编辑" }).click();
    await page.reload();
    await expect(first.locator(".bookmark-link").nth(2)).toContainText(
      "GitHub 收藏",
    );
    await page.screenshot({
      path: "test-results/navigation-desktop.png",
      fullPage: true,
    });
    await page.getByRole("button", { name: "设置与备份" }).click();
    await page.getByRole("button", { name: "立即备份" }).click();
    await expect(page.locator(".backup-list a")).toHaveCount(1);
    await expect(page.getByText("最近备份成功")).toBeVisible();
    await page.getByRole("button", { name: "关闭", exact: true }).click();
    await page
      .getByRole("button", { name: "导入书签", exact: true })
      .first()
      .click();
    await page.getByLabel("选择书签文件").setInputFiles({
      name: "favorites.html",
      mimeType: "text/html",
      buffer: Buffer.from(fixture),
    });
    await expect(
      page.locator(".import-stats>div").nth(0).locator("strong"),
    ).toHaveText("0");
    await page.getByRole("button", { name: "取消", exact: true }).click();
    expect(errors).toEqual([]);
  });
  test("drag bookmarks across folders and into an empty folder", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 1400 });
    await page.goto("/");
    const emptyResponse = await page.request.post("/api/categories", {
      headers: { "X-Nav-Request": "1" },
      data: { name: "空目标", parent_id: "" },
    });
    expect(emptyResponse.ok()).toBeTruthy();
    const initial = await (await page.request.get("/api/data")).json();
    const dailyCategory = initial.categories.find(
      (c: { name: string }) => c.name === "日常",
    );
    const extraResponse = await page.request.post("/api/bookmarks", {
      headers: { "X-Nav-Request": "1" },
      data: {
        category_id: dailyCategory.id,
        name: "目标后项",
        url: "https://example.net/after",
      },
    });
    expect(extraResponse.ok()).toBeTruthy();
    const extra = await extraResponse.json();
    await page.reload();
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const source = page
      .locator(".bookmark-link")
      .filter({ hasText: "GitHub 收藏" });
    const daily = page.locator(".bookmark-section").filter({
      has: page.getByRole("heading", { name: /^日常/ }),
    });
    async function dragTo(target: ReturnType<typeof page.locator>) {
      const handle = source
        .locator("..")
        .locator("..")
        .getByRole("button", { name: "拖动排序" });
      const from = await handle.boundingBox();
      const to = await target.boundingBox();
      expect(from).toBeTruthy();
      expect(to).toBeTruthy();
      await page.mouse.move(
        from!.x + from!.width / 2,
        from!.y + from!.height / 2,
      );
      await page.mouse.down();
      await page.mouse.move(from!.x + 10, from!.y + 10, { steps: 3 });
      await page.mouse.move(to!.x + to!.width * 0.75, to!.y + to!.height / 2, {
        steps: 20,
      });
      await page.mouse.up();
    }
    await dragTo(daily.locator(".bookmark-tile").first());
    await expect(daily.locator(".bookmark-link")).toHaveText([
      "阅读",
      "GitHub 收藏",
      "目标后项",
    ]);
    await page.reload();
    await expect(daily.locator(".bookmark-link")).toHaveText([
      "阅读",
      "GitHub 收藏",
      "目标后项",
    ]);
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    await dragTo(page.getByLabel("移入 空目标"));
    const emptySection = page.locator(".bookmark-section").filter({
      has: page.getByRole("heading", { name: /^空目标/ }),
    });
    await expect(emptySection.locator(".bookmark-link")).toContainText(
      "GitHub 收藏",
    );
    // Restore the fixture for the following mobile workflow.
    const data = await (await page.request.get("/api/data")).json();
    const bookmark = data.bookmarks.find(
      (b: { name: string }) => b.name === "GitHub 收藏",
    );
    const category = data.categories.find(
      (c: { name: string }) => c.name === "开发与设计",
    );
    const restored = await page.request.post(
      `/api/bookmarks/${bookmark.id}/move`,
      {
        headers: { "X-Nav-Request": "1" },
        data: { category_id: category.id, before_id: "" },
      },
    );
    expect(restored.ok()).toBeTruthy();
    const removed = await page.request.delete(`/api/bookmarks/${extra.id}`, {
      headers: { "X-Nav-Request": "1" },
    });
    expect(removed.ok()).toBeTruthy();
  });
  test("mobile navigation and no horizontal overflow", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/");
    await expect(page.locator(".bookmark-link")).toHaveCount(20);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBeTruthy();
    await page.getByRole("button", { name: "打开分类" }).click();
    await expect(page.locator(".sidebar")).toHaveClass(/open/);
    await page.locator(".category-label").filter({ hasText: "日常" }).click();
    await expect(page.locator(".bookmark-link")).toHaveCount(1);
    await page.screenshot({
      path: "test-results/navigation-mobile.png",
      fullPage: true,
    });
    await page.getByRole("button", { name: "打开分类" }).click();
    await page.getByRole("button", { name: "退出登录" }).click();
    await expect(page.getByRole("heading", { name: "登录" })).toBeVisible();
  });
});

test("large collections render in batches and search includes unloaded bookmarks", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.route("**/api/auth/status", (route) =>
    route.fulfill({
      json: { initialized: true, authenticated: true },
    }),
  );
  await page.route("**/api/data", (route) =>
    route.fulfill({
      json: {
        categories: [
          { id: "parent", parent_id: "", name: "上级分类", sort_order: 0 },
          { id: "child", parent_id: "parent", name: "大量书签", sort_order: 0 },
        ],
        bookmarks: Array.from({ length: 240 }, (_, i) => ({
          id: `bookmark-${i}`,
          category_id: "child",
          name: `书签 ${i}`,
          url: `https://example.com/${i}`,
          note: "",
          icon: "",
          sort_order: i,
        })),
      },
    }),
  );
  await page.goto("/");
  await expect(page.locator(".bookmark-link")).toHaveCount(80);
  await page.getByRole("button", { name: "打开分类" }).click();
  await expect(
    page
      .locator(".category-row")
      .filter({ hasText: "上级分类" })
      .locator(".count"),
  ).toHaveText("240");
  await expect(
    page
      .locator(".category-row")
      .filter({ hasText: "大量书签" })
      .locator(".count"),
  ).toHaveText("240");
  await page.getByRole("button", { name: "关闭分类" }).click();
  await page.getByRole("textbox", { name: "搜索书签" }).fill("书签 239");
  await expect(page.locator(".bookmark-link")).toHaveCount(1);
  await expect(page.locator(".bookmark-link")).toContainText("书签 239");
  await page.getByRole("button", { name: "清空搜索" }).click();
  await expect(page.locator(".bookmark-link")).toHaveCount(80);
  await page.locator(".load-more").scrollIntoViewIfNeeded();
  await expect(page.locator(".bookmark-link")).toHaveCount(160);
  await page.locator(".load-more").scrollIntoViewIfNeeded();
  await expect(page.locator(".bookmark-link")).toHaveCount(240);
  await expect(page.locator(".load-more")).toHaveCount(0);
  await page.getByRole("textbox", { name: "搜索书签" }).fill("书签 239");
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "搜索书签" })).toHaveValue("");
  await expect(page.locator(".bookmark-link")).toHaveCount(240);
  await expect(page.locator(".bookmark-grid .drag-handle")).toHaveCount(240);
  await expect(
    page.locator(".bookmark-grid .drag-handle").first(),
  ).toBeEnabled();
  await page.getByRole("textbox", { name: "搜索书签" }).fill("书签 239");
  await expect(page.locator(".bookmark-grid .drag-handle")).toHaveCount(1);
  await expect(page.locator(".bookmark-grid .drag-handle")).toBeDisabled();
  await page.getByRole("button", { name: "清空搜索" }).click();
  await expect(
    page.locator(".bookmark-grid .drag-handle").first(),
  ).toBeEnabled();
});
