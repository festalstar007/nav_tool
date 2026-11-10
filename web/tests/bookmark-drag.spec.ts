import { test, expect } from "@playwright/test";
import type { CollisionDetection } from "@dnd-kit/core";
import { bookmarkCollision, insertAfter } from "../src/bookmark-drag";

const rect = (left: number, top: number, width = 160, height = 47) => ({
  left,
  top,
  right: left + width,
  bottom: top + height,
  width,
  height,
});
function collisions(pointer: { x: number; y: number } | null, empty = false) {
  // Reproduce the source overlapping the destination during a cross-group drag.
  const rectangles = new Map([
    ["source", rect(0, 100)],
    ["section", rect(0, 80, 400, 140)],
    ["target", rect(0, 100)],
    ["next", rect(180, 100)],
  ]);
  const args = {
    active: { id: "source" },
    pointerCoordinates: pointer,
    collisionRect: rect(pointer?.x ?? 0, pointer?.y ?? 100),
    droppableRects: rectangles,
    droppableContainers: [...rectangles.keys()]
      .filter((id) => !empty || id === "section" || id === "source")
      .map((id) => ({
        id,
        data: { current: id === "section" ? { section: true, empty } : {} },
      })),
  } as unknown as Parameters<CollisionDetection>[0];
  return bookmarkCollision(args).map((hit) => hit.id);
}

test("destination card wins over the dragged source and enclosing folder", () => {
  expect(collisions({ x: 80, y: 120 })).toEqual(["target"]);
  expect(collisions({ x: 240, y: 120 })).toEqual(["next"]);
});
test("folder gaps and empty folders remain drop targets", () => {
  expect(collisions({ x: 170, y: 180 })).toEqual(["section"]);
  expect(collisions({ x: 80, y: 120 }, true)).toEqual(["section"]);
});
test("keyboard sorting ignores nonempty folder wrappers", () => {
  expect(collisions(null)[0]).toBe("target");
  expect(collisions(null, true)).toEqual(["section"]);
});
test("insertion marker and saved position agree across columns and rows", () => {
  const target = rect(180, 100);
  expect(insertAfter(rect(170, 100), target)).toBe(false);
  expect(insertAfter(rect(190, 100), target)).toBe(true);
  expect(insertAfter(rect(180, 45), target)).toBe(false);
  expect(insertAfter(rect(180, 155), target)).toBe(true);
});
