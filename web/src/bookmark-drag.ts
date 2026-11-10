import {
  closestCenter,
  pointerWithin,
  rectIntersection,
  type CollisionDetection,
} from "@dnd-kit/core";

type Rect = { left: number; top: number; width: number; height: number };
export function insertAfter(drag: Rect, target: Rect): boolean {
  const sameRow =
    Math.abs(drag.top + drag.height / 2 - target.top - target.height / 2) <
    target.height / 2;
  return sameRow
    ? drag.left + drag.width / 2 >= target.left + target.width / 2
    : drag.top + drag.height / 2 >= target.top + target.height / 2;
}

export const bookmarkCollision: CollisionDetection = (args) => {
  // The moving source must not intercept a hit on the destination bookmark.
  const droppableContainers = args.droppableContainers.filter(
    (container) => container.id !== args.active.id,
  );
  const candidates = { ...args, droppableContainers };
  const hits = args.pointerCoordinates
    ? pointerWithin(candidates)
    : closestCenter({
        ...candidates,
        droppableContainers: droppableContainers.filter(
          (container) =>
            !container.data.current?.section || container.data.current?.empty,
        ),
      });
  // A section encloses its cards; an individual card gives an exact position.
  const itemHits = hits.filter(
    (hit) =>
      !droppableContainers.find((container) => container.id === hit.id)?.data
        .current?.section,
  );
  if (itemHits.length) return itemHits;
  if (hits.length) return hits;
  return args.pointerCoordinates ? rectIntersection(candidates) : [];
};
