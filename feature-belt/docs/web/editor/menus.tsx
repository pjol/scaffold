"use client";

import { DOCS_SLASH_ITEMS, docsFilterSlash, docsFormatDate, docsSafeLink, docsTitle, docsToday, useAccessDirectory, useDocsSearch, type DocsMention, type DocsSlashItem } from "@scaffoldapp/shared";
import { Avatar, Button, Input, cn } from "@scaffoldapp/ui-web";
import {
  Bold,
  Calendar,
  CheckSquare,
  ChevronRight,
  Code,
  Database,
  FileText,
  Globe,
  Heading1,
  Heading2,
  Heading3,
  Image as ImageIcon,
  Italic,
  Link2,
  List,
  ListOrdered,
  MessageSquarePlus,
  Minus,
  Paperclip,
  Quote,
  Strikethrough,
  Table,
  Text,
  Underline,
  UserRound,
  Lightbulb,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Floating, floatingItemClass } from "../Floating";

const SLASH_ICON: Record<string, ReactNode> = {
  paragraph: <Text />,
  heading_1: <Heading1 />,
  heading_2: <Heading2 />,
  heading_3: <Heading3 />,
  bulleted_list: <List />,
  numbered_list: <ListOrdered />,
  to_do: <CheckSquare />,
  toggle: <ChevronRight />,
  quote: <Quote />,
  callout: <Lightbulb />,
  divider: <Minus />,
  page: <FileText />,
  "mention-page": <Link2 />,
  database: <Database />,
  image: <ImageIcon />,
  file: <Paperclip />,
  bookmark: <Globe />,
  code: <Code />,
  table: <Table />,
  "mention-person": <UserRound />,
  date: <Calendar />,
};

export function slashIcon(id: string): ReactNode {
  return SLASH_ICON[id] ?? <Text />;
}

export interface CaretMenuState {
  blockId: string;
  /** Offset of the trigger character ("/" or "@") in the block's text. */
  start: number;
  query: string;
  rect: DOMRect;
  index: number;
}

/** The "/" menu: filters as you type; the editor drives the selection with the arrow keys. */
export function SlashMenu({ state, items, onPick, onClose, onHover }: { state: CaretMenuState; items: DocsSlashItem[]; onPick: (item: DocsSlashItem) => void; onClose: () => void; onHover: (i: number) => void }) {
  const listRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${state.index}"]`)?.scrollIntoView({ block: "nearest" });
  }, [state.index]);
  let group = "";
  return (
    <Floating open anchor={state.rect} onClose={onClose} role="listbox" label="Insert a block" className="w-72">
      <div ref={listRef} className="flex max-h-80 flex-col overflow-y-auto" id="docs-slash-list">
        {items.length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">No blocks match.</p> : null}
        {items.map((it, i) => {
          const head = it.group !== group ? (group = it.group) : null;
          return (
            <div key={it.id} className="flex flex-col">
              {head ? <p className="px-3 pt-2 pb-1 type-caption font-semibold text-fg-muted">{head}</p> : null}
              <button
                type="button"
                role="option"
                id={`docs-slash-${it.id}`}
                data-index={i}
                aria-selected={i === state.index}
                tabIndex={-1}
                className={floatingItemClass}
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => onHover(i)}
                onClick={() => onPick(it)}
              >
                <span aria-hidden className="flex size-8 items-center justify-center rounded-sm border border-border bg-surface text-fg-muted">
                  {slashIcon(it.id)}
                </span>
                <span className="min-w-0 flex-1 truncate">{it.label}</span>
                {it.hint ? <span className="type-caption text-fg-subtle">{it.hint}</span> : null}
              </button>
            </div>
          );
        })}
      </div>
    </Floating>
  );
}

export function filteredSlash(query: string, opts: { canCreatePages: boolean }): DocsSlashItem[] {
  const items = DOCS_SLASH_ITEMS.filter((i) => opts.canCreatePages || (i.action !== "page" && i.action !== "database"));
  return docsFilterSlash(query, items);
}

export interface MentionChoice {
  key: string;
  label: string;
  mention: DocsMention;
  /** Text the chip stores. */
  text: string;
  icon: ReactNode;
  group: "People" | "Pages" | "Dates";
}

