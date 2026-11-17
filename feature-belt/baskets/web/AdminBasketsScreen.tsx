"use client";

import {
  BASKETS_STATUS_LABEL,
  errorMessage,
  useArchiveBasket,
  useBasketsAdminList,
  useMe,
  useRenameBasket,
  useResolveBasketReports,
  useSetBasketHidden,
  type BasketsAdminBasket,
  type BasketsAdminFilter,
} from "@scaffoldapp/shared";
import { Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, IconButton, Input, Menu, MenuItem, MenuSeparator, SegmentedControl, TextArea, toast } from "@scaffoldapp/ui-web";
import { Archive, CheckCheck, Eye, EyeOff, MoreVertical, Pencil, SearchX, ShoppingBasket } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { BasketStatusBadge } from "./parts";
import { basketsPaths } from "./registry";
import { BASKETS_REPORT_REASONS } from "./ReportDialog";

const FILTERS: { value: BasketsAdminFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "reported", label: "Reported" },
  { value: "hidden", label: "Hidden" },
];

const reasonLabel = (r: string) => BASKETS_REPORT_REASONS.find((x) => x.value === r)?.label.replace(/^The /, "") ?? r;

type Pending = { kind: "rename" | "hide" | "show" | "archive" | "resolve"; b: BasketsAdminBasket } | null;

