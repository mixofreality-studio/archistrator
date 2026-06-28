---
name: the-method-doctrine
description: Reference for Juval Löwy's Prime Directive and the 9 directives from Appendix C of Righting Software. Use when another skill needs the authoritative statement of a directive, the rationale behind it, or the book chapters where it is developed.
---

# The Method — Doctrine

This is a pure reference skill. It holds the non-negotiable rules of *The Method*: the Prime Directive and the 9 directives. Other skills cite it instead of restating doctrine inline, to avoid drift.

Source: Juval Löwy, *Righting Software* (2019), [Appendix C "Design Standard"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appc).

## The Prime Directive

[Appendix C §1 "The Prime Directive"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec1):

> **Never design against the requirements.**

The hallmark of a bad design: when requirements change, the design has to change. *The Method* prevents that by decomposing on **volatility**, not on features or domains. The Prime Directive is restated and motivated in [Ch. 4 §1.2 "Design Prime Directive"](../../../research/rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev2sec2).

A directive is a rule you should never violate — doing so is certain to cause project failure. A guideline is advice you may override only with strong and unusual justification. The Prime Directive and the 9 directives below are all directives.

## The 9 Directives

From [Appendix C §2 "Directives"](../../../research/rightingsoftware/OEBPS/xhtml/appc.xhtml#appclev1sec2). The directive numbers and headline statements (the "Directive" column) are verbatim from the book and not negotiable. The "What it means" column is a short summary written for this skill, not book text — do not quote it as canonical. When you need the canonical wording or rationale, follow the link in the "Developed in" column.

| # | Directive | What it means | Developed in |
|---|---|---|---|
| 1 | Avoid functional decomposition. | Do not decompose a system into components named after features ("OrderProcessing", "Reporting"). Functional decomposition leaks requirements into the architecture; the system cannot absorb change. | [Ch. 2 §1–2](../../../research/rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev1sec1) |
| 2 | Decompose based on volatility. | Each component encapsulates one area of volatility. Volatility identified along the two axes (same customer over time; all customers at one time). | [Ch. 2 §3 "Identifying Volatility"](../../../research/rightingsoftware/OEBPS/xhtml/ch02.xhtml#ch02lev1sec3) |
| 3 | Provide a composable design. | The architect's mission is the smallest set of components that compose to satisfy all core use cases — not one component per use case, not a god service. Build like Lego, not like a jigsaw. | [Ch. 4 §2 "Composable Design"](../../../research/rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev1sec2) |
| 4 | Offer features as aspects of integration, not implementation. | Features emerge from how components interact, not from a dedicated "feature component". A new feature should be a new orchestration of existing components. | [Ch. 4 §3 "There Is No Feature"](../../../research/rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev1sec3) |
| 5 | Design iteratively, build incrementally. | Iterate on the design until it is right; then build the system incrementally by adding use cases against a stable architecture. Do not iterate on the system itself. | [Ch. 4 §4 "Handling Change"](../../../research/rightingsoftware/OEBPS/xhtml/ch04.xhtml#ch04lev1sec4) |
| 6 | Design the project to build the system. | The project is itself an engineering artifact. Without a project design (network, float, critical path, options), the system has no chance of being delivered on time and on budget. | [Ch. 7 "Project Design Overview"](../../../research/rightingsoftware/OEBPS/xhtml/ch07.xhtml#ch07) |
| 7 | Drive educated decisions with viable options that differ by schedule, cost, and risk. | Never present management with one plan. Always present at least three viable options (normal, compressed, subcritical) with quantified duration, cost, and risk so the decision is theirs to make. | [Ch. 9 "Time and Cost"](../../../research/rightingsoftware/OEBPS/xhtml/ch09.xhtml#ch09), [Ch. 10 "Risk"](../../../research/rightingsoftware/OEBPS/xhtml/ch10.xhtml#ch10), [Ch. 11 "Project Design in Action"](../../../research/rightingsoftware/OEBPS/xhtml/ch11.xhtml#ch11) |
| 8 | Build the project along its critical path. | Resource the critical path first and with the best people. Compress by parallelising critical activities. Status is measured against the critical path, not against arbitrary feature lists. | [Ch. 8 "Network and Float"](../../../research/rightingsoftware/OEBPS/xhtml/ch08.xhtml#ch08) |
| 9 | Be on time throughout the project. | Every activity is on time or it is not. Slippage is detected and corrected weekly, not at the end. Earned value and projections are tracked continuously. | [Ch. 12 "Advanced Techniques"](../../../research/rightingsoftware/OEBPS/xhtml/ch12.xhtml#ch12) |

## How to cite

When another skill needs to invoke a directive, link to this file and quote the directive number and its book section. For example:

> Per [the-method-doctrine](../the-method-doctrine/SKILL.md), Directive 2 ("Decompose based on volatility") requires that every component map to exactly one volatility entry.

Do not restate the directives inline in other skills. If you find yourself rewriting Directive 1 in a third skill, link here instead.

## See also

- [the-method-layers](../the-method-layers/SKILL.md) — the layer model and interaction rules that operationalise Directives 1–4.
- [the-method-system-design-standard-check](../the-method-system-design-standard-check/SKILL.md) — the full Appendix C checklist (guidelines plus directives) applied as a quality gate.
