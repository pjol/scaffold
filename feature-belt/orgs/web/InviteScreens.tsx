"use client";

import { errorMessage, setActiveOrgId, useAcceptInvite, useAuthSession, useInvitePreview, type InvitePreview, type InvitePreviewStatus } from "@scaffoldapp/shared";
import { Banner, Button, Card, EmptyState, ErrorState, InfoTip, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { LinkIcon, LogIn } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { signInHref } from "@/lib/redirect";

const UNAVAILABLE: Record<Exclude<InvitePreviewStatus, "valid">, string> = {
  expired: "This invite expired",
  revoked: "This invite was withdrawn",
  used: "This link has reached its limit",
  accepted: "This invite was already used",
  declined: "This invite was declined",
};

function Frame({ children }: { children: ReactNode }) {
  return <div className="mx-auto flex w-full max-w-form flex-col gap-section px-gutter py-section">{children}</div>;
}

function PreviewSkeleton() {
  return (
    <SkeletonGroup label="Loading invite" className="flex flex-col items-center gap-4">
      <Skeleton shape="circle" className="size-20" />
      <Skeleton className="h-8 w-64" />
      <Skeleton className="h-12 w-48" />
    </SkeletonGroup>
  );
}

function InviteCard({ preview, children }: { preview: InvitePreview; children: ReactNode }) {
  const who = preview.invited_by_name ? `${preview.invited_by_name} invited you` : "You're invited";
  return (
    <Card className="flex flex-col items-center gap-6 text-center">
      <AppAvatar name={preview.org.name} src={preview.org.avatar_url} size="xl" eager />
      <div className="flex flex-col items-center gap-2">
        <h1 className="flex items-center gap-2 type-title text-fg">
          {preview.org.name}
          <InfoTip
            content={`${who} as ${preview.role === "admin" ? "an admin" : "a member"}.${preview.email_hint ? ` The invite is for ${preview.email_hint}.` : ""}`}
            size="md"
          />
        </h1>
      </div>
      <div className="flex w-full flex-col gap-3">{children}</div>
    </Card>
  );
}

function Unavailable({ status }: { status: Exclude<InvitePreviewStatus, "valid"> }) {
  return (
    <EmptyState
      icon={<LinkIcon />}
      title={UNAVAILABLE[status]}
      info="Ask whoever invited you for a new link."
      action={
        <Button asChild variant="secondary" size="auto">
          <Link href="/">Open the app</Link>
        </Button>
      }
    />
  );
}

function usePreviewState(token: string) {
  const preview = useInvitePreview(token);
  let body: ReactNode = null;
  if (preview.isPending) body = <PreviewSkeleton />;
  else if (preview.isError)
    body =
      preview.error.status === 404 ? (
        <EmptyState icon={<LinkIcon />} title="This invite link isn't valid" info="Check the link, or ask for a new one." />
      ) : (
        <ErrorState title="Couldn't load the invite" info={preview.error.message} onRetry={() => void preview.refetch()} retrying={preview.isFetching} />
      );
  else if (preview.data.status !== "valid") body = <Unavailable status={preview.data.status} />;
  return { preview, body };
}

/** `/invites/<token>` (public): what the link opens. Signed-in people go straight to the accept step. */
export function InvitePreviewScreen({ token }: { token: string }) {
  const auth = useAuthSession();
  const router = useRouter();
  const signedIn = auth.isLoaded && auth.isSignedIn;
  const { preview, body } = usePreviewState(token);
  const next = `/orgs/join/${encodeURIComponent(token)}`;

  useEffect(() => {
    if (signedIn) router.replace(next);
  }, [signedIn, next, router]);

  return (
    <Frame>
      {!auth.isLoaded || signedIn ? (
        <PreviewSkeleton />
      ) : (
        (body ??
        (preview.data && (
          <InviteCard preview={preview.data}>
            <Button asChild size="lg" fullWidth leftIcon={<LogIn />}>
              <Link href={signInHref(next)}>Sign in to join</Link>
            </Button>
          </InviteCard>
        )))
      )}
    </Frame>
  );
}

/** `/orgs/join/<token>` (signed in, consent accepted): confirm and join. */
export function AcceptInviteScreen({ token }: { token: string }) {
  const router = useRouter();
  const accept = useAcceptInvite();
  const { preview, body } = usePreviewState(token);
  const [problem, setProblem] = useState<string | null>(null);

  const join = () => {
    setProblem(null);
    accept.mutate(token, {
      onSuccess: (org) => {
        setActiveOrgId(org.id);
        toast.success(`You joined ${org.name}`);
        router.replace(`/settings/orgs/${org.id}`);
      },
      onError: (e) => setProblem(errorMessage(e, "Couldn't join. Try again.")),
    });
  };

  return (
    <Frame>
      {body ??
        (preview.data && (
          <InviteCard preview={preview.data}>
            {problem && <Banner tone="danger">{problem}</Banner>}
            <Button size="lg" fullWidth loading={accept.isPending} onClick={join}>
              Join
            </Button>
            <Button asChild variant="ghost" size="lg" fullWidth>
              <Link href="/">Not now</Link>
            </Button>
          </InviteCard>
        ))}
    </Frame>
  );
}
