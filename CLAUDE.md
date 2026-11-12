# Doxie Scanner

A Go server and phone web app that drive a Doxie Pro DX400 from a Raspberry Pi and send scans to Paperless-ngx. `README.md` has setup, the API and the package map. `PRODUCT.md` and `DESIGN.md` govern the web app: read them before changing it.

## Tasks

The facts `/file-issue`, `/worktree` and `/task` need about this repo. The process lives in those skills.

- **Forge:** GitHub `jasonlearst/doxie-scanner`. The `upstream` remote is the project this was forked from; it gets no issues or PRs from here.
- **Visibility:** public. *Public repository* below adds this repo's rules.
- **Labels:** `bug`, `enhancement`, `chore`.
- **Setup:** none. Go's module and build caches are shared, and `.worktreeinclude` copies `.env` and `.env.local`.
- **Checks:**
  1. **Fast**, every task: `./test.sh`, which runs vet, build, tests and the 95% coverage gate CI enforces.
  2. **Browser**, for web-app changes once the code is settled: start the worktree's app as *Running the app* says, then screenshot every changed screen with `scripts/screenshot.sh <url> <width> <light|dark> <out.png> [height]` at 390px (phone), 907px (the window the user reviews in) and 1280px, light and dark, and look at each one. Routes are hash URLs such as `http://127.0.0.1:<port>/#/scans/<id>`. Check taps, drags and dialogs in Chrome through the `claude-in-chrome` skill.
  3. **Scanner**, the owner tier.
- **Owner tier:** the real DX400, which the owner has to bring to this Mac, switch on and feed. A task needs it for a criterion tagged `[scanner]`, or for a change to how the app talks to the scanner or feeds paper: `internal/doxiedx400`, `internal/scsiusb`, the feed path in `internal/scanjobs`. Runbook: `.claude/scanner-checks.md`.
- **CI:** yes, `.github/workflows/ci.yml` on every PR.
- **Docs:** `README.md`, when the API, configuration or documented behavior changes.

Merge PRs with **Create a merge commit**, GitHub's form of the global `--no-ff` rule, then `git pull` in the primary checkout.

### Running the app

Start it from the worktree with the Bash tool's `run_in_background`, on port 18000 plus the issue number, with its data in the scratchpad:

```sh
DOXIE_DATA_DIR=<scratchpad>/data DOXIE_LISTEN_ADDR=127.0.0.1:<port> go run .
```

Wait for it with `curl -s --retry 30 --retry-connrefused --retry-delay 1 http://127.0.0.1:<port>/api/version`, and stop it when you're done.

Screens that show a scan need one in the data folder. Make it from the printed test sheet: `mkdir -p <data>/jobs/<id>/pages`, then `sips -s format jpeg ~/Downloads/doxie-test-page.pdf --out <data>/jobs/<id>/pages/001f.jpg`, then write `<data>/jobs/<id>/meta.json` in the shape of `storage.JobMeta`, with one front page `001f` sized 612 × 792 at 72 dpi. The app sees it without a restart.

Leave Paperless unset unless the change is in sending. Then start the app from a scratchpad script that loads `.env.local`, and ask before each send: it files a real document in the user's Paperless.

### Filing issues

- Read the version from `GET /api/version` on the instance the user was using, and ask for its address if you don't have it. For anything on screen, note the device, browser and window width.
- The README's package table maps the code, and its API section lists the read-only `GET`s that show what happened. `GET /api/scans/{id}` has a scan's pages, edits and uploads.
- When the instance runs a release older than `origin/main`, check `git log --oneline v<version>..origin/main -- <paths>`. If that already fixed it, tell the user which commit, and file nothing.
- Paperless-ngx does OCR, rotation and filing, and `PRODUCT.md` leaves those to it, so check a feature request isn't one of them.
- Proposed behavior covers each surface the change touches: the phone layout and the wider ones in `DESIGN.md`, the scanner and its button, sending to Paperless, the API, and storage and retention.
- Acceptance criteria include a regression test against the fake scanner or fake Paperless.

## Public repository

Anyone can read and search this repository's issues, PRs and commits, and the scanner handles real household mail, where medical and financial letters are routine. An edited issue keeps its old text in a visible edit history, and deleted text may already be cached or indexed, so sanitize every title, body, comment and commit message before it's posted. Replace with a placeholder:

- **Page contents:** text, numbers, names, dates or amounts read off a page. Describe the document by kind and shape ("a two-page letter", "a long thermal receipt"), and never more specifically than that for a medical, financial or legal one.
- **Scan and Paperless names:** scan names, Paperless titles, tags, correspondents, document types and document IDs become `<scan name>`, `<tag>`, `<correspondent>` and so on.
- **People:** names, email and postal addresses, phone and account numbers.
- **The user's network:** host names, IP addresses and URLs of their scanner and Paperless become `<scanner-host>` and `<paperless-url>`. Nothing from `.env` or `.env.local` is ever posted.
- **Logs and API responses:** quote only the lines that matter, after the replacements above.
- **Screenshots and page images:** describe them in words. The only page that may be pictured is the printed test sheet, `~/Downloads/doxie-test-page.pdf`.

After filing an issue, tell the user what you replaced.
