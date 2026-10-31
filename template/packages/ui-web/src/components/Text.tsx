import { createElement, type HTMLAttributes, type ReactNode } from "react";
import { cn } from "../lib/cn";

type Tone = "default" | "muted" | "subtle" | "primary" | "danger" | "success" | "inherit";
const tones: Record<Tone, string> = {
  default: "text-fg",
  muted: "text-fg-muted",
  subtle: "text-fg-subtle",
  primary: "text-primary-strong",
  danger: "text-danger-strong",
  success: "text-success-strong",
  inherit: "",
};

export interface TextProps extends HTMLAttributes<HTMLElement> {
  /** Element to render. */
  as?: keyof HTMLElementTagNameMap;
  tone?: Tone;
  children?: ReactNode;
}

function make(displayName: string, typeClass: string, defaultTag: keyof HTMLElementTagNameMap) {
  const C = ({ as, tone = "default", className, ...rest }: TextProps) => createElement(as ?? defaultTag, { className: cn(typeClass, tones[tone], className), ...rest });
  C.displayName = displayName;
  return C;
}

/** The one landing headline, if any (fluid). `<h1>`. */
export const Hero = make("Hero", "type-hero text-balance", "h1");
/** Page H1 on desktop. `<h1>`. */
export const Display = make("Display", "type-display text-balance", "h1");
/** H2. */
export const Headline = make("Headline", "type-headline text-balance", "h2");
/** Screen title (H1 on app screens). `<h1>`. */
export const Title = make("Title", "type-title text-balance", "h1");
/** Card titles, list and section headers. `<h2>`. */
export const TitleSm = make("TitleSm", "type-title-sm", "h2");
/** Lead text. `<p>`. */
export const BodyLg = make("BodyLg", "type-body-lg", "p");
/** Default copy. `<p>`. */
export const Body = make("Body", "type-body", "p");
/** Form labels, dense UI. `<span>`. */
export const Label = make("Label", "type-label", "span");
/** Metadata and timestamps; never a subtitle. `<span>`. */
export const Caption = make("Caption", "type-caption", "span");
