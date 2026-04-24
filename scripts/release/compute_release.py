#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass


INITIAL_TAG = "v0.1.0"
BREAKING_CHANGE_RE = re.compile(r"(?im)^BREAKING CHANGES?:")
BREAKING_BANG_RE = re.compile(r"^[A-Za-z]+(?:\([^)]+\))?!:\s+")
FEAT_RE = re.compile(r"^feat(?:\([^)]+\))?!?:\s+", re.IGNORECASE)
PATCH_RE = re.compile(r"^(?:fix|hotfix)(?:\([^)]+\))?!?:\s+", re.IGNORECASE)
SEMVER_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")


@dataclass(frozen=True)
class SemVer:
    major: int
    minor: int
    patch: int

    @classmethod
    def parse(cls, value: str) -> "SemVer":
        match = SEMVER_RE.fullmatch(value)
        if not match:
            raise ValueError(f"invalid semver tag: {value}")
        return cls(*(int(group) for group in match.groups()))

    def bump(self, kind: str) -> "SemVer":
        if kind == "major":
            return SemVer(self.major + 1, 0, 0)
        if kind == "minor":
            return SemVer(self.major, self.minor + 1, 0)
        if kind == "patch":
            return SemVer(self.major, self.minor, self.patch + 1)
        raise ValueError(f"unsupported bump kind: {kind}")

    def tag(self) -> str:
        return f"v{self.major}.{self.minor}.{self.patch}"

    def docker_tag(self) -> str:
        return f"{self.major}.{self.minor}.{self.patch}"


def env(name: str) -> str:
    value = os.getenv(name)
    if value is None or value == "":
        raise SystemExit(f"missing required environment variable: {name}")
    return value


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def semver_tags() -> list[str]:
    output = git("tag", "--list", "v*.*.*", "--sort=-version:refname")
    return [line for line in output.splitlines() if line]


def latest_semver_tag() -> str:
    tags = semver_tags()
    return tags[0] if tags else ""


def semver_tags_on_commit(sha: str) -> list[str]:
    output = git("tag", "--points-at", sha, "--list", "v*.*.*", "--sort=-version:refname")
    return [line for line in output.splitlines() if line]


def github_get(url: str, token: str) -> list[dict]:
    request = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "X-GitHub-Api-Version": "2022-11-28",
        },
    )
    with urllib.request.urlopen(request) as response:
        return json.load(response)


def fetch_pr_commit_messages(repo: str, pr_number: str, token: str) -> list[str]:
    messages: list[str] = []
    page = 1
    while True:
        query = urllib.parse.urlencode({"per_page": "100", "page": str(page)})
        url = f"https://api.github.com/repos/{repo}/pulls/{pr_number}/commits?{query}"
        commits = github_get(url, token)
        if not commits:
            break
        for item in commits:
            messages.append(item["commit"]["message"])
        if len(commits) < 100:
            break
        page += 1
    return messages


def release_kind(title: str, body: str, commit_messages: list[str]) -> str:
    text_blobs = [title, body, *commit_messages]
    if any(BREAKING_CHANGE_RE.search(blob or "") for blob in text_blobs):
        return "major"

    candidate_lines: list[str] = []
    if title:
        candidate_lines.append(title.strip())
    if body:
        candidate_lines.extend(line.strip() for line in body.splitlines())
    for message in commit_messages:
        candidate_lines.extend(line.strip() for line in message.splitlines())

    if any(BREAKING_BANG_RE.match(line) for line in candidate_lines if line):
        return "major"
    if any(FEAT_RE.match(line) for line in candidate_lines if line):
        return "minor"
    if any(PATCH_RE.match(line) for line in candidate_lines if line):
        return "patch"
    return "none"


def write_outputs(values: dict[str, str]) -> None:
    output_path = os.getenv("GITHUB_OUTPUT")
    lines = [f"{key}={value}" for key, value in values.items()]
    if output_path:
        with open(output_path, "a", encoding="utf-8") as fh:
            fh.write("\n".join(lines))
            fh.write("\n")
    print(json.dumps(values, ensure_ascii=False, indent=2))


def main() -> int:
    repo = env("GITHUB_REPOSITORY")
    token = env("GITHUB_TOKEN")
    merge_commit_sha = env("MERGE_COMMIT_SHA")
    pr_number = env("PR_NUMBER")
    pr_title = os.getenv("PR_TITLE", "")
    pr_body = os.getenv("PR_BODY", "")

    commit_messages = fetch_pr_commit_messages(repo, pr_number, token)
    kind = release_kind(pr_title, pr_body, commit_messages)
    all_tags = semver_tags()
    current_tags = semver_tags_on_commit(merge_commit_sha)

    if current_tags:
        next_tag = current_tags[0]
        semver = SemVer.parse(next_tag)
        previous_tag = next((tag for tag in all_tags if tag != next_tag), "")
        write_outputs(
            {
                "release_required": "true",
                "release_kind": "existing",
                "previous_tag": previous_tag,
                "git_tag": next_tag,
                "docker_tag": semver.docker_tag(),
                "tag_exists": "true",
            }
        )
        return 0

    previous_tag = latest_semver_tag()

    if not previous_tag:
        # Bootstrap the first automated release at the requested initial version.
        next_tag = INITIAL_TAG
        semver = SemVer.parse(next_tag)
        release_required = "true"
        kind = "initial" if kind == "none" else kind
    elif kind == "none":
        write_outputs(
            {
                "release_required": "false",
                "release_kind": "none",
                "previous_tag": previous_tag,
                "git_tag": "",
                "docker_tag": "",
                "tag_exists": "false",
            }
        )
        return 0
    else:
        semver = SemVer.parse(previous_tag).bump(kind)
        next_tag = semver.tag()
        release_required = "true"

    tag_exists = "true" if subprocess.run(
        ["git", "rev-parse", "-q", "--verify", f"refs/tags/{next_tag}"],
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    ).returncode == 0 else "false"
    if tag_exists == "true":
        tagged_commit = git("rev-list", "-n", "1", next_tag)
        if tagged_commit != merge_commit_sha:
            raise SystemExit(
                f"refusing to reuse existing tag {next_tag} because it points to {tagged_commit}, not {merge_commit_sha}"
            )

    write_outputs(
        {
            "release_required": release_required,
            "release_kind": kind,
            "previous_tag": previous_tag,
            "git_tag": next_tag,
            "docker_tag": semver.docker_tag(),
            "tag_exists": tag_exists,
        }
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except urllib.error.HTTPError as exc:
        sys.stderr.write(f"GitHub API request failed: {exc.code} {exc.reason}\n")
        raise
