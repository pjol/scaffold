# Branch scopes

One file per branch, `branch-scopes/<branch-name>.md`, up to date before the branch merges: date range, repos touched, total active hours and how they were measured, features large to small with hours each (nearest 0.1 h), a table of smaller fixes, totals, and volume (files, insertions/deletions, migrations, new routes). Later sittings append `# Round N`.

Hours are measured wall-clock time from session-transcript timestamps (the `time-accounting` skill: `~/SKILLS/time-accounting`, github.com/pjol/SKILLS), never estimated from diff size. If the clock wasn't consulted, report volume and say time was not measured.
