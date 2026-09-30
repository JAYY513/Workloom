# Design Visual Frontend

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Agent Skill](https://img.shields.io/badge/Agent-Skill-111827)](SKILL.md)

`design-visual-frontend` is an Agent Skill for designing, implementing, redesigning, and reviewing frontend interfaces. It helps coding agents move beyond generic template styling by making them reason about the product's real subject, user task, composition, responsive behavior, content truth, accessibility, and rendered visual evidence.

The Skill can be used with Codex, Claude Code, and other tools that support the open `SKILL.md`-based Agent Skills format.

## What It Does

- Classifies the interface as task-led, command-led, inspection-led, narrative-led, reading-led, or conversion-led.
- Defines the visual hierarchy before implementation through a compact decision sheet.
- Grounds the design in real product content, workflows, assets, and domain conventions.
- Prevents common AI-generated UI patterns such as empty split screens, decorative card grids, fake metrics, oversized headings, and stacked visual effects.
- Establishes reusable rules for typography, color roles, spacing, surfaces, controls, states, and motion.
- Requires explicit mobile, tablet, desktop, wide, and ultra-wide behavior.
- Reviews accessibility, loading/error/empty states, keyboard focus, touch targets, reduced motion, and engineering integrity.
- Uses browser screenshots and interaction checks when browser tooling is available, while clearly reporting when visual verification was not possible.

## When To Use It

Use this Skill for:

- websites and landing pages;
- login, signup, checkout, booking, search, and form flows;
- dashboards, admin tools, monitors, editors, and operational consoles;
- product pages, portfolios, publications, and content-heavy experiences;
- frontend redesigns and anti-template visual improvements;
- visual UI reviews that need concrete, evidence-based findings;
- responsive implementation and browser-based quality verification.

## How It Works

The repository is not a component library or a fixed visual theme. It is a structured reasoning system for an AI coding agent.

1. `SKILL.md` defines when the Skill should activate and the workflow the agent must follow.
2. The agent inspects the request, codebase, stack, assets, and available browser tools.
3. It identifies the dominant surface archetype and the interface's true protagonist.
4. It creates a compact design decision sheet covering hierarchy, content truth, composition, tokens, signature behavior, and likely failure risks.
5. It loads only the reference files relevant to the task.
6. It implements or reviews the interface using the existing project conventions.
7. When possible, it renders the real page at the required viewport matrix and checks the non-compensating review gates.
8. A critical failure requires revision; attractive details cannot compensate for a broken task hierarchy, fake content, poor responsiveness, or inaccessible interaction.

This approach separates design judgment from decoration. The agent first fixes the subject, page regions, eye path, and product workflow, then applies typography, color, material, and one justified expressive device.

## Install In Codex

### User-level installation

Clone the repository into the Codex skills directory:

```bash
git clone https://github.com/Xialiang98/design-visual-frontend.git ~/.codex/skills/design-visual-frontend
```

On Windows PowerShell:

```powershell
git clone https://github.com/Xialiang98/design-visual-frontend.git "$HOME\.codex\skills\design-visual-frontend"
```

Start a new Codex task after installation so the Skill catalog is refreshed. You can invoke it explicitly:

```text
Use $design-visual-frontend to redesign this dashboard and verify it at mobile, desktop, and wide viewports.
```

Codex may also select the Skill automatically when the request matches the description in `SKILL.md`.

## Install In Claude Code

### User-level installation

```bash
git clone https://github.com/Xialiang98/design-visual-frontend.git ~/.claude/skills/design-visual-frontend
```

On Windows PowerShell:

```powershell
git clone https://github.com/Xialiang98/design-visual-frontend.git "$HOME\.claude\skills\design-visual-frontend"
```

### Project-level installation

To share the Skill with everyone working in one repository, clone or copy it into:

```text
your-project/.claude/skills/design-visual-frontend/
```

Open a new Claude Code session after installation. Ask Claude to use the Skill by name, or invoke the discovered skill command when it is available in your Claude Code version:

```text
Use the design-visual-frontend skill to review this login flow and fix the critical visual failures.
```

## Install In Other Agent Tools

For any Agent Skills-compatible tool, place this entire repository in that tool's skills directory. The final layout must keep `SKILL.md`, `references/`, and `agents/` together:

```text
design-visual-frontend/
|-- SKILL.md
|-- agents/
|   `-- openai.yaml
`-- references/
    |-- core-design.md
    |-- surface-archetypes.md
    |-- review-gates.md
    |-- components-content.md
    |-- domain-guidance.md
    `-- frontend-implementation.md
```

If a tool uses a different extension format, point its system instructions or skill loader at `SKILL.md` and preserve the relative reference paths.

## Example Prompts

```text
Use $design-visual-frontend to build the actual admin dashboard as the first screen. Keep it dense, operational, and responsive.
```

```text
Review this product page with design-visual-frontend. Report critical visual and usability failures first, then implement the fixes.
```

```text
Redesign this login page. Make authentication the protagonist, keep the primary action visible at 390x844, and verify the wide-screen composition.
```

```text
Create two materially different composition directions for this cultural portfolio, choose the stronger direction from the brief, and implement it.
```

## Reference System

| File | Purpose |
| --- | --- |
| `SKILL.md` | Activation rules, core workflow, composition constraints, verification matrix, and completion standard |
| `references/core-design.md` | Subject, hierarchy, viewport composition, typography, color, material, and expression |
| `references/surface-archetypes.md` | Page-type classification and archetype-specific failure modes |
| `references/review-gates.md` | Non-compensating quality gates and visual evidence protocol |
| `references/components-content.md` | Controls, states, navigation, component grammar, UI copy, and content truth |
| `references/domain-guidance.md` | Domain-specific workflows and credibility requirements |
| `references/frontend-implementation.md` | Responsive implementation, accessibility, motion, assets, and browser verification |
| `agents/openai.yaml` | Display metadata and default prompt for compatible OpenAI tooling |

## Design Principles

- The user's primary job determines the protagonist.
- Every major region must have a functional, evidentiary, narrative, or perceptual role.
- Real content and real product objects are stronger than decorative genre signals.
- One subject-specific signature is better than several generic visual effects.
- Mobile and ultra-wide layouts are designed intentionally, not derived by simple scaling.
- Browser evidence is preferred over judging source code alone.
- A severe failure in one critical dimension cannot be averaged away by strengths elsewhere.

## Limitations

- The Skill improves an agent's process; it does not replace product requirements, real assets, user research, or domain expertise.
- Visual verification depends on browser tooling being available in the host environment.
- The Skill does not ship UI components, fonts, images, or a runtime dependency.
- Host tools differ in how they discover and invoke skills. Consult the current documentation for your agent if its skills directory has changed.

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution workflow. Useful contributions include clearer review gates, additional domain guidance, reproducible failure examples, and compatibility improvements for Agent Skills hosts.

## Contact

Maintainer: [Xialiang98](https://github.com/Xialiang98)

WeChat: `myloveynx0629`

![WeChat QR code](./%E5%BE%AE%E4%BF%A1.jpg)

## License

Released under the [MIT License](LICENSE).
