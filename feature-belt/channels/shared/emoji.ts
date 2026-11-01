/**
 * Emoji shortcodes the composer completes after ":" and the renderer turns into emoji
 * (`:tada:` → 🎉). Names follow the common Slack/GitHub set.
 */
export const CHANNELS_EMOJI: Readonly<Record<string, string>> = {
  "+1": "👍", thumbsup: "👍", "-1": "👎", thumbsdown: "👎", ok_hand: "👌", clap: "👏", pray: "🙏", raised_hands: "🙌",
  muscle: "💪", wave: "👋", v: "✌️", crossed_fingers: "🤞", handshake: "🤝", raised_hand: "✋", writing_hand: "✍️",
  point_up: "☝️", point_down: "👇", point_left: "👈", point_right: "👉", eyes: "👀", brain: "🧠",
  smile: "😄", grin: "😁", grinning: "😀", joy: "😂", rofl: "🤣", wink: "😉", blush: "😊", innocent: "😇",
  slightly_smiling_face: "🙂", upside_down_face: "🙃", heart_eyes: "😍", star_struck: "🤩", kissing_heart: "😘",
  thinking_face: "🤔", thinking: "🤔", neutral_face: "😐", expressionless: "😑", unamused: "😒", roll_eyes: "🙄",
  grimacing: "😬", relieved: "😌", sleeping: "😴", sleepy: "😪", sweat_smile: "😅", sob: "😭", cry: "😢",
  scream: "😱", rage: "😡", angry: "😠", sunglasses: "😎", nerd_face: "🤓", partying_face: "🥳", hugging_face: "🤗",
  shushing_face: "🤫", zipper_mouth_face: "🤐", face_with_monocle: "🧐", exploding_head: "🤯", mask: "😷",
  face_with_thermometer: "🤒", skull: "💀", ghost: "👻", robot: "🤖", see_no_evil: "🙈", hear_no_evil: "🙉",
  speak_no_evil: "🙊", heart: "❤️", orange_heart: "🧡", yellow_heart: "💛", green_heart: "💚", blue_heart: "💙",
  purple_heart: "💜", broken_heart: "💔", sparkling_heart: "💖", "100": "💯", fire: "🔥", tada: "🎉", confetti_ball: "🎊",
  sparkles: "✨", star: "⭐", star2: "🌟", zap: "⚡", boom: "💥", rocket: "🚀", trophy: "🏆", medal: "🏅",
  white_check_mark: "✅", heavy_check_mark: "✔️", ballot_box_with_check: "☑️", x: "❌", warning: "⚠️",
  no_entry: "⛔", question: "❓", exclamation: "❗", heavy_plus_sign: "➕", heavy_minus_sign: "➖",
  arrow_right: "➡️", arrow_left: "⬅️", arrow_up: "⬆️", arrow_down: "⬇️", arrows_counterclockwise: "🔄",
  red_circle: "🔴", large_green_circle: "🟢", large_yellow_circle: "🟡", large_blue_circle: "🔵",
  bulb: "💡", memo: "📝", pencil2: "✏️", pushpin: "📌", round_pushpin: "📍", paperclip: "📎", link: "🔗",
  lock: "🔒", unlock: "🔓", key: "🔑", hammer: "🔨", wrench: "🔧", gear: "⚙️", hammer_and_wrench: "🛠️",
  calendar: "📆", date: "📅", clock3: "🕒", hourglass: "⌛", stopwatch: "⏱️", chart_with_upwards_trend: "📈",
  chart_with_downwards_trend: "📉", bar_chart: "📊", clipboard: "📋", email: "📧", envelope: "✉️",
  inbox_tray: "📥", outbox_tray: "📤", package: "📦", phone: "📞", iphone: "📱", computer: "💻", keyboard: "⌨️",
  bug: "🐛", books: "📚", book: "📖", mag: "🔍", bell: "🔔", no_bell: "🔕", mega: "📣", loudspeaker: "📢",
  speech_balloon: "💬", thought_balloon: "💭", moneybag: "💰", dollar: "💵", credit_card: "💳", gift: "🎁",
  balloon: "🎈", birthday: "🎂", cake: "🍰", coffee: "☕", tea: "🍵", beer: "🍺", beers: "🍻", wine_glass: "🍷",
  champagne: "🍾", pizza: "🍕", hamburger: "🍔", taco: "🌮", popcorn: "🍿", apple: "🍎", avocado: "🥑",
  doughnut: "🍩", cookie: "🍪", sunny: "☀️", cloud: "☁️", umbrella: "☔", snowflake: "❄️", rainbow: "🌈",
  ocean: "🌊", seedling: "🌱", evergreen_tree: "🌲", palm_tree: "🌴", cactus: "🌵", sunflower: "🌻", rose: "🌹",
  earth_americas: "🌎", moon: "🌙", mountain: "⛰️", snow_capped_mountain: "🏔️", camping: "🏕️", tent: "⛺",
  beach_umbrella: "🏖️", house: "🏠", office: "🏢", car: "🚗", bus: "🚌", train: "🚆", airplane: "✈️", bike: "🚲",
  climbing: "🧗", runner: "🏃", swimmer: "🏊", dancer: "💃", soccer: "⚽", basketball: "🏀", tennis: "🎾",
  dog: "🐶", cat: "🐱", unicorn: "🦄", bee: "🐝", turtle: "🐢", tiger: "🐯", panda_face: "🐼", owl: "🦉",
  poop: "💩", headphones: "🎧", movie_camera: "🎥", camera: "📷", art: "🎨", musical_note: "🎵", video_game: "🎮",
  dart: "🎯", jigsaw: "🧩", money_with_wings: "💸", hourglass_flowing_sand: "⏳", sos: "🆘", new: "🆕", free: "🆓",
};

/** Names in the order the picker shows them when nothing is typed. */
export const CHANNELS_FREQUENT_EMOJI = ["+1", "heart", "joy", "tada", "fire", "eyes", "pray", "white_check_mark", "raised_hands", "100", "thinking_face", "rocket"] as const;

/** Quick reactions on hover / long press (any emoji is accepted). */
export const CHANNELS_QUICK_REACTIONS = ["👍", "❤️", "😂", "🎉", "👀", "✅"] as const;

/** Shortcodes starting with (then containing) `query`, at most `limit`. */
export function searchChannelsEmoji(query: string, limit = 8): { name: string; emoji: string }[] {
  const q = query.toLowerCase().replace(/^:/, "");
  if (!q) return CHANNELS_FREQUENT_EMOJI.slice(0, limit).map((name) => ({ name, emoji: CHANNELS_EMOJI[name] ?? "" }));
  const names = Object.keys(CHANNELS_EMOJI);
  const starts = names.filter((n) => n.startsWith(q));
  const contains = names.filter((n) => !n.startsWith(q) && n.includes(q));
  return [...starts, ...contains].slice(0, limit).map((name) => ({ name, emoji: CHANNELS_EMOJI[name] ?? "" }));
}
