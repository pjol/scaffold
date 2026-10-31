# Writing

Meaning first: every string says the fact the reader needs, then stops. This applies to UI copy, errors, email, notifications, docs and commit messages.

## Rules

1. **Lead with the meaning.** Put the thing the reader acts on first. "Payment failed. Try another card." not "Unfortunately, we were unable to process…".
2. **Factual where flourish isn't needed.** Functional copy (errors, confirmations, hints, receipts, admin tools, transactional email) is one sentence, two at most.
3. **No subtitles.** Never an explanatory line under a heading, label, row, card, dialog title or empty state. If context is needed, it goes in a tooltip (`info` prop → InfoTip). Components have no `description`/`subtitle` props.
4. **No meta-language.** Talk about the thing, never about the screen, the design or the writing ("Here you can…", "This page shows…").
5. **Numerals, not words** in functional strings: "20 minutes", "3 new messages".
6. **Sentence case** everywhere: titles, buttons, tabs, menu items. No ALL-CAPS labels except tiny overlines.
7. **Buttons are verbs** that name the outcome: "Save", "Send", "Delete account", "Pay $24". Never "Submit", "OK", "Click here".
8. **Errors say what happened and what to do**, never blame, never expose internals: "That file is over 10 MB. Choose a smaller one." The API's `message` field is user-facing copy and follows these rules.
9. **Empty states**: a short title and one action. "No messages yet" + "Start a conversation". No paragraph.
10. **Confirmations of destructive actions** name the object and the consequence: "Delete this post? It can't be undone."
11. **Warmth survives as plainness**: "Thank you!" is fine; a simile in a password reset is not.
12. **No fabrication.** Every claim, number, testimonial or history must be true and checkable. No fake stats, fake users or placeholder testimonials anywhere.

## Banned

Words: seamless, elevate, unlock, delve, curated, journey, experience (as a noun for the product), effortless, empower, supercharge, game-changer, cutting-edge, revolutionize, robust (in UI), "we're passionate about", "simply", "just" as a softener, "Whether you're…", "Oops!", "Uh-oh".

Patterns: three-card feature rows (icon + bold word + sentence) on any page; left-border accent bars; gradient text; emoji in UI chrome; decorative fake data; drop shadows at rest on flat UI.

`scaffold doctor` flags the banned words in `.tsx` strings; reviewers catch the rest.

## Worked examples

| Instead of | Write |
|---|---|
| "A code lasts twenty minutes, about as long as it takes to drink what you have already poured." | "Your code expires in 20 minutes." |
| "Your email is confirmed, thank you. Anything you ordered as a guest under that address has just been attached to this account…" | "Your email is confirmed. Thank you!" |
| Heading "Notifications" + subtitle "Choose how you'd like to hear from us" | Heading "Notifications" (an InfoTip if a toggle needs explaining) |
| "Oops! Something went wrong. Please try again later." | "Couldn't save. Check your connection and try again." |
| "Unlock premium features" | "Upgrade to Pro" |

## Docs and agent-facing text

Same rules, plus: write for an agent with no context, state the rule before the reason, use tables for reference data, and cut anything that doesn't change what the reader does.
