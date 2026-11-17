"use client";

import {
  DOCS_ICON_COLORS,
  DOCS_ICON_SET,
  errorMessage,
  useDocsFileUrl,
  useDocsUpdatePage,
  useDocsUploadFile,
  type DocsCoverColor,
  type DocsCustomIcon,
  type DocsIconName,
  type DocsPage,
} from "@scaffoldapp/shared";
import { Button, Input, Tab, TabPanel, Tabs, TabsList, cn, toast } from "@scaffoldapp/ui-web";
import {
  AlarmClock, Anchor, Archive, Atom, Bell, BookOpen, Bookmark, Briefcase, Bug, Building2, Calendar, Camera, Car, ChartBar, ChartPie, CircleCheck,
  ClipboardList, Clock, Cloud, Code, Coffee, Compass, Cpu, Database, DollarSign, Eye, FileText, Film, Flag, Flame, FlaskConical, Folder, Gift, GitBranch,
  Globe, GraduationCap, Handshake, Headphones, Heart, House, Image, Inbox, KeyRound, Layers, LayoutDashboard, Leaf, Lightbulb, Link, ListChecks, Lock, Mail,
  Map, Medal, Megaphone, MessageSquare, Mic, Moon, Music, NotebookPen, Package, Palette, PenTool, Phone, Plane, Puzzle, Rocket, Scale, School, Search,
  Server, Settings, Shield, ShoppingCart, Sparkles, Star, Sun, Table2, Target, Terminal, TreePine, TrendingUp, Trophy, Truck, Umbrella, Upload, User,
  UserRoundCheck, Users, Utensils, Wallet, Wrench, Zap, type LucideIcon,
} from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { EmojiPicker } from "./EmojiPicker";

/** The icon set (shared DOCS_ICON_SET) as components; keep it in step with the shared list. */
export const DOCS_LUCIDE: Record<DocsIconName, LucideIcon> = {
  "file-text": FileText, "book-open": BookOpen, "notebook-pen": NotebookPen, "clipboard-list": ClipboardList, "list-checks": ListChecks, "circle-check": CircleCheck,
  target: Target, flag: Flag, rocket: Rocket, lightbulb: Lightbulb, star: Star, heart: Heart, bookmark: Bookmark, calendar: Calendar, clock: Clock,
  "alarm-clock": AlarmClock, map: Map, compass: Compass, globe: Globe, house: House, "building-2": Building2, briefcase: Briefcase, users: Users, user: User,
  "user-round-check": UserRoundCheck, megaphone: Megaphone, "message-square": MessageSquare, mail: Mail, phone: Phone, bell: Bell, code: Code, terminal: Terminal,
  bug: Bug, "git-branch": GitBranch, database: Database, server: Server, cpu: Cpu, shield: Shield, lock: Lock, "key-round": KeyRound, palette: Palette,
  "pen-tool": PenTool, image: Image, camera: Camera, film: Film, music: Music, mic: Mic, headphones: Headphones, "layout-dashboard": LayoutDashboard, layers: Layers,
  "chart-bar": ChartBar, "chart-pie": ChartPie, "trending-up": TrendingUp, "dollar-sign": DollarSign, wallet: Wallet, "shopping-cart": ShoppingCart, package: Package,
  truck: Truck, plane: Plane, car: Car, "graduation-cap": GraduationCap, school: School, "flask-conical": FlaskConical, atom: Atom, leaf: Leaf, "tree-pine": TreePine,
  sun: Sun, moon: Moon, cloud: Cloud, umbrella: Umbrella, coffee: Coffee, utensils: Utensils, gift: Gift, trophy: Trophy, medal: Medal, sparkles: Sparkles, zap: Zap,
  flame: Flame, anchor: Anchor, puzzle: Puzzle, wrench: Wrench, settings: Settings, folder: Folder, archive: Archive, inbox: Inbox, link: Link, search: Search,
  eye: Eye, handshake: Handshake, scale: Scale,
};

/** Text colour per palette role for tinted icons. */
export const ICON_TINT: Record<DocsCoverColor, string> = {
  neutral: "text-fg-muted",
  primary: "text-primary",
  accent: "text-accent",
  info: "text-info",
  success: "text-success",
  warning: "text-warning",
  danger: "text-danger",
};

const SWATCH: Record<DocsCoverColor, string> = {
  neutral: "bg-ink-400",
  primary: "bg-primary",
  accent: "bg-accent",
  info: "bg-info",
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-danger",
};

const SIZE = {
  xs: { box: "size-5", svg: "size-4", text: "type-body" },
  sm: { box: "size-6", svg: "size-4", text: "type-body" },
  md: { box: "size-8", svg: "size-6", text: "type-title" },
  lg: { box: "size-16", svg: "size-12", text: "type-hero" },
} as const;

/**
 * A page's icon: its emoji, an icon from the set tinted with a palette role, or an image
 * uploaded to the page; otherwise the page or database glyph (or nothing with `fallback={false}`).
 */
