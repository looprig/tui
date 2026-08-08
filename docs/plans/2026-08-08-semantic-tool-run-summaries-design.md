# Semantic Tool-Run Summaries

## Goal

Replace collapsed tool-run labels such as `3 tools · Bash, ReadFile, ReadFile`
with compact activity summaries such as `1 command executed, 2 files read`.

## Design

The TUI remains the owner of this presentation-only vocabulary. The transcript
continues to store concrete tool calls unchanged; only `toolRunSummary` groups
committed calls into semantic activity categories and renders each category with
correct singular or plural wording.

Known built-in tools use these categories:

| Tool | Singular | Plural |
|---|---|---|
| `ReadFile` | `file read` | `files read` |
| `WriteFile` | `file written` | `files written` |
| `EditFile` | `file edited` | `files edited` |
| `Glob` | `file search` | `file searches` |
| `Grep` | `content search` | `content searches` |
| `Bash` | `command executed` | `commands executed` |
| `ProcessOutput` | `process checked` | `processes checked` |
| `ProcessInput` | `process input sent` | `process inputs sent` |
| `ProcessStop` | `process stopped` | `processes stopped` |
| `WebSearch` | `web search` | `web searches` |
| `Fetch` | `page fetched` | `pages fetched` |
| `TaskCreate` | `task created` | `tasks created` |
| `TaskUpdate` | `task updated` | `tasks updated` |
| `TaskGet` | `task read` | `tasks read` |
| `TaskList` | `task list viewed` | `task lists viewed` |
| `AskUser` | `question asked` | `questions asked` |
| `Skill` | `skill loaded` | `skills loaded` |
| `StartAgent` | `agent started` | `agents started` |
| `MessageAgent` | `agent message sent` | `agent messages sent` |
| `ListAgents` | `agent check` | `agent checks` |
| `StopAgent` | `agent stopped` | `agents stopped` |

Unrecognized tools, including dynamic MCP tools, share a fallback category:
`tool used` / `tools used`. Categories appear in order of first use. A category
gets ` ✗` when any call assigned to it failed or was cancelled, and the summary
node remains failure-tinted when any category failed.

`Subagent` remains unchanged. It is a synthetic TUI card produced by reconciling
`StartAgent` with child-loop activity, and its existing named, nested presentation
contains more useful information than an activity count. The reserved
`_looprig_final_output` mechanism is internal structured-output plumbing and does
not participate in ordinary TUI tool runs.

## Testing

Table-driven unit tests cover every known mapping, singular and plural grammar,
first-use ordering, unknown aggregation, mixed known and unknown calls, and
category-level failure markers. Existing render tests continue to verify that the
collapsed rail node uses the formatter and receives failure tinting.
