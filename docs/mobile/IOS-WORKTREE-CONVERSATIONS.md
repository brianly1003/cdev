# iOS Worktree Conversations

## Goal

Show Claude conversations from repo worktrees inside the existing workspace conversation flow on iPhone.

The user should not need to think in terms of separate workspaces for:

- the main repo
- Claude-created git worktrees
- task-runner worktrees created by `cdev`

They should think in terms of:

- one workspace
- many related sessions

## UX Decision

Do not create a separate "worktree conversations" screen.

Use the existing `/resume` session picker and session history viewer as the primary surface.

## Expected Behavior

When the selected workspace is `Lazy`, `/resume` should list:

- sessions from `/Users/brianly/Projects/Lazy`
- sessions from git worktrees of that repo
- sessions started by `cdev` in task worktrees for that repo

Each row should make the source clear without splitting the navigation model.

## Session Picker Requirements

### Row Metadata

Each row should be able to show:

- session summary
- running/historical state
- branch when available
- worktree/project badge when `project_path` differs from the workspace root

Recommended row labels:

- `Main` for the workspace root
- worktree name derived from `project_path` for worktree sessions

### Search

Search should match:

- summary
- session ID
- project path
- derived project/worktree name

### Filters

Recommended filters:

- `All`
- `Main`
- `Worktrees`
- `Running`

This keeps the main entry point stable while making worktree sessions discoverable.

## Session History Requirements

The session history header should show:

- summary
- runtime badge
- message count
- branch when available
- project/worktree label
- running/historical state

If the session is resumable, keep the existing resume button. The resume action should still operate on the parent workspace and selected session ID.

## Running Task Sessions

Running task conversations should reuse the same history viewer. They do not need a second transcript system.

The minimum viable behavior is:

- a running worktree session appears in `/resume`
- opening it shows the live or latest transcript content
- the header indicates which worktree produced the session

## Data Contract Needed From `cdev`

For Claude workspace session history, iOS needs:

- `session_id`
- `summary`
- `message_count`
- `last_updated`
- `status`
- `workspace_id`
- `project_path`

Without `project_path`, worktree sessions are indistinguishable from main-repo sessions.

## Non-Goals

- no separate top-level worktree workspace list
- no separate "external sessions transcript" navigation tree
- no new API family dedicated only to worktrees in v1
