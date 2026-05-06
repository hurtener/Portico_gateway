---
name: summarize_diff
description: Summarise a diff in 3-5 bullets.
arguments:
  - name: owner
    required: true
  - name: repo
    required: true
  - name: pr_number
    required: true
---

# Summarise PR {{.owner}}/{{.repo}}#{{.pr_number}}

Read the diff via `github.get_pull_request_diff` and produce 3-5
bullets covering: (1) what changed, (2) why, (3) what didn't change
that maybe should have.
