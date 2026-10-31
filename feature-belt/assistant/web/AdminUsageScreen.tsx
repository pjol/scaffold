"use client";

import { formatTokenCount, useAdminAssistantUsage } from "@scaffoldapp/shared";
import { Card, EmptyState, ErrorState, SegmentedControl } from "@scaffoldapp/ui-web";
import { BarChart3 } from "lucide-react";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";

type Range = "7" | "30" | "90";
const RANGES: { value: Range; label: string }[] = [
  { value: "7", label: "7 days" },
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
];

const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2, maximumFractionDigits: 2 });

/** Admin → AI usage: assistant messages, tokens and estimated cost per day, all users. */
export function AdminUsageScreen() {
  const [range, setRange] = useUrlState<Range>("days", "30");
  const usage = useAdminAssistantUsage(Number(range));
  const rows = usage.data?.items ?? [];

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="AI usage" info="Estimated cost uses list prices per model; the provider's invoice is the real figure. Tokens in include cache reads and writes." />
      <div className="flex flex-col gap-4">
        <SegmentedControl aria-label="Period" options={RANGES} value={range} onValueChange={setRange} fullWidth={false} className="self-start" />
        <Card padding="none">
          {usage.isPending ? (
            <TableSkeleton rows={6} cols={5} />
          ) : usage.isError ? (
            <ErrorState compact title="Couldn't load usage" onRetry={() => void usage.refetch()} retrying={usage.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<BarChart3 />} title="No assistant use in this period" />
          ) : (
            <Table label="Assistant usage by day" className="min-w-160">
              <THead>
                <TH>Day</TH>
                <TH align="right">Users</TH>
                <TH align="right">Messages</TH>
                <TH align="right">Tokens in</TH>
                <TH align="right">Tokens out</TH>
                <TH align="right">Est. cost</TH>
              </THead>
              <tbody>
                {rows.map((d) => (
                  <TR key={d.day}>
                    <TD className="whitespace-nowrap">{formatDate(`${d.day}T12:00:00Z`)}</TD>
                    <TD align="right">{d.users}</TD>
                    <TD align="right">{d.messages}</TD>
                    <TD align="right">{formatTokenCount(d.input_tokens + d.cache_read_tokens + d.cache_write_tokens)}</TD>
                    <TD align="right">{formatTokenCount(d.output_tokens)}</TD>
                    <TD align="right">{usd.format(Number(d.cost_usd))}</TD>
                  </TR>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </div>
    </div>
  );
}
