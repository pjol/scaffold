import { MessagesSquare, SquarePen } from "lucide-react";
import type { HomeTile, NavItem, QuickAction } from "../registry";

/** Side nav / bottom tab: the forum. */
export const forumNav: NavItem = { href: "/forum", label: "Forum", icon: MessagesSquare };

/** Home tile (its own constant: the CLI never inserts the same line twice in one file). */
export const forumHomeTile: HomeTile = { href: "/forum", label: "Forum", icon: MessagesSquare };

/** Admin tab: communities and their moderators. */
export const forumAdminNav: NavItem = { href: "/admin/forum", label: "Forum", icon: MessagesSquare };

/** ⌘K: write a post. */
export const forumNewPostAction: QuickAction = { id: "forum.new_post", label: "New forum post", icon: SquarePen, keywords: ["forum", "post", "write"], href: "/forum/submit" };
