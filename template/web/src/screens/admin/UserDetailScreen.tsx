"use client";

import { errorMessage, useAdminAudit, useAdminUser, useDeleteUser, useMe, useUpdateUser, type AdminRole, type AdminUser } from "@scaffoldapp/shared";
import { Button, Card, ConfirmDialog, EmptyState, ErrorState, IconButton, SegmentedControl, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { ChevronLeft, Copy, ScrollText, ShieldCheck, ShieldMinus, Trash2, UserX } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import { AuditTable } from "@/components/admin/AuditTable";
import { RoleBadge, StatusBadge } from "@/components/admin/badges";
import { TableSkeleton } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Section } from "@/components/shell/PageHeader";
import { formatDateTime } from "@/lib/format";

type Status = "active" | "suspended" | "banned";
type Role = "none" | AdminRole;

const STATUS_CONFIRM: Record<Status, { title: (n: string) => string; body: string; label: string; tone: "primary" | "danger" }> = {
  active: { title: (n) => `Reinstate ${n}?`, body: "Their account becomes active again.", label: "Reinstate", tone: "primary" },
  suspended: { title: (n) => `Suspend ${n}?`, body: "They can still sign in and see their data, but can't change anything until reinstated.", label: "Suspend", tone: "danger" },
  banned: { title: (n) => `Ban ${n}?`, body: "They can't sign in until reinstated.", label: "Ban", tone: "danger" },
};

function BackToUsers() {
  return (
    <Link href="/admin/users" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
      <ChevronLeft className="size-4" aria-hidden />
      Users
    </Link>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-4">
      <dt className="type-label text-fg-muted sm:w-32 sm:shrink-0">{label}</dt>
      <dd className="flex min-w-0 items-center gap-2 type-body text-fg">{children}</dd>
    </div>
  );
}

function Controls({ user, isAdmin, self }: { user: AdminUser; isAdmin: boolean; self: boolean }) {
  const router = useRouter();
  const update = useUpdateUser();
  const del = useDeleteUser();
  const [pendingStatus, setPendingStatus] = useState<Status | null>(null);
  const [pendingRole, setPendingRole] = useState<Role | null>(null);
  const [deleting, setDeleting] = useState(false);
  const name = user.display_name || user.email;
  const status = (user.status === "deleted" ? "active" : user.status) as Status;
  const role: Role = user.admin_role ?? "none";
  const roleLocked = !isAdmin || self;

  return (
    <>
      <Section title="Status" info={self ? "You can't change your own account here." : "Suspended accounts can read but not change anything. Banned accounts can't sign in."}>
        <SegmentedControl<Status>
          aria-label="Account status"
          className="max-w-form"
          value={status}
          onValueChange={(v) => v !== status && setPendingStatus(v)}
          options={[
            { value: "active", label: "Active", disabled: self },
            { value: "suspended", label: "Suspended", disabled: self },
            { value: "banned", label: "Banned", disabled: self },
          ]}
        />
      </Section>
      <Section
        title="Role"
        info={
          !isAdmin
            ? "Only admins can change roles."
            : self
              ? "You can't change your own role."
              : "Support can use this console and change account status. Admins can also change roles and delete accounts."
        }
      >
        <SegmentedControl<Role>
          aria-label="Staff role"
          className="max-w-form"
          value={role}
          onValueChange={(v) => v !== role && setPendingRole(v)}
          options={[
            { value: "none", label: "None", disabled: roleLocked },
            { value: "support", label: "Support", icon: <ShieldMinus aria-hidden />, disabled: roleLocked },
            { value: "admin", label: "Admin", icon: <ShieldCheck aria-hidden />, disabled: roleLocked },
          ]}
        />
      </Section>
      {isAdmin && !self && (
        <Section title="Delete account">
          <Button variant="danger" size="auto" leftIcon={<Trash2 />} className="self-start" onClick={() => setDeleting(true)}>
            Delete account
          </Button>
        </Section>
      )}

      <ConfirmDialog
        open={pendingStatus !== null}
        onOpenChange={(o) => !o && setPendingStatus(null)}
        title={pendingStatus ? STATUS_CONFIRM[pendingStatus].title(name) : ""}
        tone={pendingStatus ? STATUS_CONFIRM[pendingStatus].tone : "primary"}
        confirmLabel={pendingStatus ? STATUS_CONFIRM[pendingStatus].label : "Confirm"}
        errorText={(e) => errorMessage(e)}
        onConfirm={async () => {
          if (!pendingStatus) return;
          await update.mutateAsync({ id: user.id, status: pendingStatus });
          toast.success("Status updated");
        }}
      >
        {pendingStatus ? <p>{STATUS_CONFIRM[pendingStatus].body}</p> : null}
      </ConfirmDialog>

      <ConfirmDialog
        open={pendingRole !== null}
        onOpenChange={(o) => !o && setPendingRole(null)}
        title={pendingRole === "none" ? `Remove ${name}'s staff role?` : `Make ${name} ${pendingRole === "admin" ? "an admin" : "support"}?`}
        tone={pendingRole === "none" ? "danger" : "primary"}
        confirmLabel={pendingRole === "none" ? "Remove role" : "Change role"}
        errorText={(e) => errorMessage(e)}
        onConfirm={async () => {
          if (!pendingRole) return;
          await update.mutateAsync({ id: user.id, admin_role: pendingRole === "none" ? "" : pendingRole });
          toast.success("Role updated");
        }}
      >
        <p>{pendingRole === "none" ? "They lose access to this console." : "Takes effect on their next request. The change is recorded in the audit log."}</p>
      </ConfirmDialog>

      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`Delete ${name}'s account?`}
        tone="danger"
        confirmLabel="Delete account"
        requireText="DELETE"
        errorText={(e) => errorMessage(e)}
        onConfirm={async () => {
          await del.mutateAsync(user.id);
          toast.success("Account deleted");
          router.replace("/admin/users");
        }}
      >
        <p>Their name, email, photo and files are deleted now and they can't sign in again. The audit log keeps this action. It can't be undone.</p>
      </ConfirmDialog>
    </>
  );
}

