---
name: pitago-plannotator-review
description: Use Pitago's capability-aware Plannotator review command to hand a plan, document, folder, or latest reply to the human without requiring Herdr, Orca, or a particular Plannotator UI.
---

# Pitago Plannotator review

When a plan, specification, document, or recent assistant reply needs human review:

1. Write the artifact to a stable Markdown file before opening review.
2. Run `/annotate <path>` from Pitago, or use `/annotate last` for the latest reply.
3. End the turn. Do not poll the review surface or scrape terminal output.
4. Address the numbered feedback when it arrives as the next user message.

If `/annotate doctor` reports no available review UI, do not claim that review is pending. Report the exact missing capability and leave the artifact at its stable path. The user can install `plannotator-tui`, enable the Pi Plannotator extension, or use the clipboard fallback.

If a Plannotator extension command is available, it remains available through Pitago's normal command palette. Do not duplicate or intercept its command names.
