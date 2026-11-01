import type { HomeTile } from "./registry";

/**
 * Badge counts for registry nav items. Registry lists are static module constants, so
 * this calls the same hooks in the same order on every render (rules of hooks hold).
 */
export function useNavBadges(items: readonly HomeTile[]): (number | undefined)[] {
  return items.map((item) => item.useBadge?.());
}
