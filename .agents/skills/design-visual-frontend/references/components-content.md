# Components and Content

Use one optical and behavioral grammar across controls and visible copy.

## Component Grammar

Define a small system:

- control heights;
- radius levels mapped to object role;
- border strength by elevation;
- one icon family and stroke/fill style;
- one shadow/light model;
- consistent hover, focus, active, selected, loading, disabled, and error behavior.

Controls should look built into the same surface, not pasted on as stickers.

## Forms and Task Controls

- Put persistent labels above fields; placeholders are examples, not labels.
- Keep primary actions explicit: `验证身份`, `保存更改`, `发布文章`, not `继续` or `提交` when a precise verb exists.
- Provide field-level error copy that names the problem and recovery.
- Keep focus visible on the actual background.
- Stabilize control dimensions across loading and success states.
- Use a checkbox or toggle for binary choices, a segmented control for modes, and a menu/select for option sets.
- Keep touch targets at least 44px when touch use is expected.
- For login and recovery, include provider choice, password visibility, lockout/recovery, device/session or MFA context when relevant.

## Buttons

Provide default, hover, pressed, focus, loading, disabled, and success/error feedback where relevant. Use one primary action per action area. Icon-only controls need accessible names and tooltips when the symbol is not universally familiar.

## Navigation

Show current location and deliberate mobile behavior. Do not allow desktop navigation to wrap accidentally. Content-heavy products often need search or command access more than a decorative CTA.

## Containers and Cards

Use a card only when a boundary represents a real object or group. Prefer spacing, alignment, rules, or surface bands for ordinary grouping. Do not nest cards. Do not give unrelated objects the same card anatomy merely for consistency.

## UI States

Design realistic loading, empty, error, offline, denied, expired, locked, partial, and disabled states. State must not depend on color alone.

## Content Voice

Write from inside the product world.

Define:

- speaker and audience role;
- real nouns the interface manages;
- precise verbs users perform;
- native statuses and constraints.

Prefer short labels, commands, statuses, and operational messages over paragraphs explaining the product theme.

Bad:

- "用一个安全、清晰的入口回到你的工作空间。"
- "开始探索下一代智能体验。"

Better:

- "身份凭证校验"
- "设备未登记"
- "提交付款审批"
- "稿件初审中"

Never show prototype notes, replacement instructions, implementation plans, or missing-data disclaimers to end users.

## Content Truth

Use this order:

1. User-provided content and brand assets.
2. Existing repository content and terminology.
3. Publicly familiar domain facts and conventions.
4. Plausible neutral labels and states without claims.
5. Omit evidence that cannot be supported.

Do not invent contact details, legal numbers, certifications, clients, metrics, testimonials, operational telemetry, or product screenshots.

