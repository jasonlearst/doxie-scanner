---
name: file-issue
description: Analyse a bug or idea you noticed while using the scanner app and log it as a sanitized GitHub issue, ready for a fresh /task session. Usage: /file-issue <what you saw>
disable-model-invocation: true
---

Capture `$ARGUMENTS` as an issue a future session can work cold, with no access to this conversation. Analyse and specify; leave the fix to `/task`. When `$ARGUMENTS` describes several unrelated problems, give each its own issue.

Investigate without editing the checkout: other sessions share it. What you gather can hold personal details, and step 5 strips them before anything leaves this machine.

1. **Pin down what happened.** Which screen or action, what the user expected, what they got, and on which device, browser and window width. Read the version from `GET /api/version` on the instance they were using, and ask for its address if you don't have it. Put any screenshot into words: `gh` can't attach images. If you can't name the screen and the expected behavior, ask one round of questions. Done when someone else could reproduce it from your description.

2. **Locate.** Find the code behind it as it is on `origin/main`, whichever branch the checkout is on: `git fetch origin`, then `git grep -n <pattern> origin/main` and `git show origin/main:<path>`. The README's package table maps the areas. Note `path:line` for each part. Read related issues, open and closed, with `gh issue list --repo jasonlearst/doxie-scanner --state all --search "<words>"`. Done when you can name where the current behavior comes from.

3. **Analyse.** For a bug, find the cause and the evidence for it: the code path, read-only `GET`s to the instance's API (the README lists them; `GET /api/scans/{id}` has a scan's pages, edits and uploads), and logs the user shares. When a test would settle it, write one in a throwaway worktree, `git worktree add --detach <scratchpad>/probe origin/main`, run it, keep it for the issue, then `git worktree remove --force <scratchpad>/probe`. In a task worktree session that command is refused, so leave the test as a suggestion. Grade the cause **confirmed** (reproduced, or seen in a response or log) or **suspected** (read from code), and name what would confirm a suspicion. If the instance runs a release older than `origin/main`, check `git log --oneline v<version>..origin/main -- <paths>`; when that already fixed it, tell the user which commit and file nothing. For a feature, check whether Paperless-ngx already does it, since `PRODUCT.md` leaves OCR, rotation and filing to Paperless. Done when the cause is confirmed, or the issue says exactly what's left to check.

4. **Write** the body to a scratchpad file with Write, in these sections:
   - **Problem:** what the user did, expected and got, from their point of view: steps to reproduce, the version, and the device, browser and width for anything on screen.
   - **Analysis:** where the behavior comes from, as `path:line` at the `origin/main` commit (`git rev-parse --short origin/main`); the cause, graded, with its evidence; the probe test, if you wrote one.
   - **Proposed behavior:** concrete, for each surface it touches: phone layout, the wider layouts (scan list beside the scan from 900px, sidebar from 1100px), the scanner and its button, sending to Paperless, the API, storage and retention.
   - **Acceptance criteria:** a checkbox list, each item observable, including a regression test against the fake scanner or fake Paperless, `./test.sh` green, and README updates when the API, configuration or documented behavior changes. Start an item only the real scanner can prove with `[scanner]`.
   - **Open questions:** the decisions only the user can make, each with your proposed answer.

5. **Sanitize.** The issue is public for good. Read the title and body as a stranger would, and apply every rule in the *Public repository* section of `CLAUDE.md`. Then run this and clear every hit, keeping one only when it points at public material such as this repository or Paperless's docs:

   ```sh
   grep -nE '([0-9]{1,3}\.){3}[0-9]{1,3}|https?://|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+' <file>
   ```

   If the problem depends on what was on a real page (a form that crops wrong, a page hidden as blank), show the user the sanitized title and body, and file only once they approve. Done when nothing in the title or body ties it to the user's documents, people or network.

6. **File** each issue as its own Bash call. The label is `bug`, `enhancement` or `chore`, and the title names the problem or the wish in a few words, as the user would.

   ```sh
   gh issue create --repo jasonlearst/doxie-scanner --title "<title>" --label <label> --body-file <file>
   ```

7. **Report** each URL, a two-line summary, what you replaced in step 5, and the open questions. If something personal reached a filed issue anyway, tell the user at once: an edit leaves the old text in the issue's visible history, so whether to delete that revision or the whole issue is their call.
