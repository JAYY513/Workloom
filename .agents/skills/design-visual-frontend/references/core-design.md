# Core Design Decisions

Use this reference to make visual decisions before styling details.

## 1. Start With the Subject

Name the actual subject, audience, and job. Derive visual character from the subject's real objects, materials, workflows, language, and evidence.

Good sources of identity:

- product photography, maps, documents, data, inventory, tools, publications, people, places;
- a real workflow or live component;
- typography and composition derived from the organization's communication style;
- a repeatable interaction principle tied to the product.

Weak substitutes:

- abstract cards, invented dashboards, fake terminal output;
- generic technology symbols;
- genre decoration standing in for product evidence.

## 2. Compose Visual Weight

Visual weight comes from size, contrast, density, saturation, sharpness, isolation, and motion.

Define:

- `P1`: the intended first mass or message;
- `P2`: the context that explains P1;
- `P3`: proof, detail, or orientation;
- `Action`: the next user decision.

At thumbnail size, P1 and Action should remain identifiable. If an atmospheric region, giant title, illustration, or logo is louder than the dominant job, change the composition before adjusting color.

Use alignment and proximity to create relationships before borders and cards. Alternate dense and quiet zones with purpose. Empty space must create focus, rhythm, context, safety, or anticipation; otherwise it is unused space.

## 3. Design Across Viewports

Do not design a single screenshot and compress it later.

For each major region, define its role at mobile, desktop, and wide desktop. On mobile, reorder around the primary job. On wide screens, expand meaningful regions or add context; do not leave fixed content floating between empty gutters.

Warning signs:

- the page becomes a complete composition after cropping away the outer 25%;
- a task panel stays under 640px while more than one third of the viewport carries no meaningful role;
- a full-height divider separates two regions with unrelated scale and purpose;
- desktop branding survives while the primary action falls below the mobile fold.

## 4. Use Typography as Structure

Choose type for the product relationship, language, and reading conditions.

- Limit the size and weight ladder.
- Reserve display scale for narrative or brand moments, not compact working panels.
- Control line measure and deliberate line breaks.
- Break Chinese headings by meaning; do not rely on accidental wrapping or negative tracking.
- Use monospace only for genuine code, identifiers, or tabular technical content.
- Test long labels, errors, CJK/Latin mixtures, and localization.

Large type is not automatically confident. If reducing the headline destroys the composition, the page probably lacks a real subject or strong region structure.

## 5. Build a Role-Based Color System

Define:

- page, base, raised, inset, and overlay surfaces;
- primary, secondary, muted, and inverse text;
- subtle, regular, strong, and focus borders;
- primary action and restrained accent;
- success, warning, danger, and information states;
- hover, active, selected, disabled, and focus states.

Use contrast and value before decorative hue. Color should not carry hierarchy alone. Avoid default palettes chosen only to signal a genre: purple-blue AI gradients, cream-brown premium palettes, dark slate SaaS, or neon cyber styling.

## 6. Keep Material and Light Coherent

Choose one surface model:

- flat utility;
- editorial paper;
- tactile material;
- photographic depth;
- glass/refraction with meaningful content behind it;
- dense technical surface tied to real data or controls.

Give shadows and glow a source. Use elevation only for elements that are actually above others. Do not outline every object or apply the same radius and shadow to all components.

## 7. Spend Expression Once

Choose one subject-specific signature: an object treatment, unusual but useful composition, data interaction, image crop system, typographic behavior, or meaningful motion sequence.

Generic motif budget:

- gradient glow;
- particle or node field;
- background grid;
- giant ring or orbit;
- pseudo-terminal text;
- glass panels;
- noise overlay;
- bento blocks;
- oversized headline;
- decorative telemetry.

Allow one by default. Every additional motif needs a different content, navigation, evidence, or interaction job that can be demonstrated in the rendered page. Combining several for atmosphere alone creates genre imitation.

## 8. Diagnose Before Decorating

Use this order:

1. Correct the protagonist.
2. Correct region roles and proportions.
3. Correct eye path and content hierarchy.
4. Correct typography and component scale.
5. Correct color and material.
6. Add one justified expressive device.

When a screenshot feels wrong, describe the visible relationship first. Example: "The form uses 22% of the wide viewport while the remaining white field has no role." Then name the cause and change the structural variable.
