# Review Gates

Apply these gates to browser evidence when browser tooling is available. They are non-compensating: one failed critical gate means the design is not ready, regardless of strengths elsewhere. When browser tooling is unavailable, use the gates to identify implementation risks, clearly label them as unverified, and hand them to the user for manual visual inspection.

## Gate A: Five-Second Clarity

At each required viewport, identify:

- what the surface is;
- who or what it belongs to;
- the primary subject/job;
- the next action or continuation.

Fail when decoration, a logo, a giant headline, or empty space delays the answer on a task, command, inspection, or conversion surface.

## Gate B: Protagonist and Action

The protagonist must match the surface archetype. The primary action must read as part of the same visual event.

Task-led hard checks:

- short tasks show required inputs and primary action at `390x844`;
- the task remains the strongest working object at desktop and wide desktop;
- brand or atmosphere does not receive more visual weight than the task.

## Gate C: Spatial Completion

Name the role of every major region at `1440`, `1920`, and `2560` when applicable.

Fail when:

- a fixed panel is surrounded by expanding unused fields;
- cropping the outer 25% removes no meaningful context;
- a full-height divider becomes the strongest vertical object without representing real peer regions;
- large gutters have no functional, evidentiary, narrative, or perceptual consequence.

## Gate D: Cliche Budget and Authenticity

Count generic motifs: grid, particles, giant ring, neon glow, pseudo-terminal, glass, noise, bento, huge headline, decorative telemetry.

- `0-1`: acceptable when coherent with the subject.
- `2`: require distinct, visible jobs for both; revise if they only create atmosphere.
- `3+`: rebuild unless the user or brand explicitly calls for a maximalist genre and each motif still has a distinct function.

Fail when removing effects reveals a generic centered stack, split hero, or undersized subject.

## Gate E: Content Truth and Domain Fit

Fail when visible credibility depends on invented facts, metrics, clients, testimonials, certifications, contact/legal data, product screenshots, or telemetry.

Pass only when the page contains domain-specific objects, controls, evidence, actions, and states that would not survive an industry swap.

## Gate F: Typography, Color, and Material

Check:

- deliberate line breaks and readable measure;
- stable CJK/Latin/digit alignment;
- limited scale and weights;
- role-based color and sufficient contrast;
- one coherent depth/light model;
- controls and icons from one optical family.

Fail when giant type compensates for weak composition, dark values collapse together, glow lacks a source, or components look pasted onto the surface.

## Gate G: Product Reality and Accessibility

Test:

- long and short content;
- missing media where applicable;
- loading, error, disabled, empty, permission, locked, or offline states;
- keyboard order and focus;
- labels and accessible names;
- touch targets;
- reduced motion;
- browser console errors.

Fail for horizontal overflow, clipped text, overlapping controls, dead interactions, inaccessible focus, or state conveyed only by color.

## Gate H: Engineering Integrity

Check document uniqueness, asset loading, responsive rules, stable dimensions, cleanup of listeners/animation, and plausible performance. Prefer transform/opacity for motion. Avoid unnecessary dependencies and continuous expensive effects.

## Viewport Review Protocol

Use a real viewport API and `deviceScaleFactor: 1` when browser tooling is available.

1. Render `390x844`, `768x1024`, `1440x900`, and `1920x1080`.
2. Add `2560x1080` for split-screen, full-bleed, fixed-width, or cinematic compositions.
3. Wait for fonts, images, and application state.
4. Capture raw screenshots before cropping.
5. Inspect at 100% and thumbnail scale.
6. Interact with navigation, forms, menus, tabs, loading, and errors.
7. Fix the weakest critical gate, then recapture the same viewport.

If browser tooling is unavailable, do not fabricate an evidence log or passing score. Report the viewport matrix as a manual verification requirement and name the specific relationships most likely to fail.

## Evidence Log

For each failed or borderline gate, record:

`Observation -> Cause -> Structural treatment -> Recheck viewport`

Example:

`At 2560px the 480px login form occupies a small island in a blank white half -> fixed content and hard split do not scale -> integrate organization context around the task and remove the full-height divide -> recheck 1920 and 2560.`

## Quality Scale

Rate each dimension from 1 to 4, but do not sum scores.

- `1 Broken`: contradicts the task or visibly fails.
- `2 Usable`: works but is generic, weak, or unresolved.
- `3 Strong`: specific, coherent, and resilient.
- `4 Exceptional`: distinctive and deeply fitted without sacrificing usability.

Critical dimensions:

- protagonist clarity;
- spatial completion;
- domain/content authenticity;
- typography and component coherence;
- responsive product reality;
- accessibility.

Completion requires every critical dimension at least `3`. Record the weakest dimension and visible evidence. A high score elsewhere cannot compensate.
