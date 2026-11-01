import type { OrgRole } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-web";

export const ROLE_LABEL: Record<OrgRole, string> = { owner: "Owner", admin: "Admin", member: "Member" };
export const ROLE_TONE: Record<OrgRole, BadgeTone> = { owner: "primary", admin: "info", member: "neutral" };
