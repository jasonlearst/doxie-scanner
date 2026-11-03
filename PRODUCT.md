# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

A household, not a single expert. Anyone at home walks up to the scanner by the printer with paper in hand and a phone in the other hand; they may never have seen the setup explained. One person (the owner) also does occasional large catch-up sessions on a backlog of old paper.

## Product Purpose

Turn paper into well-filed documents in Paperless-ngx without a computer. A Doxie Pro DX400 scanner sits next to the printer, attached to a Raspberry Pi; the phone app (served by the Pi on the home network) starts scans, shows what came out, lets people fix pages and split a stack into separate documents, then sends them to Paperless. Success: paper goes in, correct documents come out in Paperless, and nothing is silently lost.

## Positioning

The vendor's software needs a computer plugged into the scanner. This runs unattended on a Pi and drives the scanner directly, including its own scan button, so scanning starts at the scanner and finishes on a phone.

## Operating Context

- Two session shapes, roughly equal: quick daily mail (a few sheets: scan, glance, send) and backlog batches (stacks of old paper: review, split, tag).
- Scans start from the app or the scanner's own button. Button scans wait in an inbox until someone reviews and sends them; nothing auto-sends.
- The scanner sleeps after sitting idle and must be woken with its power button; the app has to say so plainly.
- Three feeders: top tray (stacks), rear slot (single sheets, cards), front tray (must be fed promptly or it jams).
- Paperless does OCR, auto-rotation and automatic filing (matching rules, workflows); this app should not duplicate those.

## Capabilities and Constraints

- Scan settings: resolution 200/300/600 dpi; sides auto (two-sided, blank backs hidden) / two-sided / one-sided; color auto / color / gray / black & white. The scanner's button uses saved settings.
- Originals are never modified; rotate, crop, brightness/contrast and color mode are reversible edits. Blank-looking backs are hidden, never deleted, and can be restored.
- Send to Paperless: one or more documents cut from a scan, each with optional title, date, correspondent, document type and tags chosen from Paperless's own lists; upload status is tracked with a link to the filed document.
- Vanilla HTML/CSS/JS embedded in a Go binary; no build step. Runs on a Raspberry Pi 3B (1 GB RAM), so the client should stay light and images are served pre-sized.
- Plain-language scanner conditions: asleep/off, no paper, paper jam, cover open, double feed.

## Brand Commitments

- The owner chose the category standard on purpose: conventional scanner-app structure, executed at the polish of Apple's built-in apps (Notes, Files, Settings). Conventions are embraced without irony; no smuggled quirk.
- Light and dark appearance follow the device.

## Evidence on Hand

No branding, logo, or marketing assets. The scanner is a Doxie Pro DX400; "Doxie" is the vendor's trademark and must not be used as this app's identity. No user testimonials or metrics exist.

## Product Principles

1. Never lose paper: every scanned side is kept until a person deletes it.
2. The scanner and Paperless do their jobs; the app connects them and stays out of the way.
3. Anyone in the house can finish a scan without help.
4. Fast for one sheet, capable for a hundred.
