import { test, expect } from "@playwright/test";

test("batch selection, cancellation, failure and pruning refresh", async ({
  page,
}) => {
  const category = {
    id: "folder",
    parent_id: "",
    name: "待清理",
    sort_order: 0,
  };
  const initial = {
    categories: [category],
    bookmarks: Array.from({ length: 100 }, (_, i) => ({
      id: `b${i}`,
      category_id: category.id,
      name: `测试书签${i}`,
      url: `https://example.com/${i}`,
      note: "",
      icon: "",
      sort_order: i,
    })),
  };
  let current = initial;
  let fail = true;
  let deletes = 0;
  await page.route("**/api/auth/status", (route) =>
    route.fulfill({ json: { initialized: true, authenticated: true } }),
  );
  await page.route("**/api/data", (route) => route.fulfill({ json: current }));
  await page.route("**/api/bookmarks/batch-delete", (route) => {
    deletes++;
    const ids = route.request().postDataJSON().ids;
    expect(ids).toHaveLength(100);
    expect(new Set(ids).size).toBe(100);
    if (fail)
      return route.fulfill({
        status: 400,
        json: { error: "删除失败，请重试" },
      });
    current = { categories: [], bookmarks: [] };
    return route.fulfill({ json: { ok: true } });
  });
  await page.goto("/");
  await page.locator(".category-label").click();
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await page.getByRole("button", { name: "批量选择", exact: true }).click();
  await expect(page.getByRole("button", { name: "拖动排序" })).toHaveCount(0);
  await page.getByLabel("选择书签 测试书签0", { exact: true }).check();
  await page.getByLabel("搜索书签").fill("测试书签9");
  await expect(page.locator(".batch-toolbar")).toContainText("已选 0 项");
  await page.getByRole("button", { name: "全选当前结果" }).click();
  await expect(page.locator(".batch-toolbar")).toContainText("已选 11 项");
  await page.getByLabel("搜索书签").fill("");
  await page.getByRole("button", { name: "全选当前结果" }).click();
  await expect(page.locator(".batch-toolbar")).toContainText("已选 100 项");
  await page.getByRole("button", { name: "删除所选", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("上级分类也会删除");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  expect(deletes).toBe(0);
  await page.getByRole("button", { name: "删除所选", exact: true }).click();
  await page.getByRole("button", { name: "确认删除", exact: true }).click();
  await expect(page.locator(".toast")).toContainText("删除失败");
  await expect(page.locator(".batch-toolbar")).toContainText("已选 100 项");
  fail = false;
  await page.getByRole("button", { name: "确认删除", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".category-label")).toHaveCount(0);
  await expect(page.locator(".bookmark-link")).toHaveCount(0);
  await expect(page.locator(".page-heading")).toHaveCount(0);
  await expect(page.locator(".batch-toolbar")).toContainText("已选 0 项");
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
});
