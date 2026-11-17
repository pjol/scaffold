package docs

// Built-in templates, authored here as Markdown (the import syntax:
// headings, lists, to-dos, quotes, callouts as "> <emoji> text", code,
// tables, <details> toggles) plus database definitions. A line that is
// exactly {{database:<key>}} or {{page:<key>}} becomes that inline database
// or a link to that subpage. Database rows are examples that show how the
// views work; people delete them.

// tplProp is a database property: options for select, multi_select and
// status (status options as "Name|group", group todo, doing or done).
type tplProp struct {
	Name    string
	Type    string
	Options []string
	Format  string // number format
}

type tplView struct {
	Name    string
	Type    string // table | board | list | calendar
	GroupBy string // property name (board)
	Date    string // property name (calendar)
	Sort    string // property name, ascending
	Desc    bool
}

// tplDatabase is a database a template creates. Row values are keyed by
// property name: option names for selects, "+Nd" (days from today) for
// dates, "@me" for a person property (the person using the template).
type tplDatabase struct {
	Key   string
	Title string
	Icon  string
	Props []tplProp
	Views []tplView
	Rows  []map[string]any
}

type builtinTemplate struct {
	Key       string
	Name      string
	Icon      string
	Category  string
	Info      string
	Cover     string // a coverGallery name
	FullWidth bool
	Markdown  string
	Databases []tplDatabase     // inline databases ({{database:key}})
	Subpages  []builtinTemplate // subpages ({{page:key}})
	Database  *tplDatabase      // the template is a database page
}

var statusTodo = []string{"Not started|todo", "In progress|doing", "Done|done"}

