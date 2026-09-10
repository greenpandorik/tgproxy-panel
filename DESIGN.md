---
name: "TGProxy Panel"
description: "Tabler-inspired fluid operations interface on the incumbent React and Tailwind system."
colors:
  bg: "#151f2c"
  bg-2: "#1d2939"
  bg-3: "#263548"
  line: "rgba(255, 255, 255, 0.07)"
  line-2: "rgba(255, 255, 255, 0.12)"
  fg: "#edf2f7"
  mute: "#a6b3c4"
  dim: "#55575f"
  status-ok: "#22c55e"
  status-warn: "#f59e0b"
  status-err: "#ff8585"
  status-info: "#80b6ff"
  brand-primary: "#e23c92"
  brand-accent: "#12a198"
  destructive-solid: "#da1313"
  destructive-solid-hover: "#b81010"
  light-bg: "#f1f5f9"
  light-bg-2: "#ffffff"
  light-bg-3: "#e9eff5"
  light-line: "rgba(0, 0, 0, 0.08)"
  light-line-2: "rgba(0, 0, 0, 0.14)"
  light-fg: "#182433"
  light-mute: "#526478"
  light-dim: "#9a9ca3"
  light-status-ok: "#167e3c"
  light-status-warn: "#986206"
  light-status-err: "#da1313"
  light-status-info: "#0b63f2"
  primary-foreground-default: "#000000"
  destructive-foreground: "#ffffff"
typography:
  display:
    fontFamily: "'Inter Variable', 'Inter', ui-sans-serif, system-ui, sans-serif"
    fontSize: "26px"
    lineHeight: "30px"
    fontWeight: 600
    letterSpacing: "-0.02em"
  title:
    fontFamily: "'Inter Variable', 'Inter', ui-sans-serif, system-ui, sans-serif"
    fontSize: "17px"
    lineHeight: "24px"
    fontWeight: 600
    letterSpacing: "-0.01em"
  body:
    fontFamily: "'Inter Variable', 'Inter', ui-sans-serif, system-ui, sans-serif"
    fontSize: "14px"
    lineHeight: "21px"
  label:
    fontFamily: "'Inter Variable', 'Inter', ui-sans-serif, system-ui, sans-serif"
    fontSize: "13px"
    lineHeight: "19px"
  micro:
    fontFamily: "'Inter Variable', 'Inter', ui-sans-serif, system-ui, sans-serif"
    fontSize: "12px"
    lineHeight: "18px"
  mono:
    fontFamily: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, 'SFMono-Regular', Menlo, monospace"
    fontSize: "12.5px"
    lineHeight: "18px"
rounded:
  control: "6px"
  surface: "8px"
  pill: "9999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  panel: "20px"
  lg: "24px"
components:
  button-primary:
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 16px"
    height: "var(--control-height)"
    backgroundColor: "{colors.brand-primary}"
    textColor: "{colors.primary-foreground-default}"
  button-outline:
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 16px"
    height: "var(--control-height)"
    backgroundColor: "{colors.bg-2}"
    textColor: "{colors.fg}"
  button-secondary:
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 16px"
    height: "var(--control-height)"
    backgroundColor: "{colors.bg-3}"
    textColor: "{colors.fg}"
  button-ghost:
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 16px"
    height: "var(--control-height)"
  button-destructive:
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 16px"
    height: "var(--control-height)"
  button-destructive-solid:
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 16px"
    height: "var(--control-height)"
    backgroundColor: "{colors.destructive-solid}"
    textColor: "{colors.destructive-foreground}"
  button-link:
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 16px"
    height: "var(--control-height)"
  input:
    backgroundColor: "{colors.bg}"
    textColor: "{colors.fg}"
    rounded: "{rounded.control}"
    padding: "4px 10px"
    typography: "{typography.body}"
  panel:
    backgroundColor: "{colors.bg-2}"
    rounded: "{rounded.surface}"
  nav-row:
    rounded: "{rounded.control}"
    height: "44px"
    padding: "0 12px"
    typography: "{typography.body}"
---

# Design System: TGProxy Panel

## Overview

The approved direction is a restrained, Tabler-inspired operations interface with a fluid workspace. It uses the existing React, Tailwind and Base UI/shadcn components; Tabler is the visual reference, with no Bootstrap migration.

The interface prioritizes legible operational state, clear navigation and configurable project identity. Personal display preferences remain separate from shared branding.

Key characteristics:

- Full-width administration layout with a collapsible navigation rail.
- Quiet surfaces, visible boundaries and compact, readable controls.
- Shared semantic tokens across light and dark themes.

## Colors

### Primary

`brand-primary` and `brand-accent` record the stylesheet defaults. **The Saved Identity Rule:** operator-saved primary and accent colors remain authoritative and override these defaults at runtime. Do not reset saved colors to match a reference screenshot. Primary button text is computed by `ThemeProvider`; the frontmatter foreground is only the stylesheet fallback. Brand text uses the theme-adjusted `--brand-ink` mix, preserving legibility independently from button fill.

