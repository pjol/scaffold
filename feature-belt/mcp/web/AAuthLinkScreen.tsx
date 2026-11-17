"use client";

import {
  errorMessage,
  useAuthSession,
  useClaimMcpAAuthInteraction,
  useDecideMcpAAuthInteraction,
  useMcpAAuthInteraction,
  useMe,
  type McpAAuthInteraction,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, Divider, ErrorState, Field, InfoTip, Input, Skeleton, SkeletonGroup, Spinner, toast } from "@scaffoldapp/ui-web";
import { Unplug } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useSignOut } from "@/components/auth/useSignOut";
import { ConsentScreen } from "@/components/legal/ConsentScreen";
import { CenteredScreen } from "@/components/shell/CenteredScreen";
import { APP_NAME } from "@/lib/app";
import { signInHref } from "@/lib/redirect";

/**
 * /connect/aauth?code=…[&callback=…]: the link page for AAuth. An agent that signs its
 * requests through a person server (Sayso, a company's own) sends the person here the first
 * time it calls for them, with a code; an agent that can't open a browser shows the address
 * and code, and the person types the code. Signed in, with the Terms accepted, the page
 * claims the code (single use; the address then carries the request id, so a reload doesn't
 * present the code again), shows which person server account is asking (its name as it
 * gives it, its verified address, and the email it vouches for compared with this
 * account's), and links or declines. The API answers with the agent's callback, if it gave
 * one. Public in proxy.ts so the sign-in redirect keeps this relative path.
 */
export function AAuthLinkScreen() {
  const params = useSearchParams();
  const code = params.get("code");
  const callback = params.get("callback");
  const requestId = params.get("request");
  const auth = useAuthSession();
  const router = useRouter();
  const signedIn = auth.isLoaded && auth.isSignedIn;
  const me = useMe({ enabled: signedIn });
  const here = linkHref({ code, callback, request: requestId });

  useEffect(() => {
    if (auth.isLoaded && !auth.isSignedIn) router.replace(signInHref(here));
  }, [auth.isLoaded, auth.isSignedIn, here, router]);

  if (!signedIn || me.isPending) return <LinkSkeleton />;
  if (me.isError && !me.data) return <Failure title="Couldn't load your account" info={errorMessage(me.error)} />;
  if (!me.data) return <LinkSkeleton />;
  if (me.data.consent.required) return <ConsentScreen />;
  const account = { email: me.data.email, active: me.data.status === "active" };
  if (requestId) return <Decision requestId={requestId} account={account} here={here} />;
  return <Claim initialCode={code} callback={callback} />;
}

function linkHref(p: { code: string | null; callback: string | null; request: string | null }): string {
  const q = new URLSearchParams();
  if (p.request) q.set("request", p.request);
  else {
    if (p.code) q.set("code", p.code);
    if (p.callback) q.set("callback", p.callback);
  }
  const s = q.toString();
  return `/connect/aauth${s ? `?${s}` : ""}`;
}

// The code each request was claimed with, kept for the tab's life so the person can compare
// it after the address drops it.
const shownCodes = new Map<string, string>();

