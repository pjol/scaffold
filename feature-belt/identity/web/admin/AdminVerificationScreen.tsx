"use client";

import { errorMessage, useAdminVerification, useApproveVerification, useMe, useRejectVerification, type AdminVerificationDetail } from "@scaffoldapp/shared";
import { Banner, Button, Card, Dialog, ErrorState, Field, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { Check, ChevronLeft, ExternalLink, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDateTime } from "@/lib/format";
import { STATE_LABEL } from "./AdminVerificationsScreen";

function RejectDialog({ v, revoke }: { v: AdminVerificationDetail; revoke: boolean }) {
  const router = useRouter();
  const reject = useRejectVerification();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    const r = reason.trim();
    if (r.length < 3) {
      setError("Give a reason of at least 3 characters. The user sees it.");
      return;
    }
    reject.mutate(
      { id: v.id, reason: r },
      {
        onSuccess: () => {
          setOpen(false);
          toast.success(revoke ? "Verification revoked" : "Verification rejected");
          router.push("/admin/identity");
        },
        onError: (e) => setError(errorMessage(e, "Couldn't save the decision. Try again.")),
      },
    );
  };
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title={revoke ? "Revoke this verification?" : "Reject this verification?"}
      trigger={
        <Button variant="danger-ghost" size="auto" leftIcon={<X />}>
          {revoke ? "Revoke" : "Reject"}
        </Button>
      }
      footer={
        <>
          <Button variant="ghost" size="auto" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button variant="danger" size="auto" loading={reject.isPending} onClick={submit}>
            {revoke ? "Revoke" : "Reject"}
          </Button>
        </>
      }
    >
      <Field label="Reason" required error={error ?? undefined} info="The user sees this.">
        <TextArea value={reason} maxLength={500} minRows={3} autoGrow onChange={(e) => setReason(e.target.value)} placeholder="The ID photo was too blurry to read. Take it again in good light." />
      </Field>
    </Dialog>
  );
}

function Photo({ url, label }: { url: string; label: string }) {
  return (
    <figure className="flex flex-col gap-2">
      <figcaption className="type-label font-semibold text-fg">{label}</figcaption>
      <a href={url} target="_blank" rel="noopener noreferrer" className="block overflow-hidden rounded-md border border-border bg-sunken outline-none focus-visible:ring-2 focus-visible:ring-ring">
        {/* Presigned, short-lived and private: never through next/image's optimiser or cache. */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src={url} alt={label} referrerPolicy="no-referrer" className="aspect-4/3 w-full object-contain" />
      </a>
    </figure>
  );
}

/** Admin → Verification → one attempt: photos through 5-minute links (the view is audited), approve or reject. */
export function AdminVerificationScreen({ id }: { id: string }) {
  const router = useRouter();
  const me = useMe();
  const v = useAdminVerification(id);
  const approve = useApproveVerification();
  const self = v.data?.user.id === me.data?.id;

  return (
    <div className="flex flex-col gap-2">
      <Link href="/admin/identity" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
        <ChevronLeft className="size-4" aria-hidden />
        Verification
      </Link>
      {v.isPending ? (
        <SkeletonGroup label="Loading verification" className="flex flex-col gap-section">
          <Skeleton className="h-8 w-56" />
          <div className="grid gap-4 sm:grid-cols-2">
            <Skeleton className="aspect-4/3" />
            <Skeleton className="aspect-4/3" />
          </div>
        </SkeletonGroup>
      ) : v.isError ? (
        <ErrorState title="Couldn't load this verification" info={errorMessage(v.error)} onRetry={() => void v.refetch()} retrying={v.isFetching} />
      ) : (
        <div className="flex flex-col gap-section">
          <PageHeader title={v.data.user.display_name || v.data.user.email} />
          {self ? <Banner tone="info">Another staff member has to review your own verification.</Banner> : null}
          {v.data.status === "rejected" && v.data.reason ? <Banner tone="danger">{v.data.reason}</Banner> : null}

          {v.data.document_url || v.data.selfie_url ? (
            <Section title="Photos" info={v.data.urls_expire_at ? `Links work until ${formatDateTime(v.data.urls_expire_at)}. Reload the page for new ones.` : undefined}>
              <div className="grid gap-4 sm:grid-cols-2">
                {v.data.document_url ? <Photo url={v.data.document_url} label="ID" /> : null}
                {v.data.selfie_url ? <Photo url={v.data.selfie_url} label="Selfie" /> : null}
              </div>
            </Section>
          ) : v.data.provider === "manual" && v.data.status === "pending" ? (
            <p className="type-label text-fg-muted">No photos sent yet.</p>
          ) : null}

          <Section title="Details">
            <Card>
              <dl className="grid gap-x-6 gap-y-2 type-label sm:grid-cols-[10rem_minmax(0,1fr)]">
                {(
                  [
                    ["Email", v.data.user.email],
                    ["Method", v.data.provider === "persona" ? "Persona" : "Photos reviewed by staff"],
                    ["Status", v.data.status === "pending" ? (v.data.submitted_at ? "Waiting for review" : "Started") : STATE_LABEL[v.data.status]],
                    ["Started", formatDateTime(v.data.created_at)],
                    ["Submitted", v.data.submitted_at ? formatDateTime(v.data.submitted_at) : "—"],
                    ["Decided", v.data.decided_at ? formatDateTime(v.data.decided_at) : "—"],
                    ["Expires", v.data.expires_at ? formatDateTime(v.data.expires_at) : "—"],
                  ] as const
                ).map(([k, val]) => (
                  <div key={k} className="contents">
                    <dt className="text-fg-muted">{k}</dt>
                    <dd className="break-words text-fg">{val}</dd>
                  </div>
                ))}
              </dl>
            </Card>
          </Section>

          <div className="flex flex-wrap gap-2">
            {v.data.status === "pending" && v.data.submitted_at && !self ? (
              <>
                <Button
                  size="auto"
                  leftIcon={<Check />}
                  loading={approve.isPending}
                  onClick={() =>
                    approve.mutate(id, {
                      onSuccess: () => {
                        toast.success("Verified");
                        router.push("/admin/identity");
                      },
                      onError: (e) => toast.error(errorMessage(e, "Couldn't approve. Try again.")),
                    })
                  }
                >
                  Approve
                </Button>
                <RejectDialog v={v.data} revoke={false} />
              </>
            ) : null}
            {v.data.status === "verified" && !self ? <RejectDialog v={v.data} revoke /> : null}
            {v.data.provider_url ? (
              <Button asChild variant="ghost" size="auto" rightIcon={<ExternalLink />}>
                <a href={v.data.provider_url} target="_blank" rel="noopener noreferrer">
                  Open in Persona
                </a>
              </Button>
            ) : null}
          </div>
        </div>
      )}
    </div>
  );
}