### Neutral

Unprefixed surface, text and border tokens describe the dark root theme; `light-` tokens describe light overrides. Page ground, panel surface and elevated surface form the structural hierarchy. The stronger hairline outlines controls and panels; the lighter hairline divides content inside them.

Status colors distinguish healthy, warning, error and informational states. Labels accompany status dots so meaning does not depend on color. Filled destructive actions use their separate solid token.

## Typography

Interface text uses the frontmatter Inter stack. JetBrains Mono identifies machine values and compact operational data. Display and title roles carry semibold hierarchy; body, label and micro roles support dense administrative content. Tables and numeric inputs use tabular figures. The uppercase micro utility adds medium weight and tracking; the micro size alone does not force uppercase.

## Layout

The shell fills the viewport with a scrolling main area and no global maximum content width. Desktop navigation is expanded (256px) or collapsed (60px); below the large breakpoint it becomes a drawer. The topbar is 64px high. Main gutters are 16px, increasing to 24px at the small breakpoint, with 24px between page sections. Panels use 20px body padding and wrapping headers.

Grouped form fields adapt to available width. Personal preferences use one, two and four columns at base, medium and extra-large widths. The branding editor adds a preview column on extra-wide screens. Comfortable controls use 38px height and 14px table-cell vertical padding; compact mode uses 32px and 8px. Coarse-pointer controls and navigation rows retain a 44px minimum target height.

## Elevation & Depth

Panels are flat, separated by surface tones and one-pixel boundaries. Popovers use the theme-specific `--shadow-popover`; do not add decorative panel shadows. Focus rings and hover treatments communicate interaction. Standard transitions use the shared fast/base durations and easing. Reduced-motion preferences suppress entrance animation and the live-status pulse.

## Shapes

Controls use the control radius, panels use the surface radius, and status dots use the pill radius. Keep these shared geometries across themes and density settings. Borders remain one pixel; panel contents clip to their rounded boundary.

## Components

### Buttons

Primary actions use runtime brand fill and its computed foreground. Outline actions use the panel surface and stronger border; secondary actions use the elevated surface; ghost actions reveal a neutral hover surface. Destructive outline actions use red text, while filled red is reserved for confirmation inside destructive dialogs. Text links use brand ink and underline on hover. Buttons provide focus rings, subtle pressed scaling and disabled opacity.

### Cards / Containers

Use `Panel` with its bordered surface, wrapping header and padded body. Headers pair a title with optional scope, count or actions, retaining room for translated labels and narrow viewports.

### Inputs / Fields

Inputs use recessed page ground inside panels, a stronger border and visible labels. Focus uses the brand ring, invalid state uses destructive color and disabled state reduces opacity. Density changes minimum height without changing meaning or label hierarchy.

### Navigation

Navigation rows pair an icon and label; the active route uses elevated surface, brand ink and medium weight. Collapsed rows retain accessible names and tooltips. The mobile drawer keeps labels visible. Shared project logos use the optional dark logo in dark theme, falling back to the main logo and then the built-in mark.

### Status badges

Operational status is a small dot plus readable text. Online, active and degraded statuses carry the existing soft pulse; terminal states remain still. Dense dot-only rendering retains an accessible text label.

### Preferences and branding

Light/dark/system theme, density and sidebar controls are personal, locally persisted choices with pressed states and immediate feedback. Shared branding uses the existing profile API. Text, colors and CSS preview until saved or discarded; image uploads save immediately, with that behavior stated beside uploads. Preserve this distinction when extending settings.

### WEB operations and websites

Runtime, carrier and policy surfaces reuse the existing panels. Unsupported,
undetermined, offline and missing-metric states stay distinct; missing is not zero.
Diagnostics group checks, preserve timestamps/history/export, and separate passed,
failed, warning and not-run counts. Not-run checks use a quiet dashed treatment
and are excluded from the executed-check total.

Update progress uses a chronological step list, readable outcomes, expandable
details and drain session/stream counts. Success, refusal, rollback and unresolved
recovery require distinct copy. The node wizard marks identity, proxy settings and
installation stages; installation shows connection/readiness instead of assuming
the copied command succeeded.

The website gallery pairs previews and category filters with explicit preview,
customize, import and assign actions. The 15 deployed websites have independent
visual identities; panel tokens do not prescribe their layouts. Static previews
and capability-gated HTTP-upstream configuration remain separate surfaces.

## Do's and Don'ts

- Do reuse semantic tokens and test both themes, densities and narrow layouts.
- Do preserve saved operator branding, including main and optional dark logos.
- Do retain visible labels, keyboard focus, reduced motion and Russian/English layout flexibility.
- Do keep panel boundaries stronger than internal dividing lines.
- Don't constrain the entire workspace to a centered maximum-width column.
- Don't import Bootstrap to imitate the Tabler reference.
- Don't apply personal theme or density choices to shared project settings.