/** Claims the code from the address, or asks for one. */
function Claim({ initialCode, callback }: { initialCode: string | null; callback: string | null }) {
  const claim = useClaimMcpAAuthInteraction();
  const router = useRouter();
  const [code, setCode] = useState("");
  const started = useRef(false);

  const submit = (value: string) => {
    claim.mutate(
      { code: value, ...(callback ? { callback } : {}) },
      {
        onSuccess: (view) => {
          shownCodes.set(view.id, value.toUpperCase());
          router.replace(`/connect/aauth?request=${encodeURIComponent(view.id)}`);
        },
      },
    );
  };

  useEffect(() => {
    if (initialCode && !started.current) {
      started.current = true;
      submit(initialCode);
    }
    // Once, on arrival with a code.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialCode]);

  // A code from the link is the agent's to replace; a typed one can be typed again.
  if (claim.isError && initialCode) {
    if (claim.error.code === "invalid_code")
      return <Failure title="This code doesn't work" info="Codes work once and last 15 minutes. Ask the agent for a new one." />;
    if (claim.error.code === "invalid_callback")
      return <Failure title="This request can't be completed" info="The agent asked to return to an address that isn't allowed, so it was stopped." />;
    return <Failure title="Couldn't open the request" info={errorMessage(claim.error)} />;
  }
  if (initialCode || claim.isSuccess) return <LinkSkeleton />;

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (code.trim()) submit(code.trim());
  };
  return (
    <CenteredScreen>
      <form onSubmit={onSubmit} className="flex flex-col gap-6">
        <h1 className="flex flex-wrap items-center gap-2 type-title text-fg">
          <span className="min-w-0 break-words">Enter the code from your agent</span>
          <InfoTip size="md" content={`An agent asking to use ${APP_NAME} through your person server shows this address and a code. Enter the code to see the request.`} />
        </h1>
        <Field
          label="Code"
          error={claim.isError ? (claim.error.code === "invalid_code" ? "That code doesn't work. Codes work once and last 15 minutes." : errorMessage(claim.error)) : undefined}
        >
          <Input
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder="ABCD-1234"
            autoComplete="one-time-code"
            autoCapitalize="characters"
            spellCheck={false}
            autoFocus
          />
        </Field>
        <Button type="submit" size="auto" loading={claim.isPending} disabled={!code.trim()} className="sm:self-end">
          Continue
        </Button>
      </form>
    </CenteredScreen>
  );
}

function Decision({ requestId, account, here }: { requestId: string; account: { email: string; active: boolean }; here: string }) {
  const req = useMcpAAuthInteraction(requestId);
  const decide = useDecideMcpAAuthInteraction();
  const signOut = useSignOut();
  const [result, setResult] = useState<{ status: "linked" | "denied"; leaving: boolean } | null>(null);

  if (req.isPending) return <LinkSkeleton />;
  if (req.isError) {
    if (req.error.code === "request_expired") return <Failure title="This request expired" info="Requests last 15 minutes. Ask the agent to try again." />;
    if (req.error.status === 404) return <Failure title="Nothing to link" info="This request isn't yours, or it no longer exists. Open the link the agent gave you." />;
    return (
      <CenteredScreen>
        <ErrorState title="Couldn't load the request" info={errorMessage(req.error)} onRetry={() => void req.refetch()} retrying={req.isFetching} compact />
      </CenteredScreen>
    );
  }
  const r = req.data;
  const name = r.person_server.name || r.person_server.host;
  const done = result ?? (r.status === "interacting" ? null : { status: r.status, leaving: false });
  const shownCode = shownCodes.get(requestId);
  if (done) return <Done status={done.status} leaving={done.leaving} name={name} />;

  const submit = (allow: boolean) =>
    decide.mutate(
      { id: requestId, allow },
      {
        onSuccess: (d) => {
          setResult({ status: d.status, leaving: !!d.redirect_url });
          if (d.redirect_url) window.location.assign(d.redirect_url);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't finish. Try again.")),
      },
    );

  return (
    <CenteredScreen>
      <div className="flex flex-col gap-6">
        <h1 className="flex flex-wrap items-center gap-2 type-title text-fg">
          <span className="min-w-0 break-words">Link {name}?</span>
          <InfoTip
            size="md"
            content={`Agents that act for you through this person server account can then ask for access to ${APP_NAME} as you. Each still needs a grant from the person server, sees only what you can see here, and follows your AI agent settings. Unlink any time in Settings → Connected apps.`}
          />
        </h1>

        {!account.active && <Banner tone="warning">Your account is suspended, so it can&apos;t link agents. Contact support.</Banner>}
        {r.email_match === "mismatch" && (
          <Banner tone="warning">
            {name} says this account belongs to {r.email}, not {account.email}. Link it only if it is your own.
          </Banner>
        )}

        <Card padding="none">
          <dl className="flex flex-col">
            <Fact label="Person server">
              <span className="flex items-center gap-1.5">
                <span className="truncate">{name}</span>
                {r.person_server.name ? <InfoTip content="The name is what the server says about itself; the address below is verified." /> : null}
              </span>
            </Fact>
            <Divider className="mx-4" />
            <Fact label="Address">
              <span className="break-all">{r.person_server.host}</span>
            </Fact>
            <Divider className="mx-4" />
            <Fact label="Its account">
              {r.email ? (
                <span className="flex flex-wrap items-center gap-x-2">
                  <span className="break-all">{r.email}</span>
                  {r.email_match === "match" ? <span className="type-caption text-fg-muted">Same as yours</span> : null}
                </span>
              ) : (
                <span className="flex items-center gap-1.5 text-fg-muted">
                  Not shared
                  <InfoTip content="The person server didn't vouch for an email, so check the code instead." />
                </span>
              )}
            </Fact>
            {shownCode ? (
              <>
                <Divider className="mx-4" />
                <Fact label="Code">
                  <span className="flex items-center gap-1.5">
                    <span className="font-mono tracking-wide">{shownCode}</span>
                    <InfoTip content="Check it matches the code the agent or your person server showed you." />
                  </span>
                </Fact>
              </>
            ) : null}
            {r.return_host ? (
              <>
                <Divider className="mx-4" />
                <Fact label="Returns to">
                  <span className="break-all">{r.return_host}</span>
                </Fact>
              </>
            ) : null}
            <Divider className="mx-4" />
            <Fact label="Your account">
              <span className="flex flex-wrap items-center gap-x-3">
                <span className="break-all">{account.email}</span>
                <button
                  type="button"
                  onClick={() => void signOut(signInHref(here))}
                  className="min-h-6 type-label text-primary-strong underline-offset-2 hover:underline"
                >
                  Switch account
                </button>
              </span>
            </Fact>
          </dl>
        </Card>

        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="secondary" size="auto" disabled={decide.isPending} onClick={() => submit(false)}>
            Cancel
          </Button>
          <Button size="auto" loading={decide.isPending} disabled={!account.active} onClick={() => submit(true)}>
            Link account
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

/** After the decision: on the way back to the agent's callback, or done here. */
function Done({ status, leaving, name }: { status: McpAAuthInteraction["status"]; leaving: boolean; name: string }) {
  if (leaving) {
    return (
      <CenteredScreen>
        <div role="status" className="flex items-center gap-3 type-body text-fg">
          <Spinner />
          <span>Returning to the agent…</span>
        </div>
      </CenteredScreen>
    );
  }
  return (
    <CenteredScreen>
      <div role="status" className="type-body text-fg">
        {status === "linked" ? `Linked ${name}. The agent carries on by itself; you can close this tab.` : "Not linked. The agent was told no; you can close this tab."}
      </div>
    </CenteredScreen>
  );
}

function Failure({ title, info }: { title: string; info: string }) {
  return (
    <CenteredScreen>
      <div className="flex flex-col items-center gap-6">
        <ErrorState icon={<Unplug strokeWidth={2} />} title={title} info={info} compact />
        <Button asChild variant="secondary" size="auto">
          <Link href="/">Open {APP_NAME}</Link>
        </Button>
      </div>
    </CenteredScreen>
  );
}

function LinkSkeleton() {
  return (
    <CenteredScreen>
      <SkeletonGroup label="Loading the request" className="flex flex-col gap-6">
        <Skeleton className="h-8 w-3/4" />
        <Skeleton className="h-56" />
        <div className="flex justify-end gap-2">
          <Skeleton className="h-10 w-24" />
          <Skeleton className="h-10 w-32" />
        </div>
      </SkeletonGroup>
    </CenteredScreen>
  );
}
