import { DOCS_COVER_GALLERY, useDocsFileUrl, type DocsCover, type DocsCoverColor, type DocsCustomIcon, type DocsIconName } from "@scaffoldapp/shared";
import { Text, useTheme, type NativeTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { LinearGradient } from "expo-linear-gradient";
import {
  AlarmClock, Anchor, Archive, Atom, Bell, BookOpen, Bookmark, Briefcase, Bug, Building2, Calendar, Camera, Car, ChartBar, ChartPie, CircleCheck,
  ClipboardList, Clock, Cloud, Code, Coffee, Compass, Cpu, Database, DollarSign, Eye, FileText, Film, Flag, Flame, FlaskConical, Folder, Gift, GitBranch,
  Globe, GraduationCap, Handshake, Headphones, Heart, House, Image as ImageIcon, Inbox, KeyRound, Layers, LayoutDashboard, Leaf, Lightbulb, Link, ListChecks, Lock, Mail,
  Map, Medal, Megaphone, MessageSquare, Mic, Moon, Music, NotebookPen, Package, Palette, PenTool, Phone, Plane, Puzzle, Rocket, Scale, School, Search,
  Server, Settings, Shield, ShoppingCart, Sparkles, Star, Sun, Table2, Target, Terminal, TreePine, TrendingUp, Trophy, Truck, Umbrella, User,
  UserRoundCheck, Users, Utensils, Wallet, Wrench, Zap, type LucideIcon,
} from "lucide-react-native";
import type { ReactNode } from "react";
import { View, type ViewStyle } from "react-native";

/** The icon set (shared DOCS_ICON_SET) as native components; keep it in step with the shared list and web. */
export const DOCS_LUCIDE_NATIVE: Record<DocsIconName, LucideIcon> = {
  "file-text": FileText, "book-open": BookOpen, "notebook-pen": NotebookPen, "clipboard-list": ClipboardList, "list-checks": ListChecks, "circle-check": CircleCheck,
  target: Target, flag: Flag, rocket: Rocket, lightbulb: Lightbulb, star: Star, heart: Heart, bookmark: Bookmark, calendar: Calendar, clock: Clock,
  "alarm-clock": AlarmClock, map: Map, compass: Compass, globe: Globe, house: House, "building-2": Building2, briefcase: Briefcase, users: Users, user: User,
  "user-round-check": UserRoundCheck, megaphone: Megaphone, "message-square": MessageSquare, mail: Mail, phone: Phone, bell: Bell, code: Code, terminal: Terminal,
  bug: Bug, "git-branch": GitBranch, database: Database, server: Server, cpu: Cpu, shield: Shield, lock: Lock, "key-round": KeyRound, palette: Palette,
  "pen-tool": PenTool, image: ImageIcon, camera: Camera, film: Film, music: Music, mic: Mic, headphones: Headphones, "layout-dashboard": LayoutDashboard, layers: Layers,
  "chart-bar": ChartBar, "chart-pie": ChartPie, "trending-up": TrendingUp, "dollar-sign": DollarSign, wallet: Wallet, "shopping-cart": ShoppingCart, package: Package,
  truck: Truck, plane: Plane, car: Car, "graduation-cap": GraduationCap, school: School, "flask-conical": FlaskConical, atom: Atom, leaf: Leaf, "tree-pine": TreePine,
  sun: Sun, moon: Moon, cloud: Cloud, umbrella: Umbrella, coffee: Coffee, utensils: Utensils, gift: Gift, trophy: Trophy, medal: Medal, sparkles: Sparkles, zap: Zap,
  flame: Flame, anchor: Anchor, puzzle: Puzzle, wrench: Wrench, settings: Settings, folder: Folder, archive: Archive, inbox: Inbox, link: Link, search: Search,
  eye: Eye, handshake: Handshake, scale: Scale,
};

/** A palette role ("primary", "info-soft", "ink-200") as the theme's colour. */
export function paletteColor(theme: NativeTheme, role: string): string {
  const c = theme.colors;
  const flat = c as unknown as Record<string, string | undefined>;
  const ink = /^ink-(\d+)$/.exec(role);
  if (ink) return (c.ink as unknown as Record<number, string | undefined>)[Number(ink[1])] ?? c.border;
  if (role === "neutral") return c.textMuted;
  const key = role.replace(/-([a-z])/g, (_, ch: string) => ch.toUpperCase());
  return flat[key] ?? c.border;
}

const COVER_SOFT: Record<DocsCoverColor, string> = {
  neutral: "ink-200",
  primary: "primary-soft",
  accent: "accent-soft",
  info: "info-soft",
  success: "success-soft",
  warning: "warning-soft",
  danger: "danger-soft",
};

const SIZES = { xs: 18, sm: 22, md: 32, lg: 56 } as const;

/** A page's icon: its emoji, a tinted icon from the set, or an uploaded image; otherwise the page glyph (or nothing). */
export function PageIcon({
  icon,
  custom,
  kind = "page",
  size = "sm",
  token,
  fallback = true,
}: {
  icon: string | null | undefined;
  custom?: DocsCustomIcon | null | undefined;
  kind?: "page" | "database";
  size?: keyof typeof SIZES;
  token?: string | null;
  fallback?: boolean;
}) {
  const theme = useTheme();
  const px = SIZES[size];
  const file = useDocsFileUrl(custom?.type === "image" ? custom.file_id : null, token);
  if (custom?.type === "lucide") {
    const Icon = DOCS_LUCIDE_NATIVE[custom.name as DocsIconName] ?? FileText;
    return <Icon size={px} color={paletteColor(theme, custom.color === "neutral" ? "neutral" : custom.color)} />;
  }
  if (custom?.type === "image") {
    return file.data?.url ? (
      <Image source={{ uri: file.data.url }} style={{ width: px, height: px, borderRadius: theme.radius.xs }} contentFit="cover" accessibilityIgnoresInvertColors />
    ) : (
      <View style={{ width: px, height: px, borderRadius: theme.radius.xs, backgroundColor: theme.colors.sunken }} />
    );
  }
  if (icon) return <Text style={{ fontSize: px * 0.82, lineHeight: px * 1.1 }}>{icon}</Text>;
  if (!fallback) return null;
  const Glyph = kind === "database" ? Table2 : FileText;
  return <Glyph size={Math.round(px * 0.8)} color={theme.colors.textMuted} />;
}

/** A page's cover: image (vertical focus), gallery gradient or colour. */
export function CoverArt({ cover, token, height = 140, children }: { cover: DocsCover; token?: string | null; height?: number; children?: ReactNode }) {
  const theme = useTheme();
  const file = useDocsFileUrl(cover.file_id, token);
  const style: ViewStyle = { height, width: "100%", overflow: "hidden", backgroundColor: theme.colors.sunken };
  if (cover.gallery && DOCS_COVER_GALLERY[cover.gallery]) {
    const [from, to] = DOCS_COVER_GALLERY[cover.gallery];
    return (
      <LinearGradient colors={[paletteColor(theme, from), paletteColor(theme, to)]} start={{ x: 0, y: 0 }} end={{ x: 1, y: 1 }} style={style}>
        {children}
      </LinearGradient>
    );
  }
  if (cover.color) {
    return <View style={[style, { backgroundColor: paletteColor(theme, COVER_SOFT[cover.color]) }]}>{children}</View>;
  }
  return (
    <View style={style}>
      {file.data?.url ? (
        <Image source={{ uri: file.data.url }} style={{ width: "100%", height: "100%" }} contentFit="cover" contentPosition={{ top: `${cover.position}%`, left: "50%" }} accessibilityIgnoresInvertColors />
      ) : null}
      {children}
    </View>
  );
}
