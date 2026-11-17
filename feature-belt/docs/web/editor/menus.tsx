"use client";

import {
  DOCS_COLOR_HUES,
  DOCS_SLASH_ITEMS,
  docsColorLabel,
  docsEmbedFor,
  docsFilterSlash,
  docsFormatDate,
  docsSafeLink,
  docsTitle,
  docsToday,
  useAccessDirectory,
  useDocsSearch,
  type DocsColor,
  type DocsMention,
  type DocsSlashItem,
} from "@scaffoldapp/shared";
import { Avatar, Button, Checkbox, Input, Select, cn } from "@scaffoldapp/ui-web";
import {
  AppWindow,
  ArrowUpRight,
  Baseline,
  Bell,
  Bold,
  Calendar,
  CheckSquare,
  ChevronRight,
  Code,
  Columns2,
  Columns3,
  Columns4,
  Copy,
  CornerDownRight,
  Database,
  FileText,
  FileVideo,
  Globe,
  Heading1,
  Heading2,
  Heading3,
  Heading4,
  Image as ImageIcon,
  Italic,
  Link2,
  List,
  ListOrdered,
  ListTree,
  MessageSquarePlus,
  Minus,
  Music,
  Navigation,
  Paperclip,
  Plus,
  Quote,
  RefreshCw,
  Repeat2,
  Sigma,
  Smile,
  SquareMousePointer,
  Strikethrough,
  Table,
  Text,
  Trash2,
  Underline,
  UserRound,
  Lightbulb,
  Workflow,
  FileType,
  Palette,
  History,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { EquationBody } from "./render";
import { Floating, floatingItemClass } from "../Floating";
import { searchEmoji } from "../EmojiPicker";
import { colorClass } from "./colors";

const SLASH_ICON: Record<string, ReactNode> = {
  paragraph: <Text />,
  heading_1: <Heading1 />,
  heading_2: <Heading2 />,
  heading_3: <Heading3 />,
  heading_4: <Heading4 />,
  bulleted_list: <List />,
  numbered_list: <ListOrdered />,
  to_do: <CheckSquare />,
  toggle: <ChevronRight />,
  toggle_heading_1: <Heading1 />,
  toggle_heading_2: <Heading2 />,
  toggle_heading_3: <Heading3 />,
  quote: <Quote />,
  callout: <Lightbulb />,
  divider: <Minus />,
  page: <FileText />,
  link_to_page: <ArrowUpRight />,
  "mention-page": <Link2 />,
  database: <Database />,
  image: <ImageIcon />,
  video: <FileVideo />,
  audio: <Music />,
  pdf: <FileType />,
  file: <Paperclip />,
  bookmark: <Globe />,
  embed: <AppWindow />,
  code: <Code />,
  mermaid: <Workflow />,
  table: <Table />,
  table_of_contents: <ListTree />,
  equation: <Sigma />,
  "inline-equation": <Sigma />,
  button: <SquareMousePointer />,
  breadcrumb: <Navigation />,
  "columns-2": <Columns2 />,
  "columns-3": <Columns3 />,
  "columns-4": <Columns4 />,
  "columns-5": <Columns4 />,
  synced: <RefreshCw />,
  "mention-person": <UserRound />,
  date: <Calendar />,
  reminder: <Bell />,
  emoji: <Smile />,
  "act-duplicate": <Copy />,
  "act-delete": <Trash2 />,
  "act-move": <CornerDownRight />,
  "act-comment": <MessageSquarePlus />,
};

export function slashIcon(id: string): ReactNode {
  if (id.startsWith("database-")) return <Database />;
  if (id.startsWith("embed-")) return <AppWindow />;
  if (id.startsWith("turn-")) return SLASH_ICON[id.slice(5)] ?? <Repeat2 />;
  if (id.startsWith("color-")) return <Palette />;
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

function ColorDot({ color }: { color: string | null }) {
  if (!color) return <span aria-hidden className="flex size-5 items-center justify-center rounded-xs border border-border type-label font-semibold text-fg">A</span>;
  const bg = color.endsWith("_background");
  return <span aria-hidden className={cn("flex size-5 items-center justify-center rounded-xs border border-border type-label font-semibold", colorClass(color as DocsColor, true), !bg && "bg-surface")}>A</span>;
}

/** The "/" menu: grouped, filtered as you type (aliases and fuzzy); the editor drives the selection with the arrow keys. */
export function SlashMenu({ state, items, onPick, onClose, onHover }: { state: CaretMenuState; items: DocsSlashItem[]; onPick: (item: DocsSlashItem) => void; onClose: () => void; onHover: (i: number) => void }) {
  const listRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${state.index}"]`)?.scrollIntoView({ block: "nearest" });
  }, [state.index]);
  let group = "";
  return (
    <Floating open anchor={state.rect} onClose={onClose} role="listbox" label="Insert a block" className="w-[min(20rem,calc(100vw-2rem))]">
      <div ref={listRef} className="flex max-h-[min(24rem,60dvh)] flex-col overflow-y-auto" id="docs-slash-list">
        {items.length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">No commands match.</p> : null}
        {items.map((it, i) => {
          const head = it.group !== group ? (group = it.group) : null;
          return (
            <div key={`${it.group}:${it.id}`} className="flex flex-col">
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
                <span aria-hidden className="flex size-8 shrink-0 items-center justify-center rounded-sm border border-border bg-surface text-fg-muted">
                  {it.action === "color" ? <ColorDot color={(it.arg as string | null) ?? null} /> : slashIcon(it.id)}
                </span>
                <span className="min-w-0 flex-1 truncate">{it.label}</span>
                {it.hint ? <span className="shrink-0 type-caption text-fg-subtle">{it.hint}</span> : null}
              </button>
            </div>
          );
        })}
      </div>
    </Floating>
  );
}

const RECENT_KEY = "scaffoldapp.docs.slash.recent";

export function recentSlash(): string[] {
  try {
    return JSON.parse(window.localStorage.getItem(RECENT_KEY) ?? "[]") as string[];
  } catch {
    return [];
  }
}

export function rememberSlash(id: string): void {
  try {
    window.localStorage.setItem(RECENT_KEY, JSON.stringify([id, ...recentSlash().filter((x) => x !== id)].slice(0, 8)));
  } catch {
    // Private mode: no recent list.
  }
}

export function filteredSlash(query: string, opts: { canCreatePages: boolean; recent: string[]; hasBlockActions: boolean }): DocsSlashItem[] {
  const items = DOCS_SLASH_ITEMS.filter(
    (i) => (opts.canCreatePages || !["page", "database", "synced"].includes(i.action ?? "")) && (opts.hasBlockActions || !["duplicate", "delete", "move", "comment", "turn", "color"].includes(i.action ?? "")),
  );
  return docsFilterSlash(query, items, opts.recent);
}

export interface MentionChoice {
  key: string;
  label: string;
  mention: DocsMention;
  /** Text the chip stores. */
  text: string;
  icon: ReactNode;
  group: "People" | "Pages" | "Dates" | "New page";
  /** A page to create with this title, then link. */
  create?: string;
  /** Opens the date and reminder form. */
  custom?: boolean;
}

function dateChoice(date: string, label: string): MentionChoice {
  return { key: `d:${date}:${label}`, label, mention: { type: "date", date }, text: docsFormatDate(date), icon: <Calendar />, group: "Dates" };
}

/** People, pages and dates for "@"; pages (and "create") for "[[" and "+". */
export function useMentionChoices(orgId: string, query: string, mode: "all" | "people" | "pages" | "dates" | "link", pageId: string): MentionChoice[] {
  const pagesMode = mode === "pages" || mode === "link";
  const people = useAccessDirectory(orgId, query, { types: "user", limit: 6, enabled: mode === "all" || mode === "people" });
  const pages = useDocsSearch(orgId, query, { limit: 6, enabled: mode === "all" || pagesMode });
  return useMemo(() => {
    const out: MentionChoice[] = [];
    if (mode === "all" || mode === "people") {
      for (const p of people.data ?? []) {
        out.push({ key: `u:${p.id}`, label: p.name, mention: { type: "user", id: p.id }, text: `@${p.name}`, icon: <Avatar size="xs" name={p.name} {...(p.avatar_url ? { src: p.avatar_url } : {})} />, group: "People" });
      }
    }
    if (mode === "all" || pagesMode) {
      for (const p of pages.data ?? []) {
        if (p.id === pageId) continue;
        out.push({ key: `p:${p.id}`, label: docsTitle(p.title), mention: { type: "page", id: p.id }, text: docsTitle(p.title), icon: p.icon ? <span className="type-body">{p.icon}</span> : <FileText />, group: "Pages" });
      }
    }
    if (mode === "link" && query.trim()) {
      out.push({ key: `new:${query}`, label: `Create “${query.trim()}”`, mention: { type: "page", id: "" }, text: query.trim(), icon: <Plus />, group: "New page", create: query.trim() });
    }
    if (mode === "all" || mode === "dates") {
      const q = query.trim().toLowerCase();
      const dates: MentionChoice[] = [dateChoice(docsToday(), "Today"), dateChoice(docsToday(1), "Tomorrow"), dateChoice(docsToday(-1), "Yesterday")];
      if (/^\d{4}-\d{2}-\d{2}$/.test(q)) dates.unshift(dateChoice(q, docsFormatDate(q)));
      out.push(...dates.filter((d) => !q || d.label.toLowerCase().startsWith(q) || (d.mention.type === "date" && d.mention.date === q) || mode === "dates"));
      if (!q || "remind".startsWith(q.slice(0, 6)) || "date".startsWith(q) || mode === "dates") {
        out.push({ key: "d:custom", label: "Pick a date or a reminder…", mention: { type: "date", date: docsToday() }, text: "", icon: <Bell />, group: "Dates", custom: true });
      }
    }
    return out;
  }, [people.data, pages.data, mode, query, pageId, pagesMode]);
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

/** ":" emoji shortcodes. */
export function EmojiMenu({ state, onPick, onClose, onHover }: { state: CaretMenuState; onPick: (emoji: string) => void; onClose: () => void; onHover: (i: number) => void }) {
  const list = searchEmoji(state.query, 8);
  if (!list.length) return null;
  return (
    <Floating open anchor={state.rect} onClose={onClose} role="listbox" label="Emoji" className="w-64">
      <div className="flex flex-col">
        {list.map((e, i) => (
          <button
            key={e.emoji}
            type="button"
            role="option"
            aria-selected={i === state.index}
            tabIndex={-1}
            className={floatingItemClass}
            onMouseDown={(ev) => ev.preventDefault()}
            onMouseEnter={() => onHover(i)}
            onClick={() => onPick(e.emoji)}
          >
            <span aria-hidden className="type-title-sm">
              {e.emoji}
            </span>
            <span className="min-w-0 flex-1 truncate type-label text-fg-muted">:{e.name}</span>
          </button>
        ))}
      </div>
    </Floating>
  );
}

export function emojiCount(query: string): number {
  return searchEmoji(query, 8).length;
}

export function emojiAt(query: string, index: number): string | undefined {
  return searchEmoji(query, 8)[index]?.emoji;
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

/** Text colours and highlights (the swatch set), plus the last one used. */
export function ColorPalette({ current, last, onPick }: { current: DocsColor | null; last?: DocsColor | null; onPick: (c: DocsColor | null) => void }) {
  const item = (c: DocsColor | null, key: string) => (
    <button
      key={key}
      type="button"
      role="menuitemradio"
      aria-checked={current === c}
      onMouseDown={(e) => e.preventDefault()}
      onClick={() => onPick(c)}
      className={cn(floatingItemClass, "min-h-9")}
    >
      <ColorDot color={c} />
      <span className="min-w-0 flex-1 truncate">{docsColorLabel(c)}</span>
    </button>
  );
  return (
    <div role="menu" className="flex max-h-[min(24rem,60dvh)] w-56 flex-col overflow-y-auto">
      {last ? (
        <>
          <p className="flex items-center gap-1 px-3 pt-2 pb-1 type-caption font-semibold text-fg-muted">
            <History aria-hidden className="size-3" />
            Last used
          </p>
          {item(last, `last-${last}`)}
        </>
      ) : null}
      <p className="px-3 pt-2 pb-1 type-caption font-semibold text-fg-muted">Text colour</p>
      {item(null, "default")}
      {DOCS_COLOR_HUES.map((h) => item(h, h))}
      <p className="px-3 pt-2 pb-1 type-caption font-semibold text-fg-muted">Background</p>
      {DOCS_COLOR_HUES.map((h) => item(`${h}_background` as DocsColor, `${h}-bg`))}
    </div>
  );
}

/** Formatting for a selection: bold, italic, underline, strike, code, link, equation, colour, comment. */
export function InlineToolbar({
  state,
  editable,
  canComment,
  active,
  color,
  lastColor,
  onMark,
  onLink,
  onEquation,
  onColor,
  onComment,
  onClose,
}: {
  state: ToolbarState;
  editable: boolean;
  canComment: boolean;
  active: { bold: boolean; italic: boolean; underline: boolean; strike: boolean; code: boolean; link: boolean; equation: boolean };
  color: DocsColor | null;
  lastColor: DocsColor | null;
  onMark: (m: "bold" | "italic" | "underline" | "strike" | "code") => void;
  onLink: () => void;
  onEquation: () => void;
  onColor: (c: DocsColor | null) => void;
  onComment: () => void;
  onClose: () => void;
}) {
  const mod = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘" : "Ctrl+";
  const [palette, setPalette] = useState<HTMLElement | null>(null);
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
          <ToolButton label={`Equation (${mod}Shift+E)`} active={active.equation} onClick={onEquation}>
            <Sigma aria-hidden />
          </ToolButton>
          <ToolButton label={`Link (${mod}K)`} active={active.link} onClick={onLink}>
            <Link2 aria-hidden />
          </ToolButton>
          <button
            type="button"
            aria-label={`Colour: ${docsColorLabel(color)}`}
            title="Colour"
            aria-haspopup="menu"
            onMouseDown={(e) => e.preventDefault()}
            onClick={(e) => setPalette(palette ? null : e.currentTarget)}
            className="flex size-8 items-center justify-center rounded-sm hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:size-11"
          >
            {color ? <ColorDot color={color} /> : <Baseline aria-hidden className="size-4 text-fg" />}
          </button>
          <Floating open={!!palette} anchor={palette} onClose={() => setPalette(null)} label="Colour" role="menu">
            <ColorPalette
              current={color}
              last={lastColor}
              onPick={(c) => {
                setPalette(null);
                onColor(c);
              }}
            />
          </Floating>
        </>
      ) : null}
      {canComment ? (
        <>
          {editable ? <span aria-hidden className="mx-1 h-5 w-px bg-border" /> : null}
          <ToolButton label={`Comment (${mod}Shift+M)`} onClick={onComment}>
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

/** A web bookmark (the server fetches its title, description and images) or an embed (allowlisted providers). */
export function UrlForm({ anchor, mode, busy, onSave, onClose }: { anchor: DOMRect; mode: "bookmark" | "embed"; busy?: boolean; onSave: (url: string, as: "bookmark" | "embed") => void; onClose: () => void }) {
  const [url, setUrl] = useState("");
  const [error, setError] = useState<{ text: string; bookmark?: boolean } | null>(null);
  return (
    <Floating open anchor={anchor} onClose={onClose} autoFocus label={mode === "embed" ? "Embed" : "Bookmark"} className="w-[min(22rem,calc(100vw-2rem))] p-3">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          const href = docsSafeLink(url);
          if (!href || href.startsWith("mailto:")) return setError({ text: "Enter a link starting with http:// or https://." });
          if (mode === "embed" && !docsEmbedFor(href)) return setError({ text: "That site can't be shown here. Add it as a bookmark instead.", bookmark: true });
          onSave(href, mode);
        }}
      >
        <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder={mode === "embed" ? "Paste a YouTube, Figma, Loom, Google Docs… link" : "Paste a link"} aria-label="Link" aria-invalid={!!error} data-autofocus />
        {error ? (
          <p className="type-caption text-danger-strong">
            {error.text}{" "}
            {error.bookmark ? (
              <button type="button" className="underline underline-offset-2" onClick={() => onSave(docsSafeLink(url)!, "bookmark")}>
                Add bookmark
              </button>
            ) : null}
          </p>
        ) : null}
        <div className="flex justify-end">
          <Button type="submit" size="sm" loading={busy}>
            {mode === "embed" ? "Embed link" : "Add bookmark"}
          </Button>
        </div>
      </form>
    </Floating>
  );
}

const REMIND_OFFSETS: { value: string; label: string; minutes: number }[] = [
  { value: "0", label: "At the time", minutes: 0 },
  { value: "5", label: "5 minutes before", minutes: 5 },
  { value: "60", label: "1 hour before", minutes: 60 },
  { value: "1440", label: "1 day before", minutes: 1440 },
  { value: "2880", label: "2 days before", minutes: 2880 },
  { value: "10080", label: "1 week before", minutes: 10080 },
];

/** A date with an optional time, and a reminder that notifies you then. */
export function DateForm({ anchor, remind: remindDefault, onSave, onClose }: { anchor: DOMRect; remind: boolean; onSave: (m: Extract<DocsMention, { type: "date" }>) => void; onClose: () => void }) {
  const [date, setDate] = useState(docsToday(remindDefault ? 1 : 0));
  const [time, setTime] = useState(remindDefault ? "09:00" : "");
  const [remind, setRemind] = useState(remindDefault);
  const [offset, setOffset] = useState("0");
  const [error, setError] = useState<string | null>(null);
  return (
    <Floating open anchor={anchor} onClose={onClose} autoFocus label={remind ? "Reminder" : "Date"} className="w-[min(20rem,calc(100vw-2rem))] p-3">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return setError("Choose a date.");
          const m: Extract<DocsMention, { type: "date" }> = { type: "date", date };
          if (time) m.time = time;
          if (remind) {
            const [y, mo, d] = date.split("-").map(Number);
            const [h, mi] = (time || "09:00").split(":").map(Number);
            const at = new Date(y!, mo! - 1, d!, h!, mi!);
            at.setMinutes(at.getMinutes() - (REMIND_OFFSETS.find((o) => o.value === offset)?.minutes ?? 0));
            if (at.getTime() < Date.now()) return setError("That reminder time has passed.");
            m.remind_at = at.toISOString().replace(/\.\d{3}Z$/, "Z");
          }
          onSave(m);
        }}
      >
        <div className="flex gap-2">
          <Input type="date" value={date} onChange={(e) => setDate(e.target.value)} aria-label="Date" data-autofocus />
          <Input type="time" value={time} onChange={(e) => setTime(e.target.value)} aria-label="Time (optional)" />
        </div>
        <Checkbox checked={remind} onCheckedChange={(v) => setRemind(v === true)} label="Remind me" />
        {remind ? <Select aria-label="When to remind you" value={offset} onValueChange={setOffset} options={REMIND_OFFSETS.map((o) => ({ value: o.value, label: o.label }))} /> : null}
        {error ? <p className="type-caption text-danger-strong">{error}</p> : null}
        <div className="flex justify-end">
          <Button type="submit" size="sm">
            {remind ? "Set reminder" : "Insert date"}
          </Button>
        </div>
      </form>
    </Floating>
  );
}

