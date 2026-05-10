#!/usr/bin/env python3
"""Validate the structure of every skill in skills/.

Mirrors the rules the Hira importer enforces (server/internal/handler/skill.go):
- Each skill lives at skills/<name>/SKILL.md.
- SKILL.md begins with a YAML frontmatter block delimited by ---.
- Frontmatter declares non-empty `name` and `description` fields.
- Every file in the skill directory is under 1 MB.

Run from the repo root:
    python3 scripts/validate_skills.py

Exits 0 on success, 1 if any skill fails validation.
"""
from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SKILLS_DIR = ROOT / "skills"
MAX_FILE_BYTES = 1 << 20  # importer caps raw fetches at 1 MB


class Issue:
    __slots__ = ("skill", "level", "message")

    def __init__(self, skill: str, level: str, message: str) -> None:
        self.skill = skill
        self.level = level
        self.message = message

    def __str__(self) -> str:
        return f"  [{self.level.upper()}] {self.skill}: {self.message}"


def parse_frontmatter(text: str) -> tuple[dict[str, str] | None, str | None]:
    """Return (fields, error). Mirrors parseSkillFrontmatter in skill.go."""
    if not text.startswith("---"):
        return None, "SKILL.md must begin with '---' frontmatter delimiter"
    rest = text[3:]
    end = rest.find("---")
    if end < 0:
        return None, "frontmatter has no closing '---' delimiter"
    block = rest[:end]
    fields: dict[str, str] = {}
    for raw_line in block.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if ":" not in line:
            continue
        key, _, value = line.partition(":")
        value = value.strip().strip('"').strip("'")
        fields[key.strip()] = value
    return fields, None


def validate_skill(skill_dir: Path) -> list[Issue]:
    name = skill_dir.name
    issues: list[Issue] = []

    skill_md = skill_dir / "SKILL.md"
    if not skill_md.is_file():
        issues.append(Issue(name, "error", "missing SKILL.md"))
        return issues

    try:
        text = skill_md.read_text(encoding="utf-8")
    except UnicodeDecodeError as e:
        issues.append(Issue(name, "error", f"SKILL.md is not valid UTF-8: {e}"))
        return issues

    fields, fm_err = parse_frontmatter(text)
    if fm_err:
        issues.append(Issue(name, "error", fm_err))
        return issues
    assert fields is not None

    declared_name = fields.get("name", "")
    if not declared_name:
        issues.append(Issue(name, "error", "frontmatter `name` is missing or empty"))
    elif declared_name != name:
        issues.append(
            Issue(
                name,
                "warning",
                f"frontmatter name '{declared_name}' does not match folder name '{name}'",
            )
        )

    description = fields.get("description", "")
    if not description:
        issues.append(Issue(name, "error", "frontmatter `description` is missing or empty"))

    for path in skill_dir.rglob("*"):
        if not path.is_file():
            continue
        size = path.stat().st_size
        if size >= MAX_FILE_BYTES:
            rel = path.relative_to(skill_dir)
            issues.append(
                Issue(
                    name,
                    "error",
                    f"file '{rel}' is {size} bytes — importer rejects files >= 1 MB",
                )
            )

    return issues


def main() -> int:
    if not SKILLS_DIR.is_dir():
        print(f"error: {SKILLS_DIR} does not exist", file=sys.stderr)
        return 1

    skills = sorted(p for p in SKILLS_DIR.iterdir() if p.is_dir())
    if not skills:
        print(f"error: no skills found under {SKILLS_DIR}", file=sys.stderr)
        return 1

    all_issues: list[Issue] = []
    for skill_dir in skills:
        all_issues.extend(validate_skill(skill_dir))

    errors = [i for i in all_issues if i.level == "error"]
    warnings = [i for i in all_issues if i.level == "warning"]

    print(f"Checked {len(skills)} skill(s) under {SKILLS_DIR.relative_to(ROOT)}/")
    for issue in all_issues:
        print(issue)

    if errors:
        print(f"\n{len(errors)} error(s), {len(warnings)} warning(s) — FAIL")
        return 1
    print(f"\nOK ({len(warnings)} warning(s))")
    return 0


if __name__ == "__main__":
    sys.exit(main())