function RenameDialog({ b, onClose }: { b: BasketsAdminBasket; onClose: () => void }) {
  const rename = useRenameBasket();
  const [name, setName] = useState(b.name);
  const [description, setDescription] = useState(b.description);
  const save = () =>
    rename.mutate(
      { symbol: b.symbol, name: name.trim(), description: description.trim() },
      {
        onSuccess: () => {
          toast.success(`Renamed ${b.symbol}`);
          onClose();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't rename the basket.")),
      },
    );
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Rename ${b.symbol}`}
      info="The market and its price feed are renamed too. The change is in the audit log."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={save} loading={rename.isPending} disabled={name.trim().length < 3}>
            Save
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <Field label="Name" required>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={48} />
        </Field>
        <Field label="Description">
          <TextArea value={description} onChange={(e) => setDescription(e.target.value)} maxLength={500} minRows={2} maxRows={6} />
        </Field>
      </form>
    </Dialog>
  );
}

function RowMenu({ b, admin, onPick }: { b: BasketsAdminBasket; admin: boolean; onPick: (p: Pending) => void }) {
  return (
    <Menu trigger={<IconButton label={`Actions for ${b.name}`} icon={<MoreVertical />} variant="ghost" size="sm" />}>
      <MenuItem icon={<Pencil />} onSelect={() => onPick({ kind: "rename", b })}>
        Rename…
      </MenuItem>
      {b.open_reports > 0 && (
        <MenuItem icon={<CheckCheck />} onSelect={() => onPick({ kind: "resolve", b })}>
          Mark reports handled
        </MenuItem>
      )}
      {b.hidden ? (
        <MenuItem icon={<Eye />} onSelect={() => onPick({ kind: "show", b })}>
          Show in discovery
        </MenuItem>
      ) : (
        <>
          <MenuSeparator />
          <MenuItem icon={<EyeOff />} destructive onSelect={() => onPick({ kind: "hide", b })}>
            Hide…
          </MenuItem>
        </>
      )}
      {admin && b.status === "active" && (
        <MenuItem icon={<Archive />} destructive onSelect={() => onPick({ kind: "archive", b })}>
          Archive…
        </MenuItem>
      )}
    </Menu>
  );
}

/** Admin → Baskets: every basket with its reports; rename, hide or show, mark reports handled; admins archive. All audited. */
export function AdminBasketsScreen() {
  const me = useMe();
  const admin = me.data?.admin_role === "admin";
  const [filter, setFilter] = useUrlState<BasketsAdminFilter>("filter", "all");
  const [q, setQ] = useUrlState("q", "");
  const list = useBasketsAdminList({ filter, q: q || undefined });
  const [pending, setPending] = useState<Pending>(null);
  const setHidden = useSetBasketHidden();
  const archive = useArchiveBasket();
  const resolve = useResolveBasketReports();
  const rows = list.data?.items ?? [];

  const run = async (p: NonNullable<Pending>) => {
    if (p.kind === "hide" || p.kind === "show") {
      await setHidden.mutateAsync({ symbol: p.b.symbol, hidden: p.kind === "hide" });
      toast.success(p.kind === "hide" ? `Hid ${p.b.symbol}` : `${p.b.symbol} is back in discovery`);
    } else if (p.kind === "archive") {
      const res = await archive.mutateAsync(p.b.symbol);
      toast.success(res.status === "archived" ? `Archived ${p.b.symbol}` : `${p.b.symbol} is winding down`);
    } else if (p.kind === "resolve") {
      await resolve.mutateAsync(p.b.symbol);
      toast.success(`Reports on ${p.b.symbol} marked handled`);
    }
  };

  const confirm =
    pending && pending.kind !== "rename"
      ? {
          hide: {
            title: `Hide “${pending.b.name}”?`,
            body: "It leaves discovery and stops taking new positions: people holding one can only close it, then it's archived. Listing the same lines won't reopen it.",
            label: "Hide",
            tone: "danger" as const,
          },
          show: { title: `Show “${pending.b.name}” again?`, body: "It returns to discovery. Its trading status stays as it is.", label: "Show", tone: "primary" as const },
          archive: {
            title: `Archive “${pending.b.name}”?`,
            body: "Its market takes only closing orders while people hold positions, then halts. Listing the same lines again reopens it.",
            label: "Archive",
            tone: "danger" as const,
          },
          resolve: { title: `Mark reports on “${pending.b.name}” handled?`, body: "It returns to discovery unless it's hidden.", label: "Mark handled", tone: "primary" as const },
        }[pending.kind]
      : null;

  return (
    <Screen title="Baskets" info="Baskets people listed. Reports come from people; enough open reports take a basket out of discovery until you review it.">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <SearchInput value={q} onSearch={setQ} placeholder="Name or symbol" label="Search baskets" className="sm:max-w-sm" />
          <SegmentedControl aria-label="Show" options={FILTERS} value={filter} onValueChange={(v) => setFilter(v)} fullWidth={false} />
        </div>
        <Card padding="none">
          {list.isPending ? (
            <TableSkeleton rows={6} cols={5} />
          ) : list.isError ? (
            <ErrorState compact title="Couldn't load baskets" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={q || filter !== "all" ? <SearchX /> : <ShoppingBasket />} title={q || filter !== "all" ? "No matching baskets" : "No baskets yet"} />
          ) : (
            <>
              <Table label="Baskets" className="min-w-160">
                <THead>
                  <TH>Basket</TH>
                  <TH>Status</TH>
                  <TH align="right">Entries</TH>
                  <TH>Reports</TH>
                  <TH className="hidden md:table-cell">Listed by</TH>
                  <TH className="hidden lg:table-cell">Listed</TH>
                  <TH align="right">
                    <span className="sr-only">Actions</span>
                  </TH>
                </THead>
                <tbody>
                  {rows.map((b) => (
                    <TR key={b.id}>
                      <TD className="w-full max-w-0">
                        <Link href={basketsPaths.basket(b.symbol)} className="block truncate font-semibold text-fg underline-offset-2 hover:underline">
                          {b.name}
                        </Link>
                        <span className="block truncate type-caption text-fg-muted">{b.symbol}</span>
                      </TD>
                      <TD className="whitespace-nowrap">
                        <span className="flex items-center gap-1">
                          {b.status === "active" && <span className="text-fg-muted">{BASKETS_STATUS_LABEL.active}</span>}
                          <BasketStatusBadge basket={b} staff />
                        </span>
                      </TD>
                      <TD align="right" className="tabular-nums">
                        {b.entries}
                      </TD>
                      <TD className="max-w-48 truncate text-fg-muted">{b.open_reports > 0 ? `${b.open_reports} · ${b.report_reasons.map(reasonLabel).join(", ")}` : "—"}</TD>
                      <TD className="hidden whitespace-nowrap text-fg-muted md:table-cell">{b.creator}</TD>
                      <TD className="hidden whitespace-nowrap text-fg-muted lg:table-cell">{formatDate(b.created_at)}</TD>
                      <TD align="right">
                        <RowMenu b={b} admin={admin} onPick={setPending} />
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
      {pending?.kind === "rename" && <RenameDialog b={pending.b} onClose={() => setPending(null)} />}
      {pending && confirm && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setPending(null)}
          title={confirm.title}
          confirmLabel={confirm.label}
          tone={confirm.tone}
          onConfirm={() => run(pending)}
          errorText={(e) => errorMessage(e, "That didn't work. Try again.")}
        >
          {confirm.body}
        </ConfirmDialog>
      )}
    </Screen>
  );
}
