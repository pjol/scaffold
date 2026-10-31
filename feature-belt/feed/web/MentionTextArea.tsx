"use client";

import { insertMention, mentionQueryAt, useFeedPeople, type FeedAuthor } from "@scaffoldapp/shared";
import { cn, Spinner, TextArea } from "@scaffoldapp/ui-web";
import { forwardRef, useEffect, useId, useImperativeHandle, useRef, useState, type KeyboardEvent } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";

export type MentionPerson = Pick<FeedAuthor, "id" | "display_name">;

export interface MentionTextAreaProps {
  value: string;
  onChange: (value: string) => void;
  /** People picked from suggestions; the composer turns them into mentions on submit. */
  people: MentionPerson[];
  onPeopleChange: (people: MentionPerson[]) => void;
  placeholder?: string;
  "aria-label"?: string;
  maxLength?: number;
  minRows?: number;
  maxRows?: number;
  autoFocus?: boolean;
  disabled?: boolean;
  /** ⌘/Ctrl + Enter. */
  onSubmit?: () => void;
  className?: string;
}

/** A growing text area that suggests people after "@" (arrow keys, Enter or Tab to pick, Escape to dismiss). */
export const MentionTextArea = forwardRef<HTMLTextAreaElement | null, MentionTextAreaProps>(function MentionTextArea(
  { value, onChange, people, onPeopleChange, placeholder, maxLength, minRows = 2, maxRows = 12, autoFocus, disabled, onSubmit, className, ...rest },
  ref,
) {
  const inner = useRef<HTMLTextAreaElement | null>(null);
  useImperativeHandle(ref, () => inner.current as HTMLTextAreaElement, []);
  const listId = useId();
  const [caret, setCaret] = useState(0);
  const [dismissed, setDismissed] = useState<number | null>(null);
  const [active, setActive] = useState(0);
  const mention = mentionQueryAt(value, caret);
  const open = !!mention && mention.query.length > 0 && dismissed !== mention.start;
  const suggestions = useFeedPeople(open ? mention.query : "");
  const items = open ? (suggestions.data?.items ?? []) : [];
  const pendingCaret = useRef<number | null>(null);

  useEffect(() => setActive(0), [mention?.query]);
  useEffect(() => {
    if (pendingCaret.current === null || !inner.current) return;
    const pos = pendingCaret.current;
    pendingCaret.current = null;
    inner.current.focus();
    inner.current.setSelectionRange(pos, pos);
    setCaret(pos);
  }, [value]);

  const pick = (person: FeedAuthor) => {
    if (!mention) return;
    const next = insertMention(value, mention.start, caret, person);
    pendingCaret.current = next.caret;
    if (!people.some((p) => p.id === person.id)) onPeopleChange([...people, { id: person.id, display_name: person.display_name }]);
    onChange(next.text);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (onSubmit && e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      onSubmit();
      return;
    }
    if (!open || items.length === 0) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setActive((a) => (a + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length);
    } else if (e.key === "Enter" || e.key === "Tab") {
      const person = items[active];
      if (person) {
        e.preventDefault();
        pick(person);
      }
    } else if (e.key === "Escape" && mention) {
      e.preventDefault();
      setDismissed(mention.start);
    }
  };

  const track = () => setCaret(inner.current?.selectionStart ?? value.length);

  return (
    <div className={cn("relative", className)}>
      <TextArea
        {...rest}
        ref={inner}
        value={value}
        placeholder={placeholder}
        maxLength={maxLength}
        minRows={minRows}
        maxRows={maxRows}
        autoFocus={autoFocus}
        disabled={disabled}
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={open && items.length > 0}
        aria-controls={listId}
        aria-activedescendant={open && items[active] ? `${listId}-${items[active].id}` : undefined}
        onChange={(e) => {
          onChange(e.target.value);
          setCaret(e.target.selectionStart ?? e.target.value.length);
        }}
        onSelect={track}
        onClick={track}
        onKeyUp={track}
        onKeyDown={onKeyDown}
      />
      {open && (items.length > 0 || suggestions.isFetching) ? (
        <ul
          id={listId}
          role="listbox"
          aria-label="People"
          className="absolute inset-x-0 top-full z-20 mt-1 flex max-h-72 flex-col overflow-y-auto rounded-md border border-border bg-surface-raised p-1 shadow-md"
        >
          {items.length === 0 ? (
            <li className="flex min-h-control items-center justify-center">
              <Spinner size={16} label="Finding people" />
            </li>
          ) : (
            items.map((p, i) => (
              <li
                key={p.id}
                id={`${listId}-${p.id}`}
                role="option"
                aria-selected={i === active}
                onMouseDown={(e) => {
                  e.preventDefault();
                  pick(p);
                }}
                onMouseEnter={() => setActive(i)}
                className={cn("flex min-h-control cursor-pointer items-center gap-3 rounded-sm px-2 type-label text-fg pointer-coarse:min-h-tap", i === active && "bg-sunken")}
              >
                <AppAvatar name={p.display_name} src={p.avatar_url} size="xs" />
                <span className="truncate">{p.display_name}</span>
              </li>
            ))
          )}
        </ul>
      ) : null}
    </div>
  );
});
