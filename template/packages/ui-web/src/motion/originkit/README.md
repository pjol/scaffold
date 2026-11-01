# OriginKit

[OriginKit](https://originkit.dev) is a library of animated React + Tailwind components (text morphs, typewriters, shiny pills, emoji bursts, swipe stacks, carousels, cursors…). Its licence forbids vendoring the components into templates, so this scaffold contains none of them. Each project fetches the ones it uses with the owner's own key.

## Slots the template already fills

| Moment | OriginKit component | Stand-in that ships here |
|---|---|---|
| Live label changes ("Saving" → "Saved") | `textmorph`, `scrambletext`, `random-letter-swap` | `SwapText` |
| Success moment (sent, paid, first post) | `emojiburst`, `starburst` | `Celebrate` |
| Highlighted status pill | `shiny-pill` | `Badge` |
| Onboarding cards | `swipe-stack` | `Stagger` + `Card` |
| Live counters | `number-ticker` | `AnimatedNumber` |

Use the stand-in unless the design round picked an OriginKit piece for that moment.

## Adding one to a project

1. Get the owner's key (`cmp_live_…`, originkit.dev → Settings → API Integration) into `ORIGINKIT_API_KEY` in your shell. Never commit it; it is not an app env var.
2. Fetch the component (React, Tailwind, TypeScript):
   - CLI: `npx originkit@latest add <slug>`, or
   - MCP: register `https://mcp.originkit.dev/vellumai` with header `Authorization: Bearer $ORIGINKIT_API_KEY` and call `get_component` with `{ "name": "<slug>", "stack": "nextjs", "styling": "tailwind", "typescript": true }`. `list_components` is free; fetches are rate limited per key.
3. Save it as `web/src/components/motion/originkit/<name>.tsx` (the path in `concrete/design.md`), then:
   - add `"use client"` at the top;
   - change `from "framer-motion"` to `from "motion/react"` (same API);
   - replace hard-coded colours, fonts and radii with tokens (`bg-primary`, `text-fg`, `var(--app-accent)`, `rounded-md`); `scaffold doctor` flags raw colours;
   - take springs from `useMotionTokens()` instead of literal numbers;
   - honour reduced motion: read `useReducedMotion()` and render the final state without looping or travelling when it is true;
   - remove any subtitle/description props it ships with (house rule: context goes in an `InfoTip`).
4. Use it in place of the stand-in and check it at 375 px and desktop, light and dark, with reduced motion on.

Web only. Mobile uses Reanimated equivalents built from the same motion tokens.
