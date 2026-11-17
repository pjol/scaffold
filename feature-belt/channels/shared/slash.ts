/**
 * Slash commands typed in the composer ("/remind me to … tomorrow"). Built-ins cover
 * reminders, membership, topic, calls, status, Do Not Disturb, polls, DMs, search and
 * collapsing media; other features add theirs with `registerChannelsSlashCommand` from the
 * app's stitching. Commands run on the client with the context the screen provides.
 */
import type { QueryClient } from "@tanstack/react-query";
import type { HttpClient } from "../api/http";
import { presenceEndpoints } from "../presence/api";
import { channelsEndpoints, type ChannelsEndpoints } from "./api";
import { channelsTimeZone, channelsWhen, isChannelsDirect } from "./format";
import { channelsKeys } from "./hooks";
import type { ChannelsDetail } from "./types";

export interface ChannelsSlashContext {
  /** Text after the command, with picked mentions encoded as `<@uuid>`. */
  args: string;
  channel: ChannelsDetail;
  /** Replying in this thread, or null. */
  threadRootId: string | null;
  meId: string;
  /** The app's API client: build any feature's endpoints with it. */
  http: HttpClient;
  api: ChannelsEndpoints;
  qc: QueryClient;
  navigate: (path: string) => void;
  notify: (message: string, tone?: "success" | "error") => void;
  /** Send a message in the current conversation (or thread). */
  send: (body: string) => Promise<unknown>;
  /** Replace the composer's text (e.g. leave a correction for the person). */
  setText: (text: string) => void;
  /** Open the poll composer, prefilled. */
  openPoll: (question: string) => void;
  /** Collapse or expand link previews and media in this conversation on this device. */
  setCollapsed: (collapsed: boolean) => void;
}

export interface ChannelsSlashCommand {
  /** Without the slash, lowercase: "remind". */
  name: string;
  /** "/remind me to … in 20 minutes". */
  usage: string;
  /** What it does, in a few words (autocomplete). */
  hint: string;
  /** Hide where it can't work (e.g. /invite in a DM). */
  available?: (channel: ChannelsDetail) => boolean;
  run: (ctx: ChannelsSlashContext) => Promise<void> | void;
}

const registry = new Map<string, ChannelsSlashCommand>();

/** Add (or replace) a command. Names are 2–24 lowercase letters, digits or dashes. */
export function registerChannelsSlashCommand(cmd: ChannelsSlashCommand): void {
  if (!/^[a-z][a-z0-9-]{1,23}$/.test(cmd.name)) throw new Error(`channels: invalid slash command name "${cmd.name}"`);
  registry.set(cmd.name, cmd);
}

/** Commands for this conversation whose name starts with `prefix`, built-ins first then by name. */
export function matchChannelsSlashCommands(prefix: string, channel: ChannelsDetail): ChannelsSlashCommand[] {
  const p = prefix.toLowerCase();
  return [...registry.values()].filter((c) => c.name.startsWith(p) && (c.available?.(channel) ?? true)).sort((a, b) => a.name.localeCompare(b.name));
}

/** "/remind me to x" → { name: "remind", args: "me to x" }; null when it isn't a command. */
export function parseChannelsSlash(text: string): { name: string; args: string } | null {
  const m = /^\/([a-z][a-z0-9-]{1,23})(?:\s+([\s\S]*))?$/i.exec(text.trim());
  if (!m) return null;
  return { name: (m[1] ?? "").toLowerCase(), args: (m[2] ?? "").trim() };
}

export function findChannelsSlashCommand(name: string): ChannelsSlashCommand | undefined {
  return registry.get(name.toLowerCase());
}

const USER_TOKEN = /<@([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})>/g;

/** People named in args: picked mentions, else @words matched against the workspace. */
async function resolvePeople(ctx: ChannelsSlashContext, args: string): Promise<{ ids: string[]; rest: string; unknown: string[] }> {
  const ids = [...args.matchAll(USER_TOKEN)].map((m) => m[1] ?? "");
  let rest = args.replace(USER_TOKEN, " ");
  const unknown: string[] = [];
  for (const word of rest.match(/@[\p{L}\p{N}_.-]+/gu) ?? []) {
    const q = word.slice(1).toLowerCase();
    const page = await ctx.api.people(ctx.channel.org_id, q);
    const hit = page.items.find((p) => p.display_name.toLowerCase().replace(/\s+/g, "") === q) ?? (page.items.length === 1 ? page.items[0] : undefined);
    if (hit) {
      ids.push(hit.id);
      rest = rest.replace(word, " ");
    } else unknown.push(word);
  }
  return { ids: [...new Set(ids)].filter((id) => id !== ctx.meId), rest: rest.replace(/\s+/g, " ").trim(), unknown };
}

