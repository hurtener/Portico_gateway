---
name: find
description: Find a file matching a description.
arguments:
  - name: pattern
    description: Glob or short description.
    required: true
---

# Find: {{.pattern}}

Use `fs.list` with sensible glob patterns to narrow the search, then
`fs.read` to confirm matches. Stop as soon as you find a confident
answer; don't blindly traverse.
