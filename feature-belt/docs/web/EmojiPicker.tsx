"use client";

import { Input } from "@scaffoldapp/ui-web";
import { useMemo, useState } from "react";

/**
 * A searchable set of emoji for page icons, callouts and ":" shortcodes. The first keyword is the shortcode
 * (":smile"), the rest are search words.
 */
export const EMOJI: [string, string][] = [
  ["😀", "grinning smile happy"], ["😃", "smiley happy"], ["😄", "smile happy joy"], ["😁", "grin beaming"], ["😂", "joy laugh tears"], ["🤣", "rofl laughing"],
  ["😊", "blush smile"], ["🙂", "slight_smile"], ["😉", "wink"], ["😍", "heart_eyes love"], ["🥰", "smiling_hearts love"], ["😘", "kiss"],
  ["😎", "sunglasses cool"], ["🤓", "nerd"], ["🤔", "thinking hmm"], ["🤨", "raised_eyebrow skeptical"], ["😐", "neutral"], ["😴", "sleeping tired"],
  ["😅", "sweat_smile relief"], ["😬", "grimacing awkward"], ["🙄", "eye_roll"], ["😮", "open_mouth wow surprised"], ["😢", "cry sad"], ["😭", "sob crying"],
  ["😡", "rage angry"], ["🤯", "exploding_head mind blown"], ["🥳", "partying celebrate"], ["🤩", "star_struck"], ["😇", "innocent halo"], ["🤗", "hugging hug"],
  ["👍", "thumbsup +1 yes like"], ["👎", "thumbsdown -1 no"], ["👌", "ok_hand"], ["🙏", "pray please thanks"], ["💪", "muscle strong"], ["✌️", "v peace"],
  ["🤞", "crossed_fingers luck"], ["👀", "eyes look"], ["💯", "100 hundred"], ["✔️", "heavy_check_mark"], ["❌", "x cross no"], ["⭕", "o circle"],

  ["📄", "page document"], ["📘", "book handbook manual"], ["📚", "books library reading"], ["📝", "note memo write"], ["🗂️", "folder index projects"],
  ["📁", "folder"], ["📌", "pin important"], ["📎", "attachment clip"], ["🔖", "bookmark"], ["🗒️", "notepad"], ["📋", "clipboard checklist"],
  ["✅", "done check task"], ["☑️", "check box"], ["🎯", "goal target"], ["🚀", "launch rocket ship"], ["💡", "idea tip light"], ["⚠️", "warning caution"],
  ["❗", "important alert"], ["❓", "question help"], ["ℹ️", "info information"], ["🔒", "lock private secret"], ["🔑", "key access"], ["🛡️", "security shield"],
  ["📅", "calendar date schedule"], ["🗓️", "calendar plan"], ["⏰", "alarm time deadline"], ["⏳", "waiting hourglass"], ["📈", "growth chart up"], ["📉", "decline chart"],
  ["📊", "chart stats report"], ["💰", "money budget"], ["💳", "payment card"], ["🧾", "receipt invoice"], ["🏷️", "tag label"], ["📦", "package box release"],
  ["🛠️", "tools build fix"], ["⚙️", "settings gear config"], ["🧪", "test experiment"], ["🐛", "bug issue"], ["💻", "computer code laptop"], ["🖥️", "desktop"],
  ["📱", "phone mobile app"], ["🌐", "web globe internet"], ["🔗", "link url"], ["✉️", "email mail letter"], ["💬", "chat comment message"], ["📣", "announcement megaphone"],
  ["🔔", "notification bell"], ["👋", "welcome hello wave"], ["🙌", "celebrate hands"], ["🎉", "party celebrate"], ["👏", "applause"], ["❤️", "heart love"],
  ["⭐", "star favorite"], ["🔥", "fire hot"], ["✨", "sparkles new"], ["🌱", "growth plant seedling"], ["🌳", "tree nature"], ["☀️", "sun summer"],
  ["🌙", "moon night"], ["🦉", "owl night"], ["☕", "coffee break"], ["🍕", "pizza food"], ["🏠", "home house"], ["🏢", "office building company"],
  ["🧭", "compass direction strategy"], ["🗺️", "map roadmap"], ["🧩", "puzzle piece"], ["🎨", "design art palette"], ["✏️", "pencil edit draft"], ["🖊️", "pen sign"],
  ["📷", "camera photo"], ["🎬", "video film"], ["🎵", "music"], ["🎓", "learning education training"], ["🧠", "brain thinking"], ["🤝", "deal partnership handshake"],
  ["👥", "team people group"], ["👤", "person user profile"], ["🧑‍💻", "developer engineer"], ["🧗", "climbing sport"], ["🏃", "run sport"], ["⚽", "football soccer"],
  ["🏆", "trophy win award"], ["🥇", "first gold medal"], ["🚩", "flag milestone"], ["🔍", "search find research"], ["🧹", "cleanup broom"], ["♻️", "recycle refresh"],
  ["🔄", "sync update repeat"], ["➕", "add plus"], ["🆕", "new"], ["🪨", "rock stone"], ["🧱", "brick wall foundation"], ["🔧", "wrench maintenance"],
  ["🚧", "construction wip"], ["🛒", "shopping cart order"], ["✈️", "travel trip"], ["🚗", "car transport"], ["🏥", "health hospital"], ["⚖️", "legal law balance"],
];

/** Emoji grid with a search box. */
/** Emoji whose shortcode or words start with q (shortcode matches first). */
export function searchEmoji(q: string, limit = 8): { emoji: string; name: string }[] {
  const s = q.trim().toLowerCase();
  if (!s) return [];
  const head: { emoji: string; name: string }[] = [];
  const rest: { emoji: string; name: string }[] = [];
  for (const [emoji, words] of EMOJI) {
    const list = words.split(" ");
    if (list[0]!.startsWith(s)) head.push({ emoji, name: list[0]! });
    else if (list.some((w) => w.startsWith(s))) rest.push({ emoji, name: list[0]! });
  }
  return [...head, ...rest].slice(0, limit);
}

export function EmojiPicker({ onPick, onRemove }: { onPick: (emoji: string) => void; onRemove?: () => void }) {
  const [q, setQ] = useState("");
  const list = useMemo(() => {
    const s = q.trim().toLowerCase();
    return s ? EMOJI.filter(([, k]) => k.includes(s)) : EMOJI;
  }, [q]);
  return (
    <div className="flex w-72 flex-col gap-2 p-2">
      <div className="flex items-center gap-2">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search icons" aria-label="Search icons" data-autofocus />
        {onRemove ? (
          <button type="button" className="shrink-0 rounded-sm px-2 py-1 type-label text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring" onClick={onRemove}>
            Remove
          </button>
        ) : null}
      </div>
      <div role="listbox" aria-label="Icons" className="grid max-h-60 grid-cols-8 gap-0.5 overflow-y-auto">
        {list.map(([e, k]) => (
          <button
            key={e}
            type="button"
            role="option"
            aria-selected={false}
            aria-label={k.split(" ")[0]}
            title={k.split(" ")[0]}
            className="flex size-8 items-center justify-center rounded-sm type-title-sm hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
            onClick={() => onPick(e)}
          >
            {e}
          </button>
        ))}
        {list.length === 0 ? <p className="col-span-8 px-2 py-3 type-label text-fg-muted">No icons match.</p> : null}
      </div>
    </div>
  );
}