/** "30m", "1h", "2 hours", "90 minutes", "1 day" → milliseconds. */
export function parseChannelsDuration(text: string): number | null {
  const m = /^(\d{1,4})\s*(m|min|mins|minutes?|h|hr|hrs|hours?|d|days?)$/i.exec(text.trim());
  if (!m) return null;
  const n = Number(m[1]);
  const unit = (m[2] ?? "").toLowerCase()[0];
  const ms = unit === "m" ? 60_000 : unit === "h" ? 3_600_000 : 86_400_000;
  return n > 0 ? n * ms : null;
}

function errorText(e: unknown, fallback: string): string {
  const msg = (e as { message?: string } | null)?.message;
  return typeof msg === "string" && msg ? msg : fallback;
}

const channelOnly = (c: ChannelsDetail) => !isChannelsDirect(c);

const builtins: ChannelsSlashCommand[] = [
  {
    name: "remind",
    usage: "/remind me to … tomorrow at 9am",
    hint: "Set a reminder for yourself",
    run: async (ctx) => {
      if (!/^me\b/i.test(ctx.args)) {
        ctx.notify('Reminders are for yourself: "/remind me to … in 20 minutes".', "error");
        return;
      }
      try {
        const r = await ctx.api.createReminder(ctx.channel.org_id, { command: ctx.args, tz: channelsTimeZone() });
        void ctx.qc.invalidateQueries({ queryKey: ["channels", "reminders", ctx.channel.org_id] });
        ctx.notify(`Reminder set for ${channelsWhen(r.remind_at)}.`, "success");
      } catch (e) {
        ctx.setText(`/remind ${ctx.args}`);
        ctx.notify(errorText(e, "Couldn't set that reminder."), "error");
      }
    },
  },
  {
    name: "mute",
    usage: "/mute",
    hint: "Mute or unmute this conversation",
    available: (c) => c.member,
    run: async (ctx) => {
      const muted = !ctx.channel.muted;
      const d = await ctx.api.setPrefs(ctx.channel.id, { muted });
      ctx.qc.setQueryData(channelsKeys.channel(ctx.channel.id), d);
      void ctx.qc.invalidateQueries({ queryKey: channelsKeys.workspace(ctx.channel.org_id) });
      ctx.notify(muted ? "Muted. You won't be notified." : "Unmuted.", "success");
    },
  },
  {
    name: "invite",
    usage: "/invite @person",
    hint: "Add people to this channel",
    available: (c) => channelOnly(c) && c.permissions.post,
    run: async (ctx) => {
      const { ids, unknown } = await resolvePeople(ctx, ctx.args);
      if (unknown.length) ctx.notify(`No one matches ${unknown.join(", ")}.`, "error");
      if (!ids.length) return;
      try {
        const d = await ctx.api.addMembers(ctx.channel.id, ids);
        ctx.qc.setQueryData(channelsKeys.channel(ctx.channel.id), d);
        ctx.notify(ids.length === 1 ? "Added 1 person." : `Added ${ids.length} people.`, "success");
      } catch (e) {
        ctx.notify(errorText(e, "Couldn't add them."), "error");
      }
    },
  },
  {
    name: "leave",
    usage: "/leave",
    hint: "Leave this channel",
    available: (c) => channelOnly(c) && c.member,
    run: async (ctx) => {
      await ctx.api.leave(ctx.channel.id);
      void ctx.qc.invalidateQueries({ queryKey: channelsKeys.workspace(ctx.channel.org_id) });
      ctx.notify(`You left #${ctx.channel.name}.`, "success");
      ctx.navigate("/channels");
    },
  },
  {
    name: "topic",
    usage: "/topic New topic",
    hint: "Set the topic",
    available: (c) => c.permissions.post || c.permissions.reply,
    run: async (ctx) => {
      try {
        const d = await ctx.api.update(ctx.channel.id, { topic: ctx.args });
        ctx.qc.setQueryData(channelsKeys.channel(ctx.channel.id), d);
        ctx.notify(ctx.args ? "Topic set." : "Topic cleared.", "success");
      } catch (e) {
        ctx.notify(errorText(e, "Couldn't set the topic."), "error");
      }
    },
  },
  {
    name: "call",
    usage: "/call",
    hint: "Start or join a call here",
    run: async (ctx) => {
      try {
        const { path } = await ctx.api.startCall(ctx.channel.id);
        ctx.navigate(path);
      } catch (e) {
        ctx.notify(errorText(e, "Couldn't start a call."), "error");
      }
    },
  },
  {
    name: "status",
    usage: "/status 🌴 On vacation",
    hint: "Set your status (empty clears it)",
    run: async (ctx) => {
      const p = presenceEndpoints(ctx.http);
      if (!ctx.args) {
        await p.clearStatus();
        ctx.notify("Status cleared.", "success");
        return;
      }
      const m = /^(\p{Extended_Pictographic}️?|:[a-z0-9_+-]+:)\s*(.*)$/u.exec(ctx.args);
      const emoji = m ? (m[1] ?? "") : "💬";
      const text = (m ? (m[2] ?? "") : ctx.args).slice(0, 100);
      try {
        await p.setStatus({ emoji, text, clear_after: "never" });
        void ctx.qc.invalidateQueries({ queryKey: ["presence"] });
        ctx.notify("Status set.", "success");
      } catch (e) {
        ctx.notify(errorText(e, "Couldn't set your status."), "error");
      }
    },
  },
  {
    name: "away",
    usage: "/away",
    hint: "Set yourself away, or back",
    run: async (ctx) => {
      const p = presenceEndpoints(ctx.http);
      const me = await p.me();
      const away = !me.away;
      await p.setAway(away);
      void ctx.qc.invalidateQueries({ queryKey: ["presence"] });
      ctx.notify(away ? "You're away." : "You're back.", "success");
    },
  },
  {
    name: "dnd",
    usage: "/dnd 1h (or /dnd off)",
    hint: "Pause notifications",
    run: async (ctx) => {
      const p = presenceEndpoints(ctx.http);
      if (/^(off|stop|end)$/i.test(ctx.args)) {
        await p.clearDND();
        void ctx.qc.invalidateQueries({ queryKey: ["presence"] });
        ctx.notify("Notifications are back on.", "success");
        return;
      }
      const ms = parseChannelsDuration(ctx.args || "1h");
      if (!ms || ms > 7 * 86_400_000) {
        ctx.notify('Give a duration like "30m", "2h" or "1 day" (up to 7 days).', "error");
        return;
      }
      const until = new Date(Date.now() + ms).toISOString();
      await p.setDND(until);
      void ctx.qc.invalidateQueries({ queryKey: ["presence"] });
      ctx.notify(`Notifications paused until ${channelsWhen(until)}.`, "success");
    },
  },
  {
    name: "shrug",
    usage: "/shrug [message]",
    hint: "Send ¯\\_(ツ)_/¯",
    available: (c) => c.permissions.post || c.permissions.reply,
    run: async (ctx) => {
      await ctx.send(`${ctx.args ? `${ctx.args} ` : ""}¯\\\\\\_(ツ)\\_/¯`);
    },
  },
  {
    name: "poll",
    usage: '/poll Lunch? or /poll "Lunch?" "Tacos" "Pizza"',
    hint: "Ask a question with options",
    available: (c) => c.permissions.post || c.permissions.reply,
    run: async (ctx) => {
      const quoted = [...ctx.args.matchAll(/"([^"]+)"|“([^”]+)”/g)].map((m) => (m[1] ?? m[2] ?? "").trim()).filter(Boolean);
      if (quoted.length >= 3) {
        const [question = "", ...options] = quoted;
        try {
          await ctx.api.createPoll(ctx.channel.id, { question, options: options.slice(0, 10), thread_root_id: ctx.threadRootId });
        } catch (e) {
          ctx.notify(errorText(e, "Couldn't post the poll."), "error");
        }
        return;
      }
      ctx.openPoll(quoted[0] ?? ctx.args);
    },
  },
  {
    name: "msg",
    usage: "/msg @person message",
    hint: "Send someone a direct message",
    run: async (ctx) => {
      const { ids, rest, unknown } = await resolvePeople(ctx, ctx.args);
      if (unknown.length || ids.length !== 1) {
        ctx.notify(unknown.length ? `No one matches ${unknown.join(", ")}.` : "Name one person: /msg @person message.", "error");
        return;
      }
      try {
        const dm = await ctx.api.openDM(ctx.channel.org_id, ids);
        if (rest) await channelsEndpoints(ctx.http).send(dm.id, { body: rest });
        void ctx.qc.invalidateQueries({ queryKey: channelsKeys.workspace(ctx.channel.org_id) });
        ctx.navigate(`/channels/${dm.id}`);
      } catch (e) {
        ctx.notify(errorText(e, "Couldn't send that."), "error");
      }
    },
  },
  {
    name: "search",
    usage: "/search query",
    hint: "Search messages",
    run: (ctx) => ctx.navigate(`/channels/search${ctx.args ? `?q=${encodeURIComponent(ctx.args)}` : ""}`),
  },
  { name: "collapse", usage: "/collapse", hint: "Hide previews and media here", run: (ctx) => ctx.setCollapsed(true) },
  { name: "expand", usage: "/expand", hint: "Show previews and media here", run: (ctx) => ctx.setCollapsed(false) },
];

for (const cmd of builtins) registerChannelsSlashCommand(cmd);
