import type { AccessLevel, AccessOrgRole, AccessTeamRole } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-web";

/** Grant levels as people read them. */
export const LEVEL_LABEL: Record<AccessLevel, string> = { view: "Can view", comment: "Can comment", edit: "Can edit", manage: "Can manage" };

/** Tooltip for each level. */
export const LEVEL_INFO: Record<AccessLevel, string> = {
  view: "Open and read it.",
  comment: "Read it and leave comments.",
  edit: "Change it, but not who has access.",
  manage: "Change it and who has access.",
};

export const TEAM_ROLE_LABEL: Record<AccessTeamRole, string> = { lead: "Lead", member: "Member" };
export const TEAM_ROLE_TONE: Record<AccessTeamRole, BadgeTone> = { lead: "primary", member: "neutral" };

export const ORG_ROLE_LABEL: Record<AccessOrgRole, string> = { owner: "Owners", admin: "Admins", member: "Members" };

/** "1 member", "12 members". */
export function memberCount(n: number | null | undefined): string {
  const v = n ?? 0;
  return `${v} ${v === 1 ? "member" : "members"}`;
}
