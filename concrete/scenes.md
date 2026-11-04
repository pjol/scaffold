# Scenes

Showpiece moments on the web: an opening, an unboxing, a reel, a reveal, a level-up. Everyday UI motion stays in design.md (nothing over 600 ms on interaction); a scene may run for seconds because the person started it and can skip it. Distilled from Candidate Strike (2026-11); its 3D side is 3d.md.

## Structure

- **One continuous scene.** A stage machine in one component (`idle → opening → rolling → landed → reveal → result`), stages swapped with `AnimatePresence`. Stages that are destinations get a path, pushed with `window.history.pushState` (no router navigation, so nothing remounts) and restored on `popstate`, so refresh, share and Back land somewhere sensible.
- **Decide the outcome first.** A pure planner picks the result and builds everything the animation will show before anything moves, taking its random source as a parameter (stack.md: simulate the odds). The animation only plays out the plan. Near-misses are designed into the plan (a rare item just before the winner, another just after it), never made by bending the motion.
- **One beat sheet.** Every timed moment lives in one module of named times in seconds from the trigger (`timeline.ts`: `{ latch: 0, lid: 0.42, cut: 1.32, … }`). The 3D scene animates against it and the page schedules sounds and cuts from it, so picture and sound stay together even when the 3D hasn't loaded. Never chain one layer's `onComplete` into another's start.
- **Timers in a registry.** Schedule through a `later(ms, fn)` that records each id; clear them all on skip, stage change and unmount.
- **Skippable.** A visible Skip jumps to the planned end state, the same frame the animation would reach (pass a counter, `skipSignal`, bumped per press).
- **Reduced motion** skips the sequence and cuts to the result; it is not a faster version. A themed root adds a `prefers-reduced-motion` rule that zeroes CSS animation and transition durations for everything inside it.

## Reels and other position-driven motion

- Track position in content units (items under the marker), not pixels, so a resize mid-roll keeps the same item under the line. Re-measure the item pitch with a `ResizeObserver`.
- Move one strip with `transform: translate3d` (one composited layer) from a `requestAnimationFrame` loop. Never animate `left`, never re-render React per frame.
- Ease with a curve you can differentiate: position `1 − (1 − p)^k` (k ≈ 2.8 gives a long crawl at the end), speed `(1 − p)^(k − 1)`. The speed sets each tick's sound as an item crosses the marker.
- Start with the window already filled and plan enough items after the target to fill a 4K-wide window. Stop off-centre inside the target (8–40% of its width from the centre, either side), never on an edge.

## Effects

Glows, rays and sheens break house rules, so they live only in a themed area (design.md), coloured from its theme file. The grade or rarity sets one group of variables on the scene's root (`--rv`, `--rv-bright`, `--rv-glow`, `--rv-soft`) and every effect reads them.

- **God-rays:** a `repeating-conic-gradient` square (`max(160vw, 160vh)`) masked by `radial-gradient(closest-side, black, transparent 70%)`, turned by the `rotate` property (60 s, linear), faded and scaled in.
- **Burst:** a radial-gradient disc scaling from 0.3 to 1 behind the result.
- **Sparks:** spans carrying their own custom properties (`--x`, `--s`, `--t`, `--d`) from a seeded list, all on one keyframe: variety without JS animation.
- **Flash:** a full-screen overlay keyed by a timestamp so every trigger restarts it; white for an ordinary result, a scaling gold flash for the rare one.
- **Foil:** a gradient at `background-size: 260%` swept by `background-position` under `mix-blend-mode: screen`. **Holo:** a conic gradient under `color-dodge` with an animated `hue-rotate`. Over a logo, these are a second `BrandMark` layer (design.md).
- **Win and lose:** the winner pulses with `filter: brightness()`; the rest dim to `saturate(0.5) brightness(0.8)`.

## Sound

- Synthesised with Web Audio, no files: oscillators and filtered noise through one master bus (a compressor and a convolution reverb whose impulse is generated decaying noise). The game's or product's cues are matched in shape, never sampled.
- Create the `AudioContext` on the first gesture (`primeAudio()` in the click handler; resume it if suspended). Calls before then are no-ops. Never autoplay.
- Mute is a per-browser setting (localStorage, read and written in try/catch) exposed through `useSyncExternalStore`, so every control shows the same state. A mute control sits in the scene's chrome.
- Cues are scheduled from the beat sheet, never from animation callbacks.

## Verification

Watch the scene at real speed in a real browser; headless SwiftShader distorts timing (3d.md). Check Skip at every stage, reduced motion, mute, refresh and Back on each stage's URL, a resize mid-reel, and the odds by simulation.
