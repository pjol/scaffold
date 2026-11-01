import type { AdminRole, UserStatus } from "@scaffoldapp/shared";
import { Badge, type BadgeTone } from "@scaffoldapp/ui-web";

export const STATUS_LABEL: Record<UserStatus, string> = { active: "Active", suspended: "Suspended", banned: "Banned", deleted: "Deleted" };
const STATUS_TONE: Record<UserStatus, BadgeTone> = { active: "success", suspended: "warning", banned: "danger", deleted: "neutral" };
export const ROLE_LABEL: Record<AdminRole, string> = { support: "Support", admin: "Admin" };

export function StatusBadge({ status }: { status: UserStatus }) {
  return (
    <Badge tone={STATUS_TONE[status]} dot>
      {STATUS_LABEL[status]}
    </Badge>
  );
}

export function RoleBadge({ role }: { role: AdminRole | null }) {
  if (!role) return null;
  return <Badge tone={role === "admin" ? "primary" : "info"}>{ROLE_LABEL[role]}</Badge>;
}