/** Edit an inline equation's TeX with a live preview. */
export function EquationForm({ anchor, initial, onSave, onRemove, onClose }: { anchor: DOMRect; initial: string; onSave: (tex: string) => void; onRemove?: () => void; onClose: () => void }) {
  const [tex, setTex] = useState(initial);
  return (
    <Floating open anchor={anchor} onClose={onClose} autoFocus label="Equation" className="w-[min(24rem,calc(100vw-2rem))] p-3">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (tex.trim()) onSave(tex.trim());
          else onRemove?.();
        }}
      >
        <Input value={tex} onChange={(e) => setTex(e.target.value)} placeholder="x^2 + y^2 = z^2" aria-label="TeX" className="font-mono" data-autofocus />
        <div className="min-h-10 rounded-sm bg-sunken px-2">
          <EquationBody expression={tex} placeholder="Type TeX to see the equation" />
        </div>
        <div className="flex justify-end gap-2">
          {onRemove ? (
            <Button type="button" variant="ghost" size="sm" onClick={onRemove}>
              Remove
            </Button>
          ) : null}
          <Button type="submit" size="sm">
            Done
          </Button>
        </div>
      </form>
    </Floating>
  );
}

export interface PasteChoice {
  id: "mention" | "bookmark" | "embed" | "url";
  label: string;
  icon: ReactNode;
}

/** After pasting a link: keep it as a link, or make it a mention, bookmark or embed. */
export function PasteMenu({ anchor, choices, index, onPick, onClose, onHover }: { anchor: DOMRect; choices: PasteChoice[]; index: number; onPick: (c: PasteChoice) => void; onClose: () => void; onHover: (i: number) => void }) {
  return (
    <Floating open anchor={anchor} onClose={onClose} role="listbox" label="Paste as" className="w-56">
      <p className="px-3 pt-2 pb-1 type-caption font-semibold text-fg-muted">Paste as</p>
      {choices.map((c, i) => (
        <button
          key={c.id}
          type="button"
          role="option"
          aria-selected={i === index}
          tabIndex={-1}
          className={floatingItemClass}
          onMouseDown={(e) => e.preventDefault()}
          onMouseEnter={() => onHover(i)}
          onClick={() => onPick(c)}
        >
          {c.icon}
          <span className="min-w-0 flex-1 truncate">{c.label}</span>
        </button>
      ))}
    </Floating>
  );
}

export const PASTE_ICONS = { mention: <Link2 />, bookmark: <Globe />, embed: <AppWindow />, url: <Link2 /> } as const;
