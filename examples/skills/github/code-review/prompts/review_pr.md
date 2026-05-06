---
name: review_pr
description: Step-by-step PR review prompt template.
arguments:
  - name: owner
    description: Repository owner.
    required: true
  - name: repo
    description: Repository name.
    required: true
  - name: pr_number
    description: Pull request number.
    required: true
---

# Review PR {{.owner}}/{{.repo}}#{{.pr_number}}

You are reviewing a pull request. Follow this sequence:

1. Fetch PR metadata with `github.get_pull_request`.
2. Read the diff with `github.get_pull_request_diff`.
3. For non-trivial files, fetch the file contents with `github.get_file_contents`.
4. Group findings by severity: must-fix / should-fix / nit.
5. If you decide to post a comment, use `github.create_review_comment` —
   this tool requires approval and is destructive.
