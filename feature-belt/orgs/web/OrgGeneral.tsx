"use client";

import { errorMessage, hasErrorCode, ORG_AVATAR_PURPOSE, orgRoleAtLeast, orgsNoun, useDeleteOrg, useMe, useRemoveOrgMember, useUpdateOrg, useUpload, type Org } from "@scaffoldapp/shared";
import { Button, ConfirmDialog, Field, Input, toast } from "@scaffoldapp/ui-web";
import { Camera, LogOut, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";

const PHOTO_TYPES = ["image/jpeg", "image/png", "image/webp"];
const PHOTO_MAX = 5 * 1024 * 1024;
const SLUG = /^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$/;

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-4">
      <dt className="type-label text-fg-muted sm:w-32 sm:shrink-0">{label}</dt>
      <dd className="min-w-0 type-body text-fg">{children}</dd>
    </div>
  );
}

function Photo({ org }: { org: Org }) {
  const upload = useUpload();
  const update = useUpdateOrg(org.id);
  const input = useRef<HTMLInputElement>(null);
  const busy = upload.isPending || update.isPending;
  const onFile = async (file: File | undefined) => {
    if (!file) return;
    if (!PHOTO_TYPES.includes(file.type)) return void toast.error("Choose a JPEG, PNG or WebP image.");
    if (file.size > PHOTO_MAX) return void toast.error("That picture is over 5 MB. Choose a smaller one.");
    try {
      const r = await upload.mutateAsync({ file, purpose: ORG_AVATAR_PURPOSE, maxBytes: PHOTO_MAX });
      await update.mutateAsync({ avatar_key: r.key });
      toast.success("Picture updated");
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't update the picture. Try again."));
    }
  };
  return (
    <Section title="Picture">
      <div className="flex flex-wrap items-center gap-4">
        <AppAvatar name={org.name} src={org.avatar_url} size="xl" />
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" size="auto" leftIcon={<Camera />} loading={upload.isPending} disabled={busy} onClick={() => input.current?.click()}>
            {org.avatar_url ? "Change picture" : "Upload picture"}
          </Button>
          {org.avatar_url && (
            <Button
              variant="ghost"
              size="auto"
              disabled={busy}
              onClick={() =>
                update.mutate(
                  { avatar_key: "" },
                  { onSuccess: () => toast.success("Picture removed"), onError: (e) => toast.error(errorMessage(e, "Couldn't remove the picture. Try again.")) },
                )
              }
            >
              Remove
            </Button>
          )}
        </div>
        <input
          ref={input}
          type="file"
          accept={PHOTO_TYPES.join(",")}
          hidden
          onChange={(e) => {
            void onFile(e.target.files?.[0]);
            e.target.value = "";
          }}
        />
      </div>
    </Section>
  );
}

function DetailsForm({ org }: { org: Org }) {
  const update = useUpdateOrg(org.id);
  const [name, setName] = useState(org.name);
  const [slug, setSlug] = useState(org.slug);
  const [errors, setErrors] = useState<{ name?: string; slug?: string }>({});
  useEffect(() => {
    setName(org.name);
    setSlug(org.slug);
  }, [org.name, org.slug]);
  const dirty = name.trim() !== org.name || slug.trim() !== org.slug;

  const save = (e: FormEvent) => {
    e.preventDefault();
    const n = name.trim();
    const s = slug.trim().toLowerCase();
    const next: typeof errors = {};
    if (!n) next.name = "Enter a name.";
    if (!SLUG.test(s) || s.includes("--")) next.slug = "Use 3 to 40 lowercase letters, numbers or single hyphens.";
    setErrors(next);
    if (next.name || next.slug) return;
    update.mutate(
      { ...(n !== org.name ? { name: n } : {}), ...(s !== org.slug ? { slug: s } : {}) },
      {
        onSuccess: () => toast.success("Saved"),
        onError: (err) => (hasErrorCode(err, "conflict") ? setErrors({ slug: errorMessage(err) }) : toast.error(errorMessage(err, "Couldn't save. Try again."))),
      },
    );
  };

  return (
    <form className="flex max-w-form flex-col gap-6" onSubmit={save} noValidate>
      <div className="flex flex-col gap-4">
        <Field label="Name" error={errors.name} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} />
        </Field>
        <Field label="URL name" info="Lowercase letters, numbers and hyphens. Links that use the old name stop working." error={errors.slug} required>
          <Input value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} maxLength={40} autoCapitalize="none" spellCheck={false} />
        </Field>
      </div>
      <Button type="submit" size="auto" className="self-start" loading={update.isPending && update.variables?.avatar_key === undefined} disabled={!dirty}>
        Save
      </Button>
    </form>
  );
}

/** General tab: picture, name and URL name (admins), facts (members), leave, delete (owners). */
export function OrgGeneral({ org }: { org: Org }) {
  const router = useRouter();
  const me = useMe();
  const leave = useRemoveOrgMember(org.id);
  const del = useDeleteOrg();
  const [leaving, setLeaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const admin = orgRoleAtLeast(org.role, "admin");

  return (
    <div className="flex flex-col gap-section">
      {admin ? (
        <>
          <Photo org={org} />
          <Section title="Details">
            <DetailsForm org={org} />
          </Section>
        </>
      ) : (
        <Section title="Details">
          <dl className="flex flex-col gap-3">
            <Fact label="Name">{org.name}</Fact>
            <Fact label="URL name">{org.slug}</Fact>
            <Fact label="Members">{org.member_count}</Fact>
            <Fact label="Joined">{formatDate(org.joined_at)}</Fact>
          </dl>
        </Section>
      )}

      <Section title="Leave" info={org.role === "owner" ? "Owners can leave once someone else is an owner too." : undefined}>
        <Button variant="danger-ghost" size="auto" leftIcon={<LogOut />} className="self-start" onClick={() => setLeaving(true)}>
          Leave {orgsNoun()}
        </Button>
      </Section>

      {org.role === "owner" && (
        <Section title={`Delete ${orgsNoun()}`}>
          <Button variant="danger" size="auto" leftIcon={<Trash2 />} className="self-start" onClick={() => setDeleting(true)}>
            Delete {orgsNoun()}
          </Button>
        </Section>
      )}

      <ConfirmDialog
        open={leaving}
        onOpenChange={setLeaving}
        title={`Leave ${org.name}?`}
        tone="danger"
        confirmLabel="Leave"
        errorText={(e) => errorMessage(e, "Couldn't leave. Try again.")}
        onConfirm={async () => {
          if (!me.data) return;
          await leave.mutateAsync({ userId: me.data.id, leave: true });
          toast.success(`You left ${org.name}`);
          router.replace("/settings/orgs");
        }}
      >
        <p>You lose access to its content right away.</p>
      </ConfirmDialog>

      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`Delete ${org.name}?`}
        tone="danger"
        confirmLabel={`Delete ${orgsNoun()}`}
        requireText="DELETE"
        errorText={(e) => errorMessage(e, `Couldn't delete the ${orgsNoun()}. Try again.`)}
        onConfirm={async () => {
          await del.mutateAsync(org.id);
          toast.success(`${org.name} deleted`);
          router.replace("/settings/orgs");
        }}
      >
        <p>{`All ${org.member_count} members lose access, and pending invites and links stop working. It can't be undone.`}</p>
      </ConfirmDialog>
    </div>
  );
}
