"use client";

import {
  PRESENCE_CLEAR_AFTER_OPTIONS,
  PRESENCE_DND_OPTIONS,
  PRESENCE_STATUS_EMOJI,
  PRESENCE_STATUS_PRESETS,
  activeCustomStatus,
  errorMessage,
  isPresenceDND,
  isPresenceEmoji,
  presenceDndUntil,
  presenceStatusRequest,
  presenceUntilLabel,
  useClearPresenceStatus,
  useMyPresence,
  useSetPresenceDND,
  useSetPresenceStatus,
  type PresenceClearAfter,
} from "@scaffoldapp/shared";
import { Button, Dialog, Field, IconButton, Input, Menu, MenuItem, Select, cn, toast } from "@scaffoldapp/ui-web";
import { SmilePlus, X } from "lucide-react";
import { useEffect, useState } from "react";
import { openPresenceDialog, usePresenceDialog } from "./store";

const clearLabel = new Map(PRESENCE_CLEAR_AFTER_OPTIONS.map((o) => [o.value, o.label]));

/** Set a status: emoji, text, when it clears; presets when empty. */
function StatusDialog({ open }: { open: boolean }) {
  const mine = useMyPresence();
  const set = useSetPresenceStatus();
  const clear = useClearPresenceStatus();
  const current = activeCustomStatus(mine.data);
  const [emoji, setEmoji] = useState("");
  const [text, setText] = useState("");
  const [clearAfter, setClearAfter] = useState<PresenceClearAfter>("today");
  const [typedEmoji, setTypedEmoji] = useState("");

  useEffect(() => {
    if (!open) return;
    setEmoji(current?.emoji ?? "");
    setText(current?.text ?? "");
    setClearAfter(current?.clear_after ?? "today");
    setTypedEmoji("");
    // Reset to the saved status only when the dialog opens.
  }, [open]);

  const close = () => openPresenceDialog(null);
  const empty = !emoji && !text.trim();
  const save = () =>
    set.mutate(presenceStatusRequest(emoji, text.trim(), clearAfter), {
      onSuccess: () => {
        toast.success("Status updated.");
        close();
      },
      onError: (e) => toast.error(errorMessage(e, "Couldn't save your status.")),
    });

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      title="Set a status"
      info="Everyone who can see you online sees your status. It clears on its own when the time you pick runs out."
      footer={
        <>
          {current && (
            <Button
              variant="ghost"
              loading={clear.isPending}
              onClick={() =>
                clear.mutate(undefined, {
                  onSuccess: () => {
                    toast.success("Status cleared.");
                    close();
                  },
                  onError: (e) => toast.error(errorMessage(e)),
                })
              }
            >
              Clear status
            </Button>
          )}
          <Button disabled={empty} loading={set.isPending} onClick={save}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-6">
        <div className="flex items-center gap-2">
          <Menu
            align="start"
            trigger={
              <button
                type="button"
                aria-label={emoji ? `Emoji ${emoji}. Change` : "Choose an emoji"}
                className="flex size-control shrink-0 items-center justify-center rounded-md border border-border-strong bg-surface type-title-sm transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:size-tap"
              >
                {emoji || <SmilePlus aria-hidden className="size-5 text-fg-muted" />}
              </button>
            }
          >
            <div className="grid grid-cols-4 gap-1 p-1">
              {PRESENCE_STATUS_EMOJI.map((e) => (
                <MenuItem key={e} onSelect={() => setEmoji(e)}>
                  <span className="type-title-sm" aria-label={e}>
                    {e}
                  </span>
                </MenuItem>
              ))}
            </div>
          </Menu>
          <Input
            value={text}
            maxLength={100}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !empty) save();
            }}
            placeholder="What's your status?"
            aria-label="Status"
            wrapperClassName="min-w-0 flex-1"
            trailing={
              !empty ? (
                <IconButton
                  size="sm"
                  variant="ghost"
                  label="Clear"
                  icon={<X />}
                  onClick={() => {
                    setEmoji("");
                    setText("");
                  }}
                />
              ) : undefined
            }
          />
        </div>
        {empty ? (
          <ul className="flex flex-col gap-1" aria-label="Suggested statuses">
            {PRESENCE_STATUS_PRESETS.map((p) => (
              <li key={p.text}>
                <button
                  type="button"
                  onClick={() => {
                    setEmoji(p.emoji);
                    setText(p.text);
                    setClearAfter(p.clear_after);
                  }}
                  className="flex min-h-tap-sm w-full items-center gap-3 rounded-md px-3 py-2 text-left transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                >
                  <span aria-hidden className="type-body-lg">
                    {p.emoji}
                  </span>
                  <span className="min-w-0 flex-1 truncate type-body text-fg">{p.text}</span>
                  <span className="shrink-0 type-caption text-fg-muted">{clearLabel.get(p.clear_after)}</span>
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <div className="flex flex-col gap-4">
            <Field label="Clear after">
              <Select value={clearAfter} onValueChange={(v) => setClearAfter(v as PresenceClearAfter)} options={PRESENCE_CLEAR_AFTER_OPTIONS.map((o) => ({ value: o.value, label: o.label }))} />
            </Field>
            <Field label="Any emoji" error={typedEmoji && !isPresenceEmoji(typedEmoji.trim()) ? "Enter an emoji." : undefined}>
              <Input
                value={typedEmoji}
                onChange={(e) => {
                  setTypedEmoji(e.target.value);
                  if (isPresenceEmoji(e.target.value.trim())) setEmoji(e.target.value.trim());
                }}
                placeholder="Type or paste an emoji"
              />
            </Field>
          </div>
        )}
      </div>
    </Dialog>
  );
}

/** Pause notifications for a while, or resume them. */
function DndDialog({ open }: { open: boolean }) {
  const mine = useMyPresence();
  const dnd = useSetPresenceDND();
  const on = isPresenceDND(mine.data);
  const close = () => openPresenceDialog(null);
  const pick = (until: string | null) =>
    dnd.mutate(until, {
      onSuccess: () => {
        toast.success(until ? `Notifications paused ${presenceUntilLabel(until)}.` : "Notifications resumed.");
        close();
      },
      onError: (e) => toast.error(errorMessage(e, "Couldn't change notifications.")),
    });
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      title={on ? `Paused ${presenceUntilLabel(mine.data?.dnd_until)}` : "Pause notifications"}
      size="sm"
      info="While paused you get no push notifications. Messages and badges still arrive in the app."
    >
      <ul className="flex flex-col gap-2">
        {on && (
          <li>
            <Button fullWidth variant="primary" loading={dnd.isPending && dnd.variables === null} onClick={() => pick(null)}>
              Resume notifications
            </Button>
          </li>
        )}
        {PRESENCE_DND_OPTIONS.map((o) => (
          <li key={o.label}>
            <Button fullWidth variant="secondary" className={cn(dnd.isPending && "pointer-events-none")} onClick={() => pick(presenceDndUntil(o))}>
              {o.label}
            </Button>
          </li>
        ))}
      </ul>
    </Dialog>
  );
}

/** Both presence dialogs, opened from the account menu (`openPresenceDialog`). Mounted once per session. */
export function PresenceDialogs() {
  const open = usePresenceDialog();
  return (
    <>
      <StatusDialog open={open === "status"} />
      <DndDialog open={open === "dnd"} />
    </>
  );
}
