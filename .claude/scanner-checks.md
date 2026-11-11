# Scanner checks

The owner tier's runbook, which `/task` follows in place of its generic owner-tier steps. The real DX400 needs the user: someone has to bring it to this Mac, switch it on, load paper and clear jams. Everything a session can do alone comes first, and the user comes to the scanner once.

## Order

1. **Prepare.** Fast and Browser checks green, code committed.
2. **Write the checks** in the format below, with all setup in one request.
3. **Open a draft PR.** Push, then open the PR as a draft with the checks under `## Scanner pending`. Do this before asking the user anything: an unanswered question then loses nothing, and the PR can't be merged by accident.
4. **Call the user.** Send a `PushNotification` if the session has that tool. Then ask a blocking `AskUserQuestion` with the setup: the scanner plugged into this Mac and switched on, the paper each check needs (copies of the printed test sheet unless a check needs something else), how many checks and roughly how long.
5. **Preflight.** Start the worktree's app as *Running the app* in `CLAUDE.md` says, and read `GET /api/scanner/status`. Only one process can hold the scanner: if another local instance has it, ask before stopping one you didn't start. If it isn't connected, tell the user exactly what to do (plug it in, press the power button) and stop.
6. **Run the checks** one at a time, waiting for the user's result before giving the next. Drive them through the worktree's app, as a user would. For a check of the protocol itself, stop the app and run `go test -tags hardware -run TestHardwareScan -v ./internal/doxiedx400`, whose variables are in the README's Development section.
7. **On a failure,** fix it, re-run the lower tiers the fix touches, push, restart the app and repeat only the affected checks.
8. **Finish.** Replace `## Scanner pending` with the results table below, update the PR body, and once every check has passed, mark the PR ready. Stop the app so the scanner is free, and tell the user they can move it back.

If the user doesn't answer, stop and say so. There's no timeout: the draft PR holds the checks, and a PR with pending checks stays a draft.

## Resuming

`/task <issue>` on an issue whose draft PR has `## Scanner pending` skips to step 4 and runs only the checks still pending.

## Check format

One block per check. Numbers run through the whole check, and every step belongs to "You" or "I'll":

```
Scanner check 2 of 3: two sheets from the top tray scan straight through (about 1 min)

You:
1. Put two copies of the test sheet in the top tray.
2. Tell me when they're in.

I'll start a 300 dpi two-sided scan from the app.

Expected: both sheets feed without a pause, and the scan has 4 pages.
Reply: pass, or fail and what you saw.
```

Steps name the real paper path, buttons and screens. The user handles paper and presses the scanner's buttons, and every command is one the session runs itself. Plan around the quirks in the README's *Using the scanner* section: paper left in the front tray for about a minute jams, and the scanner switches itself off after a long idle.

## PR Scanner section

Every PR has a `## Scanner` section, in one of three forms:

- **Not needed:** one line saying why.
- **Pending:** titled `## Scanner pending`, holding the checks, on a draft PR.
- **Done:** this table.

```
| Check | Expected | Seen | Feeder, dpi, sides |
|---|---|---|---|
```
