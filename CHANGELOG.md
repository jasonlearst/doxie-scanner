# Changelog

All notable changes to this project are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project uses [Semantic Versioning](https://semver.org/) — tags look
like `v1.2.0`, and each one gets its own section below.

## [2.0.2] - 2026-11-03

### Fixed

- Scanning a stack still stopped after the first sheet in 2.0.1, and
  the next sheet was left half fed in the scanner. A USB capture of
  Doxie's own driver showed that it keeps the scan job open for the
  whole stack and repeats only a short setup before each sheet; this
  app ended and restarted the job around every sheet. It now follows
  Doxie's sequence, and a four-sheet stack scans straight through.

## [2.0.1] - 2026-11-03

### Fixed

- Scanning a stack on a Raspberry Pi 3 stopped partway into the second
  sheet and left it stuck in the scanner. The scanner starts pulling in
  the next sheet as soon as one ends and gives up if it isn't asked for
  within a few seconds, and the app was processing each sheet first,
  which takes several seconds on a Pi. Sheets are now processed while
  the next one feeds.
- If a sheet can't be processed, the scan stops after the sheet that is
  already moving, which is read to the end so it leaves the scanner.
- A sheet left partway through the scanner is reported in plain words,
  with what to do, instead of a raw scanner code.

## [2.0.0] - 2026-11-03

This release makes the app a scanning station for a Raspberry Pi that
hands finished documents to Paperless-ngx. It replaces the 1.x
interface and drops features that Paperless already provides.

### Added

- Send scans to Paperless-ngx, split into several documents if needed,
  with a title, date, correspondent, document type and tags picked from
  Paperless's own lists. The app follows each upload until Paperless has
  filed it, then links to the document. Set `DOXIE_PAPERLESS_URL` and
  `DOXIE_PAPERLESS_TOKEN` to turn it on.
- The scanner's own Scan button starts a scan with its own saved
  settings. Button scans wait for review; nothing is sent by itself.
- Scan settings: 200, 300 or 600 dpi; one side, both sides, or Auto
  (both sides, blank backs hidden); color, gray, black & white, or Auto,
  which picks per page.
- Brightness, contrast and color mode per page, alongside rotate, crop,
  hide, delete and reorder. Edits never change the scanned image, so
  they can always be undone.
- PDF downloads of a whole scan, one document or a single page.
- Scans are deleted a set time after Paperless files them.
- Docker images for Raspberry Pi (linux/arm64 and linux/arm/v7) next to
  linux/amd64, and a compose file for running on a Pi.

### Changed

- A new phone-first web interface, also laid out for tablets and
  laptops, replaces the Bootstrap one.
- The DX400 driver uses the scanner's on-device JPEG mode, several times
  faster than reading raw pixels. Scanner JPEGs are kept exactly as they
  arrive.
- Scanner status and errors are in plain language, including when the
  scanner has switched itself off. The app reconnects by itself when it
  is turned back on.

### Removed

- "Extract Text" and `DOXIE_OCR_LANG`. Paperless runs OCR on everything
  it receives.
- Combining pages from different scans into one PDF, and PNG/JPEG export
  of single pages.
- The Spanish translation. The new interface is English only for now.
- Scans saved by 1.x are not carried over to the new storage format.

## [1.0.8] - 2026-09-14

### Changed

- Docker build now uses Go 1.27 and Alpine 3.24 (previously 1.24/3.20),
  and `golang.org/x/image` is updated from 0.12.0 to 0.46.0 — bumping
  `go.mod`'s minimum Go version to 1.26. CI now actually builds the
  Dockerfile on every push/PR (it previously only ran `go vet`/`build`/
  `test`, so a base-image bump could pass CI without the image itself
  ever being built).

### Fixed

- `TestNewJobID_Uniqueness` could rarely fail on entirely correct code:
  it drew 50 real random samples from a 65536-value space and asserted
  no collision, a ~2% chance event by the birthday paradox. Now
  deterministic.

## [1.0.7] - 2026-07-21

### Added

- The web UI now supports English and Spanish. Language is auto-detected
  from the browser on first visit and can be overridden with a dropdown
  in the navbar; the choice is remembered for future visits. Adding
  another language is just a new `locales/<code>.json` file — no code
  changes needed.

## [1.0.6] - 2026-07-21

### Added

- "Extract Text" on any page's viewer runs OCR and shows the recognized
  text in a copy-pasteable box. Automatically corrects for a crooked
  scan first (via `unpaper`'s deskew pass) before handing it to
  `tesseract` — real scans are rarely perfectly straight, and a few
  degrees of skew measurably hurts OCR accuracy. Defaults to English
  (`DOXIE_OCR_LANG` to change it, plus the matching
  `tesseract-ocr-data-<lang>` package in a custom build). Text isn't
  cached — like Export, it's regenerated on demand each time.

## [1.0.5] - 2026-07-17

### Added

- The combine-into-PDF bar now shows a thumbnail of every selected page,
  draggable to set the final PDF's page order, with a per-thumbnail
  remove button — no more guessing what order pages will end up in or
  hunting down a page's original thumbnail just to deselect it.
  Thumbnails are labeled by their position in the combined document (1,
  2, 3...), not their original page number, since two pages from
  different scans can otherwise both claim to be "page 1". Click a
  thumbnail for a plain, action-free preview of that page.
- Combine-into-PDF and "Export scan as PDF" now let you choose PNG
  (lossless) or JPEG (smaller) for how pages are embedded, defaulting to
  JPEG. Real scans are noisy enough (CIS sensor noise) that PNG's
  lossless compression barely helps — a typical page runs 8-14MB as PNG
  but only 1-2MB as JPEG at quality 90 with no visible difference on text
  content; PNG remains available for scans of photos/art where JPEG
  artifacts would actually show.

- The per-page export menu now has two JPEG options — "JPEG (high
  quality)" (quality 100) and "JPEG (smaller)" (quality 90, the same
  tier PDF export uses) — so getting a compact single image no longer
  requires wrapping it in a PDF just to get the smaller file size.

### Changed

- Standalone JPEG image export (the per-page export menu, not PDF) is
  now quality 100 by default, up from 90 — since PDF export is now the
  deliberately smaller/convenience choice (see above), this is the
  deliberately higher-quality one. The per-page PDF export option itself
  now embeds JPEG (was PNG) with no separate toggle, since the plain PNG
  download right next to it in the same menu already covers the lossless
  case.

- Duplex scanning is now on by default. The previously documented
  front-side color cast didn't reproduce under isolated testing against
  a freshly power-cycled scanner (see README's "Duplex is on by
  default"); it was most likely a symptom of a degraded scanner state
  from earlier back-to-back testing, not an inherent hardware limit.
- Renamed the scan grid's "tile" terminology to "thumbnail" throughout
  (CSS classes, help text), matching what the combine bar already called
  its own page previews — the UI used both words for the same kind of
  element.

## [1.0.0] - 2026-07-15

### Added

- Initial release: a standalone web app for the Doxie Pro DX400 —
  scan (simplex/duplex/multi-page), review, rotate/crop, and export
  pages as PNG/JPEG/PDF, or combine pages from any past scans into one
  PDF, all from the browser
- Duplex sides are independent pages (not a front/back pair); deleting a
  page renumbers the rest to close the gap
- Drag-and-drop page reordering with live preview
- Docker image (linux/amd64), `docker-compose.yml`, and a udev rule for
  non-root USB access
