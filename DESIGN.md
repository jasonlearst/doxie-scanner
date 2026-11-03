---
name: Scanner
description: A household scanner app in Apple's system idiom; scan, fix pages, split, send to Paperless.
colors:
  tint: "#0066d6"
  tint-dark: "#4da3ff"
  tint-fill-dark: "#0a6fdb"
  on-tint: "#ffffff"
  grouped-bg: "#f2f2f7"
  grouped-bg-dark: "#000000"
  cell: "#ffffff"
  cell-dark: "#1c1c1e"
  cell-pressed: "#d1d1d6"
  cell-pressed-dark: "#3a3a3c"
  fill: "rgba(118, 118, 128, 0.12)"
  fill-dark: "rgba(118, 118, 128, 0.24)"
  label: "#000000"
  label-dark: "#ffffff"
  label-secondary: "#6c6c70"
  label-secondary-dark: "#aeaeb2"
  separator: "#c6c6c8"
  separator-dark: "#38383a"
  green-text: "#1f7a37"
  green-dot: "#34c759"
  green-dark: "#30d158"
  orange-text: "#b34700"
  orange-dot: "#ff9500"
  orange-dark: "#ff9f0a"
  red-text: "#c4001a"
  red-dot: "#ff3b30"
  red-dark: "#ff453a"
typography:
  large-title:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", system-ui, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, sans-serif"
    fontSize: "34px"
    fontWeight: 700
    lineHeight: "41px"
    letterSpacing: "-0.02em"
  title-2:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", system-ui, sans-serif"
    fontSize: "28px"
    fontWeight: 700
    lineHeight: "34px"
    letterSpacing: "-0.02em"
  status-title:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", system-ui, sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: "25px"
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", system-ui, sans-serif"
    fontSize: "17px"
    fontWeight: 600
    lineHeight: "22px"
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", system-ui, sans-serif"
    fontSize: "17px"
    fontWeight: 400
    lineHeight: "22px"
    letterSpacing: "-0.01em"
  subhead:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: "20px"
  footnote:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: "18px"
  section-header:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: "18px"
    letterSpacing: "0.02em"
  tab-label:
    fontFamily: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", system-ui, sans-serif"
    fontSize: "11px"
    fontWeight: 500
    letterSpacing: "0em"
rounded:
  page: "3px"
  thumb: "4px"
  control-inner: "8px"
  field: "10px"
  group: "12px"
  big: "14px"
  pill: "18px"
  capsule: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
  xxl: "32px"
  gutter-wide: "40px"
components:
  button-primary:
    backgroundColor: "{colors.tint}"
    textColor: "{colors.on-tint}"
    typography: "{typography.headline}"
    rounded: "{rounded.group}"
    padding: "0 16px"
    height: "44px"
  button-primary-big:
    backgroundColor: "{colors.tint}"
    textColor: "{colors.on-tint}"
    rounded: "{rounded.big}"
    height: "54px"
    width: "100%"
  button-tinted:
    backgroundColor: "{colors.fill}"
    textColor: "{colors.tint}"
    typography: "{typography.headline}"
    rounded: "{rounded.group}"
    padding: "0 16px"
    height: "44px"
  button-plain:
    textColor: "{colors.tint}"
    padding: "0 8px"
    height: "44px"
  button-plain-destructive:
    textColor: "{colors.red-text}"
    padding: "0 8px"
    height: "44px"
  button-disabled:
    backgroundColor: "{colors.fill}"
    textColor: "{colors.label-secondary}"
    rounded: "{rounded.group}"
    height: "44px"
  list-group:
    backgroundColor: "{colors.cell}"
    rounded: "{rounded.group}"
  list-cell:
    textColor: "{colors.label}"
    typography: "{typography.body}"
    padding: "11px 16px"
    height: "48px"
  list-cell-pressed:
    backgroundColor: "{colors.cell-pressed}"
  text-field:
    backgroundColor: "{colors.fill}"
    textColor: "{colors.label}"
    typography: "{typography.body}"
    rounded: "{rounded.field}"
    padding: "10px 12px"
    height: "44px"
  segmented-control:
    backgroundColor: "{colors.fill}"
    rounded: "{rounded.field}"
    padding: "2px"
  segmented-control-selected:
    backgroundColor: "{colors.cell}"
    textColor: "{colors.label}"
    rounded: "{rounded.control-inner}"
    height: "36px"
  chip:
    backgroundColor: "{colors.fill}"
    textColor: "{colors.label}"
    typography: "{typography.subhead}"
    rounded: "{rounded.pill}"
    padding: "0 14px"
    height: "36px"
  chip-selected:
    backgroundColor: "{colors.tint}"
    textColor: "{colors.on-tint}"
  split-divider:
    backgroundColor: "{colors.grouped-bg}"
    textColor: "{colors.label-secondary}"
    typography: "{typography.subhead}"
    height: "44px"
    width: "100%"
  split-divider-on:
    backgroundColor: "{colors.tint}"
    textColor: "{colors.on-tint}"
  tab-bar-item:
    textColor: "{colors.label-secondary}"
    typography: "{typography.tab-label}"
  tab-bar-item-active:
    textColor: "{colors.tint}"
  toast:
    backgroundColor: "{colors.label}"
    textColor: "{colors.grouped-bg}"
    rounded: "20px"
    padding: "10px 18px"
