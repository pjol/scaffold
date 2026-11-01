"use client";

import { IconButton, TextArea, cn } from "@scaffoldapp/ui-web";
import { ArrowUp, Square } from "lucide-react";
import { useRef, useState, type KeyboardEvent } from "react";

export interface ComposerProps {
  value: string;
  onChange: (value: string) => void;
  /** Sends the trimmed text; the owner clears `value` once the message is accepted. */
  onSend: (text: string) => Promise<boolean>;
  onStop: () => void;
  streaming: boolean;
  disabled?: boolean;
  maxChars: number;
}

/** Message box: Enter sends, Shift+Enter adds a line; Stop replaces Send while a reply streams. */
export function Composer({ value: text, onChange: setText, onSend, onStop, streaming, disabled = false, maxChars }: ComposerProps) {
  const [sending, setSending] = useState(false);
  const ref = useRef<HTMLTextAreaElement>(null);
  const trimmed = text.trim();
  const over = text.length > maxChars;
  const canSend = !disabled && !streaming && !sending && trimmed.length > 0 && !over;

  const send = async () => {
    if (!canSend) return;
    setSending(true);
    try {
      await onSend(trimmed);
    } finally {
      setSending(false);
      ref.current?.focus();
    }
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      void send();
    }
  };

  return (
    <div className="flex flex-col gap-1">
      <div className={cn("flex items-end gap-2 rounded-xl border border-border-strong bg-surface p-2 transition-[border-color,box-shadow] focus-within:border-ring focus-within:ring-1 focus-within:ring-ring", disabled && "opacity-60")}>
        <TextArea
          ref={ref}
          aria-label="Message"
          placeholder="Message the assistant"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKeyDown}
          disabled={disabled}
          minRows={1}
          maxRows={8}
          className="min-h-0 flex-1 resize-none border-0 bg-transparent px-2 py-1.5 shadow-none focus:border-0 focus:ring-0"
        />
        {streaming ? (
          <IconButton label="Stop reply" tooltip variant="secondary" icon={<Square />} onClick={onStop} />
        ) : (
          <IconButton label="Send" tooltip variant="primary" icon={<ArrowUp />} disabled={!canSend} loading={sending} onClick={() => void send()} />
        )}
      </div>
      {text.length > maxChars * 0.8 ? (
        <p className={cn("px-2 text-right type-caption tabular-nums", over ? "text-danger-strong" : "text-fg-muted")} aria-live="polite">
          {text.length}/{maxChars}
        </p>
      ) : null}
    </div>
  );
}
