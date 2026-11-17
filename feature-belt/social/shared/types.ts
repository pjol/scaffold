/** Social DTOs, mirroring backend/internal/social. Times are ISO strings. */

/** Who sees a part of a profile. */
export type SocialVisibility = "public" | "followers" | "private";

/** How a person appears anywhere social shows them. */
export interface SocialRef {
  user_id: string;
  /** "" for a deleted account. */
  handle: string;
  display_name: string;
  avatar_url: string | null;
}

export interface SocialLink {
  url: string;
  /** "" shows the host. */
  label: string;
}

/** How the caller relates to a profile. Whether the caller blocked them is moderation's (`useIsBlocked`). */
export interface SocialProfileViewer {
  is_self: boolean;
  /** The caller follows them. */
  following: boolean;
  /** They follow the caller. */
  follows_you: boolean;
}

/** `GET /v1/social/profiles/{handle}`. `user.handle` is current: an old handle resolves to it. */
export interface SocialProfile {
  user: SocialRef;
  bio: string;
  links: SocialLink[];
  joined_at: string;
  followers: number;
  following: number;
  /** Per registered section ("activity", "connections", …): whether the caller may see it. */
  sections: Record<string, boolean>;
  /** A block separates the caller and the owner: only the name and photo are sent. */
  restricted: boolean;
  viewer: SocialProfileViewer;
}

export interface SocialSection {
  key: string;
  label: string;
  default: SocialVisibility;
}

export interface SocialPrivacyRow extends SocialSection {
  visibility: SocialVisibility;
}

/** `GET /v1/social/me`: what the caller edits. */
export interface SocialMyProfile {
  user: SocialRef;
  bio: string;
  links: SocialLink[];
  privacy: SocialPrivacyRow[];
  handle_changed_at: string | null;
  /** When the handle may change next; null: now. */
  handle_change_at: string | null;
  limits: { bio_max_chars: number; max_links: number; handle_cooldown_hours: number; handle_redirect_hours: number };
}

/** `PATCH /v1/social/me`: absent fields stay. */
export interface UpdateSocialProfileRequest {
  bio?: string;
  links?: SocialLink[];
  /** Section key → visibility. */
  privacy?: Record<string, SocialVisibility>;
}

export type SocialHandleProblem = "handle_invalid" | "handle_reserved" | "handle_taken" | "content_rejected";

/** `GET /v1/social/handles/{handle}`. */
export interface SocialHandleCheck {
  handle: string;
  available: boolean;
  code?: SocialHandleProblem;
  message?: string;
}

/** A row in followers/following lists, search and suggestions. */
export interface SocialPerson extends SocialRef {
  followers: number;
  /** The caller follows them. */
  is_following: boolean;
  follows_you: boolean;
  /** Lists: when the follow began. */
  since?: string;
  /** Suggestions: how many people the caller follows follow them (absent for "most followed" fill). */
  mutuals?: number;
}

/** The answer to a follow or unfollow. */
export interface SocialFollowState {
  user_id: string;
  following: boolean;
  followers: number;
}

/** One activity item as the reader sees it. */
export interface SocialActivityItem {
  id: string;
  /** Module-prefixed kind ("social.followed", "feed.post"). */
  kind: string;
  /** Lucide icon name the kind suggests ("user-plus"); "" for none. */
  icon: string;
  actor: SocialRef;
  /** Another person the item is about, with their current handle. */
  subject: SocialRef | null;
  title: string;
  summary: string;
  /** App path, the same on web and mobile ("" = not linkable). */
  link: string;
  data: Record<string, unknown>;
  created_at: string;
}

/** Profile tabs on web (`?tab=`) and mobile. */
export type SocialProfileTab = "activity" | "followers" | "following";

/** Error codes the social API returns besides the common ones. */
export type SocialErrorCode = SocialHandleProblem | "handle_cooldown" | "self_follow" | "blocked" | "follow_limit";
