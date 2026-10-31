/** Ticket categories (the backend's CHECK list). */
export type SupportCategory = "question" | "bug" | "account" | "billing" | "other";
/** open = waiting on staff · answered = waiting on the user · closed. */
export type SupportStatus = "open" | "answered" | "closed";

export const SUPPORT_CATEGORIES: readonly SupportCategory[] = ["question", "bug", "account", "billing", "other"];
export const SUPPORT_CATEGORY_LABEL: Record<SupportCategory, string> = {
  question: "Question",
  bug: "Something's broken",
  account: "Account",
  billing: "Billing",
  other: "Other",
};
export const SUPPORT_STATUS_LABEL: Record<SupportStatus, string> = { open: "Open", answered: "Answered", closed: "Closed" };

/** Storage purpose for attachments (`POST /v1/uploads`), private and allowed on the consent screen. */
export const SUPPORT_ATTACHMENT_PURPOSE = "support_attachment";
export const SUPPORT_MAX_ATTACHMENTS = 5;
export const SUPPORT_MAX_ATTACHMENT_BYTES = 25 * 1024 * 1024;
/** Content types the backend accepts for attachments. */
export const SUPPORT_ATTACHMENT_TYPES: readonly string[] = [
  "image/jpeg",
  "image/png",
  "image/heic",
  "image/webp",
  "image/gif",
  "application/pdf",
  "video/mp4",
  "video/quicktime",
  "text/plain",
];
export const SUPPORT_SUBJECT_MAX = 120;
export const SUPPORT_BODY_MAX = 5000;

/** A ticket as its owner sees it. */
export interface SupportTicket {
  id: string;
  /** Human reference: #1042. */
  number: number;
  category: SupportCategory;
  subject: string;
  status: SupportStatus;
  client_platform: string;
  client_version: string;
  last_message_at: string;
  closed_at: string | null;
  created_at: string;
  updated_at: string;
}

/** A file on a message; `url` is a download link valid for 15 minutes. */
export interface SupportAttachment {
  id: string;
  file_name: string;
  content_type: string;
  size_bytes: number;
  url: string;
  created_at: string;
}

/** One thread entry. `author_id` is set in staff views only; `internal` notes never reach the user. */
export interface SupportMessage {
  id: string;
  ticket_id: string;
  author_id: string | null;
  author_role: "user" | "staff";
  author_name: string;
  internal: boolean;
  body: string;
  attachments: SupportAttachment[];
  created_at: string;
}

export interface SupportTicketDetail extends SupportTicket {
  messages: SupportMessage[];
}

/** An uploaded file to attach: the key from `POST /v1/uploads` (purpose `support_attachment`). */
export interface SupportAttachmentInput {
  key: string;
  name: string;
  size: number;
}

export interface CreateTicketRequest {
  category: SupportCategory;
  subject: string;
  body: string;
  attachments?: SupportAttachmentInput[];
}

export interface ReplyTicketRequest {
  body: string;
  attachments?: SupportAttachmentInput[];
}

export interface MyTicketListParams {
  status?: SupportStatus | undefined;
  limit?: number | undefined;
  cursor?: string | undefined;
}

// ---- staff -------------------------------------------------------------------

export interface SupportUserRef {
  id: string;
  email: string;
  display_name: string;
  status: string;
}

export interface SupportStaffRef {
  id: string;
  email: string;
  display_name: string;
  admin_role?: "support" | "admin";
}

/** A queue row. */
export interface AdminSupportTicket extends SupportTicket {
  user: SupportUserRef;
  assignee: SupportStaffRef | null;
  message_count: number;
}

export interface AdminSupportTicketDetail extends AdminSupportTicket {
  messages: SupportMessage[];
}

export interface AdminTicketListParams {
  status?: SupportStatus | undefined;
  category?: SupportCategory | undefined;
  /** `me`, `none` or a staff user id. */
  assignee?: string | undefined;
  user_id?: string | undefined;
  /** Subject, email or #number. */
  q?: string | undefined;
  /** `oldest` works the queue first-in first-out; default newest first. */
  sort?: "newest" | "oldest" | undefined;
  limit?: number | undefined;
  cursor?: string | undefined;
}

export interface AdminReplyRequest {
  body: string;
  /** A note only staff see. */
  internal?: boolean;
  /** Close the ticket after replying (not with a note). */
  close?: boolean;
}

export interface AdminUpdateTicketRequest {
  status?: SupportStatus;
  /** Staff user id, or "" to unassign. */
  assignee_id?: string;
}

/** `GET /v1/admin/support/stats`. */
export interface SupportStats {
  open: number;
  answered: number;
  closed: number;
  unassigned_open: number;
  mine_open: number;
  /** Last message of the longest-waiting open ticket. */
  oldest_open_at: string | null;
}