var builtinTemplates = []builtinTemplate{
	{
		Key: "meeting-notes", Name: "Meeting notes", Icon: "📝", Category: "Meetings", Cover: "glacier",
		Info: "Agenda, notes, decisions and action items for one meeting.",
		Markdown: `> 🗓️ **Date:** @today · **Attendees:** add people with @

## Agenda

1. Updates
2. Discussion
3. Next steps

## Notes

-

## Decisions

-

## Action items

- [ ] Owner: task, due date
- [ ]
`,
	},
	{
		Key: "one-on-one", Name: "1:1", Icon: "🤝", Category: "Meetings", Cover: "orchid",
		Info: "A running agenda for one-to-one meetings: check-in, topics, feedback and follow-ups.",
		Markdown: `> 💬 Add topics before the meeting. Newest meeting at the top.

## Check-in

- How are you doing this week?
- What's taking most of your energy?

## Their topics

-

## My topics

-

## Feedback

<details>
<summary>What's going well</summary>

-

</details>

<details>
<summary>What could be better</summary>

-

</details>

## Action items

- [ ]
`,
	},
	{
		Key: "weekly-agenda", Name: "Weekly team meeting", Icon: "🗓️", Category: "Meetings", Cover: "tide",
		Info: "Wins, numbers, blockers and decisions for a recurring team meeting.",
		Markdown: `> ⏱️ 30 minutes. Fill in your section before the meeting starts.

## Wins

-

## Numbers

| Metric | This week | Last week | Note |
| --- | --- | --- | --- |
| | | | |

## Blockers

-

## Decisions needed

-

## Action items

- [ ]
`,
	},
	{
		Key: "retrospective", Name: "Retrospective", Icon: "🔁", Category: "Meetings", Cover: "dusk",
		Info: "What went well, what didn't and what to try next, with owners.",
		Markdown: `> 🧭 Everyone adds notes for 10 minutes, then the group groups and votes.

## What went well

-

## What didn't go well

-

## Ideas to try

-

## Action items

- [ ] Owner: action
`,
	},
	{
		Key: "project-brief", Name: "Project brief", Icon: "🚀", Category: "Projects", Cover: "dawn",
		Info: "Why the project exists, what success looks like, scope, milestones and risks.",
		Markdown: `> 📌 **Owner:** @ · **Status:** Draft · **Target:** date

## Overview

One paragraph a newcomer can read to understand the project.

## Problem

What is broken or missing today, for whom, and how we know.

## Goals

-

## Non-goals

-

## Scope

| In scope | Out of scope |
| --- | --- |
| | |

## Milestones

| Milestone | Date | Owner |
| --- | --- | --- |
| Kickoff | | |
| First release | | |
| Launch | | |

## Risks

<details>
<summary>Risk: describe it</summary>

Likelihood, impact and what we'll do about it.

</details>

## Team

-
`,
	},
	{
		Key: "roadmap", Name: "Roadmap", Icon: "🗺️", Category: "Projects",
		Info: "Initiatives by status and quarter, with a board, a table and a calendar of target dates.",
		Database: &tplDatabase{
			Key: "roadmap", Title: "Roadmap", Icon: "🗺️",
			Props: []tplProp{
				{Name: "Status", Type: "status", Options: []string{"Planned|todo", "In progress|doing", "Shipped|done"}},
				{Name: "Quarter", Type: "select", Options: []string{"Q1", "Q2", "Q3", "Q4"}},
				{Name: "Team", Type: "select", Options: []string{"Product", "Design", "Engineering", "Marketing"}},
				{Name: "Priority", Type: "select", Options: []string{"High", "Medium", "Low"}},
				{Name: "Target date", Type: "date"},
				{Name: "Owner", Type: "person"},
			},
			Views: []tplView{
				{Name: "By status", Type: "board", GroupBy: "Status"},
				{Name: "All initiatives", Type: "table", Sort: "Target date"},
				{Name: "Timeline", Type: "calendar", Date: "Target date"},
				{Name: "By quarter", Type: "board", GroupBy: "Quarter"},
			},
			Rows: []map[string]any{
				{"title": "Example initiative: describe the outcome", "Status": "Planned", "Quarter": "Q1", "Priority": "High", "Target date": "+30d", "Owner": "@me"},
				{"title": "Example initiative in progress", "Status": "In progress", "Quarter": "Q1", "Priority": "Medium", "Target date": "+14d"},
				{"title": "Example shipped initiative", "Status": "Shipped", "Quarter": "Q1", "Priority": "Low", "Target date": "+0d"},
			},
		},
	},
	{
		Key: "task-tracker", Name: "Task tracker", Icon: "✅", Category: "Projects",
		Info: "Tasks with status, assignee, priority and due dates, on a board, a table and a calendar.",
		Database: &tplDatabase{
			Key: "tasks", Title: "Tasks", Icon: "✅",
			Props: []tplProp{
				{Name: "Status", Type: "status", Options: statusTodo},
				{Name: "Assignee", Type: "person"},
				{Name: "Due", Type: "date"},
				{Name: "Priority", Type: "select", Options: []string{"High", "Medium", "Low"}},
				{Name: "Tags", Type: "multi_select", Options: []string{"Bug", "Feature", "Chore"}},
			},
			Views: []tplView{
				{Name: "Board", Type: "board", GroupBy: "Status"},
				{Name: "All tasks", Type: "table", Sort: "Due"},
				{Name: "Calendar", Type: "calendar", Date: "Due"},
				{Name: "List", Type: "list"},
			},
			Rows: []map[string]any{
				{"title": "Example task: write the first draft", "Status": "In progress", "Assignee": "@me", "Due": "+2d", "Priority": "High"},
				{"title": "Example task: review with the team", "Status": "Not started", "Due": "+5d", "Priority": "Medium"},
				{"title": "Example task: share the result", "Status": "Not started", "Due": "+7d", "Priority": "Low"},
			},
		},
	},
	{
		Key: "engineering-rfc", Name: "Engineering RFC", Icon: "🧪", Category: "Engineering", Cover: "slate",
		Info: "Propose a technical change: motivation, design, alternatives, rollout and open questions.",
		Markdown: `> 📄 **Status:** Draft · **Authors:** @ · **Reviewers:** @ · **Decision by:** date

## Summary

Two or three sentences: what changes and why.

## Motivation

What problem this solves, with evidence. What happens if we do nothing.

## Detailed design

Describe the change: data model, APIs, components, failure modes.

` + "```go\n// Sketch the key interface or schema here.\n```" + `

## Alternatives considered

| Option | Pros | Cons |
| --- | --- | --- |
| | | |

## Rollout

- [ ] Behind a flag
- [ ] Migration plan
- [ ] Monitoring and alerts
- [ ] Rollback plan

## Security and privacy

What data this touches and who can reach it.

## Open questions

-
`,
	},
	{
		Key: "design-spec", Name: "Design spec", Icon: "🎨", Category: "Design", Cover: "aurora",
		Info: "Context, users, requirements, flows and an accessibility checklist for a design.",
		Markdown: `> 🎯 **Designer:** @ · **Status:** Exploring · **Figma or prototype:** link

## Context

Why we're designing this now.

## Users and jobs

- **Who:**
- **Job to be done:**

## Requirements

| Requirement | Priority | Notes |
| --- | --- | --- |
| | Must | |
| | Should | |

## Flows

1. Entry point
2. Main steps
3. Success and error states

## Visual design

Key screens, states (empty, loading, error) and motion.

## Accessibility

- [ ] Contrast meets WCAG AA
- [ ] Works with the keyboard alone
- [ ] Screen reader labels
- [ ] Respects reduced motion
- [ ] Works at 375 px

## Open questions

-
`,
	},
	{
		Key: "bug-tracker", Name: "Bug tracker", Icon: "🐞", Category: "Engineering",
		Info: "Bugs by status and severity, with a triage board and a table sorted by severity.",
		Database: &tplDatabase{
			Key: "bugs", Title: "Bugs", Icon: "🐞",
			Props: []tplProp{
				{Name: "Status", Type: "status", Options: []string{"Reported|todo", "Triaged|todo", "In progress|doing", "Fixed|done", "Won't fix|done"}},
				{Name: "Severity", Type: "select", Options: []string{"S1 Critical", "S2 Major", "S3 Minor", "S4 Trivial"}},
				{Name: "Area", Type: "select", Options: []string{"Web", "Mobile", "API", "Infrastructure"}},
				{Name: "Assignee", Type: "person"},
				{Name: "Reported", Type: "date"},
				{Name: "Found in", Type: "text"},
			},
			Views: []tplView{
				{Name: "Triage", Type: "board", GroupBy: "Status"},
				{Name: "By severity", Type: "table", Sort: "Severity"},
				{Name: "By area", Type: "board", GroupBy: "Area"},
			},
			Rows: []map[string]any{
				{"title": "Example bug: what happens, and what you expected", "Status": "Reported", "Severity": "S3 Minor", "Area": "Web", "Reported": "+0d", "Found in": "version or page"},
			},
		},
	},
	{
		Key: "team-wiki", Name: "Team wiki", Icon: "📚", Category: "Team", Cover: "meadow",
		Info: "A home for your team: start here, how you work, tools and a glossary as subpages.",
		Markdown: `> 👋 Everything someone joining the team needs, in one place. Keep it current: each page names an owner.

## Start here

{{page:start-here}}

{{page:how-we-work}}

{{page:tools}}

{{page:glossary}}

## Quick links

- Team channel:
- Planning board:
- On-call rota:
`,
		Subpages: []builtinTemplate{
			{Key: "start-here", Name: "Start here", Icon: "👋", Markdown: `## Who we are

What the team owns and who's on it.

## Your first week

- [ ] Meet your onboarding buddy
- [ ] Get access to the tools
- [ ] Read How we work
- [ ] Ship a small change
`},
			{Key: "how-we-work", Name: "How we work", Icon: "🧭", Markdown: `## Rituals

| Meeting | When | Purpose |
| --- | --- | --- |
| Planning | | |
| Standup | | |
| Retro | | |

## Decisions

How we make and record decisions.

## Communication

Where to ask questions, and how quickly we answer.
`},
			{Key: "tools", Name: "Tools and access", Icon: "🛠️", Markdown: `| Tool | What for | How to get access |
| --- | --- | --- |
| | | |
`},
			{Key: "glossary", Name: "Glossary", Icon: "📖", Markdown: `<details>
<summary>Term</summary>

What it means here.

</details>
`},
		},
	},
	{
		Key: "onboarding", Name: "Onboarding checklist", Icon: "🌱", Category: "Team", Cover: "citrus",
		Info: "A three-week checklist for someone new: people to meet, tools and a first project.",
		Markdown: `> 🌱 Welcome! Work through this with your buddy. Tick things off as you go.

## Week 1: settle in

- [ ] Accounts and tools set up
- [ ] Meet your manager and buddy
- [ ] Read the team wiki
- [ ] Join the team channels

## Week 2: learn the work

- [ ] Shadow a planning meeting
- [ ] Pair on a task
- [ ] Ship a first small change

## Week 3: own something

- [ ] Pick up a project
- [ ] Share what you've learned
- [ ] 30-minute feedback chat
`,
	},
	{
		Key: "okrs", Name: "OKRs", Icon: "🎯", Category: "Planning", Cover: "ember",
		Info: "Objectives with measurable key results, owners and progress you update each week.",
		Markdown: `> 🎯 **Period:** quarter · Update progress every Friday.

## Objective 1: describe the outcome you want

{{database:key-results}}

## How we score

- **0.7 and above:** on track
- **0.4 to 0.7:** at risk
- **Below 0.4:** off track
`,
		Databases: []tplDatabase{{
			Key: "key-results", Title: "Key results", Icon: "📈",
			Props: []tplProp{
				{Name: "Objective", Type: "select", Options: []string{"Objective 1", "Objective 2", "Objective 3"}},
				{Name: "Progress", Type: "number", Format: "percent"},
				{Name: "Status", Type: "status", Options: []string{"Not started|todo", "On track|doing", "At risk|doing", "Achieved|done"}},
				{Name: "Owner", Type: "person"},
			},
			Views: []tplView{
				{Name: "All key results", Type: "table"},
				{Name: "By objective", Type: "board", GroupBy: "Objective"},
			},
			Rows: []map[string]any{
				{"title": "Example: raise weekly active teams from 40 to 60", "Objective": "Objective 1", "Progress": 25.0, "Status": "On track", "Owner": "@me"},
				{"title": "Example: cut onboarding time from 5 days to 2", "Objective": "Objective 1", "Progress": 0.0, "Status": "Not started"},
			},
		}},
	},
	{
		Key: "decision-log", Name: "Decision log", Icon: "⚖️", Category: "Planning",
		Info: "Record decisions with their status, owner, date and impact, so the why isn't lost.",
		Database: &tplDatabase{
			Key: "decisions", Title: "Decision log", Icon: "⚖️",
			Props: []tplProp{
				{Name: "Status", Type: "status", Options: []string{"Proposed|todo", "Decided|done", "Revisited|doing"}},
				{Name: "Decided on", Type: "date"},
				{Name: "Owner", Type: "person"},
				{Name: "Area", Type: "select", Options: []string{"Product", "Engineering", "Design", "Operations"}},
				{Name: "Impact", Type: "select", Options: []string{"High", "Medium", "Low"}},
			},
			Views: []tplView{
				{Name: "All decisions", Type: "table", Sort: "Decided on", Desc: true},
				{Name: "By status", Type: "board", GroupBy: "Status"},
			},
			Rows: []map[string]any{
				{"title": "Example: what was decided, in one line", "Status": "Decided", "Decided on": "+0d", "Owner": "@me", "Impact": "Medium"},
			},
		},
	},
	{
		Key: "product-requirements", Name: "Product requirements", Icon: "📋", Category: "Product", Cover: "lagoon",
		Info: "Problem, goals and metrics, user stories, requirements and a launch plan for a feature.",
		Markdown: `> 🧩 **PM:** @ · **Status:** Draft · **Engineering:** @ · **Design:** @

## Problem

Who has it, how often, and what it costs them.

## Goals and metrics

| Goal | Metric | Target |
| --- | --- | --- |
| | | |

## User stories

- [ ] As a ___, I want ___ so that ___.
- [ ]

## Requirements

| Requirement | Priority | Notes |
| --- | --- | --- |
| | P0 | |
| | P1 | |

## Launch plan

1. Internal preview
2. Beta
3. General availability

## FAQ

<details>
<summary>Question people will ask</summary>

The answer.

</details>
`,
	},
	{
		Key: "content-calendar", Name: "Content calendar", Icon: "📣", Category: "Marketing",
		Info: "Plan posts and articles by publish date and channel, on a calendar and a status board.",
		Database: &tplDatabase{
			Key: "content", Title: "Content calendar", Icon: "📣",
			Props: []tplProp{
				{Name: "Status", Type: "status", Options: []string{"Idea|todo", "Writing|doing", "In review|doing", "Published|done"}},
				{Name: "Publish date", Type: "date"},
				{Name: "Channel", Type: "multi_select", Options: []string{"Blog", "Newsletter", "Social", "Video"}},
				{Name: "Owner", Type: "person"},
			},
			Views: []tplView{
				{Name: "Calendar", Type: "calendar", Date: "Publish date"},
				{Name: "Pipeline", Type: "board", GroupBy: "Status"},
				{Name: "All content", Type: "table", Sort: "Publish date"},
			},
			Rows: []map[string]any{
				{"title": "Example post: working title", "Status": "Writing", "Publish date": "+7d", "Channel": []any{"Blog", "Newsletter"}, "Owner": "@me"},
			},
		},
	},
	{
		Key: "reading-list", Name: "Reading list", Icon: "📖", Category: "Personal",
		Info: "Books, articles and papers to read, with status, type and a link.",
		Database: &tplDatabase{
			Key: "reading", Title: "Reading list", Icon: "📖",
			Props: []tplProp{
				{Name: "Status", Type: "status", Options: []string{"To read|todo", "Reading|doing", "Finished|done"}},
				{Name: "Type", Type: "select", Options: []string{"Book", "Article", "Paper", "Video"}},
				{Name: "Author", Type: "text"},
				{Name: "Link", Type: "url"},
				{Name: "Recommend", Type: "checkbox"},
			},
			Views: []tplView{
				{Name: "By status", Type: "board", GroupBy: "Status"},
				{Name: "All", Type: "table"},
			},
			Rows: []map[string]any{
				{"title": "Example: a book you want to read", "Status": "To read", "Type": "Book"},
			},
		},
	},
	{
		Key: "daily-journal", Name: "Daily journal", Icon: "✍️", Category: "Personal", Cover: "sand",
		Info: "Short prompts for planning the day and reflecting at the end of it.",
		Markdown: `## Today's focus

- [ ] The one thing that matters most
- [ ]
- [ ]

## Notes

-

## End of day

> 🌙 What went well? What would I do differently tomorrow?
`,
	},
}
