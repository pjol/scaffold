/**
 * Client seams an app sets from its stitching, once at startup and outside React.
 *
 * Schedule: what "Schedule a meeting" does on the calls screen. By default it opens calls'
 * own `ScheduleMeetingDialog`; an app can schedule elsewhere instead, for example in a
 * calendar that attaches a meeting itself:
 * `setCallsScheduleAction(({ navigate }) => navigate(calendarNewEventLink({ video: true })))`.
 * The action must not lead back to this one (a calendar's video seam creates meetings with
 * `calls.CreateMeeting` on the server, never through this screen).
 *
 * Source link: where a meeting another feature made (`meeting.source`) is managed. Calls
 * never edits or cancels those itself (the owner would fall out of step); with a link it
 * offers "Open in …" instead.
 *
 * Time zone: `setCallsTimeZone` (from the shared package, web and mobile alike) sets the zone
 * Calls shows times in, e.g. the person's calendar zone.
 */
import type { CallsSource } from "@scaffoldapp/shared";

export interface CallsScheduleContext {
  /** Opens an app path. */
  navigate: (href: string) => void;
  /** The active organization, if any. */
  orgId: string | null;
}

export type CallsScheduleAction = (ctx: CallsScheduleContext) => void;

/** Where a sourced meeting is managed: an app path and the menu label ("Open in Calendar"). */
export interface CallsSourceLink {
  href: string;
  label: string;
}

export type CallsSourceLinker = (source: CallsSource) => CallsSourceLink | null;

let scheduleAction: CallsScheduleAction | null = null;
let sourceLinker: CallsSourceLinker | null = null;

/** Replaces the Schedule action; null restores calls' own dialog. */
export function setCallsScheduleAction(action: CallsScheduleAction | null): void {
  scheduleAction = action;
}

/** The app's Schedule action, or null for calls' own dialog. */
export function callsScheduleAction(): CallsScheduleAction | null {
  return scheduleAction;
}

/** Sets how a meeting's source maps to the place it's managed; null removes the links. */
export function setCallsSourceLink(linker: CallsSourceLinker | null): void {
  sourceLinker = linker;
}

/** The link for a meeting's source, or null (no source, or the app gave none). */
export function callsSourceLink(source: CallsSource | null | undefined): CallsSourceLink | null {
  return source && sourceLinker ? sourceLinker(source) : null;
}
