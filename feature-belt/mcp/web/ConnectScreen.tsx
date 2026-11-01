"use client";

import { errorMessage, useAuthSession, useDecideMcpOAuthRequest, useMcpOAuthRequest, useMe, type McpOAuthRequest, type McpOAuthScope } from "@scaffoldapp/shared";
import { Banner, Button, Card, Divider, ErrorState, InfoTip, Skeleton, SkeletonGroup, Spinner, Switch, toast } from "@scaffoldapp/ui-web";
import { Eye, Unplug } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { useSignOut } from "@/components/auth/useSignOut";
import { ConsentScreen } from "@/components/legal/ConsentScreen";
import { CenteredScreen } from "@/components/shell/CenteredScreen";
import { APP_NAME } from "@/lib/app";
import { signInHref } from "@/lib/redirect";

/**
 * /connect?request=…: the consent page of the app's OAuth authorization server. An agent
 * (Claude, ChatGPT, Cursor, Claude Code) sends the person here from /oauth/authorize; they
 * sign in (Clerk or the dev picker), accept the Terms if they haven't, see who is asking,
 * where the browser goes next and what access is requested, then allow or deny. The API
 * answers with the client's redirect URL. Public in proxy.ts so the sign-in redirect keeps
 * this relative path (Clerk's own redirect would carry an absolute URL).
 */
export function ConnectScreen() {
  const params = useSearchParams();
  const requestId = params.get("request");
  const errorCode = params.get("error");
  const auth = useAuthSession();
  const router = useRouter();
  const signedIn = auth.isLoaded && auth.isSignedIn;
  const me = useMe({ enabled: signedIn && !!requestId });

  useEffect(() => {
    if (requestId && auth.isLoaded && !auth.isSignedIn) router.replace(signInHref(`/connect?request=${encodeURIComponent(requestId)}`));
  }, [auth.isLoaded, auth.isSignedIn, requestId, router]);

  if (errorCode || !requestId) return <Failure code={errorCode ?? "missing"} />;
  if (!signedIn || me.isPending) return <ConnectSkeleton />;
  if (me.isError && !me.data) return <Failure code="account" detail={errorMessage(me.error, "Couldn't load your account.")} />;
  if (!me.data) return <ConnectSkeleton />;
  if (me.data.consent.required) return <ConsentScreen />;
  return <Consent requestId={requestId} email={me.data.email} active={me.data.status === "active"} />;
}

const SERVER_ERROR = { title: "Couldn't start the connection", info: "Try again from the app in a moment." };

const FAILURES: Record<string, { title: string; info: string }> = {
  missing: { title: "Nothing to connect", info: "Open this page from the app you're connecting." },
  expired: { title: "This request expired", info: "Requests last 10 minutes and work once. Start again from the app you're connecting." },
  unknown_client: { title: "This app isn't registered", info: "Its registration may have lapsed. Start again from the app; it registers itself again." },
  redirect_mismatch: { title: "This request can't be completed", info: "The app asked to return to an address it didn't register, so it was stopped." },
  account: { title: "Your account can't connect apps", info: "" },
  server_error: SERVER_ERROR,
};

function Failure({ code, detail }: { code: string; detail?: string }) {
  const f = FAILURES[code] ?? SERVER_ERROR;
  return (
    <CenteredScreen>
      <div className="flex flex-col items-center gap-6">
        <ErrorState icon={<Unplug strokeWidth={2} />} title={f.title} info={detail ?? f.info} compact />
        <Button asChild variant="secondary" size="auto">
          <Link href="/">Open {APP_NAME}</Link>
        </Button>
      </div>
    </CenteredScreen>
  );
}

function ConnectSkeleton() {
  return (
    <CenteredScreen>
      <SkeletonGroup label="Loading the request" className="flex flex-col gap-6">
        <Skeleton className="h-8 w-3/4" />
        <Skeleton className="h-48" />
        <div className="flex justify-end gap-2">
          <Skeleton className="h-10 w-24" />
          <Skeleton className="h-10 w-24" />
        </div>
      </SkeletonGroup>
    </CenteredScreen>
  );
}

