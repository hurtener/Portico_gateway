> **architectural software**
> — calm, structured, precise, premium, trustworthy, and slightly intellectual.

Because **Portico** as a name suggests:

* threshold
* structure
* control
* entryway
* stability
* designed access

So the visual language should feel like a mix of:

* **modern infrastructure product**
* **editorial / documentation clarity**
* **architectural minimalism**
* **soft enterprise polish**

---

# 1. Brand soul

If I had to describe the Portico design language in words:

## Core attributes

* **Calm**
* **Structured**
* **Architectural**
* **Trusted**
* **Systemic**
* **Quietly premium**
* **Technical without feeling cold**
* **Minimal, but not sterile**

## What it should *not* feel like

* overly playful SaaS
* glassmorphism startup cliché
* cyberpunk terminal aesthetic
* generic Tailwind dashboard
* heavy “dark mode only hacker tool”
* ultra-corporate blue-gray monotony

---

# 2. Visual direction

My preferred direction would be:

## **Soft architectural minimalism**

A design system with:

* neutral mineral base colors
* one elegant accent
* strong spacing rhythm
* crisp typography
* restrained shadows
* panels that feel like surfaces, not “cards everywhere”
* subtle grid/order cues
* premium documentation feel

Think:

* **stone / slate / parchment / graphite**
* plus a controlled **accent teal / blue-green**
* maybe a subtle **bronze / brass support accent** for warmth

That would make Portico feel different from typical infra tools.

---

# 3. High-level aesthetic system

I’d split the aesthetic into 3 layers:

## A. Foundation

This is the universal Portico language:

* typography
* spacing
* radii
* colors
* icon style
* motion
* surface hierarchy

## B. Surface expression

Same system, different emphasis depending on context:

### Console

* denser
* more structured
* stronger hierarchy
* operational clarity
* more status colors

### Docs

* more editorial
* more whitespace
* more readable long-form content
* softer visual noise

### GitHub / brand site

* more aspirational
* more expressive layouts
* stronger brand moments
* more visual metaphor

So it’s one system, not three brands.

---

# 4. Color strategy

I would build Portico around a **neutral architectural palette** with one primary accent.

## Recommended palette direction

### Neutrals

* chalk / stone / fog
* slate / graphite / ink

### Primary accent

* muted teal or blue-green

Why teal?

* trustworthy like blue
* more distinctive than standard SaaS blue
* feels infrastructural, calm, technical
* pairs beautifully with stone neutrals

### Secondary accent

* subtle bronze / brass / amber-gray
* used sparingly
* gives warmth and architectural identity

---

# 5. Suggested token palette

## Core neutrals

### Light mode

```txt
bg.canvas        #F6F4EF
bg.surface       #FBFAF7
bg.elevated      #FFFFFF
bg.subtle        #F1EEE8

border.soft      #E7E1D8
border.default   #D9D1C5
border.strong    #BDAF9E

text.primary     #1F252B
text.secondary   #58616B
text.tertiary    #7A848E
text.muted       #98A1A8

icon.default     #5E6872
icon.subtle      #8C959D
```

### Dark mode

```txt
bg.canvas        #111417
bg.surface       #171C20
bg.elevated      #1D2328
bg.subtle        #222A31

border.soft      #2A333B
border.default   #36414A
border.strong    #4A5864

text.primary     #F3F5F6
text.secondary   #C7CED3
text.tertiary    #97A3AD
text.muted       #73808B

icon.default     #A7B1B8
icon.subtle      #7E8A94
```

---

## Brand accent

### Primary accent

```txt
accent.primary         #2D6F73
accent.primary.hover   #245D61
accent.primary.active  #1D4D50
accent.primary.soft    #D8EAE8
accent.primary.subtle  #EAF4F3
accent.on-primary      #FFFFFF
```

This is the “Portico green-blue”.

### Secondary warm accent