function dateChoice(date: string, label: string): MentionChoice {
  return { key: `d:${date}:${label}`, label, mention: { type: "date", date }, text: docsFormatDate(date), icon: <Calendar />, group: "Dates" };
}

/** People, pages and dates for "@". */
export function useMentionChoices(orgId: string, query: string, mode: "all" | "people" | "pages" | "dates", pageId: string): MentionChoice[] {
  const people = useAccessDirectory(orgId, query, { types: "user", limit: 6, enabled: mode === "all" || mode === "people" });
  const pages = useDocsSearch(orgId, query, { limit: 6, enabled: mode === "all" || mode === "pages" });
  return useMemo(() => {
    const out: MentionChoice[] = [];
    if (mode === "all" || mode === "people") {
      for (const p of people.data ?? []) {
        out.push({ key: `u:${p.id}`, label: p.name, mention: { type: "user", id: p.id }, text: `@${p.name}`, icon: <Avatar size="xs" name={p.name} {...(p.avatar_url ? { src: p.avatar_url } : {})} />, group: "People" });
      }
    }
    if (mode === "all" || mode === "pages") {
      for (const p of pages.data ?? []) {
        if (p.id === pageId) continue;
        out.push({ key: `p:${p.id}`, label: docsTitle(p.title), mention: { type: "page", id: p.id }, text: docsTitle(p.title), icon: p.icon ? <span className="type-body">{p.icon}</span> : <FileText />, group: "Pages" });
      }
    }
    if (mode === "all" || mode === "dates") {
      const q = query.trim().toLowerCase();
      const dates: MentionChoice[] = [dateChoice(docsToday(), "Today"), dateChoice(docsToday(1), "Tomorrow"), dateChoice(docsToday(-1), "Yesterday")];
      if (/^\d{4}-\d{2}-\d{2}$/.test(q)) dates.unshift(dateChoice(q, docsFormatDate(q)));
      out.push(...dates.filter((d) => !q || d.label.toLowerCase().startsWith(q) || d.mention.type === "date" && d.mention.date === q || mode === "dates"));
    }
    return out;
  }, [people.data, pages.data, mode, query, pageId]);
}

export function MentionMenu({ state, choices, onPick, onClose, onHover }: { state: CaretMenuState; choices: MentionChoice[]; onPick: (c: MentionChoice) => void; onClose: () => void; onHover: (i: number) => void }) {
  let group = "";
  return (
    <Floating open anchor={state.rect} onClose={onClose} role="listbox" label="Mention" className="w-72">
      <div className="flex max-h-80 flex-col overflow-y-auto">
        {choices.length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">No matches.</p> : null}
        {choices.map((c, i) => {
          const head = c.group !== group ? (group = c.group) : null;
          return (
            <div key={c.key} className="flex flex-col">
              {head ? <p className="px-3 pt-2 pb-1 type-caption font-semibold text-fg-muted">{head}</p> : null}
              <button
                type="button"
                role="option"
                aria-selected={i === state.index}
                tabIndex={-1}
                className={floatingItemClass}
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => onHover(i)}
                onClick={() => onPick(c)}
              >
                <span aria-hidden className="flex size-6 items-center justify-center text-fg-muted">
                  {c.icon}
                </span>
                <span className="min-w-0 flex-1 truncate">{c.label}</span>
              </button>
            </div>
          );
        })}
      </div>
    </Floating>
  );
}

export interface ToolbarState {
  blockId: string;
  start: number;
  end: number;
  rect: DOMRect;
}

function ToolButton({ label, active, onClick, children }: { label: string; active?: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-label={label}
      aria-pressed={active}
      title={label}
      onMouseDown={(e) => e.preventDefault()}
      onClick={onClick}
      className={cn(
        "flex size-8 items-center justify-center rounded-sm text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring [&_svg]:size-4 pointer-coarse:size-11",
        active && "bg-primary-soft text-primary-strong",
      )}
    >
      {children}
    </button>
  );
}

