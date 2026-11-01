import { useChannelsMentionCount } from "@scaffoldapp/shared";
import { Hash } from "lucide-react";
import type { HomeTile, NavItem } from "../registry";

/** Side nav / tab bar entry with the count of mentions and DM messages waiting. */
export const channelsNav: NavItem = { href: "/channels", label: "Channels", icon: Hash, useBadge: useChannelsMentionCount };

/** Home tile (same destination; a separate constant so the CLI can wire both lines). */
export const channelsHome: HomeTile = { href: "/channels", label: "Channels", icon: Hash, useBadge: useChannelsMentionCount };

/** Admin tab: open a channel's staff view from an id or a reported-message link. */
export const channelsAdminNav: NavItem = { href: "/admin/channels", label: "Channels", icon: Hash };
