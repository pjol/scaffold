"use client";

import {
  errorMessage,
  useConfig,
  useIdentityStatus,
  useStartVerification,
  useSubmitIdentityDocuments,
  useUpload,
  type IdentityStatus,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, ErrorState, Field, InfoTip, PhotoUploader, Skeleton, SkeletonGroup, toast, type UploadedPhoto } from "@scaffoldapp/ui-web";
import { BadgeCheck, Clock, ShieldAlert, ShieldCheck, ShieldQuestion } from "lucide-react";
import { useState, type ReactNode } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";

/** Manual review: ID photo + selfie, uploaded privately, then submitted together. */
function DocumentsForm() {
  const upload = useUpload();
  const submit = useSubmitIdentityDocuments();
  const [doc, setDoc] = useState<UploadedPhoto[]>([]);
  const [selfie, setSelfie] = useState<UploadedPhoto[]>([]);
  const [busy, setBusy] = useState(0);
  const docKey = doc[0]?.key;
  const selfieKey = selfie[0]?.key;
  const up = async (file: File, signal: AbortSignal) => {
    const r = await upload.mutateAsync({ file, purpose: "identity_document", signal });
    return { key: r.key, url: null };
  };
  return (
    <Section title="Send your photos" info="Staff look at them once to confirm your identity, then delete them.">
      <div className="flex max-w-form flex-col gap-6">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Photo of your ID" info="A passport, driving licence or national ID card. All four corners in frame, text readable.">
            <PhotoUploader max={1} capture="environment" label="Add ID photo" onChange={setDoc} onUploadingChange={(u) => setBusy((n) => n + (u ? 1 : -1))} upload={(f, { signal }) => up(f, signal)} />
          </Field>
          <Field label="Selfie" info="Your face, well lit, no sunglasses or hat.">
            <PhotoUploader max={1} capture="user" label="Add selfie" onChange={setSelfie} onUploadingChange={(u) => setBusy((n) => n + (u ? 1 : -1))} upload={(f, { signal }) => up(f, signal)} />
          </Field>
        </div>
        <Button
          size="auto"
          className="self-start"
          disabled={!docKey || !selfieKey || busy > 0}
          loading={submit.isPending}
          onClick={() =>
            submit.mutate(
              { document_key: docKey ?? "", selfie_key: selfieKey ?? "" },
              { onSuccess: () => toast.success("Sent for review"), onError: (e) => toast.error(errorMessage(e, "Couldn't send your photos. Try again.")) },
            )
          }
        >
          Send for review
        </Button>
      </div>
    </Section>
  );
}

function StartButton({ label, status }: { label: string; status: IdentityStatus }) {
  const start = useStartVerification();
  const run = () =>
    start.mutate(
      { redirect_uri: `${window.location.origin}/settings/identity` },
      {
        onSuccess: (res) => {
          if (res.flow?.hosted_url) window.location.assign(res.flow.hosted_url);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't start verification. Try again.")),
      },
    );
  return (
    <Button size="auto" className="self-start" leftIcon={<ShieldCheck />} loading={start.isPending} onClick={run} disabled={status.attempts_left === 0 && status.status === "rejected"}>
      {label}
    </Button>
  );
}

function StateRow({ icon, title, info, children }: { icon: ReactNode; title: string; info?: ReactNode; children?: ReactNode }) {
  return (
    <Card className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-sunken text-fg [&_svg]:size-5">{icon}</span>
        <h2 className="flex min-w-0 flex-1 items-center gap-2 type-title-sm text-fg">
          {title}
          {info ? <InfoTip content={info} /> : null}
        </h2>
      </div>
      {children}
    </Card>
  );
}

function Body({ status }: { status: IdentityStatus }) {
  const config = useConfig();
  const support = config.data?.support_email;
  const manual = status.provider === "manual";
  switch (status.status) {
    case "verified":
      return (
        <StateRow
          icon={<BadgeCheck className="text-primary-strong" />}
          title="Verified"
          info={[status.verified_at ? `Verified ${formatDate(status.verified_at)}.` : "", status.expires_at ? `Valid until ${formatDate(status.expires_at)}.` : ""].filter(Boolean).join(" ") || undefined}
        />
      );
    case "pending":
      if (status.submitted) return <StateRow icon={<Clock />} title="In review" info="You'll get a notification when it's done." />;
      return manual ? (
        <div className="flex flex-col gap-section">
          <StateRow icon={<ShieldQuestion />} title="Not verified yet" />
          <DocumentsForm />
        </div>
      ) : (
        <StateRow icon={<ShieldQuestion />} title="Verification started">
          <StartButton label="Continue verification" status={status} />
        </StateRow>
      );
    case "rejected":
      return (
        <StateRow icon={<ShieldAlert className="text-danger-strong" />} title="Not verified">
          {status.reason ? <Banner tone="danger">{status.reason}</Banner> : null}
          {status.attempts_left > 0 ? (
            <StartButton label="Try again" status={status} />
          ) : support ? (
            <Button asChild variant="secondary" size="auto" className="self-start">
              <a href={`mailto:${support}?subject=Identity%20verification`}>Contact support</a>
            </Button>
          ) : null}
        </StateRow>
      );
    case "expired":
      return (
        <StateRow icon={<ShieldQuestion />} title="Verification expired">
          <StartButton label="Verify again" status={status} />
        </StateRow>
      );
    default:
      return (
        <StateRow icon={<ShieldQuestion />} title="Not verified" info={manual ? "You send a photo of your ID and a selfie; staff confirm it." : "A verification partner checks your ID and a selfie."}>
          <StartButton label="Verify your identity" status={status} />
        </StateRow>
      );
  }
}

/** Settings → Verification (`/settings/identity`): status and the next step. */
export function IdentityScreen() {
  const status = useIdentityStatus();
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Verification" />
      {status.isPending ? (
        <SkeletonGroup label="Loading verification" className="flex max-w-form flex-col gap-4">
          <Skeleton className="h-24" />
        </SkeletonGroup>
      ) : status.isError ? (
        <ErrorState title="Couldn't load your verification" info={errorMessage(status.error)} onRetry={() => void status.refetch()} retrying={status.isFetching} />
      ) : (
        <div className="flex max-w-form flex-col gap-section">
          <Body status={status.data} />
        </div>
      )}
    </div>
  );
}