---

# Design System: Scanner

## Overview

**Creative North Star: "The Built-In App"**

This is the category standard, executed as if Apple had shipped it next to Notes, Files and Settings. The household walks up to the scanner with paper in one hand and a phone in the other; every surface is something they already know how to read: a large title, a status cell, one blue button, inset grouped lists on a grouped gray ground, sheets that rise from the bottom, a tab bar. Conventions are used without irony and nothing is smuggled in to make it look designed.

Density is relaxed on the phone and tightens only where the owner works through a backlog: on a laptop the scan list sits as a 340px master column beside the open scan. Light and dark follow the device; dark is true black ground with raised #1c1c1e cells. Color carries meaning, never decoration: one tint for "you can act here", and green, orange, red for what the scanner and Paperless are doing.

The only expressive moment is the scanner itself: pages arrive into the grid as each sheet comes out, and the placeholder for the next sheet carries a slow light sweep like the scan head passing down it.

**Key Characteristics:**
- Apple system idiom: system font, large titles, inset grouped lists, sheets, segmented controls, tab bar.
- One tint, deepened for contrast; semantic status colors only.
- Every target at least 44px tall.
- Flat cells on a tonal ground; shadows only on paper and on things that float.
- Light and dark follow the device.

## Colors

A grayscale grouped-list world with one accessible blue and three status colors, each status split into a text tone and a brighter dot tone.

