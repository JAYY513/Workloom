---
name: design-visual-frontend
description: Design, build, redesign, or critically review distinctive frontend interfaces with strong composition, domain authenticity, responsive behavior, component coherence, and browser-verified visual quality. Use for websites, landing pages, login and form flows, dashboards, admin tools, product pages, portfolios, visual UI reviews, anti-template redesigns, or any request where frontend aesthetics and usability both matter.
---

# Design Visual Frontend

Create frontend UI whose composition, content, and interaction fit the actual subject and user task. Do not treat visual quality as a list of fashionable effects. Diagnose the page structure first, implement from a compact design decision sheet, and verify rendered evidence across the required viewports.

## Core Workflow

1. Inspect the brief, existing project, assets, stack, and user-provided references.
2. Classify the dominant surface using `references/surface-archetypes.md`.
3. Write the compact decision sheet below before UI code.
4. Read only the references routed by the task.
5. Build from tokens and reusable component rules.
6. When browser tooling is available, render the real page at the viewport matrix below and inspect the result visually.
7. Apply every hard gate in `references/review-gates.md`; rebuild when a gate fails.
8. Verify interaction, focus, errors, loading, reduced motion, and realistic content. When browser tooling is unavailable, inspect the implementation and give the user a concise manual visual-check list instead of claiming rendered verification.

For reviews or diagnosis requests, stop after evidence-backed findings unless the user also asks for changes.

## Compact Decision Sheet

Keep this short. It is a decision artifact, not a design essay.

1. `Surface`: archetype, primary user, and single primary job.
2. `P1 / P2 / P3 / Action`: first read, supporting context, proof/detail, and next action.
3. `Content truth`: real assets/data, user-provided content, public facts, or omitted claims.
4. `Composition`: one mobile and one wide ASCII wireframe with named region roles.
5. `System`: palette roles, type roles, spacing rhythm, radius, border/elevation, icon family.
6. `Signature`: one subject-specific memorable device and why it belongs.
7. `Cliche budget`: generic visual motifs used; one may be used without special justification, while additional motifs need distinct subject-specific jobs.
8. `Failure risks`: the two most likely structural failures and how browser evidence will detect them.

If the task is open-ended and a materially different composition could better serve the user, compare two directions. They must differ in protagonist, region roles, evidence, and interaction attitude. Do not generate directions that differ only in palette or decoration. Ask the user to choose only when the alternatives change the product relationship or brand position; otherwise select using brief evidence and proceed.

## Reference Routing

Always read:

- `references/core-design.md`
- `references/surface-archetypes.md`
- `references/review-gates.md`

Read when applicable:

- Forms, controls, dashboards, navigation, UI copy, or state: `references/components-content.md`
- Recognizable industry or institution: `references/domain-guidance.md`
- Frontend implementation or browser verification: `references/frontend-implementation.md`

Do not load every reference by default. Avoid re-reading a reference already read in the current task unless the design changes substantially.

## Hard Composition Rules

- The dominant user job determines the protagonist. A login page foregrounds authentication; a dashboard foregrounds state and actions; a product page foregrounds the product.
- Every major desktop region must have a functional, evidentiary, narrative, or perceptual role.
- A full-height divider is a strong visual object. Use it only when both sides are meaningful peers or interaction crosses the boundary. Do not divide marketing atmosphere from a small task panel.
- On wide screens, fixed-width content must not become stranded inside expanding empty fields.
- For compact task surfaces such as login, search, booking, and checkout, keep required inputs and the primary action visible at `390x844` when the task length permits.
- Brand display type, illustrations, ambient motion, or product marks must not outweigh the active task on task-led screens.
- Use one leading signature device. Grids, particles, giant rings, neon glow, pseudo-terminal text, glass, bento grids, huge type, noise, and decorative telemetry are generic motifs, not evidence of a subject.
- Treat one generic motif as the default budget. Additional motifs must perform different, visible content or interaction jobs and must be supported by the brief; otherwise remove them.
- Never use fake metrics, testimonials, logos, legal identifiers, certifications, contact details, screenshots, or operational data to make a design feel credible.

## Visual Verification Matrix

When browser automation or browser control is available, capture and inspect the actual rendered UI:

| Viewport | Required use |
| --- | --- |
| `390x844` | Mobile order, task visibility, touch targets, wrapping |
| `768x1024` | Tablet transition and intermediate breakpoints |
| `1440x900` | Standard desktop composition |
| `1920x1080` | Wide desktop space roles |
| `2560x1080` | Required for full-bleed, split-screen, fixed-width, or cinematic layouts |

The `2560x1080` check cannot be replaced by a `1440px` screenshot. Wide-screen failures often remain invisible at standard desktop sizes.

Use browser automation or browser control already available in the environment. Prefer a real viewport API over browser window sizing. Inspect screenshots at 100% scale and interact with the page rather than judging HTML alone.

Do not install a browser, automation framework, runtime, or audit dependency solely to satisfy this skill unless the user asks for it. If no browser capability is available, state that rendered visual verification was not performed and hand the viewport matrix to the user for manual inspection.

## Implementation Contract

- Preserve the existing stack and local patterns when present.
- Encode choices as tokens or reusable classes/components.
- Use one icon family and accessible controls.
- Provide real states: default, hover, focus, active, disabled, loading, error, empty, permission, or offline where relevant.
- Keep visible copy inside the product's world. Put implementation notes in comments or delivery notes.
- Respect reduced motion and avoid expensive continuous effects.
- Do not substitute static linting, source inspection, or an accessibility checklist for visual browser evidence.

## Completion Standard

Do not declare completion when any hard gate fails. Do not average severe problems into a passing score. The weakest critical dimension controls readiness.

Report:

- what was built or reviewed;
- which viewports and interactions were verified, or which require manual inspection because browser tooling was unavailable;
- the weakest remaining dimension and its visible evidence;
- any limitation that could not be tested.
