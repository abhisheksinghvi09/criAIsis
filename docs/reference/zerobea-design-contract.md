# Approved final design contract

This specification supersedes historical variation and logo-exploration instructions in older notes.

## Appearance

| Setting | Approved value |
|---|---|
| First-use mode | Light |
| Light colors | Former V2 Blue |
| Dark colors | Former V3 Neutral |
| Interface font | V2 DM Sans for headings, navigation, controls, body and chart annotations |
| Technical font | JetBrains Mono for technical values and evidence |
| Mode switch | Existing sidebar control |
| Appearance tabs | Removed from the interface |
| Logo | Original Boundary logo with its opening reduced from 90° to 42° |
| Signal geometry | Original rising line, three dots and stroke weights preserved |
| Logo dropdown | Removed; brand link returns to Overview |

The approved boundary path is `M39.58846 15 A18 18 0 1 0 41.60666 27.74241`. Keep the logo circular; do not use the rejected elongated zero, shield, pulse or connected-logo concepts. Both colors and fonts must follow mode without resetting the active route, form, filters or personal review state. Old palette preferences must not override the final pairing.

Inactive historical styling may remain to preserve the established cascade; it is not an alternate user-facing appearance or permission to restore comparison tabs. Any later stylesheet consolidation must preserve the approved rendering.

## Product behavior

Preserve the seven-route product: Overview, Activity, Audit log, Test scenarios, Content scanner, Detection rules, and Roles & policies. Retain their existing advanced views, controls, payloads, evidence and recovery behavior.

Keep the rounded component hierarchy, consistent chart colors, permanent chart legends, rounded outer bar ends, compact filters, readable scrolling, anchored guides and centered explanations. Keep the protection animation compact and honor reduced motion.

Preserve the Overview priority queue, personal Review now/Next/Backlog/Reviewed lanes, Focus on priorities, notification inbox, watch preferences, independent source states, retry and evidence dialogs. These are personal review tools; they do not implement shared incident assignment or backend resolution. Inbox preferences do not filter the Overview queue.

Preserve deliberate loading and revalidation of pending approvals, existing action guards and terminal-response checks. Opening an approval or review dialog never approves or executes a tool. Copy handoff prepares text for manual sharing; it sends no external message.

Keep backend enforcement, authentication, the API client, endpoint contracts, dependency manifests and lockfiles, build configuration, deployment scripts and original `project/CLAUDE.md` unchanged. Do not add Ask Zerobea, fleet management, policy-engine changes or external notification delivery.

## Reference and implementation

`reference/zerobea-main-theme-preview.html` is the exact approved design reference, with simulated services. Production source and rebuilt assets are in `project/`. Reuse the implementation rather than approximating it from screenshots. Verify both modes, all routes, keyboard flow, dialogs, text zoom and live gateway behavior before a production rollout.
