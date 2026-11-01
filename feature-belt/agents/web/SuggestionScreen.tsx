"use client";

import { errorMessage, isNotFound, useAgentsSuggestion } from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, EmptyState, ErrorState, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { ChevronLeft, ExternalLink, SearchX } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDateTime } from "@/lib/format";
import { STATUS_LABEL, STATUS_TONE, VIA_LABEL, agentIcon } from "./labels";
import { SuggestionActions, SuggestionChange } from "./SuggestionCard";

function Back() {
  return (
    <Button asChild variant="ghost" size="sm" leftIcon={<ChevronLeft />} className="self-start">
      <Link href="/agents/suggestions">Suggestions</Link>
    </Button>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="type-caption text-fg-muted">{label}</dt>
      <dd className="type-body text-fg">{children}</dd>
    </div>
  );
}

/** /agents/<id>: one suggestion with the full change, the outcome, and Accept / Reject. */
export function AgentsSuggestionScreen({ id }: { id: string }) {
  const q = useAgentsSuggestion(id);
  if (q.isPending) {
    return (
      <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
        <Back />
        <SkeletonGroup label="Loading suggestion" className="flex flex-col gap-4">
          <Skeleton className="h-10 w-3/4" />
          <Skeleton className="h-24 w-full rounded-lg" />
          <Skeleton className="h-48 w-full rounded-lg" />
        </SkeletonGroup>
      </div>
    );
  }
  if (q.isError) {
    return (
      <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
        <Back />
        <Card>
          {isNotFound(q.error) ? (
            <EmptyState compact icon={<SearchX />} title="Suggestion not found" />
          ) : (
            <ErrorState compact title="Couldn't load this suggestion" info={errorMessage(q.error)} onRetry={() => void q.refetch()} retrying={q.isFetching} />
          )}
        </Card>
      </div>
    );
  }
  const s = q.data;
  const Icon = agentIcon(s.agent.via);
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      <Back />
      <PageHeader title={s.summary} action={<Badge tone={STATUS_TONE[s.status]}>{STATUS_LABEL[s.status]}</Badge>} />
      <div className="flex flex-col gap-section">
        <Card padding="compact">
          <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Fact label="Agent">
              <span className="flex items-center gap-2" title={VIA_LABEL[s.agent.via] ?? s.agent.via}>
                <Icon className="size-4 shrink-0 text-fg-muted" aria-hidden />
                <span className="truncate">{s.agent.client}</span>
              </span>
            </Fact>
            <Fact label="Where">{s.feature_label}</Fact>
            <Fact label="Suggested">{formatDateTime(s.created_at)}</Fact>
            {s.decided_at ? (
              <Fact label={s.status === "expired" ? "Expired" : "Decided"}>{formatDateTime(s.decided_at)}</Fact>
            ) : (
              <Fact label="Expires">{formatDateTime(s.expires_at)}</Fact>
            )}
          </dl>
        </Card>

        {s.status === "accepted" && <Banner tone="success">Done. The change was made as you.</Banner>}
        {s.status === "accepting" && <Banner tone="info">Applying this change now.</Banner>}
        {s.status === "rejected" && <Banner tone="neutral">You rejected this. Nothing was changed.</Banner>}
        {(s.status === "failed" || s.status === "expired") && s.error && <Banner tone={s.status === "failed" ? "danger" : "neutral"}>{s.error.message}</Banner>}

        <Section
          title={s.tool_title}
          info={`${s.agent.client} called ${s.tool}${s.source ? ` on ${s.source}` : ""}. Accepting runs it as you, so your access now decides whether it works.`}
          action={
            s.link ? (
              <Button asChild variant="ghost" size="sm" leftIcon={<ExternalLink />}>
                <Link href={s.link}>Open</Link>
              </Button>
            ) : undefined
          }
        >
          <SuggestionChange s={s} />
        </Section>
        <SuggestionActions s={s} size="auto" />
      </div>
    </div>
  );
}
