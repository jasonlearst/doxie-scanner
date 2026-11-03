---
version: 1
slug: "internal-web-static-index-html"
primary_target: "internal/web/static/index.html"
related_targets: ["internal/web/static/app.js","internal/web/static/style.css"]
---

# Surface: scanner web app (phone-first, scales to laptop)

Mode: Operate. Audience: anyone in the household at the scanner, phone in hand; the owner also runs backlog sessions on a laptop or tablet. Task: scan, check and fix pages, split into documents, send to Paperless. Constraints: vanilla HTML/CSS/JS embedded in the Go binary, light enough for a Raspberry Pi 3B; every state the scanner can be in must read plainly.

## Direction contract

THESIS: The household scanner as an Apple-quality utility: one obvious Scan button, honest scanner status, pages you can fix and send. It refuses the vendor app's clutter and the settings-dashboard look.

OWN-WORLD: Apple system idiom. System font stack, large titles, inset grouped lists on grouped background (light #F2F2F7, dark #000), cells #FFF / #1C1C1E, one tint (systemBlue #007AFF / #0A84FF), semantic status only (green ready, orange asleep or attention, red error), thin line icons, sheets for settings, segmented controls, 44pt targets; light and dark follow the device.

STORY: Walk up, read "Ready, paper in the top tray", tap Scan or press the scanner's button, watch pages arrive, fix what's off, mark where documents split, send to Paperless, see each one filed.

FIRST VIEWPORT: Phone 390: large title "Scanner"; status cell (dot plus "Ready · Paper loaded", or "Asleep · Press the power button"); full-width blue Scan button; settings row beneath ("300 dpi · Both sides (auto) · Colour (auto)", opens a sheet); "Or press the button on the scanner"; "To review" inset list with thumbnails and status; tab bar Scan · Scans · Settings. Laptop: sidebar of scans beside the detail. Signature interaction: pages arrive live into the scan as each sheet comes out, and split markers sit between thumbnails as tappable dividers.

FORM: The category standard (canon), quality bar Apple's built-in apps (Notes, Files, Settings). seed:49bded12a671bc5a

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
