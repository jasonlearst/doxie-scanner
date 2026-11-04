---
name: task
description: Work one GitHub issue end to end, from a new session or after /clear — worktree, implement, verify, open the PR. Usage: /task <issue-number>
disable-model-invocation: true
---

Work issue `$ARGUMENTS` in `jasonlearst/doxie-scanner`. The issue is the brief and the PR is the hand-off; this session's chat is disposable.

## 1. Brief

`gh issue view $ARGUMENTS --repo jasonlearst/doxie-scanner --json title,body,comments --jq '.title, .body, (.comments[] | .body)'`. Restate the goal and acceptance criteria in a few lines. If the issue has open questions that change what you build, ask the user before writing code. If its cause is graded suspected, confirming it comes first.

Decide whether the task needs the real scanner: a `[scanner]` criterion, or a change to how the app talks to the scanner or feeds paper (`internal/doxiedx400`, `internal/scsiusb`, the feed path in `internal/scanjobs`). If it does, say so in the restated goal, with roughly how many checks and how long, and read `.claude/skills/task/scanner.md`.

Done when every acceptance criterion is something you can check, and the user knows whether they'll be needed at the scanner.

If `gh issue view $ARGUMENTS --repo jasonlearst/doxie-scanner --json closedByPullRequestsReferences --jq '.closedByPullRequestsReferences[].number'` prints a PR, read it with the command in `CLAUDE.md` and reuse its branch in step 2. A draft with `## Scanner pending` resumes at the scanner checks.

## 2. Worktree

Every task gets its own worktree, before any file is edited. Never `git switch` in the primary checkout: other sessions share it.

Find where the session is with `git rev-parse --show-toplevel`. In this issue's worktree, carry on: this step is done. In the primary checkout, go to the commands below.

In another task's worktree (`/clear` doesn't change the working directory), leave it first:

1. Run `git status --short` and `git log --oneline HEAD --not --remotes=origin` as two separate commands. If either prints anything, stop and name what's unsaved. Don't remove or switch anything.
2. Note the worktree's path, then call `ExitWorktree` with `action: "keep"`: its PR may still be in review. A worktree session refuses the commands below, so don't try them first. If `ExitWorktree` reports no active worktree session, the session isn't isolated and they run from here. If one is refused anyway, stop and ask the user to start again from the primary checkout.

Then, using `main`'s copy of the script because the checkout may be on any branch:

```sh
git fetch origin
bash <(git show origin/main:scripts/worktree.sh) clean --yes --keep <path of the worktree just left>
bash <(git show origin/main:scripts/worktree.sh) new $ARGUMENTS <type>/<issue>-<kebab-summary>
```

Leave out `--keep` if no worktree was left. `type` is the commit prefix: `feat`, `fix`, `ui`, `chore`, …. Tell the user what `clean` removed. `new` prints the worktree's path, and reuses it when the task is being resumed. Call `EnterWorktree` with that `path`, then work only there. Done when `git branch --show-current` in the worktree shows this task's branch.

## 3. Build

From here on, write Bash commands by the worktree-session rules in `CLAUDE.md`. A new worktree needs no setup: Go's module and build caches are shared, and `worktree.sh` copied `.env` and `.env.local`. Read the code, plus `DESIGN.md` for a web-app change. Add or update tests alongside the change; a `fix/` branch adds a test that fails without the fix. Update the README when the API, configuration or documented behavior changes. Done when the code meets each acceptance criterion and its tests exist.

## 4. Verify

Run the tiers in order. A tier starts only when the one before it is green, so the user is never called to the scanner for a build a test could have rejected.

| Tier | Runs | When | Needs the user |
|---|---|---|---|
| **Fast** | `./test.sh` | Every task, as often as you like | No |
| **Browser** | The app run from the worktree, screenshotted headless | Web-app changes, once the code is settled | No |
| **Scanner** | The real DX400, following `.claude/skills/task/scanner.md` | When step 1 said so | Yes |

For the Browser tier, start the app with `run_in_background` on port 18000 plus the issue number, with its data in the scratchpad, then wait for it with `curl -s --retry 30 --retry-connrefused --retry-delay 1 http://127.0.0.1:<port>/api/version`:

```sh
DOXIE_DATA_DIR=<scratchpad>/data DOXIE_LISTEN_ADDR=127.0.0.1:<port> go run .
```

Screens that show a scan need one in that folder. Make it from the test sheet: `mkdir -p <data>/jobs/<id>/pages`, then `sips -s format jpeg ~/Downloads/doxie-test-page.pdf --out <data>/jobs/<id>/pages/001f.jpg`, then write `<data>/jobs/<id>/meta.json` in the shape of `storage.JobMeta`, with one front page `001f` sized 612 × 792 at 72 dpi. The app sees it without a restart. Leave Paperless unset unless the change is in sending. Then start the app from a scratchpad script that loads `.env.local`, and ask before each send, because it files a real document in the user's Paperless.

Screenshot every changed screen with `scripts/screenshot.sh <url> <width> <light|dark> <out.png> [height]` at 390px (phone), 907px (the window the user reviews in) and 1280px, light and dark, and look at each one. Routes are hash URLs such as `http://127.0.0.1:<port>/#/scans/<id>`. Check taps, drags and dialogs in Chrome through the `claude-in-chrome` skill. Stop the app when you're done.

Done when every tier the task needs is green and every acceptance criterion has been observed, not assumed.

## 5. Hand off

A scanner task has already pushed and opened its draft PR: finish it as `scanner.md` says, then go to item 4.

1. Commit with Conventional Commit subjects and the session's attribution trailers, sanitized by the *Public repository* rules in `CLAUDE.md`.
2. `git push -u origin HEAD`.
3. `gh pr create --repo jasonlearst/doxie-scanner --base main --body-file <file>`, with a sanitized body of: summary, `Closes #$ARGUMENTS`, what changed, verification (commands and results, screens and widths checked, screenshot paths), a `## Scanner` section (results, pending with the runbook, or not needed and why), and anything left for a follow-up issue.
4. Wait for CI with `gh pr checks <pr> --repo jasonlearst/doxie-scanner --watch`, and fix and push until it's green.
5. Keep the worktree: the user may test it and review fixes may follow. A later `/task` removes it once the PR is merged.
6. Give the user the PR URL, the follow-ups, the worktree's path and how to try it from there. Work the issue raised but didn't ask for goes to `/file-issue`, not into this PR.
