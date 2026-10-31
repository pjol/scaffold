"use client";

import { errorMessage, shortAddress, useAdminUnlinkWallet, useAdminWallets, type AdminWallet } from "@scaffoldapp/shared";
import { Badge, Card, ConfirmDialog, EmptyState, ErrorState, IconButton, toast } from "@scaffoldapp/ui-web";
import { SearchX, Unlink, Wallet as WalletIcon } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const HEX_PREFIX = /^0x[0-9a-f]{4,40}$/i;

const PROOF_LABEL: Record<AdminWallet["proof"], string> = { siwe: "Signed message", eip1271: "Contract signature", privy: "Privy" };

/** Turn the search box into API params: a user id, an address (or 4+ digit prefix), or invalid. */
function parseQuery(q: string): { address?: string; user_id?: string } | null {
  const v = q.trim();
  if (!v) return {};
  if (UUID.test(v)) return { user_id: v.toLowerCase() };
  if (HEX_PREFIX.test(v)) return { address: v.toLowerCase() };
  return null;
}

/** Admin → Wallets: who owns an address, a user's wallets, unlink. Every search is audited. */
export function AdminWalletsScreen() {
  const [q, setQ] = useUrlState("q", "");
  const params = parseQuery(q);
  const wallets = useAdminWallets({ ...(params ?? {}), limit: 50 }, { enabled: params !== null });
  const unlink = useAdminUnlinkWallet();
  const [target, setTarget] = useState<AdminWallet | null>(null);
  const rows = wallets.data?.items ?? [];

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Wallets" info="Each search is recorded in the audit log, because it ties addresses to people." />
      <div className="flex flex-col gap-4">
        <SearchInput value={q} onSearch={setQ} placeholder="Address, 0x prefix or user ID" label="Search wallets" className="sm:max-w-md" />
        <Card padding="none">
          {params === null ? (
            <EmptyState compact icon={<SearchX />} title="Enter an address (0x and 4 or more digits) or a user ID" />
          ) : wallets.isPending ? (
            <TableSkeleton rows={6} cols={4} />
          ) : wallets.isError ? (
            <ErrorState compact title="Couldn't load wallets" info={wallets.error.message} onRetry={() => void wallets.refetch()} retrying={wallets.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={q ? <SearchX /> : <WalletIcon />} title={q ? "No matching wallets" : "No wallets linked yet"} />
          ) : (
            <>
              <Table label="Wallets" className="min-w-160">
                <THead>
                  <TH>Address</TH>
                  <TH>User</TH>
                  <TH className="hidden md:table-cell" info="How the user proved control: a signed message, a smart-contract wallet's signature, or a Privy embedded wallet.">
                    Proof
                  </TH>
                  <TH className="hidden lg:table-cell" align="right">
                    Linked
                  </TH>
                  <TH align="right">
                    <span className="sr-only">Actions</span>
                  </TH>
                </THead>
                <tbody>
                  {rows.map((w) => (
                    <TR key={w.id}>
                      <TD>
                        <span className="flex items-center gap-2">
                          <span className="tabular-nums" title={w.address}>
                            {shortAddress(w.address, 6)}
                          </span>
                          {w.is_primary && (
                            <Badge size="sm" tone="primary">
                              Primary
                            </Badge>
                          )}
                          {w.kind === "embedded" && (
                            <Badge size="sm" tone="neutral">
                              Embedded
                            </Badge>
                          )}
                        </span>
                      </TD>
                      <TD className="max-w-64 truncate">
                        <Link href={`/admin/users/${w.user_id}`} className="font-semibold text-fg underline-offset-2 hover:underline">
                          {w.user_display_name || w.user_email}
                        </Link>
                      </TD>
                      <TD className="hidden text-fg-muted md:table-cell">{PROOF_LABEL[w.proof]}</TD>
                      <TD className="hidden whitespace-nowrap text-fg-muted lg:table-cell" align="right">
                        {formatDate(w.created_at)}
                      </TD>
                      <TD align="right">
                        <IconButton label={`Unlink ${shortAddress(w.address)}`} tooltip icon={<Unlink />} variant="danger" size="sm" onClick={() => setTarget(w)} />
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={wallets.hasNextPage} isFetchingNextPage={wallets.isFetchingNextPage} fetchNextPage={wallets.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
      <ConfirmDialog
        open={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
        title={target ? `Unlink ${shortAddress(target.address)} from ${target.user_display_name || target.user_email}?` : "Unlink wallet?"}
        confirmLabel="Unlink"
        tone="danger"
        onConfirm={async () => {
          if (!target) return;
          await unlink.mutateAsync(target.id);
          toast.success("Wallet unlinked");
        }}
        errorText={(e) => errorMessage(e, "Couldn't unlink the wallet. Try again.")}
      >
        The user can link it again by signing with it. This is recorded in the audit log.
      </ConfirmDialog>
    </div>
  );
}
