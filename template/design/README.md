# Design

Design rounds for this app, run with the process in `~/scaffold/concrete/design-process.md`: three complete options → five iterations of the pick → lock + five palettes → final → adopt into `packages/tokens`.

```
design/
  assets/         shared content kit: data.js (generated from the dev DB/API), content.js (real copy), kit.js (unstyled helpers), icons
  index.html      the current comparison board
  <option>/       index.html (style guide), screens, tokens.css, shots/
  tools/capture.mjs   screenshots + raw-colour lint for an option
```

`node design/tools/capture.mjs design/<option>` after every change to an option; it must pass.

## Status

No rounds yet. Record each round here: date, the options, the user's pick and constraints, and where the result lives.