```txt
accent.warm            #9A7653
accent.warm.soft       #EEE3D8
accent.warm.subtle     #F7F1EB
accent.on-warm         #FFFFFF
```

Use this very sparingly — for highlights, diagrams, select badges, or editorial moments.

---

## Semantic status colors

Keep these slightly muted; don’t go too saturated.

```txt
success.default   #2F7A55
success.soft      #DDEFE4
warning.default   #A36A1B
warning.soft      #F5E7CD
danger.default    #B24A3B
danger.soft       #F6DFDB
info.default      #356E9A
info.soft         #DCEAF4
```

And in dark mode:

```txt
success.soft      #183726
warning.soft      #3A2A15
danger.soft       #44211D
info.soft         #1B3446
```

---

# 6. Typography

Typography should do a lot of the brand work.

I would use:

## Primary sans

A clean, modern, highly readable sans.

Good candidates:

* **Inter**
* **Geist**
* **Manrope**
* **Instrument Sans**
* **Söhne** (if premium/commercial is okay)
* **IBM Plex Sans** if you want slightly more technical character

### My pick

**Inter** or **Geist** for practicality.

---

## Optional editorial serif

For marketing/docs/hero moments only:

* **Newsreader**
* **Source Serif 4**
* **IBM Plex Serif**
* **Fraunces** (if you want more personality)

This could help Portico feel more architectural/editorial.

### Usage

* landing page hero
* section titles
* quotes / principles
* not for console UI

---

## Type scale

A good starting system:

```txt
display.xl   56/64  weight 600
display.lg   44/52  weight 600
heading.1    36/44  weight 600
heading.2    30/38  weight 600
heading.3    24/32  weight 600
heading.4    20/28  weight 600
title        18/26  weight 600
body.lg      17/28  weight 400
body.md      15/24  weight 400
body.sm      14/22  weight 400
label        13/18  weight 500
mono.sm      13/20  weight 500
```

---

## Monospace

For code, tool names, schema hashes, IDs, JSON, CLI:

* **JetBrains Mono**
* **IBM Plex Mono**
* **Geist Mono**

I’d use **JetBrains Mono** or **Geist Mono**.

---

# 7. Spacing system

This is important. The UI should feel ordered and breathable.

Use an 8px base, but allow 4px for tighter internals.

```txt
space.1   4
space.2   8
space.3   12
space.4   16
space.5   20
space.6   24
space.8   32
space.10  40
space.12  48
space.16  64
space.20  80
space.24  96
```

## Layout rhythm

* **tight controls**: 8–12
* **component padding**: 16–24
* **section spacing**: 32–48
* **page spacing**: 64–96

Portico should never feel cramped.

---

# 8. Radius system

I would avoid over-rounding.
This is not a bubbly consumer app.

Use:

```txt
radius.xs   6
radius.sm   8
radius.md   12
radius.lg   16
radius.xl   20
radius.pill 999
```

## Usage

* buttons / inputs: 10–12
* panels: 14–16
* large feature blocks: 20
* badges: pill or 999

The overall feel should be **softly architectural**, not cartoon-rounded.

---

# 9. Shadows and depth

Keep shadows restrained.

The brand should rely more on:

* contrast between surfaces
* borders
* spacing
* subtle elevation

than on big floating cards.

## Light mode shadows

```txt
shadow.sm  0 1px 2px rgba(16,24,40,.04)
shadow.md  0 4px 12px rgba(16,24,40,.06)
shadow.lg  0 12px 32px rgba(16,24,40,.08)
```

## Dark mode shadows

Dark mode should use even subtler shadows plus borders.

Also consider:

```txt
ring.focus  0 0 0 3px rgba(45,111,115,.18)
```

---

# 10. Border philosophy

Borders matter a lot here.

I would define Portico surfaces using:

* subtle borders
* occasional inset separators
* clean divisions
* light internal grid lines

This reinforces the “architectural” feel.

Examples:

* sidebars separated by a clean 1px border
* section headers with low-contrast dividers
* tables with gentle row separators
* inspector panels with slightly darker surface + border

---

# 11. Surface hierarchy

I’d define 4 primary surface levels:

```txt
surface.canvas
surface.base
surface.raised
surface.overlay
```

## Example usage

* `canvas`: page background
* `base`: primary content panels
* `raised`: cards, dialogs, floating controls
* `overlay`: command palette, dropdowns, modal overlays

The console should not look like “100 cards on a gray page”.
Instead it should feel like **intentional zones**.

---

# 12. Component personality

## Buttons

Buttons should be:

* medium weight
* compact but not tiny
* clean
* slightly rounded
* not too glossy

### Variants

* primary
* secondary
* ghost
* subtle
* destructive

### Example tokens

```txt
button.height.sm   32
button.height.md   40
button.height.lg   48

button.primary.bg        accent.primary
button.primary.text      #FFFFFF
button.secondary.bg      bg.elevated
button.secondary.border  border.default
button.secondary.text    text.primary
button.ghost.text        text.secondary
```

---

## Inputs

Should feel stable and quiet.

```txt
input.bg            bg.elevated
input.border        border.default
input.border.hover  border.strong
input.border.focus  accent.primary
input.text          text.primary
input.placeholder   text.tertiary
input.radius        12
input.height        40
```

---

## Tables

This is a big one for the console.

Tables should feel:

* clean
* readable
* softly structured
* not dense enterprise ugliness

Use:

* sticky header
* subtle zebra optional
* nice monospace for IDs
* muted metadata
* hover row emphasis

---

## Badges

Important for:

* server health
* transport type
* risk classes
* skill type
* plan/entitlement

Badge style:

* soft background
* muted text
* compact
* sometimes icon-led

---

## Navigation

I’d use:

* left sidebar for console
* clear section grouping
* thin separators
* active row with subtle filled state, not loud bright blue

---

# 13. Motion

Motion should be:

* short
* subtle
* utilitarian
* elegant

Use:

* 120–180ms for most interactions
* 220ms for panels/drawers
* eased transitions, no bouncy motion

Good motion cues:

* hover opacity
* slight elevation shift
* accordion expand/collapse
* panel slide
* skeleton shimmer very subtle

Avoid:

* flashy animations
* huge springy transitions
* “product marketing motion” inside console

---

# 14. Iconography

Use a consistent outline icon set:

* **Lucide**
* **Heroicons**
* **Phosphor** (if you want slightly softer)

For Portico I’d probably choose **Lucide**.

Why:

* crisp
* technical
* clean
* works well across docs and app

---

# 15. Illustration / visual motifs

I would create a subtle brand motif inspired by:

* portals
* thresholds
* structured layers
* frames
* arches
* corridors
* routing lines
* grids
* system nodes

But abstractly.

## Graphic language

* linear architectural diagrams
* nested frames
* routing paths
* sectional layouts
* soft blueprint-like compositions

This can be used on:

* GitHub README
* website hero
* docs cover art
* empty states
* onboarding screens

This would make Portico feel unique without requiring mascots or heavy illustration.

---

# 16. Specific expression by surface

## A. UI Console

This should feel:

* operational
* calm
* inspectable
* trustworthy

### Visual characteristics

* denser layout
* more panelization
* more status cues
* more tables / inspectors / filters
* monochrome foundation with teal highlights

### Good UI patterns

* left nav
* main workspace
* right inspector optional
* command bar on top
* breadcrumb + page title
* segmented controls
* tabs with underline or subtle pill
* split panes for schema/details/logs

---

## B. Documentation

This should feel:

* editorial
* precise
* very readable
* premium technical writing

### Characteristics

* wider line-height
* more whitespace
* calmer rhythm
* nice code blocks
* tasteful callouts
* diagrams that feel architectural, not cartoonish

### Docs tokens emphasis

