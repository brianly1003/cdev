# Worktree Session Architecture

## Problem

Claude stores transcripts under `~/.claude/projects/<encoded-project-path>`. A git worktree is a different project path than the parent repo, so Claude creates a different transcript directory for each worktree.

`cdev` previously assumed one workspace path mapped to one Claude project path:

- worktree creation could default to `"."`, causing `git worktree add` to run from the `cdev` repo instead of the target workspace repo
- webhook and LIVE session resolution required an exact path match
- historical session lookup, message lookup, and file watching only checked the parent workspace path
- session-file watcher state was keyed one-per-workspace, which blocked concurrent worktree sessions for the same logical repo

The result was:

- task worktrees could be created from the wrong repository
- Claude sessions started in worktrees did not resolve back to the parent workspace
- `/resume` and session history in `cdev-ios` missed worktree conversations

## Design Goals

- Keep one logical workspace per repo family
- Treat the Claude project path as separate from the workspace root
- Allow one workspace to surface transcripts from the root repo and all related worktrees
- Keep `cdev-ios` subscribed to the parent workspace while still showing which worktree produced a session

## Core Concepts

### Workspace

The logical repo the user subscribes to in `cdev` and `cdev-ios`.

Example:

- workspace name: `Lazy`
- workspace path: `/Users/brianly/Projects/Lazy`

### Project Path

The exact working directory Claude used when it created the transcript.

Examples:

- `/Users/brianly/Projects/Lazy`
- `/Users/brianly/Projects/Lazy/.claude/worktrees/fix-login`
- `/tmp/cdev/worktrees/1234abcd-fix-bug`

### Repo Family

The git repository identity shared by a repo root and all of its worktrees.

`cdev` resolves this using:

```bash
git rev-parse --path-format=absolute --git-common-dir
```

Two paths belong to the same logical workspace if they resolve to the same git common dir.

## Runtime Rules

### Worktree Creation

- The source repo path must come from the resolved workspace, never from `"."`
- `AgentTask.WorktreePath` means the created worktree output path only
- Claude must start with `cmd.Dir` set to the created worktree path

### Workspace Resolution

When `cdev` receives a path from hooks, LIVE detection, or task execution:

1. Resolve exact workspace ID/name/path if possible
2. If the path is inside a workspace root, map it to that workspace
3. If the path is a git worktree with the same git common dir as a workspace root, map it to that workspace

### Session Lookup

For Claude sessions, `cdev` must no longer assume:

```text
workspace path -> one ~/.claude/projects directory
```

Instead it must resolve:

```text
workspace -> candidate project paths -> session file
```

Candidate project paths are:

- the workspace root
- active managed session project paths
- git worktrees reported by `git worktree list --porcelain`

### Public Session Metadata

Claude history and session responses should expose:

- `workspace_id`: parent logical workspace
- `project_path`: exact Claude working directory

`project_path` is required so mobile clients can distinguish:

- main repo session
- worktree session
- task-runner worktree session

## API Implications

The existing workspace-scoped APIs remain the primary interface:

- `workspace/session/history`
- `workspace/session/messages`
- `workspace/session/watch`

The contract changes are:

- history aggregates sessions from all project paths in the workspace repo family
- each session includes `project_path`
- message lookup and file watching resolve the real session file from `project_path`

No separate worktree-only API is needed in v1.

## iOS Implications

`cdev-ios` should keep one workspace-level resume surface. Worktree sessions are not separate top-level workspaces.

The UI should:

- show worktree sessions in the existing session picker
- label rows whose `project_path` differs from the workspace root
- allow filtering between main sessions and worktree sessions
- open the same history viewer, with the project/worktree label visible in the header

## Rollout

1. Fix worktree creation to use the target workspace repo path
2. Make workspace resolution worktree-aware via git common dir
3. Make Claude history/messages/watch resolve across root + worktree project paths
4. Surface `project_path` in iOS resume/history UI