export function PageIcon({
  icon,
  custom,
  kind = "page",
  size = "sm",
  token,
  fallback = true,
  className,
}: {
  icon: string | null | undefined;
  custom?: DocsCustomIcon | null | undefined;
  kind?: "page" | "database";
  size?: keyof typeof SIZE;
  /** Public link token, for an image icon on a published page. */
  token?: string | null;
  fallback?: boolean;
  className?: string;
}) {
  const s = SIZE[size];
  const file = useDocsFileUrl(custom?.type === "image" ? custom.file_id : null, token);
  if (custom?.type === "lucide") {
    const Icon = DOCS_LUCIDE[custom.name as DocsIconName] ?? FileText;
    return (
      <span aria-hidden className={cn("flex shrink-0 items-center justify-center", s.box, className)}>
        <Icon className={cn(s.svg, ICON_TINT[custom.color] ?? "text-fg-muted")} />
      </span>
    );
  }
  if (custom?.type === "image") {
    return (
      <span aria-hidden className={cn("flex shrink-0 items-center justify-center overflow-hidden rounded-xs", s.box, className)}>
        {file.data?.url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img src={file.data.url} alt="" className="size-full object-cover" />
        ) : (
          <span className="size-full bg-sunken" />
        )}
      </span>
    );
  }
  if (icon) {
    return (
      <span aria-hidden className={cn("flex shrink-0 items-center justify-center leading-none", s.box, s.text, className)}>
        {icon}
      </span>
    );
  }
  if (!fallback) return null;
  const Glyph = kind === "database" ? Table2 : FileText;
  return (
    <span aria-hidden className={cn("flex shrink-0 items-center justify-center", s.box, className)}>
      <Glyph className={cn(s.svg, "text-fg-muted")} />
    </span>
  );
}

/** Pick a page icon: an emoji, an icon from the set in a palette colour, or an uploaded image. */
export function IconPicker({ page, onDone }: { page: DocsPage; onDone: () => void }) {
  const update = useDocsUpdatePage(page.id);
  const upload = useDocsUploadFile(page.id);
  const file = useRef<HTMLInputElement>(null);
  const [q, setQ] = useState("");
  const [color, setColor] = useState<DocsCoverColor>(page.custom_icon?.type === "lucide" ? page.custom_icon.color : "neutral");
  const icons = useMemo(() => DOCS_ICON_SET.filter((n) => !q.trim() || n.includes(q.trim().toLowerCase())), [q]);
  const onErr = (e: unknown) => toast.error(errorMessage(e, "Couldn't change the icon."));
  const hasIcon = !!page.icon || !!page.custom_icon;
  return (
    <div className="flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2 p-2">
      <Tabs defaultValue={page.custom_icon?.type === "lucide" ? "icons" : "emoji"}>
        <div className="flex items-center gap-2">
          <TabsList aria-label="Icon type">
            <Tab value="emoji">Emoji</Tab>
            <Tab value="icons">Icons</Tab>
            <Tab value="upload">Upload</Tab>
          </TabsList>
          {hasIcon ? (
            <Button
              variant="ghost"
              size="sm"
              className="ml-auto"
              onClick={() => {
                onDone();
                update.mutate({ icon: null, custom_icon: null }, { onError: onErr });
              }}
            >
              Remove
            </Button>
          ) : null}
        </div>
        <TabPanel value="emoji">
          <EmojiPicker
            onPick={(emoji) => {
              onDone();
              update.mutate({ icon: emoji }, { onError: onErr });
            }}
          />
        </TabPanel>
        <TabPanel value="icons">
          <div className="flex flex-col gap-2 pt-2">
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search icons" aria-label="Search icons" leadingIcon={<Search />} />
            <div role="radiogroup" aria-label="Icon colour" className="flex flex-wrap gap-1">
              {DOCS_ICON_COLORS.map((c) => (
                <button
                  key={c}
                  type="button"
                  role="radio"
                  aria-checked={color === c}
                  aria-label={`${c} colour`}
                  onClick={() => setColor(c)}
                  className={cn("size-6 rounded-pill border-2 focus-visible:outline-2 focus-visible:outline-ring", SWATCH[c], color === c ? "border-fg" : "border-transparent")}
                />
              ))}
            </div>
            <div className="grid max-h-56 grid-cols-8 gap-1 overflow-y-auto" role="listbox" aria-label="Icons">
              {icons.map((name) => {
                const Icon = DOCS_LUCIDE[name];
                return (
                  <button
                    key={name}
                    type="button"
                    role="option"
                    aria-selected={page.custom_icon?.type === "lucide" && page.custom_icon.name === name}
                    aria-label={name.replace(/-/g, " ")}
                    title={name.replace(/-/g, " ")}
                    onClick={() => {
                      onDone();
                      update.mutate({ custom_icon: { type: "lucide", name, color } }, { onError: onErr });
                    }}
                    className="flex aspect-square items-center justify-center rounded-sm hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                  >
                    <Icon aria-hidden className={cn("size-5", ICON_TINT[color])} />
                  </button>
                );
              })}
              {icons.length === 0 ? <p className="col-span-8 px-2 py-3 type-label text-fg-muted">No icons match.</p> : null}
            </div>
          </div>
        </TabPanel>
        <TabPanel value="upload">
          <div className="flex flex-col items-start gap-2 pt-3">
            <Button variant="secondary" size="sm" leftIcon={<Upload />} loading={upload.isPending} onClick={() => file.current?.click()}>
              Upload an image
            </Button>
            <p className="type-caption text-fg-muted">Square images look best (at least 280 × 280).</p>
            <input
              ref={file}
              type="file"
              accept="image/png,image/jpeg,image/webp,image/gif"
              hidden
              onChange={(e) => {
                const f = e.target.files?.[0];
                e.target.value = "";
                if (!f) return;
                upload.mutate(
                  { file: f, name: f.name },
                  {
                    onSuccess: (up) => {
                      onDone();
                      update.mutate({ custom_icon: { type: "image", file_id: up.id } }, { onError: onErr });
                    },
                    onError: (err) => toast.error(errorMessage(err, "Couldn't upload the image.")),
                  },
                );
              }}
            />
          </div>
        </TabPanel>
      </Tabs>
    </div>
  );
}
