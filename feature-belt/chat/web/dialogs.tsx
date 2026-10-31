"use client";

import { CHAT_MUTE_OPTIONS, CHAT_QUICK_REACTIONS, muteRequest } from "@scaffoldapp/shared";
import { Button, Dialog, Field, Input } from "@scaffoldapp/ui-web";
import { useEffect, useState } from "react";

/** Mute notifications for a while or until unmuted. */
export function MuteDialog({ open, onOpenChange, onMute }: { open: boolean; onOpenChange: (open: boolean) => void; onMute: (v: { until: string } | { forever: true }) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Mute notifications" size="sm" info="New messages still show in the conversation and its unread count.">
      <ul className="flex flex-col gap-2">
        {CHAT_MUTE_OPTIONS.map((o) => (
          <li key={o.label}>
            <Button
              fullWidth
              variant="secondary"
              onClick={() => {
                onMute(muteRequest(o.minutes));
                onOpenChange(false);
              }}
            >
              {o.label}
            </Button>
          </li>
        ))}
      </ul>
    </Dialog>
  );
}

/** Contains a pictograph and no letters: what the backend accepts as a reaction. */
function looksLikeEmoji(value: string): boolean {
  return value.length > 0 && value.length <= 32 && /\p{Extended_Pictographic}|\p{Regional_Indicator}/u.test(value) && !/\p{L}|\s/u.test(value);
}

/** Pick a quick reaction or type any emoji. */
export function EmojiDialog({ open, onOpenChange, onPick }: { open: boolean; onOpenChange: (open: boolean) => void; onPick: (emoji: string) => void }) {
  const [value, setValue] = useState("");
  useEffect(() => {
    if (open) setValue("");
  }, [open]);
  const trimmed = value.trim();
  const valid = looksLikeEmoji(trimmed);
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add a reaction"
      size="sm"
      footer={
        <Button disabled={!valid} onClick={() => onPick(trimmed)}>
          React
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        <ul className="grid grid-cols-6 gap-2" aria-label="Quick reactions">
          {CHAT_QUICK_REACTIONS.map((e) => (
            <li key={e}>
              <button
                type="button"
                onClick={() => onPick(e)}
                aria-label={`React with ${e}`}
                className="flex size-tap w-full items-center justify-center rounded-md type-title transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
              >
                {e}
              </button>
            </li>
          ))}
        </ul>
        <Field label="Any emoji" error={trimmed && !valid ? "Enter an emoji." : undefined}>
          <Input
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && valid) onPick(trimmed);
            }}
            placeholder="Type or paste an emoji"
          />
        </Field>
      </div>
    </Dialog>
  );
}
