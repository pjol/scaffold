"use client";

import { errorMessage, useMe, useOrgMembers, useRemoveOrgMember, useTransferOwnership, useUpdateMember, type Org, type OrgMember, type OrgRole } from "@scaffoldapp/shared";
import { Badge, Card, ConfirmDialog, EmptyState, ErrorState, IconButton, ListRow, Menu, MenuItem, MenuSeparator, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Crown, MoreHorizontal, ShieldCheck, UserMinus, UserRound, Users } from "lucide-react";
import { useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { ROLE_LABEL, ROLE_TONE } from "./labels";

type Pending = { kind: "role"; member: OrgMember; role: OrgRole } | { kind: "remove"; member: OrgMember } | { kind: "transfer"; member: OrgMember };

const ROLE_ICON = { owner: <Crown aria-hidden />, admin: <ShieldCheck aria-hidden />, member: <UserRound aria-hidden /> } as const;

/** What the viewer may do to `m` (mirrors the API's rules; the API has the final say). */
function actionsFor(viewer: OrgRole, m: OrgMember): { roles: OrgRole[]; transfer: boolean; remove: boolean } {
  if (viewer === "owner") {
    return { roles: (["owner", "admin", "member"] as const).filter((r) => r !== m.role), transfer: m.role !== "owner", remove: true };
  }
  if (viewer === "admin" && m.role !== "owner") {
    return { roles: m.role === "member" ? ["admin"] : ["member"], transfer: false, remove: m.role === "member" };
  }
  return { roles: [], transfer: false, remove: false };
}

function displayName(m: OrgMember): string {
  return m.display_name || m.email || "Member";
}

/** Members tab: owners first; admins change roles and remove; owners also transfer ownership. */
export function OrgMembers({ org }: { org: Org }) {
  const me = useMe();
  const members = useOrgMembers(org.id);
  const update = useUpdateMember(org.id);
  const remove = useRemoveOrgMember(org.id);
  const transfer = useTransferOwnership(org.id);
  const [pending, setPending] = useState<Pending | null>(null);
  const rows = members.data?.items ?? [];

  const confirmTitle = (() => {
    if (!pending) return "";
    const n = displayName(pending.member);
    if (pending.kind === "remove") return `Remove ${n}?`;
    if (pending.kind === "transfer") return `Make ${n} the owner?`;
    return pending.role === "owner" ? `Make ${n} an owner?` : `Make ${n} ${pending.role === "admin" ? "an admin" : "a member"}?`;
  })();
  const confirmBody = (() => {
    if (!pending) return "";
    if (pending.kind === "remove") return "They lose access right away. You can invite them again later.";
    if (pending.kind === "transfer") return "They become an owner and you become an admin.";
    if (pending.role === "owner") return "Owners can manage everything, including deleting the organization.";
    if (pending.role === "admin") return "Admins can manage members, invites and the organization's details.";
    return pending.member.role === "owner" ? "They stop being an owner." : "They stop managing members and invites.";
  })();

  return (
    <>
      <Card padding="none" className="p-1">
        {members.isPending ? (
          <SkeletonGroup label="Loading members" className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        ) : members.isError ? (
          <ErrorState compact title="Couldn't load members" info={members.error.message} onRetry={() => void members.refetch()} retrying={members.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<Users />} title="No members" />
        ) : (
          <ul className="flex flex-col">
            {rows.map((m) => {
              const self = m.user_id === me.data?.id;
              const can = self ? { roles: [], transfer: false, remove: false } : actionsFor(org.role, m);
              const hasMenu = can.roles.length > 0 || can.transfer || can.remove;
              return (
                <li key={m.user_id}>
                  <ListRow
                    title={self ? `${displayName(m)} (you)` : displayName(m)}
                    leading={<AppAvatar name={displayName(m)} src={m.avatar_url} size="md" />}
                    trailing={
                      <span className="flex items-center gap-2">
                        {m.email && <span className="hidden max-w-56 truncate type-caption text-fg-muted md:inline">{m.email}</span>}
                        <Badge tone={ROLE_TONE[m.role]}>{ROLE_LABEL[m.role]}</Badge>
                        {hasMenu ? (
                          <Menu align="end" trigger={<IconButton label={`Actions for ${displayName(m)}`} icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
                            {can.roles.map((r) => (
                              <MenuItem key={r} icon={ROLE_ICON[r]} onSelect={() => setPending({ kind: "role", member: m, role: r })}>
                                {`Make ${ROLE_LABEL[r].toLowerCase()}`}
                              </MenuItem>
                            ))}
                            {can.transfer && (
                              <MenuItem icon={<Crown aria-hidden />} onSelect={() => setPending({ kind: "transfer", member: m })}>
                                Transfer ownership
                              </MenuItem>
                            )}
                            {can.remove && (
                              <>
                                <MenuSeparator />
                                <MenuItem destructive icon={<UserMinus aria-hidden />} onSelect={() => setPending({ kind: "remove", member: m })}>
                                  Remove
                                </MenuItem>
                              </>
                            )}
                          </Menu>
                        ) : (
                          <span className="block size-control-sm shrink-0" aria-hidden />
                        )}
                      </span>
                    }
                  />
                </li>
              );
            })}
          </ul>
        )}
        <LoadMore hasNextPage={members.hasNextPage} isFetchingNextPage={members.isFetchingNextPage} fetchNextPage={members.fetchNextPage} />
      </Card>

      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(o) => !o && setPending(null)}
        title={confirmTitle}
        tone={pending?.kind === "remove" ? "danger" : "primary"}
        confirmLabel={pending?.kind === "remove" ? "Remove" : pending?.kind === "transfer" ? "Transfer" : "Change role"}
        errorText={(e) => errorMessage(e, "Couldn't change that. Try again.")}
        onConfirm={async () => {
          if (!pending) return;
          const n = displayName(pending.member);
          if (pending.kind === "remove") {
            await remove.mutateAsync({ userId: pending.member.user_id });
            toast.success(`${n} removed`);
          } else if (pending.kind === "transfer") {
            await transfer.mutateAsync(pending.member.user_id);
            toast.success(`${n} is now an owner`);
          } else {
            await update.mutateAsync({ userId: pending.member.user_id, role: pending.role });
            toast.success("Role updated");
          }
        }}
      >
        <p>{confirmBody}</p>
      </ConfirmDialog>
    </>
  );
}
