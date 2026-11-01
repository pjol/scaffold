"use client";

import { errorMessage, filesFormatSize, filesKindLabel, orgsNounTitle, useFilesAdminItem, useFilesAdminPublic, useFilesAdminSetPublic, type FilesAdminPublicItem } from "@scaffoldapp/shared";
import { Badge, Button, Card, Dialog, EmptyState, ErrorState, Field, Sheet, SkeletonGroup, SkeletonRow, Tab, Tabs, TabsList, TextArea, toast } from "@scaffoldapp/ui-web";
import { ExternalLink, Globe } from "lucide-react";
import { useState } from "react";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate, formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { filesIconTone, filesItemIcon } from "./icons";

/**
 * Admin → Files: what is shared publicly (with 30-day traffic) and what staff turned off.
 * Turning public access off stops every public link to the item and everything under it;
 * people in its org keep their access. Reports arrive in Admin → Moderation.
 */
export function AdminFilesScreen() {
  const [status, setStatus] = useUrlState<"active" | "disabled">("status", "active");
  const [itemId, setItemId] = useUrlState("item", "");
  const list = useFilesAdminPublic(status);
  const rows = list.data?.items ?? [];
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Public files" info="Items anyone with a link can open. Turning one off stops its links at once; reports come through Moderation." />
      <div className="flex flex-col gap-4">
        <Tabs value={status} onValueChange={(v) => setStatus(v as "active" | "disabled")} variant="pill">
          <TabsList aria-label="Status">
            <Tab value="active">Public now</Tab>
            <Tab value="disabled">Turned off</Tab>
          </TabsList>
        </Tabs>
        <Card padding="none">
          {list.isPending ? (
            <TableSkeleton rows={5} cols={4} />
          ) : list.isError ? (
            <ErrorState compact title="Couldn't load public files" info={errorMessage(list.error, "")} onRetry={() => void list.refetch()} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<Globe />} title={status === "active" ? "Nothing is shared publicly" : "Nothing is turned off"} />
          ) : (
            <>
              <Table label="Public files" className="min-w-160">
                <THead>
                  <TH>Item</TH>
                  <TH className="hidden md:table-cell">{orgsNounTitle()}</TH>
                  <TH align="right" info="Previews and downloads through public links in the last 30 days.">
                    Opens (30 days)
                  </TH>
                  <TH align="right">{status === "active" ? "Shared" : "Turned off"}</TH>
                </THead>
                <tbody>
                  {rows.map((r) => {
                    const Icon = filesItemIcon({ ...r, type_info: null });
                    const when = status === "active" ? r.link_created_at : r.disabled_at;
                    return (
                      <TR key={r.id} onActivate={() => setItemId(r.id)}>
                        <TD>
                          <span className="flex min-w-0 items-center gap-3">
                            <Icon className={`size-5 shrink-0 ${filesIconTone({ ...r, type_info: null })}`} aria-hidden />
                            <button type="button" className="truncate font-semibold text-fg underline-offset-2 hover:underline" onClick={() => setItemId(r.id)}>
                              {r.name}
                            </button>
                          </span>
                        </TD>
                        <TD className="hidden md:table-cell">{r.org_name}</TD>
                        <TD align="right" className="tabular-nums">
                          {r.views_30d + r.downloads_30d}
                        </TD>
                        <TD align="right">{when ? formatRelative(when) : "—"}</TD>
                      </TR>
                    );
                  })}
                </tbody>
              </Table>
              <LoadMore hasNextPage={!!list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
      <AdminItemSheet id={itemId || null} onClose={() => setItemId(null)} />
    </div>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="type-caption font-semibold text-fg-muted">{label}</dt>
      <dd className="type-body text-fg">{children}</dd>
    </div>
  );
}

function AdminItemSheet({ id, onClose }: { id: string | null; onClose: () => void }) {
  const item = useFilesAdminItem(id);
  const set = useFilesAdminSetPublic();
  const [disabling, setDisabling] = useState(false);
  const [reason, setReason] = useState("");
  const a = item.data;
  return (
    <Sheet open={id !== null} onOpenChange={(o) => !o && onClose()} title={a?.name ?? "Item"} size="md">
      {item.isPending ? (
        <SkeletonGroup label="Loading" className="flex flex-col gap-2">
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : item.isError ? (
        <ErrorState compact title="Couldn't load the item" info={errorMessage(item.error, "")} onRetry={() => void item.refetch()} />
      ) : a ? (
        <div className="flex flex-col gap-section">
          <div className="flex flex-wrap items-center gap-2">
            <Badge>{filesKindLabel({ ...a, type_info: null })}</Badge>
            {a.disabled_at ? <Badge tone="danger">Public access off</Badge> : a.links > 0 ? <Badge tone="warning">Public</Badge> : null}
            {a.trashed_at ? <Badge>In the trash</Badge> : null}
          </div>
          <dl className="flex flex-col gap-4">
            <Fact label={orgsNounTitle()}>{a.org_name}</Fact>
            <Fact label={a.type === "file" ? "Uploaded by" : "Created by"}>{a.created_by ? a.created_by.name || "Deleted user" : "Deleted user"}</Fact>
            {a.type === "file" ? <Fact label="Size">{filesFormatSize(a.size)}</Fact> : null}
            <Fact label="Working links">{a.links}</Fact>
            <Fact label="Last 30 days">
              {a.views_30d} previews, {a.downloads_30d} downloads
            </Fact>
            {a.disabled_at ? (
              <Fact label="Turned off">
                {formatDateTime(a.disabled_at)}
                {a.disabled_by ? ` by ${a.disabled_by.name || "deleted staff"}` : ""}
                {a.disabled_reason ? ` · ${a.disabled_reason}` : ""}
              </Fact>
            ) : a.link_created_at ? (
              <Fact label="Shared publicly">{formatDate(a.link_created_at)}</Fact>
            ) : null}
          </dl>
          <div className="flex flex-wrap gap-2">
            {a.url ? (
              <Button asChild variant="secondary" leftIcon={<ExternalLink />}>
                <a href={a.url} target="_blank" rel="noopener noreferrer">
                  Open file
                </a>
              </Button>
            ) : null}
            {a.disabled_at ? (
              <Button
                variant="secondary"
                loading={set.isPending}
                onClick={() =>
                  set.mutate({ id: a.id, disabled: false }, { onSuccess: () => toast.success("Public access is back on"), onError: (e) => toast.error(errorMessage(e, "Couldn't turn it back on.")) })
                }
              >
                Turn public access back on
              </Button>
            ) : (
              <Button variant="danger" onClick={() => setDisabling(true)}>
                Turn off public access
              </Button>
            )}
          </div>
          <DisableDialog
            item={disabling ? a : null}
            reason={reason}
            setReason={setReason}
            busy={set.isPending}
            onClose={() => setDisabling(false)}
            onConfirm={() =>
              set.mutate(
                { id: a.id, disabled: true, reason },
                {
                  onSuccess: () => {
                    toast.success("Public access is off");
                    setDisabling(false);
                    setReason("");
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't turn it off.")),
                },
              )
            }
          />
        </div>
      ) : null}
    </Sheet>
  );
}

function DisableDialog({ item, reason, setReason, busy, onClose, onConfirm }: { item: FilesAdminPublicItem | null; reason: string; setReason: (r: string) => void; busy: boolean; onClose: () => void; onConfirm: () => void }) {
  return (
    <Dialog
      open={item !== null}
      onOpenChange={(o) => !o && onClose()}
      title={`Turn off public access to “${item?.name ?? ""}”?`}
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="danger" loading={busy} onClick={onConfirm}>
            Turn off
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <p className="type-body text-fg-muted">Every public link to it and to everything inside stops working. Its owner is told.</p>
        <Field label="Reason" info="Shown to staff in the audit log and here.">
          <TextArea value={reason} onChange={(e) => setReason(e.target.value)} maxLength={500} minRows={2} />
        </Field>
      </div>
    </Dialog>
  );
}
