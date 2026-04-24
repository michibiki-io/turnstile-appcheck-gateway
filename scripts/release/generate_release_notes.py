#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import urllib.error
import urllib.request


TYPE_RE = re.compile(r"^(feat|fix|hotfix)(?:\([^)]+\))?(?:!)?:\s*(.+)$", re.IGNORECASE)
SECTIONS = {
    "fix": "Bug Fixes",
    "hotfix": "Hot Fixes",
    "feat": "Features",
}
SECTION_ORDER = ["Bug Fixes", "Hot Fixes", "Features"]


def git_log(range_spec: str) -> list[tuple[str, str]]:
    raw = subprocess.check_output(
        ["git", "log", "--format=%H%x1f%s", "--no-decorate", "--no-color", range_spec],
        text=True,
    )
    commits: list[tuple[str, str]] = []
    for line in raw.splitlines():
        if "\x1f" not in line:
            continue
        sha, subject = line.split("\x1f", 1)
        commits.append((sha, subject.strip()))
    return commits


def github_request(url: str, token: str | None) -> list[dict]:
    headers = {"Accept": "application/vnd.github+json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
        headers["X-GitHub-Api-Version"] = "2022-11-28"
    request = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(request) as response:
        return json.load(response)


def associated_pr_number(repo: str, sha: str, token: str | None) -> str:
    if not token:
        return ""
    url = f"https://api.github.com/repos/{repo}/commits/{sha}/pulls"
    try:
        pulls = github_request(url, token)
    except urllib.error.HTTPError:
        return ""

    for pull in pulls:
        if pull.get("merged_at") and pull.get("base", {}).get("ref") == "main":
            return str(pull["number"])
    if pulls:
        return str(pulls[0]["number"])
    return ""


def render_entry(repo: str, sha: str, subject: str, token: str | None) -> tuple[str, str] | None:
    match = TYPE_RE.match(subject)
    if not match:
        return None
    kind = match.group(1).lower()
    description = match.group(2).strip()
    pr_number = associated_pr_number(repo, sha, token)
    short_sha = sha[:7]
    if pr_number:
        return SECTIONS[kind], f"- {description} #{pr_number} ({short_sha})"
    return SECTIONS[kind], f"- {description} ({short_sha})"


def build_notes(repo: str, previous_tag: str, target: str, token: str | None) -> str:
    range_spec = f"{previous_tag}..{target}" if previous_tag else target
    entries: dict[str, list[str]] = {section: [] for section in SECTION_ORDER}

    for sha, subject in git_log(range_spec):
        rendered = render_entry(repo, sha, subject, token)
        if not rendered:
            continue
        section, line = rendered
        entries[section].append(line)

    parts: list[str] = []
    for section in SECTION_ORDER:
        lines = entries[section]
        if not lines:
            continue
        parts.append(f"## {section}")
        parts.extend(lines)
        parts.append("")

    return "\n".join(parts).rstrip() + ("\n" if parts else "")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True)
    parser.add_argument("--target", required=True)
    parser.add_argument("--previous-tag", default="")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    token = os.getenv("GITHUB_TOKEN")
    body = build_notes(args.repo, args.previous_tag, args.target, token)
    with open(args.output, "w", encoding="utf-8") as fh:
        fh.write(body)
    print(body, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