/** Formatting for a selection: bold, italic, underline, strike, code, link, comment. */
export function InlineToolbar({
  state,
  editable,
  canComment,
  active,
  onMark,
  onLink,
  onComment,
  onClose,
}: {
  state: ToolbarState;
  editable: boolean;
  canComment: boolean;
  active: { bold: boolean; italic: boolean; underline: boolean; strike: boolean; code: boolean; link: boolean };
  onMark: (m: "bold" | "italic" | "underline" | "strike" | "code") => void;
  onLink: () => void;
  onComment: () => void;
  onClose: () => void;
}) {
  const mod = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘" : "Ctrl+";
  return (
    <Floating open anchor={state.rect} onClose={onClose} placement="top-start" role="toolbar" label="Format text" className="flex items-center gap-0.5">
      {editable ? (
        <>
          <ToolButton label={`Bold (${mod}B)`} active={active.bold} onClick={() => onMark("bold")}>
            <Bold aria-hidden />
          </ToolButton>
          <ToolButton label={`Italic (${mod}I)`} active={active.italic} onClick={() => onMark("italic")}>
            <Italic aria-hidden />
          </ToolButton>
          <ToolButton label={`Underline (${mod}U)`} active={active.underline} onClick={() => onMark("underline")}>
            <Underline aria-hidden />
          </ToolButton>
          <ToolButton label={`Strikethrough (${mod}Shift+S)`} active={active.strike} onClick={() => onMark("strike")}>
            <Strikethrough aria-hidden />
          </ToolButton>
          <ToolButton label={`Code (${mod}E)`} active={active.code} onClick={() => onMark("code")}>
            <Code aria-hidden />
          </ToolButton>
          <ToolButton label={`Link (${mod}K)`} active={active.link} onClick={onLink}>
            <Link2 aria-hidden />
          </ToolButton>
        </>
      ) : null}
      {canComment ? (
        <>
          {editable ? <span aria-hidden className="mx-1 h-5 w-px bg-border" /> : null}
          <ToolButton label="Comment" onClick={onComment}>
            <MessageSquarePlus aria-hidden />
          </ToolButton>
        </>
      ) : null}
    </Floating>
  );
}

/** Add or change a link on the selection. */
export function LinkForm({ anchor, initial, onApply, onRemove, onClose }: { anchor: DOMRect; initial: string; onApply: (href: string) => void; onRemove?: (() => void) | undefined; onClose: () => void }) {
  const [value, setValue] = useState(initial);
  const [error, setError] = useState<string | null>(null);
  return (
    <Floating open anchor={anchor} onClose={onClose} autoFocus label="Link" className="w-80 p-3">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          const href = docsSafeLink(value);
          if (!href) return setError("Enter a web address or an email.");
          onApply(href);
        }}
      >
        <Input value={value} onChange={(e) => setValue(e.target.value)} placeholder="Paste or type a link" aria-label="Link" aria-invalid={!!error} data-autofocus />
        {error ? <p className="type-caption text-danger-strong">{error}</p> : null}
        <div className="flex justify-end gap-2">
          {onRemove ? (
            <Button type="button" variant="ghost" size="sm" onClick={onRemove}>
              Remove link
            </Button>
          ) : null}
          <Button type="submit" size="sm">
            Apply
          </Button>
        </div>
      </form>
    </Floating>
  );
}

/** A bookmark: the URL and a title the person types (nothing is fetched). */
export function BookmarkForm({ anchor, onSave, onClose }: { anchor: DOMRect; onSave: (url: string, title: string) => void; onClose: () => void }) {
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [error, setError] = useState<string | null>(null);
  return (
    <Floating open anchor={anchor} onClose={onClose} autoFocus label="Bookmark" className="w-80 p-3">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          const href = docsSafeLink(url);
          if (!href || href.startsWith("mailto:")) return setError("Enter a link starting with http:// or https://.");
          onSave(href, title.trim());
        }}
      >
        <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://" aria-label="Link" aria-invalid={!!error} data-autofocus />
        <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Title (optional)" aria-label="Title" maxLength={300} />
        {error ? <p className="type-caption text-danger-strong">{error}</p> : null}
        <div className="flex justify-end">
          <Button type="submit" size="sm">
            Add bookmark
          </Button>
        </div>
      </form>
    </Floating>
  );
}
