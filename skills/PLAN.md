# Hira Skills Collection — Build Plan

Working doc for the public skills collection that lives at `cli/skills/`. Captures
what's done, what's pending, and the decisions we've already made so the next
session can pick up cold.

## Goal

Publish a Hira skills collection that users can import via `hira skill import`,
mirroring on **GitHub (skills.sh)** as source of truth and **clawhub.ai** for
discovery.

Import URL once pushed:

```
hira skill import skills.sh/hira-vn/cli/<skill-name>
```

(Currently nested in the `cli` repo. If we later promote to a dedicated
`hira-vn/skills` repo, update the README import URL.)

## Status

### Done

- [x] Repo scaffold under `cli/skills/`
  - `skills/hira-issue-triage/SKILL.md` — example skill, validated
  - `README.md` — import + authoring guide, skill table
  - `scripts/validate_skills.py` — structural validator
- [x] CI: `.github/workflows/validate-skills.yml`
  - Triggers on `push` to main + PRs touching `skills/**`
  - Runs `python3 scripts/validate_skills.py` from `skills/`
- [x] Validator local-tested
  - Passes on `hira-issue-triage`
  - Catches: missing `SKILL.md`, missing frontmatter, empty `name`,
    name/folder mismatch (warning), files ≥ 1 MB

### Pending

- [ ] Push to GitHub and confirm `hira skill import skills.sh/hira-vn/cli/hira-issue-triage` succeeds end-to-end against a real workspace
- [ ] Add real skills beyond the triage example — candidates:
  - `hira-pr-summary` — summarize a PR onto an issue
  - `hira-release-notes` — collect closed issues since last tag
  - `hira-standup` — generate "what I shipped yesterday" from issue activity
- [ ] ClawHub mirror — see Open questions below
- [ ] (Optional) Auto-update README skill table from validator output, so we
      don't forget to register new skills

## Validator rules (mirrored from `server/internal/handler/skill.go`)

The Hira importer's `parseSkillFrontmatter` is the source of truth. The
validator enforces:

1. `SKILL.md` exists in every `skills/<name>/` directory
2. File begins with `---` and has a closing `---`
3. Non-empty `name:` and `description:` fields (quotes stripped)
4. Every file in the skill directory is `< 1 MB` (importer caps raw fetches at
   `1 << 20` bytes)
5. **Warning** if `name` field doesn't match the folder name

## Decisions already made

- **Location**: `cli/skills/` inside the cli repo (not a sibling repo). Easier
  to maintain alongside the importer code; can split later if it grows.
- **Validator language**: Python stdlib only — no `pip install` step in CI.
- **Workflow scope**: only triggers on `skills/**` paths so unrelated commits
  don't run the validator.
- **Authoring style** (example skill): explicit "When to use" + "Workflow" +
  "Anti-patterns" sections. Triage skill is advisory — agent must NOT mutate
  state without human confirmation.

## Open questions

- **ClawHub publish flow**: code in `server/internal/handler/skill.go` only
  shows the consumer side (`clawhub.ai/api/v1/skills/{slug}`). Need to find
  out from clawhub.ai whether they expose a publish API or if it's UI-only.
  Until that's answered, skills.sh is the only auto-publishable route.
- **Repo split**: keep at `cli/skills/` or promote to `hira-vn/skills`?
  Decision deferred until we have ≥ 5 skills.
- **Versioning**: skills.sh always serves the default branch. If we want
  pinned versions, need to think about tags or paths like
  `skills.sh/hira-vn/cli@v1/<name>` (would require importer changes).

## Resuming

To pick up next session:

1. Read this file
2. `cd cli/skills && python3 scripts/validate_skills.py` to confirm the
   collection still validates
3. Pick a pending item above
