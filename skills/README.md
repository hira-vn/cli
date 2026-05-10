# Hira Skills Collection

A collection of skills for the [Hira](https://github.com/hira-vn) AI-native task platform. Skills are markdown-defined capabilities that coding agents in Hira can invoke when working on issues.

## Importing a skill

From any Hira workspace:

```bash
hira skill import skills.sh/hira-vn/cli/<skill-name>
```

Replace `<skill-name>` with one of the directories under [`skills/`](./skills).

## Available skills

| Skill | What it does |
|-------|--------------|
| [`hira-issue-triage`](./skills/hira-issue-triage) | Suggests priority and assignee for new issues |

<!-- Add new rows above this line. Keep alphabetical. -->

## Authoring a skill

Each skill lives at `skills/<name>/` and must contain a `SKILL.md` with YAML frontmatter:

```markdown
---
name: my-skill
description: One-line trigger description — when does this skill apply?
---

# Skill body in markdown...
```

### Required frontmatter fields

| Field | Constraint |
|-------|------------|
| `name` | Non-empty. Should match the directory name. |
| `description` | Non-empty. One sentence describing when to use the skill. |

### Supporting files

You can add any number of supporting files alongside `SKILL.md` (scripts, references, prompts). They'll be downloaded into the skill's working directory when imported. **Each file must be under 1 MB** — the Hira importer caps raw fetches at that size.

`LICENSE` and `SKILL.md` are excluded from the supporting-file collection (handled separately).

### Validation

Before opening a PR, run the validator locally:

```bash
python3 scripts/validate_skills.py
```

CI runs the same check on every PR that touches `skills/**`.

## License

MIT — see [LICENSE](../LICENSE).
