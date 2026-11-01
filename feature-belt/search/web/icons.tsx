import {
  Briefcase,
  Building2,
  CalendarDays,
  Contact,
  File,
  FileCode,
  FileText,
  Folder,
  GitBranch,
  Handshake,
  Hash,
  Image,
  Link2,
  Lock,
  MapPin,
  MessageSquare,
  MessagesSquare,
  Newspaper,
  Notebook,
  Search,
  Table,
  Ticket,
  User,
  UserRound,
  Users,
  Video,
  type LucideIcon,
} from "lucide-react";

/**
 * Icons for search results by the lucide name a provider sends (`SearchResult.icon`): every
 * name the belt's features send. Imported one by one so the bundle carries only these; a
 * feature or app with its own names adds them with `registerSearchIcon` at module load.
 * Unknown names fall back to the feature's icon, then a page.
 */
const byName: Record<string, LucideIcon> = {
  briefcase: Briefcase,
  "building-2": Building2,
  calendar: CalendarDays,
  "calendar-days": CalendarDays,
  contact: Contact,
  file: File,
  "file-code": FileCode,
  "file-text": FileText,
  folder: Folder,
  "git-branch": GitBranch,
  handshake: Handshake,
  hash: Hash,
  image: Image,
  link: Link2,
  lock: Lock,
  "map-pin": MapPin,
  "message-square": MessageSquare,
  "messages-square": MessagesSquare,
  newspaper: Newspaper,
  notebook: Notebook,
  table: Table,
  ticket: Ticket,
  user: User,
  "user-round": UserRound,
  users: Users,
  video: Video,
};

const byKind: Record<string, LucideIcon> = {
  access: Users,
  calendar: CalendarDays,
  calls: Video,
  channels: Hash,
  chat: MessagesSquare,
  crm: Briefcase,
  docs: FileText,
  events: CalendarDays,
  feed: Newspaper,
  files: File,
  orgs: Building2,
  places: MapPin,
  repos: GitBranch,
};

/** Shows `icon` (a lucide name a provider sends) with this component. */
export function registerSearchIcon(name: string, icon: LucideIcon): void {
  byName[name] = icon;
}

export function searchIcon(icon: string | undefined, kind: string): LucideIcon {
  return (icon && byName[icon]) || byKind[kind.split(".")[0] ?? ""] || FileText;
}

export const SearchIcon = Search;
