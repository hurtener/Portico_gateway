---
name: explore
description: Explore an unknown Postgres database.
arguments:
  - name: question
    description: The user's high-level question.
    required: true
---

# Investigation: {{.question}}

Step 1: list schemas to scope your work.
Step 2: pick the relevant tables, describe each.
Step 3: write SELECT queries answering the question. Use LIMIT 200.