/** Admin → user: details, status and role changes (roles admin-only), deletion (admin-only), and this user's audit trail. */
export function UserDetailScreen({ id }: { id: string }) {
  const me = useMe();
  const user = useAdminUser(id);
  const audit = useAdminAudit({ target_type: "user", target_id: id, limit: 20 });

  if (user.isPending) {
    return (
      <SkeletonGroup label="Loading user" className="flex flex-col gap-section">
        <Skeleton className="h-10 w-24" />
        <div className="flex items-center gap-4">
          <Skeleton shape="circle" className="size-14" />
          <Skeleton className="h-8 w-56" />
        </div>
        <Skeleton className="h-40" />
      </SkeletonGroup>
    );
  }
  if (user.isError) {
    return (
      <div className="flex flex-col gap-2">
        <BackToUsers />
        <Card padding="compact">
          {user.error.status === 404 ? (
            <EmptyState compact icon={<UserX />} title="User not found" />
          ) : (
            <ErrorState compact title="Couldn't load this user" info={user.error.message} onRetry={() => void user.refetch()} retrying={user.isFetching} />
          )}
        </Card>
      </div>
    );
  }

  const u = user.data;
  const deleted = u.status === "deleted";
  const name = u.display_name || (deleted ? "Deleted user" : u.email);
  const isAdmin = me.data?.admin_role === "admin";
  const self = me.data?.id === u.id;
  const entries = audit.data?.items ?? [];

  return (
    <div className="flex flex-col gap-2">
      <BackToUsers />
      <div className="flex flex-col gap-6">
        <div className="flex flex-wrap items-center gap-4">
          <AppAvatar name={name} src={u.avatar_url} size="lg" eager />
          <h1 className="min-w-0 type-title text-fg">{name}</h1>
          <div className="flex items-center gap-2">
            <StatusBadge status={u.status} />
            <RoleBadge role={u.admin_role} />
          </div>
        </div>

        <div className="flex flex-col gap-section">
          <Section title="Details">
            <Card>
              <dl className="flex flex-col gap-4">
                {!deleted && <Fact label="Email">{u.email}</Fact>}
                <Fact label="User ID">
                  <code className="truncate type-label">{u.id}</code>
                  <IconButton
                    label="Copy user ID"
                    tooltip
                    size="sm"
                    icon={<Copy />}
                    onClick={() => void navigator.clipboard?.writeText(u.id).then(() => toast.success("Copied"), () => toast.error("Couldn't copy."))}
                  />
                </Fact>
                <Fact label="Joined">{formatDateTime(u.created_at)}</Fact>
                {u.deleted_at && <Fact label="Deleted">{formatDateTime(u.deleted_at)}</Fact>}
              </dl>
            </Card>
          </Section>

          {!deleted && <Controls user={u} isAdmin={isAdmin} self={self} />}

          <Section title="Activity" info="Admin actions on this account, newest first.">
            <Card padding="none">
              {audit.isPending ? (
                <TableSkeleton rows={3} cols={4} />
              ) : audit.isError ? (
                <ErrorState compact title="Couldn't load activity" info={audit.error.message} onRetry={() => void audit.refetch()} retrying={audit.isFetching} />
              ) : entries.length === 0 ? (
                <EmptyState compact icon={<ScrollText />} title="No admin actions yet" />
              ) : (
                <AuditTable entries={entries} showTarget={false} />
              )}
            </Card>
          </Section>
        </div>
      </div>
    </div>
  );
}

