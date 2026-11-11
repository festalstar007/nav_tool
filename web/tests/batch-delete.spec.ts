import { test, expect } from "@playwright/test";

test("batch selection, cancellation, failure and pruning without refetch", async ({
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
  let reads = 0;
  let fail = true;
  let deletes = 0;
  await page.route("**/api/auth/status", (route) =>
    route.fulfill({ json: { initialized: true, authenticated: true } }),
  );
  await page.route("**/api/data", (route) => {
    reads++;
    return route.fulfill({ json: initial });
  });
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
    return route.fulfill({
      json: { bookmark_ids: ids, category_ids: [category.id] },
    });
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
  await expect(page.getByRole("dialog")).toContainText("是否确认删除？");
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
  expect(reads).toBe(1);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
});

for (const mode of ["single", "batch"] as const) {
  for (const scenario of [
    "retain",
    "next",
    "previous",
    "parent",
    "ancestor",
    "all",
  ] as const) {
    test(`${mode} delete selects ${scenario} without refetch`, async ({
      page,
    }) => {
      const categories = [
        { id: "root", parent_id: "", name: "上级", sort_order: 0 },
        { id: "target", parent_id: "root", name: "当前", sort_order: 1 },
        { id: "before", parent_id: "root", name: "前一个", sort_order: 0 },
        { id: "after", parent_id: "root", name: "后一个", sort_order: 2 },
        { id: "other", parent_id: "", name: "其他", sort_order: 1 },
      ].filter((c) => {
        if (c.id === "before")
          return scenario === "next" || scenario === "previous";
        if (c.id === "after") return scenario === "next";
        if (c.id === "other") return scenario === "ancestor";
        return true;
      });
      const bookmark = (id: string, category_id: string) => ({
        id,
        category_id,
        name: id,
        url: `https://example.com/${id}`,
        note: "",
        icon: "",
        sort_order: 0,
      });
      const bookmarks = [bookmark("待删", "target")];
      if (scenario === "retain") bookmarks.push(bookmark("保留", "target"));
      if (scenario === "parent") bookmarks.push(bookmark("父级书签", "root"));
      for (const c of categories.filter((c) =>
        ["before", "after", "other"].includes(c.id),
      )) {
        bookmarks.push(bookmark(c.name, c.id));
      }
      const removed =
        scenario === "retain"
          ? []
          : scenario === "ancestor" || scenario === "all"
            ? ["target", "root"]
            : ["target"];
      let reads = 0;
      await page.route("**/api/auth/status", (route) =>
        route.fulfill({
          json: { initialized: true, authenticated: true },
        }),
      );
      await page.route("**/api/data", (route) => {
        reads++;
        return route.fulfill({ json: { categories, bookmarks } });
      });
      let deletes = 0;
      await page.route(
        mode === "single"
          ? "**/api/bookmarks/*"
          : "**/api/bookmarks/batch-delete",
        (route) => {
          deletes++;
          expect(route.request().method()).toBe(
            mode === "single" ? "DELETE" : "POST",
          );
          return route.fulfill({
            json: { bookmark_ids: ["待删"], category_ids: removed },
          });
        },
      );
      await page.goto("/");
      await page.locator(".category-label").filter({ hasText: "当前" }).click();
      await page.getByRole("button", { name: "编辑", exact: true }).click();
      if (mode === "single") {
        await page.getByRole("link", { name: "待删", exact: true }).click();
        page.once("dialog", async (dialog) => {
          expect(dialog.message()).toBe("是否确认删除？");
          await dialog.dismiss();
        });
        await page.getByRole("button", { name: "删除", exact: true }).click();
        expect(deletes).toBe(0);
        await expect(page.getByRole("dialog")).toHaveCount(1);
        page.once("dialog", async (dialog) => {
          expect(dialog.message()).toBe("是否确认删除？");
          await dialog.accept();
        });
        await page.getByRole("button", { name: "删除", exact: true }).click();
      } else {
        await page
          .getByRole("button", { name: "批量选择", exact: true })
          .click();
        await page.getByLabel("选择书签 待删", { exact: true }).check();
        await page
          .getByRole("button", { name: "删除所选", exact: true })
          .click();
        await page
          .getByRole("button", { name: "确认删除", exact: true })
          .click();
      }
      await expect(page.getByRole("dialog")).toHaveCount(0);
      await expect(
        page.getByRole("link", { name: "待删", exact: true }),
      ).toHaveCount(0);
      const expected = {
        retain: "当前",
        next: "后一个",
        previous: "前一个",
        parent: "上级",
        ancestor: "其他",
        all: "",
      }[scenario];
      if (expected) {
        await expect(
          page.locator(".category-row.active .category-label"),
        ).toHaveText(expected);
        await expect(page.locator(".bookmark-link")).toHaveCount(1);
      } else {
        await expect(page.locator(".category-row.active")).toHaveCount(0);
        await expect(page.locator(".bookmark-link")).toHaveCount(0);
      }
      expect(deletes).toBe(1);
      expect(reads).toBe(1);
    });
  }
}
