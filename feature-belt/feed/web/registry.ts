import { useFeedUnreadCount } from "@scaffoldapp/shared";
import { Newspaper } from "lucide-react";
import type { HomeTile, NavItem } from "../registry";

/** Side nav / bottom tab: the feed, with the unread count as its badge. */
export const feedNav: NavItem = { href: "/feed", label: "Feed", icon: Newspaper, useBadge: useFeedUnreadCount };

/** Home tile (a separate constant: the CLI never inserts the same line twice in one file). */
export const feedHomeTile: HomeTile = { href: "/feed", label: "Feed", icon: Newspaper, useBadge: useFeedUnreadCount };

/** Admin tab: posts, removals and announcements. */
export const feedAdminNav: NavItem = { href: "/admin/feed", label: "Feed", icon: Newspaper };
