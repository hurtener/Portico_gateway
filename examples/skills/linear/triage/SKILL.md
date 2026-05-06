# Linear Triage

Triage open issues:

1. Pull the queue with `linear.list_issues` (state = "Triage").
2. For each issue, classify (Bug / Feature / Question / Tracking).
3. Suggest priority + owner.
4. Apply changes via `linear.update_issue` ONLY after the user
   approves — every update goes through Portico's approval flow.
