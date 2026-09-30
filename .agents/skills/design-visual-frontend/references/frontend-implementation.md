# Frontend Implementation

Preserve the design decisions in real code and browser behavior.

## Stack

Inspect the repository before adding dependencies. Use the existing framework, design system, tokens, components, and icon library when suitable. For a single-file prototype, keep dependencies minimal and provide graceful fallback for remote assets.

## Tokens and Components

Encode color, typography, spacing, radius, borders, elevation, and motion as tokens. Reuse component classes or components instead of one-off styling. Keep dimensions stable when labels, loading indicators, icons, counts, or validation messages change.

## Responsive Layout

- Prefer Grid for multi-region composition.
- Define explicit mobile, desktop, wide, and ultra-wide behavior.
- Avoid fixed max-width islands inside full-bleed layouts.
- Reserve media dimensions to prevent layout shift.
- Test intermediate breakpoints, not only endpoints.
- Prevent horizontal overflow and accidental navigation wrapping.

## Motion

Motion must express cause, hierarchy, feedback, or state transition. Prefer transform and opacity. Avoid permanent pointer-following, scroll hijacking, custom cursors, parallax, and ambient loops unless the brief and audience justify them. Respect `prefers-reduced-motion` and clean up observers/listeners.

## Assets

Use real, generated, or user-provided media when the subject requires visual evidence. Optimize and size assets. Do not fabricate screenshots or factual proof. If useful media is unavailable, change the composition instead of drawing fake rectangles.

## Accessibility

- Use semantic elements and programmatic labels.
- Keep focus visible.
- Provide accessible names for icon controls.
- Ensure contrast on actual rendered backgrounds.
- Do not rely on color alone.
- Test keyboard navigation and zoom/content stress.

## Browser Verification

Use browser automation or browser control already available in the environment. Do not install additional browser tooling solely for this workflow unless the user requests it. When browser capability is available, the minimum loop is:

1. open the real page;
2. wait for rendered state;
3. capture the required viewport matrix;
4. inspect DOM and console;
5. interact with core flows;
6. recapture after structural fixes.

Do not substitute OS window sizing for a true mobile viewport. Keep inspection artifacts in a temporary or existing output directory and clean them up when the user requested a single deliverable.

When browser capability is unavailable:

1. inspect responsive rules, semantic structure, focus behavior, overflow risks, asset fallbacks, and reduced-motion handling in code;
2. do not claim that the interface was visually verified;
3. give the user the required viewport matrix and a concise list of the highest-risk visual relationships to inspect manually.
