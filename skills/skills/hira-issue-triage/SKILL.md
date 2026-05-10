---
name: hira-issue-triage
description: Use when triaging new Hira issues — analyze the title and description, suggest priority, identify missing info, and recommend an assignee based on workspace members and past issue patterns.
---

# Hira Issue Triage

Helps you triage incoming issues in a Hira workspace before they get worked on.

## When to use

A new issue arrives and:
- It has no priority set
- No assignee
- The description is unclear or missing repro steps
- You want a sanity check before assigning

## Workflow

1. Read the issue:
   `hira issue get <id> --output json`

2. Look at workspace members for assignee candidates:
   `hira workspace members --output json`

3. Scan recent issues to find owners of similar areas:
   `hira issue list --limit 30 --output json`

4. Decide priority:
   - **urgent** — production breakage, security, data loss
   - **high** — blocks a key user flow, repeated user complaints
   - **medium** — noticeable but workaround exists
   - **low** — polish, nice-to-have

5. Decide suggested assignee:
   - Past authorship of issues touching the same area
   - Stated ownership in workspace conventions
   - Leave as "unclear" if no signal — do NOT guess

6. Post your suggestion as a comment. Do NOT auto-assign or change priority — let a human confirm.

## Output format

Post a comment in this exact shape:

> **Triage suggestion**
> - Priority: `<urgent|high|medium|low>` — one-line reasoning
> - Suggested assignee: `<name>` or `unclear` — one-line reasoning
> - Missing info: bullet list, or `none`

## Anti-patterns

- Do NOT call `hira issue status` or `hira issue assign` — triage is advisory.
- Do NOT guess priority from title alone; read the description.
- If the issue is a duplicate, link to the original issue ID instead of triaging.
