# Doxie Scanner

A Go server and phone web app that drive a Doxie Pro DX400 from a Raspberry Pi and send scans to Paperless-ngx. `README.md` has setup, the API and the package map. `PRODUCT.md` and `DESIGN.md` govern the web app.

`./test.sh` is the check: vet, build, tests and the 95% coverage gate CI enforces.

## Task workflow

One session works one GitHub issue. Capture what you notice with `/file-issue <what you saw>`, then work each issue with `/task <issue>` in a fresh session or after `/clear`. Issues and PRs live on `jasonlearst/doxie-scanner`. The `upstream` remote is the project this was forked from; it gets no issues or PRs from here.

Pushing a `/task` branch and opening its PR are pre-authorized here, replacing the global local-merge default for that work. Force-pushes, version bumps, tags and releases still need the user's go-ahead. Merge PRs with **Create a merge commit**, GitHub's form of the global `--no-ff` rule, then `git pull` in the primary checkout. Leave `CHANGELOG.md` alone: it's written at release.

## Public repository

Anyone can read and search this repository's issues, PRs and commits, and the scanner handles real household mail, where medical and financial letters are routine. An edited issue keeps its old text in a visible edit history, and deleted text may already be cached or indexed, so sanitize every title, body, comment and commit message before it's posted. Replace with a placeholder:

- **Page contents:** text, numbers, names, dates or amounts read off a page. Describe the document by kind and shape ("a two-page letter", "a long thermal receipt"), and never more specifically than that for a medical, financial or legal one.
- **Scan and Paperless names:** scan names, Paperless titles, tags, correspondents, document types and document IDs become `<scan name>`, `<tag>`, `<correspondent>` and so on.
- **People:** names, email and postal addresses, phone and account numbers.
- **The user's network:** host names, IP addresses and URLs of their scanner and Paperless become `<scanner-host>` and `<paperless-url>`. Nothing from `.env` or `.env.local` is ever posted.
- **Logs and API responses:** quote only the lines that matter, after the replacements above.
- **Screenshots and page images:** describe them in words. The only page that may be pictured is the printed test sheet, `~/Downloads/doxie-test-page.pdf`.

## Worktrees

Each `/task` session works in its own worktree at `.claude/worktrees/task-<issue>`, entered with `EnterWorktree` before any edit. That location lets Claude Code work there without a permission grant. Don't `git switch` in the primary checkout: other sessions share it, often on their own branch, so read `main` there as `origin/main`.

`/task` run in another task's worktree stops if that worktree has uncommitted or unpushed work. Otherwise it leaves with `ExitWorktree` (`keep`), runs `worktree.sh` from the primary checkout and enters the new worktree. It has to leave first because a worktree session refuses any Bash command it can't confirm stays inside that worktree, which includes `bash <(git show …)` and `git -C <primary>`.

That guard can't be turned off, and it judges a command's shape, not just its git use. In a worktree session:

- One plain command per Bash call. Run independent commands as separate calls, and copy a value from one result into the next command by hand instead of using `$(…)`.
- Edit files with Edit and Write, not `python3 - <<EOF`, `cat > file <<EOF` or `sed -i`. For a scripted change or analysis, write the script to the scratchpad with Write and run it by path.
- A command whose path has spaces, quoted or escaped, is refused. Run it from a script, as `scripts/screenshot.sh` does for Chrome.
- Pass long text with a file flag such as `--body-file`.
- Wait with a blocking command and a literal ID, such as `gh pr checks <n> --watch`, not a polling loop. Start a long-running process such as the app with the Bash tool's `run_in_background`, not `&`.
- After a refusal, simplify the command. Don't retry a variation of the same shape.
- Read an issue or PR with the command below, and never with `--comments`: outside a terminal that prints only the comments, and nothing at all if there are none, with exit 0. `gh pr view` takes the same flags.

```sh
gh issue view <n> --repo jasonlearst/doxie-scanner --json title,body,comments --jq '.title, .body, (.comments[] | .body)'
```

`scripts/worktree.sh` manages them: `new <issue> <branch>` creates or reuses one from `origin/main` and copies `.env` and `.env.local`, `list` shows each with its PR and cleanup verdict, and `clean [--yes] [--keep <path>]` removes only what's safe to lose: a task worktree goes only when its PR is merged and the branch head equals the PR head. It never touches worktrees outside `.claude/worktrees/`. `scripts/worktree.test.sh` covers every verdict.