* slightly warmer backgrounds
* slightly larger body size
* softer borders
* more serif accents possible

---

## C. GitHub page / landing site

This should feel:

* open-source credible
* beautifully clear
* not too marketing-heavy
* deeply technical but polished

### Hero direction

A hero that communicates:

* “one controlled gateway for many MCP servers”
* “skills + tools + UI + governance”
* “structured access layer”

Use:

* a compositional diagram
* layered portal metaphor
* a precise product screenshot
* dark and light theme previews

---

# 17. Suggested token naming system

I’d use semantic tokens, not hard-coded ones everywhere.

## Example

```txt
color.bg.canvas
color.bg.surface
color.bg.raised
color.bg.overlay

color.text.primary
color.text.secondary
color.text.tertiary
color.text.inverse

color.border.soft
color.border.default
color.border.strong

color.brand.primary
color.brand.primary-hover
color.brand.primary-soft

color.status.success
color.status.warning
color.status.danger
color.status.info

radius.sm
radius.md
radius.lg

space.1
space.2
space.3
...

font.family.sans
font.family.mono
font.family.serif

font.size.body-md
font.size.heading-2

shadow.sm
shadow.md
shadow.lg
```

That will scale much better.

---

# 18. Example “starter token set”

If you want a concrete starting point, I’d begin with this:

## Brand core

```txt
Brand name vibe:
architectural / calm / premium infra / structured gateway
```

## Colors

```txt
Primary accent:   #2D6F73
Warm accent:      #9A7653

Light bg:         #F6F4EF
Light surface:    #FBFAF7
Light text:       #1F252B

Dark bg:          #111417
Dark surface:     #171C20
Dark text:        #F3F5F6

Border light:     #D9D1C5
Border dark:      #36414A
```

## Typography

```txt
Sans: Inter
Mono: JetBrains Mono
Serif (optional editorial): Newsreader
```

## Radius

```txt
8 / 12 / 16 / 20
```

## Spacing

```txt
4 / 8 / 12 / 16 / 24 / 32 / 48 / 64
```

---

# 19. Design principles I would formalize

I’d write these as first-class design principles for the design system:

## 1. Structured, never cluttered

Every screen should feel intentionally organized.

## 2. Calm over flashy

Use restraint. The product should feel dependable.

## 3. Surfaces, not cards everywhere

Use clear zones and hierarchy.

## 4. Technical, but humane

Readable, warm, and not intimidating.

## 5. One system, many surfaces

Console, docs, and website are siblings.

## 6. Brand through rhythm, not decoration

Spacing, typography, and color do most of the work.

## 7. State clarity is essential

Health, risk, policy, and runtime state must be instantly legible.

---

# 20. If I were to summarize the whole design direction in one paragraph

I’d say:

> **Portico should look like a calm architectural control plane for AI tooling — premium but restrained, technical but approachable, structured without feeling rigid.** Its design language should combine mineral neutrals, a muted teal brand accent, crisp typography, soft but deliberate surfaces, and a documentation-first sense of clarity. The console should feel operational and trustworthy, the docs editorial and deeply readable, and the GitHub/site presence elegant and open-source credible.

---

# 21. My recommendation

If you want the shortest practical starting point, I would lock this in:

## Recommended Portico visual direction

* **Style:** soft architectural minimalism
* **Primary accent:** muted teal
* **Support accent:** subtle bronze
* **Base palette:** stone + slate neutrals
* **Typography:** Inter + JetBrains Mono + optional Newsreader
* **Radius:** medium softness (8/12/16)
* **Mood:** calm, precise, trusted, premium infra

---

If you want, next I can do **one of these three**:

1. **Write the full Portico design system spec**
   (tokens + principles + component guidance)

2. **Create a first draft of the token file**
   in JSON / CSS variables / Tailwind theme format

3. **Generate a visual moodboard / UI concept image**
   for the Portico console + docs + landing page direction
