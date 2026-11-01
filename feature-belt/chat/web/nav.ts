import { useChatUnreadCount } from "@scaffoldapp/shared";
import { MessagesSquare } from "lucide-react";
import type { HomeTile, NavItem } from "../registry";

/** Side nav / tab bar entry with the unread message count. */
export const chatNav: NavItem = { href: "/chat", label: "Chat", icon: MessagesSquare, useBadge: useChatUnreadCount };

/** Home tile (same destination; a separate constant so the CLI can wire both lines). */
export const chatHome: HomeTile = { href: "/chat", label: "Chat", icon: MessagesSquare, useBadge: useChatUnreadCount };

/** Admin tab: open a conversation's staff view from an id or a reported-message link. */
export const chatAdminNav: NavItem = { href: "/admin/chat", label: "Chat", icon: MessagesSquare };
