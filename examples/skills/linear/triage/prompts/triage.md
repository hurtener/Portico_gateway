---
name: triage
description: Triage the next batch of issues.
arguments:
  - name: team
    description: Linear team key.
    required: true
---

# Triage queue: {{.team}}

Pull issues with `linear.list_issues` (state="Triage", team={{.team}}).
Classify each, suggest priority + owner, and ask the user to approve
before applying any updates.
