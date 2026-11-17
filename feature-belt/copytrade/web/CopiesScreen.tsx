"use client";

import { copytradeStopReasonLabel, useCopytradeCopies, useCopytradeConfig } from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, ListGroup, ListRow, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { Copy as CopyIcon } from "lucide-react";
import Link from "next/link";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { CopyStatusBadge, PctText, PersonLine, signTone, useExchangeMoney } from "./parts";

/** The caller's copies: open ones first, then stopped ones. */
export function CopiesScreen() {
  const open = useCopytradeCopies("open");
  const stopped = useCopytradeCopies("stopped");
  const config = useCopytradeConfig();
  const { money } = useExchangeMoney();

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader
        title="Copy trading"
        info="Each copy has its own account holding positions in proportion to the trader's. PnL is its value minus what you moved in."
        action={
          <Button asChild size="auto">
            <Link href="/leaders">Find traders</Link>
          </Button>
        }
      />
      {config.data?.paused && <Banner tone="warning">Copy trading is paused right now: copied orders wait. Stops and withdrawals still work.</Banner>}
      <Section title="Copying">
        {open.isPending ? (
          <SkeletonGroup label="Loading your copies" className="flex flex-col gap-2">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </SkeletonGroup>
        ) : open.isError ? (
          <ErrorState compact title="Couldn't load your copies" info={open.error.message} onRetry={() => void open.refetch()} />
        ) : open.data.items.length === 0 ? (
          <EmptyState
            icon={<CopyIcon />}
            title="You aren't copying anyone"
            info="Pick a trader and an amount; their positions are mirrored in a separate account."
            action={
              <Button asChild variant="secondary">
                <Link href="/leaders">Find traders</Link>
              </Button>
            }
          />
        ) : (
          <ListGroup>
            {open.data.items.map((c) => (
              <ListRow
                key={c.id}
                asChild
                interactive
                title={
                  <PersonLine person={c.leader}>
                    <span className="type-caption text-fg-muted">
                      {money(c.equity)} · <span className={signTone(c.pnl)}>{money(c.pnl, true)}</span> (<PctText pct={c.pnl_pct} />)
                    </span>
                  </PersonLine>
                }
                trailing={<CopyStatusBadge status={c.status} />}
              >
                <Link href={`/copy/${c.id}`} />
              </ListRow>
            ))}
          </ListGroup>
        )}
        {open.data && <LoadMore hasNextPage={open.hasNextPage} isFetchingNextPage={open.isFetchingNextPage} fetchNextPage={open.fetchNextPage} />}
      </Section>
      {stopped.data && stopped.data.items.length > 0 && (
        <Section title="Stopped">
          <ListGroup>
            {stopped.data.items.map((c) => (
              <ListRow
                key={c.id}
                asChild
                interactive
                title={
                  <PersonLine person={c.leader}>
                    <span className="type-caption text-fg-muted">
                      {copytradeStopReasonLabel(c.stop_reason)} · <span className={signTone(c.pnl)}>{money(c.pnl, true)}</span>
                    </span>
                  </PersonLine>
                }
                trailing={<CopyStatusBadge status={c.status} />}
              >
                <Link href={`/copy/${c.id}`} />
              </ListRow>
            ))}
          </ListGroup>
          <LoadMore hasNextPage={stopped.hasNextPage} isFetchingNextPage={stopped.isFetchingNextPage} fetchNextPage={stopped.fetchNextPage} />
        </Section>
      )}
    </div>
  );
}
