import type { AccessLevel, AccessTeamRole } from "@scaffoldapp/shared";

/** Grant levels as people read them. */
export const LEVEL_LABEL: Record<AccessLevel, string> = { view: "Can view", comment: "Can comment", edit: "Can edit", manage: "Can manage" };
/** Short forms for segmented controls. */
export const LEVEL_SHORT: Record<AccessLevel, string> = { view: "View", comment: "Comment", edit: "Edit", manage: "Manage" };

export const TEAM_ROLE_LABEL: Record<AccessTeamRole, string> = { lead: "Lead", member: "Member" };

/** "1 member", "12 members". */
export function memberCount(n: number | null | undefined): string {
  const v = n ?? 0;
  return `${v} ${v === 1 ? "member" : "members"}`;
}
