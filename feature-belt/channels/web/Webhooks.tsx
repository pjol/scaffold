"use client";

import { channelsShortTime, errorMessage, useChannelsWebhooks, useCreateChannelsWebhook, useDeleteChannelsWebhook, type ChannelsDetail } from "@scaffoldapp/shared";
import { Button, ConfirmDialog, Dialog, ErrorState, Field, IconButton, Input, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Copy, Plus, Trash2, Webhook } from "lucide-react";
import { useState } from "react";

/** Incoming webhooks for a channel (owners and admins): name, last used, remove; the URL is shown once. */
export function WebhooksSection({ channel }: { channel: ChannelsDetail }) {
  const list = useChannelsWebhooks(channel.id, channel.permissions.manage_integrations);
  const create = useCreateChannelsWebhook(channel.id);
  const remove = useDeleteChannelsWebhook(channel.id);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [created, setCreated] = useState<{ name: string; url: string } | null>(null);
  const [removing, setRemoving] = useState<{ id: string; name: string } | null>(null);
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h3 className="flex-1 type-title-sm text-fg">Incoming webhooks</h3>
        {!channel.archived && (
          <Button size="sm" variant="secondary" leftIcon={<Plus />} onClick={() => (setName(""), setAdding(true))}>
            Add webhook
          </Button>
        )}
      </div>
      {list.isPending ? (
        <SkeletonRow />
      ) : list.isError ? (
        <ErrorState compact title="Couldn't load webhooks" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : list.data.items.length === 0 ? (
        <p className="type-label text-fg-muted">No webhooks.</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {list.data.items.map((h) => (
            <li key={h.id} className="flex min-h-tap-sm items-center gap-3 rounded-md border border-border px-3 py-2">
              <Webhook aria-hidden className="size-4 shrink-0 text-fg-muted" />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate type-label font-semibold text-fg">{h.name}</span>
                <span className="truncate type-caption text-fg-muted">{h.last_used_at ? `Last used ${channelsShortTime(h.last_used_at)}` : "Never used"}</span>
              </span>
              <IconButton size="sm" variant="ghost" label={`Remove ${h.name}`} icon={<Trash2 />} onClick={() => setRemoving({ id: h.id, name: h.name })} />
            </li>
          ))}
        </ul>
      )}
      <Dialog
        open={adding}
        onOpenChange={setAdding}
        title="Add an incoming webhook"
        info={'Integrations POST {"text": "…"} to the URL to post here under this name. Keep the URL secret.'}
        footer={
          <Button
            disabled={!name.trim()}
            loading={create.isPending}
            onClick={() =>
              create.mutate(name.trim(), {
                onSuccess: (h) => {
                  setAdding(false);
                  setCreated({ name: h.name, url: h.url });
                },
                onError: (e) => toast.error(errorMessage(e)),
              })
            }
          >
            Create webhook
          </Button>
        }
      >
        <Field label="Integration name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} maxLength={80} placeholder="Deploys" />
        </Field>
      </Dialog>
      <Dialog open={created !== null} onOpenChange={(o) => !o && setCreated(null)} title={`${created?.name ?? "Webhook"} is ready`} info="This is the only time the URL is shown. Remove the webhook to stop it.">
        <div className="flex items-center gap-2">
          <Input readOnly value={created?.url ?? ""} aria-label="Webhook URL" wrapperClassName="flex-1" onFocus={(e) => e.currentTarget.select()} />
          <IconButton label="Copy URL" icon={<Copy />} onClick={() => created && void navigator.clipboard?.writeText(created.url).then(() => toast.success("URL copied."))} />
        </div>
      </Dialog>
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Remove ${removing?.name ?? "this webhook"}?`}
        confirmLabel="Remove"
        tone="danger"
        onConfirm={() => (removing ? remove.mutateAsync(removing.id) : undefined)}
      >
        Its URL stops working at once. Messages it posted stay.
      </ConfirmDialog>
    </section>
  );
}
