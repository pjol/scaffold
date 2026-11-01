import type { OrgDomainPolicy, OrgDomainStatus, OrgRole } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-web";

export const ROLE_LABEL: Record<OrgRole, string> = { owner: "Owner", admin: "Admin", member: "Member" };
export const ROLE_TONE: Record<OrgRole, BadgeTone> = { owner: "primary", admin: "info", member: "neutral" };

export const DOMAIN_STATUS_LABEL: Record<OrgDomainStatus, string> = { verified: "Verified", pending: "Not verified", taken: "Claimed elsewhere" };
export const DOMAIN_STATUS_TONE: Record<OrgDomainStatus, BadgeTone> = { verified: "success", pending: "warning", taken: "danger" };
export const DOMAIN_POLICY_LABEL: Record<OrgDomainPolicy, string> = { auto_join: "Join automatically", request: "Ask to join" };
export const DOMAIN_POLICY_OPTIONS = (["auto_join", "request"] as const).map((value) => ({ value, label: DOMAIN_POLICY_LABEL[value] }));