### Primary
- **Accessible System Blue** (tint, light): the single interactive color. Tab bar selection, plain buttons, back chevrons, links, "Show"/"Hide" values, focus rings, spinner arc, and the fill of primary buttons, selected chips and active split dividers. Deliberately one step deeper than iOS systemBlue (#007AFF): white 17px semibold on #007AFF falls below 4.5:1, which the craft floor's contrast rule forbids.
- **Night Blue Label** (tint-dark): the tint as text and icon color in dark appearance, where #0A84FF would read too dim as small text on #1c1c1e.
- **Night Blue Fill** (tint-fill-dark): the fill behind white labels in dark appearance (primary buttons, selected chips, active dividers). Text tint and fill tint are separate tokens in dark mode because one blue cannot be both bright enough as text on black and dark enough under white text.

### Tertiary (status)
- **Ready Green** (green-text / green-dot): green-text for status words ("Filed in Paperless"); green-dot for the 12px status dot and the sent-check badge tint. Ready, filed, done.
- **Attention Orange** (orange-text / orange-dot): scanner Off, "To review", anything waiting on a person. The text tone is burnt (#b34700 range) so it passes as 15px text on white.
- **Error Red** (red-text / red-dot): stopped scans, failed sends, destructive actions ("Delete this scan", delete confirmations), the alert glyph in an empty failed thumbnail.
- In dark appearance each status collapses to one Apple dark-mode tone (green-dark, orange-dark, red-dark) used for both text and dot.

### Neutral
- **Grouped Gray** (grouped-bg / grouped-bg-dark): the page ground, sheet ground, viewer ground, and the ground of unselected split dividers.
- **Cell White / Cell Graphite** (cell / cell-dark): list groups, notices, the laptop sidebar, the viewer toolbar, nav-arrow and badge chips.
- **Pressed Gray** (cell-pressed): active state of tappable cells.
- **Quiet Fill** (fill): translucent gray behind tinted buttons, text fields, segmented tracks, chips, and placeholder thumbnails; also the selected row in the laptop master list and sidebar.
- **Label / Secondary Label** (label, label-secondary): primary text; subtitles, section headers, footers, inactive tab items, chevrons. Secondary is #6c6c70 rather than iOS's #8a8a8e because the stock gray fails 4.5:1 as text.
- **Hairline** (separator): 0.5px cell separators inset 16px from the leading edge (72px when the row has a thumbnail), tab bar and toolbar top rules, sidebar and master-list edge.

### Named Rules
**The One Tint Rule.** Blue means "tap here" and nothing else. No second accent, no blue decoration, no blue headings.

**The Status Means Something Rule.** Green, orange and red appear only when they report a real state of the scanner, a scan, or a send. A dot is always paired with words; color never carries the state alone.

**The Deepened Stock Rule.** Where an iOS stock color fails contrast in this context (systemBlue under white text, secondaryLabel as text), use the deepened token here, never the stock value.

## Typography

**Display Font:** the system stack (-apple-system / SF Pro, falling back to system-ui, Segoe UI, Roboto, Helvetica Neue, Arial)
**Body Font:** the same stack
**Label/Mono Font:** none; numerals use tabular figures (`font-variant-numeric: tabular-nums`) wherever counts and times line up.

**Character:** One family at Apple's own text-style sizes. The system face is this world's material, chosen by the owner, not a default left in place.

### Hierarchy
- **Large Title** (700, 34px, 41px): one per top-level page ("Scanner", "Scans", "Settings"); collapses into a 17px semibold centered nav-bar title on scroll.
- **Title 2** (700, 28px, 34px): the scan name at the top of a scan's detail, with a 15px secondary line under it (date · pages · dpi).
- **Status Title** (600, 20px, 25px): the scanner status cell only ("Ready", "Off", "Scanning…").
- **Headline** (600, 17px): button labels, sheet and nav-bar titles, document headers in the page grid, the viewer's "Page N of M".
- **Body** (400, 17px, 22px): cell titles, values, field input. Base letter-spacing -0.01em.
- **Subhead** (400, 15px, 20px): cell subtitles, notices, field labels, hints under the Scan button, empty-state copy, chips and segmented labels (500, 600 when selected).
- **Footnote** (400, 13px, 18px): group footers, field hints, page captions under thumbnails.
- **Section Header** (400, 13px, 18px, +0.02em, uppercase, secondary label): the iOS inset-grouped section header above a list ("To review", "Today", "Paperless"). It names the list beneath it; it is the native grouped-list device, not an eyebrow over a heading.
- **Tab Label** (500, 11px): tab bar and viewer toolbar captions under icons.
- The full-width big button uses 20px semibold, the status-title size.

### Named Rules
**The Apple Sizes Rule.** Text sizes come from the ladder above (34, 28, 20, 17, 15, 13, 11). No in-between sizes, no light weights, no display tracking beyond -0.02em.

## Layout

Single column, phone-first. Content gutters are 16px (respecting safe-area insets); pages cap at 680px, the scan detail at 1200px. Vertical rhythm runs on 4px steps: 8 inside stacks, 12 between related blocks, 16 under titles, 28–32 between groups. Large titles sit 52px from the top.

- **Under 900px:** fixed translucent tab bar at the bottom (56px plus safe area). Sticky translucent nav bar with back button and trailing actions. The scan's action bar (Split, PDF, Send) sticks above the tab bar on a fade-to-ground gradient; Send gets the widest share (flex 1.6).
- **900–1099px:** the tab bar becomes a 232px sidebar on the cell color with a hairline right edge; items are 40px rows with tinted icons and a fill highlight when current. Pages stay single-column. Sheets become centered dialogs (max 560px). Viewer gains side arrows.
- **1100px and up:** the Scans area becomes master–detail, a sticky 340px scrolling list beside the detail (40px gutters). The selected row gets the fill and a semibold title; with nothing selected, the detail shows a centered empty state.
- **Page grid:** auto-fill thumbnails at 100px minimum (16 × 12px gaps) on phone, 150px minimum (24 × 20px) from 900px. Thumbnails keep a letter-page 8.5:11 slot.

**The 44 Rule.** Every tappable thing is at least 44px tall: buttons, back, cells (48px), fields, split dividers, nav arrows. Segmented segments and chips are 36px inside a larger hit row.

## Elevation & Depth

Flat by default, depth by tone: grouped gray ground, lighter cells on top, darker pressed state. Shadows belong to two things only, paper and floating chrome. Translucent material (92% color-mix plus `saturate(180%) blur(20px)`) is used for the tab bar and the sticky nav bar, which also gains a hairline bottom edge once scrolled. Sheets and the viewer sit over a scrim.

### Shadow Vocabulary
- **Paper** (`0 1px 3px rgba(0,0,0,0.18)`; dark `0 1px 3px rgba(0,0,0,0.6)`): every page image and list thumbnail, so scanned sheets read as paper on the ground.
- **Raised** (`0 3px 8px rgba(0,0,0,0.12), 0 1px 1px rgba(0,0,0,0.04)`; dark 0.5 / 0.3): the selected segment, viewer nav arrows, the viewer's hidden-page badge.
- **Scrim** (`rgba(0,0,0,0.32)`; dark 0.55): behind sheets and dialogs.

### Named Rules
**The Paper Casts, Cells Don't Rule.** List groups, buttons, notices and cards never take a shadow. Only scanned paper and small floating controls do.

## Shapes

Continuous, moderate rounding scaled to the object: 3px on page images (paper is nearly square-cornered), 4px on list thumbnails, 8px on segment thumbs and sidebar rows, 10px on fields and segmented tracks, 12px on list groups, buttons and notices, 14px on the big button and sheet top corners, fully round on chips (18px), dots, nav arrows and the sent-status badge. Borders are hairlines (0.5px separator) and never outline a control; fields and buttons are defined by fill. Icons are 22px Feather-style line glyphs (1.8 stroke; 16px at 2.2 for small), plus an authored scan glyph, drawn from one inline SVG sprite.

## Components

### Buttons
Quiet and certain, like Settings: filled where it matters, plain everywhere else.
- **Shape:** gently rounded (12px); big variant 14px, full width, 54px tall, 20px label. Circles and capsules (viewer arrows, badges, the toast) use the 999px capsule radius.
- **Primary:** tint fill, white semibold label. One per screen: Scan, Send, Scan again, Done in split mode.
- **Tinted (default):** quiet fill with a tint label, for secondary actions beside the primary (Split, PDF, Cancel).
- **Plain:** no fill, tint label, 8px side padding; sheet Done/Cancel/Save, inline actions. **Destructive** swaps the label to red ("Delete this scan").
- **Icon-only:** 44px square, no fill.
- **Press:** scales to 0.98 with the house ease. **Disabled:** explicit fill background and a 60% secondary-label color, never lowered opacity.
- **Focus:** 3px ring of tint at 55%, offset 2px.

### Status Cell
The first thing on the Scan page: a 12px dot (or spinner while checking/scanning) aligned to the first line of a 20px semibold title, with a 15px secondary sentence below. States in use: Ready / "Paper is loaded." (green); Ready / "Put paper in the scanner, then tap Scan." (green); Off / "Press the power button on the scanner to turn it on." (orange); Scanner is in use, Scanner problem, Can't reach the scanner app (red); Scanning… / "Pages show up as each sheet comes out." (spinner). It is a live region.

### Lists / Containers
- **Corner Style:** 12px inset group, clipped.
- **Background:** cell color on the grouped ground; pressed rows take the pressed gray.
- **Shadow Strategy:** none (see Elevation).
- **Border:** hairline separators inset 16px, or 72px when rows carry a thumbnail.
- **Internal Padding:** 11px × 16px, 48px minimum row.
- **Scan row:** 44 × 56px thumbnail with the paper shadow; title; "time · N pages" in secondary; status on its own line beneath in the status tone (orange "To review", red "Stopped: paper jam" / "Couldn't send", green "Filed in Paperless", secondary for in-progress); trailing chevron. With no cover, the thumbnail slot shows a fill tile holding a spinner (running), a red alert glyph (failed), or the scan glyph.
- **Notice:** cell-colored 12px block with a leading icon in the tone color, a semibold title in that tone and body text in label color.
- **Section headers and footers** sit outside the group, inset 16px.

### Inputs / Fields
- **Style:** borderless, quiet fill, 10px radius, 44px tall, 17px text; selects carry a drawn chevron. Ranges use the tint fill as accent.
- **Focus:** the shared 3px tint ring; caret in tint.
- **Segmented control:** fill track, 2px inset, white raised thumb with semibold label on the selected option. Backed by a radio group.
- **Chips:** pill toggles for Paperless tags; selected chips take the tint fill and white semibold label.

### Navigation
- **Tab bar (under 900px):** three items, Scan · Scans · Settings, 25px icon over an 11px label; secondary label color, tint when current. Translucent material, hairline top edge.
- **Sidebar (900px and up):** the same three as 40px rows with 22px tinted icons and 15px labels; current row gets the fill and semibold label.
- **Nav bar:** sticky, translucent; back button with a 26px chevron and the word "Back" in tint; centered title fades in after the large title scrolls away; trailing more/actions.
- **Sheets:** rise from the bottom with a 36 × 5px grabber, a three-column head (Cancel · title · action), optional centered 15px lede. From 900px they become centered dialogs that pop in at 0.98 scale.

### Page Grid and Live Arrival (signature)
Documents are labeled groups ("Document 1", count and PDF link trailing) of paper thumbnails with 13px "Page N" captions. New pages animate in (fade, 10px rise, 0.96 scale, 0.6s house ease). While scanning, a placeholder sheet in the quiet fill, captioned with its upcoming page number, loops a soft light band down the sheet (1.8s); under reduced motion the sweep is removed. Hidden pages show at 45% opacity in their own collapsible set, opened by a cell whose "Show"/"Hide" value is in tint.

### Split Mode
The grid becomes full-width rows (48 × 62px page, "Page N", "Document N"). Between each pair sits a full-width 44px divider on the grouped ground with dashed hairlines either side of "Tap to split here"; an active split is a tint-filled bar with a scissors glyph and "Document N starts here" in white semibold.

### Page Viewer
Full-screen over the grouped ground: top row (plain semibold Done · "Page N of M", or "Hidden page" when paging the hidden set), the page centered with the paper shadow, side arrows from 900px, and a bottom toolbar on the cell color of icon-over-label tint actions (Rotate, Crop, Adjust, Hide/Show, More); a destructive toolbar action takes red. It pages only through the set it was opened in, and after Hide/Show moves to a neighbor.

### Toast
Inverted pill (label color background, ground color text), 15px semibold, rises 20px into place above the tab bar. Short past-tense confirmations: "Saved", "Page hidden", "Sent to Paperless".

## Do's and Don'ts

### Do:
- **Do** use the deepened tint (#0066d6 light, #4da3ff text / #0a6fdb fill dark) anywhere iOS would use systemBlue.
- **Do** keep one primary button per screen; everything else is tinted or plain.
- **Do** put status on its own line in its tone color, and pair every status dot with words.
- **Do** give scanned pages the paper shadow and a 3px radius so they read as paper.
- **Do** keep every target at least 44px tall, including dividers in split mode.
- **Do** honor `prefers-reduced-motion`: transitions collapse and the scan-light sweep is removed.
- **Do** describe the scanner in the household's words: "Off" when it is gone from USB, "Paper is loaded." when ready.

### Don't:
- **Don't** use stock #007AFF / #0A84FF or #8a8a8e as text or behind white text; they fail 4.5:1 here.
- **Don't** dim disabled controls with opacity; use the explicit fill and muted label.
- **Don't** put shadows or outlines on list groups, buttons or notices.
- **Don't** introduce a second accent or decorative color; status colors are for status only.
- **Don't** add halos or rings to status dots.
- **Don't** use the vendor's "Doxie" name or marks as the app's identity.
