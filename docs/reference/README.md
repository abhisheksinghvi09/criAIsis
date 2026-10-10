# Reference material

These files document a *different* product ("Zerobea"), not criAIsis. They were
sitting at the repo root looking like active specs; they are kept here, demoted,
because criAIsis's visual theme (`web/src/app/theme.css`) was deliberately ported
from `zerobea-design-contract.md`'s palette and typography choices.

**Treat these as a visual/theme reference only — not a behavioral spec for
criAIsis.** `zerobea-design-contract.md` describes a seven-route product
(Overview, Activity, Audit log, Test scenarios, Content scanner, Detection
rules, Roles & policies) that criAIsis does not have, and explicitly forbids
the external notification delivery (Slack/Discord webhooks) that criAIsis is
built on. It also references `reference/` and `project/` directories that do
not exist in this repository.

- `zerobea-design-contract.md` — the approved appearance/behavior contract for
  Zerobea's dashboard: light/dark palettes, fonts, logo geometry.
- `zerobea-theme-preview.html` — the design contract's own stated "exact
  approved design reference" implementation.
- `zerobea-architecture-walkthrough.html` — an architecture walkthrough for
  the same product.
