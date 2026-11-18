import { test, expect } from "@playwright/test";

const categories = [
  { id: "tools", parent_id: "", name: "工具", sort_order: 0 },
  { id: "child", parent_id: "tools", name: "子分类", sort_order: 0 },
  { id: "other", parent_id: "", name: "其他", sort_order: 1 },
];
const bookmarks = [
  {
    id: "google",
    category_id: "tools",
    name: "搜索引擎",
    url: "https://www.google.com/",
  },
  {
    id: "github",
    category_id: "child",
    name: "GitHub",
    url: "https://github.com/",
  },
  {
    id: "gitlab",
    category_id: "other",
    name: "GitLab",
    url: "https://gitlab.com/",
  },
].map((bookmark, sort_order) => ({
  ...bookmark,
  sort_order,
  note: "",
  icon: "",
}));

test.beforeEach(async ({ page }) => {
  await page.route("**/api/auth/status", (route) =>
    route.fulfill({
      json: { initialized: true, authenticated: true },
    }),
  );
  await page.route("**/api/data", (route) =>
    route.fulfill({ json: { categories, bookmarks } }),
  );
});

test("prefix, category scope, navigation, timeout and opening the selected link", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .locator(".category-label")
    .filter({ hasText: /^工具$/ })
    .click();
  const status = page.locator(".locate-status");
  const active = page.locator(".located .bookmark-link");
  await page.keyboard.type("G");
  await expect(status).toContainText("2 个匹配");
  await expect(active).toHaveText("搜索引擎");
  await page.keyboard.press("ArrowUp");
  await expect(active).toHaveText("GitHub");
  await page.keyboard.press("ArrowDown");
  await expect(active).toHaveText("搜索引擎");
  await page.keyboard.type("i");
  await expect(active).toHaveText("GitHub");
  await page.keyboard.press("Backspace");
  await expect(status).toContainText("2 个匹配");
  await page.waitForTimeout(850);
  await page.keyboard.type("gi");
  await expect(status.locator("strong")).toHaveText("gi");
  await expect(active).toHaveText("GitHub");
  // Inspect the real anchor click without depending on an external website.
  await page.evaluate(() => {
    document.querySelectorAll(".bookmark-link").forEach((link) => {
      link.addEventListener("click", (event) => {
        event.preventDefault();
        document.body.dataset.opened = (
          event.currentTarget as HTMLAnchorElement
        ).href;
      });
    });
  });
  await page.keyboard.press("Enter");
  await expect(page.locator("body")).toHaveAttribute(
    "data-opened",
    "https://github.com/",
  );
  await page.keyboard.press("Escape");
  await expect(status).toHaveCount(0);
  await page.keyboard.type("zzz");
  await expect(status).toContainText("无匹配");
  await page.keyboard.press("Enter");
  await expect(page.locator("body")).toHaveAttribute(
    "data-opened",
    "https://github.com/",
  );
  await page
    .locator(".category-label")
    .filter({ hasText: /^其他$/ })
    .click();
  await expect(status).toHaveCount(0);
  await page.keyboard.type("g");
  await expect(active).toHaveText("GitLab");
});

test("inputs, composition, modifiers, editing and dialogs do not trigger navigation", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.locator(".bookmark-link")).toHaveCount(3);
  const status = page.locator(".locate-status");
  await page.keyboard.press("Control+g");
  await page.keyboard.press("Alt+g");
  await page.evaluate(() => {
    window.dispatchEvent(new CompositionEvent("compositionstart"));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "g" }));
    window.dispatchEvent(new CompositionEvent("compositionend"));
    window.dispatchEvent(
      new KeyboardEvent("keydown", { key: "g", isComposing: true }),
    );
    window.dispatchEvent(
      new KeyboardEvent("keydown", { key: "g", keyCode: 229 }),
    );
  });
  await expect(status).toHaveCount(0);
  await page.keyboard.type("g");
  await expect(status).toBeVisible();
  await page.getByLabel("搜索书签").focus();
  await expect(status).toHaveCount(0);
  await page.keyboard.type("git");
  await expect(page.getByLabel("搜索书签")).toHaveValue("git");
  await expect(status).toHaveCount(0);
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await page.keyboard.type("g");
  await expect(status).toHaveCount(0);
  await page.getByRole("button", { name: "完成编辑" }).click();
  await page.keyboard.type("g");
  await expect(status).toBeVisible();
  await page.getByRole("button", { name: "添加书签", exact: true }).click();
  await page.getByRole("textbox", { name: "名称", exact: true }).fill("Google");
  await expect(status).toHaveCount(0);
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(status).toHaveCount(0);
});

test("locates and reveals bookmarks beyond the first render batch", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.route("**/api/data", (route) =>
    route.fulfill({
      json: {
        categories: [categories[0]],
        bookmarks: Array.from({ length: 240 }, (_, i) => ({
          id: `b${i}`,
          category_id: "tools",
          name: i === 239 ? "Zebra" : `书签 ${i}`,
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
  await page.keyboard.type("z");
  await expect(page.locator(".located .bookmark-link")).toHaveText("Zebra");
  await expect(page.locator(".located")).toBeInViewport();
  await expect(page.locator(".bookmark-link")).toHaveCount(240);
});