function Consent({ requestId, email, active }: { requestId: string; email: string; active: boolean }) {
  const req = useMcpOAuthRequest(requestId);
  const decide = useDecideMcpOAuthRequest();
  const signOut = useSignOut();
  const [write, setWrite] = useState(true);
  const [leaving, setLeaving] = useState<McpOAuthRequest | null>(null);

  if (req.isPending) return <ConnectSkeleton />;
  if (req.isError) {
    if (req.error.code === "request_expired") return <Failure code="expired" />;
    return (
      <CenteredScreen>
        <ErrorState title="Couldn't load the request" info={errorMessage(req.error)} onRetry={() => void req.refetch()} retrying={req.isFetching} compact />
      </CenteredScreen>
    );
  }
  const r = req.data;
  const name = r.client.name || "An unnamed app";
  if (leaving) return <Leaving request={leaving} name={name} />;

  const wantsWrite = r.scopes.includes("write");
  const submit = (allow: boolean) => {
    const scopes: McpOAuthScope[] = write ? ["read", "write"] : ["read"];
    decide.mutate(
      { id: requestId, allow, ...(allow && wantsWrite ? { scopes } : {}) },
      {
        onSuccess: (d) => {
          setLeaving(r);
          window.location.assign(d.redirect_url);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't finish. Try again.")),
      },
    );
  };
  const switchAccount = () => void signOut(signInHref(`/connect?request=${encodeURIComponent(requestId)}`));

  return (
    <CenteredScreen>
      <div className="flex flex-col gap-6">
        <h1 className="flex flex-wrap items-center gap-2 type-title text-fg">
          <span className="min-w-0 break-words">Connect {name}?</span>
          <InfoTip size="md" content={`A connected app acts as you: it sees what you can see in ${APP_NAME}, and nothing more. Disconnect it any time in Settings → Connected apps.`} />
        </h1>

        {!active && <Banner tone="warning">Your account is suspended, so it can&apos;t connect apps. Contact support.</Banner>}
        {r.redirect_kind === "loopback" && (
          <Banner tone="warning">It returns to an app on this computer. Allow it only if you started this from an app you trust.</Banner>
        )}
        {r.redirect_kind === "app" && (
          <Banner tone="warning">It returns to an app on this device ({r.redirect_host}). Allow it only if you started this from an app you trust.</Banner>
        )}

        <Card padding="none">
          <dl className="flex flex-col">
            <Fact label="App">
              <span className="flex items-center gap-1.5">
                <span className="truncate">{name}</span>
                <InfoTip
                  content={
                    r.client.kind === "metadata_document"
                      ? `Name published by ${r.client.host ?? "the app"}.`
                      : "The app chose this name itself. Check where it returns to before you allow it."
                  }
                />
              </span>
            </Fact>
            {r.client.host ? (
              <>
                <Divider className="mx-4" />
                <Fact label="Published by">{r.client.host}</Fact>
              </>
            ) : null}
            <Divider className="mx-4" />
            <Fact label="Returns to">
              <span className="break-all">{r.redirect_host}</span>
            </Fact>
            <Divider className="mx-4" />
            <Fact label="Account">
              <span className="flex flex-wrap items-center gap-x-3">
                <span className="break-all">{email}</span>
                <button type="button" onClick={switchAccount} className="min-h-6 type-label text-primary-strong underline-offset-2 hover:underline">
                  Switch account
                </button>
              </span>
            </Fact>
          </dl>
        </Card>

        <section className="flex flex-col gap-3">
          <h2 className="type-title-sm text-fg">It will be able to</h2>
          <ul className="flex flex-col gap-2">
            <li className="flex min-h-control items-center gap-3 type-body text-fg">
              <Eye aria-hidden className="size-5 shrink-0 text-fg-muted" />
              See what you can see in {APP_NAME}
            </li>
            {wantsWrite ? (
              <li>
                <Switch
                  checked={write}
                  onCheckedChange={setWrite}
                  label="Make changes as you"
                  info="Create and update things on your behalf, within what your account may do. Turn off to allow reading only."
                />
              </li>
            ) : null}
          </ul>
        </section>

        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="secondary" size="auto" disabled={decide.isPending} onClick={() => submit(false)}>
            Deny
          </Button>
          <Button size="auto" loading={decide.isPending} disabled={!active} onClick={() => submit(true)}>
            Allow
          </Button>
        </div>
      </div>
    </CenteredScreen>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-h-14 flex-col justify-center gap-1 px-4 py-3 sm:flex-row sm:items-center sm:justify-start sm:gap-4">
      <dt className="type-label text-fg-muted sm:w-28 sm:shrink-0">{label}</dt>
      <dd className="min-w-0 flex-1 type-body text-fg">{children}</dd>
    </div>
  );
}

/** After a decision: the browser is on its way back to the client. Native and loopback clients may leave this tab open. */
function Leaving({ request, name }: { request: McpOAuthRequest; name: string }) {
  const [done, setDone] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setDone(true), 2500);
    return () => clearTimeout(t);
  }, []);
  return (
    <CenteredScreen>
      <div role="status" className="flex items-center gap-3 type-body text-fg">
        {done && request.redirect_kind !== "web" ? (
          <span>Done. You can close this tab and go back to {name}.</span>
        ) : (
          <>
            <Spinner />
            <span>Returning to {name}…</span>
          </>
        )}
      </div>
    </CenteredScreen>
  );
}
