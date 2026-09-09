#!/usr/bin/env python3
"""Offline structural contract for the WeavePress CI/CD release chain."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/ci-and-mirror.yml"
HOOK = ROOT / "deploy/nas/post-receive"
JENKINS = ROOT / "deploy/jenkins"
DIRECT_PIPELINES = {
    "server": JENKINS / "WeavePressServer.groovy",
    "gateway": JENKINS / "WeavePressGateway.groovy",
}
SYNC_PIPELINES = {
    "server": JENKINS / "ServerSyncGitHubAndDeploy.groovy",
    "gateway": JENKINS / "GatewaySyncGitHubAndDeploy.groovy",
}
JOBS = JENKINS / "jobs.json"
PRODUCTION_CONTRACT = ROOT / "deploy/tests/production-contract.sh"
PINNED_UBUNTU = (
    "ubuntu@sha256:"
    "0e0a0fc6d18feda9db1590da249ac93e8d5abfea8f4c3c0c849ce512b5ef8982"
)
SHA_RE = "^[0-9a-f]{40}$"


def fail(message: str) -> None:
    raise AssertionError(message)


def read_required(path: Path) -> str:
    if not path.is_file():
        fail(f"missing required CI/CD file: {path.relative_to(ROOT)}")
    raw = path.read_bytes()
    if b"\r" in raw:
        fail(f"CI/CD file must use LF line endings: {path.relative_to(ROOT)}")
    return raw.decode("utf-8")


def require(text: str, literal: str, context: str) -> None:
    if literal not in text:
        fail(f"{context} must contain: {literal}")


def reject(text: str, pattern: str, context: str) -> None:
    if re.search(pattern, text, re.MULTILINE | re.IGNORECASE):
        fail(f"{context} contains rejected pattern: {pattern}")


def require_before(text: str, first: str, second: str, context: str) -> None:
    first_at = text.find(first)
    second_at = text.find(second)
    if first_at < 0 or second_at < 0 or first_at >= second_at:
        fail(f"{context} must place {first!r} before {second!r}")


def assert_workflow() -> None:
    text = read_required(WORKFLOW)
    context = str(WORKFLOW.relative_to(ROOT))
    require(text, "push:\n    branches: [master]", context)
    require(text, "workflow_dispatch:", context)

    jobs_section = text.split("\njobs:\n", 1)
    if len(jobs_section) != 2:
        fail(f"{context} must define a jobs mapping")
    jobs = re.findall(r"^  ([a-z][a-z0-9-]*):\s*$", jobs_section[1], re.MULTILINE)
    expected_jobs = ["server", "admin", "web", "production-contract", "mirror"]
    if jobs != expected_jobs:
        fail(f"{context} jobs must be exactly and in order: {expected_jobs}; got {jobs}")

    require(text, "needs: [server, admin, web, production-contract]", context)
    require(text, "fetch-depth: 0", context)
    require(text, "go-version: '1.25.x'", context)
    if text.count("node-version: '24'") != 2:
        fail(f"{context} must configure Node 24 for Admin and Web")
    if text.count("version: 11.21.0") != 2:
        fail(f"{context} must configure pnpm 11.21.0 for Admin and Web")

    for literal in (
        "go test ./...",
        "go build ./...",
        "go vet ./...",
        "pnpm install --frozen-lockfile",
        "pnpm lint",
        "pnpm check:type",
        "pnpm test:unit",
        "pnpm build",
        "pnpm typecheck",
        "bash deploy/tests/production-contract.sh",
        "${{ secrets.NAS_SSH_KEY }}",
        "${{ secrets.NAS_KNOWN_HOSTS }}",
        'export GIT_SSH_COMMAND="ssh -i ~/.ssh/nas_mirror_key -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes"',
        'git remote add nas "ssh://${NAS_USER}@${NAS_HOST}:${NAS_PORT}/volume1/docker/weavepress-git/WeavePress.git"',
        "git push --force nas HEAD:refs/heads/master",
    ):
        require(text, literal, context)

    pull = f"docker pull {PINNED_UBUNTU}"
    inspect = f"docker image inspect {PINNED_UBUNTU}"
    run_contract = "bash deploy/tests/production-contract.sh"
    require_before(text, pull, inspect, context)
    require_before(text, inspect, run_contract, context)
    reject(text, r"ubuntu:[0-9]", context)
    reject(text, r"docker run[^\n]*(?!.*--network none)", context)

    production_contract = read_required(PRODUCTION_CONTRACT)
    require(production_contract, "--pull never", str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    require(production_contract, "--network none", str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    require(production_contract, PINNED_UBUNTU, str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    reject(production_contract, r"ubuntu:[0-9]", str(PRODUCTION_CONTRACT.relative_to(ROOT)))


def assert_hook() -> None:
    text = read_required(HOOK)
    context = str(HOOK.relative_to(ROOT))
    for literal in (
        "set -Eeuo pipefail",
        'MASTER_REF="refs/heads/master"',
        'ENABLE_FILE="/volume1/docker/weavepress-git/.enable-auto-deploy"',
        'SECRETS_DIR="/volume1/docker/weavepress-git/.secrets"',
        'LOG_FILE="/volume1/docker/weavepress-git/post-receive.log"',
        "^0{40}$",
        'git ls-tree -r --name-only "$newrev"',
        'git diff --name-only "$oldrev" "$newrev"',
        "server/*|deploy/production/*)",
        "admin/*|web/*|deploy/Dockerfile.gateway|deploy/nginx.conf)",
        "WeavePress/WeavePressServer",
        "WeavePress/WeavePressGateway",
        'read_secret "$SECRETS_DIR/zdzq-hook-user"',
        'read_secret "$SECRETS_DIR/zdzq-hook-token"',
        "curl --noproxy '*'",
        '--data-urlencode "BRANCH=master"',
        '--data-urlencode "APP_SHA=${app_sha}"',
        '--data-urlencode "DEPLOY=true"',
    ):
        require(text, literal, context)
    require_before(text, 'if [[ ! -f "$ENABLE_FILE" ]]', 'trigger_job "$SERVER_JOB"', context)
    require_before(text, 'if [[ ! -f "$ENABLE_FILE" ]]', 'trigger_job "$GATEWAY_JOB"', context)
    require_before(text, 'server_changed=false', 'trigger_job "$SERVER_JOB"', context)
    require_before(text, 'gateway_changed=false', 'trigger_job "$GATEWAY_JOB"', context)
    if text.count('trigger_job "$SERVER_JOB" "$newrev"') != 1:
        fail(f"{context} must dispatch the Server job at most once per receive")
    if text.count('trigger_job "$GATEWAY_JOB" "$newrev"') != 1:
        fail(f"{context} must dispatch the Gateway job at most once per receive")
    reject(text, r"set\s+-x", context)
    reject(text, r"\.enable-auto-deploy.*(?:touch|mkdir|install|>)", context)
    reject(text, r"log .*\b(?:token|password|secret|user)=", context)


def assert_direct_pipeline(component: str, path: Path) -> None:
    text = read_required(path)
    context = str(path.relative_to(ROOT))
    for literal in (
        "string(name: 'BRANCH', defaultValue: 'master'",
        "string(name: 'APP_SHA', defaultValue: ''",
        "booleanParam(name: 'PUSH_ACR', defaultValue: false",
        "booleanParam(name: 'DEPLOY', defaultValue: false",
        "ssh://chenhua@192.168.31.240/volume1/docker/weavepress-git/WeavePress.git",
        "weavepress-tencent-prod-ssh",
        "aliyun-acr-zdzq",
        "StrictHostKeyChecking=yes",
        "merge-base --is-ancestor \"$APP_SHA\" origin/master",
        'test "$actual_sha" = "$APP_SHA"',
        "attempt=1",
        '[ "$attempt" -lt 3 ] || return 1',
        "docker logout registry.cn-hangzhou.aliyuncs.com",
        "DOCKER_CONFIG",
        "set +x",
        "printf '%s\\n%s\\n' \"$ACR_USER\" \"$ACR_PASSWORD\" | ssh",
        f'"$APP_SHA --component={component}"',
    ):
        require(text, literal, context)
    if text.count(SHA_RE) < 1:
        fail(f"{context} must validate a complete lowercase 40-character APP_SHA")
    require_before(text, "docker login", "docker push", context)
    require_before(text, "trap cleanup EXIT", "docker push", context)
    reject(text, r"StrictHostKeyChecking=accept-new", context)
    reject(text, r"docker\s+login[^\n]*--password(?:\s|=)(?!-stdin)", context)
    reject(text, r"(?:ACR_PASSWORD|GITHUB_TOKEN)\s*=\s*['\"][^$'\"]", context)

    if component == "server":
        builds = (
            "docker build -f source/server/deployments/Dockerfile --build-arg TARGET=api -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:$APP_SHA source/server",
            "docker build -f source/server/deployments/Dockerfile --build-arg TARGET=worker -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:$APP_SHA source/server",
            "docker build -f source/server/deployments/Dockerfile --build-arg TARGET=migrate -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate:$APP_SHA source/server",
        )
        for command in builds:
            require(text, command, context)
        if text.count("docker build -f source/server/deployments/Dockerfile") != 3:
            fail(f"{context} must build exactly API, Worker, and Migrate images")
        for gate in ("migration completion", "API healthy", "Server image SHA"):
            require(text, gate, context)
    else:
        require(
            text,
            "docker build -f source/deploy/Dockerfile.gateway -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-gateway:$APP_SHA source",
            context,
        )
        if text.count("docker build -f source/deploy/Dockerfile.gateway") != 1:
            fail(f"{context} must build exactly one Gateway image")
        for endpoint in ("https://wp.pdurl.cn/ready", "https://wp.pdurl.cn/"):
            require(text, endpoint, context)
        require(text, "when { expression { return params.DEPLOY == true } }", context)
        require(text, "Gateway image SHA", context)


def assert_sync_pipeline(component: str, path: Path) -> None:
    text = read_required(path)
    context = str(path.relative_to(ROOT))
    direct_job = (
        "WeavePress/WeavePressServer"
        if component == "server"
        else "WeavePress/WeavePressGateway"
    )
    for literal in (
        "string(name: 'BRANCH', defaultValue: 'master'",
        "booleanParam(name: 'DEPLOY_WHEN_UNCHANGED', defaultValue: false",
        "github-nas-mirror",
        "http://192.168.31.227:7890",
        "https://github.com/chenbb0128/WeavePress.git",
        "ssh://chenhua@192.168.31.240/volume1/docker/weavepress-git/WeavePress.git",
        "refs/heads/master",
        "StrictHostKeyChecking=yes",
        'git push --force-with-lease="refs/heads/master:$nas_sha" nas HEAD:refs/heads/master',
        "UNCHANGED=false",
        "UNCHANGED=true",
        direct_job,
        "if (!params.DEPLOY_WHEN_UNCHANGED)",
        "booleanParam(name: 'DEPLOY', value: true)",
    ):
        require(text, literal, context)
    require_before(text, "if [ \"$github_sha\" = \"$nas_sha\" ]", "git clone", context)
    require_before(
        text,
        'git push --force-with-lease="refs/heads/master:$nas_sha" nas HEAD:refs/heads/master',
        "UNCHANGED=false",
        context,
    )
    require_before(text, "UNCHANGED=true", "params.DEPLOY_WHEN_UNCHANGED", context)
    if text.count("build job:") != 1:
        fail(f"{context} must trigger the direct job only in the unchanged branch")
    reject(text, r"git push\s+--mirror", context)
    reject(text, r"git push\s+--force\s", context)
    reject(text, r"refs/heads/\$", context)
    reject(text, r"set\s+-x", context)
    reject(text, r"StrictHostKeyChecking=accept-new", context)


def assert_jobs() -> None:
    read_required(JOBS)
    expected = {
        "folder": "WeavePress",
        "jobs": [
            {
                "name": "WeavePressServer",
                "displayName": "后端 发布 NAS 代码镜像",
                "pipeline": "WeavePressServer.groovy",
            },
            {
                "name": "ServerSyncGitHubAndDeploy",
                "displayName": "后端 同步 GitHub 最新并发布",
                "pipeline": "ServerSyncGitHubAndDeploy.groovy",
            },
            {
                "name": "WeavePressGateway",
                "displayName": "前台与管理端 发布 NAS 代码镜像",
                "pipeline": "WeavePressGateway.groovy",
            },
            {
                "name": "GatewaySyncGitHubAndDeploy",
                "displayName": "前台与管理端 同步 GitHub 最新并发布",
                "pipeline": "GatewaySyncGitHubAndDeploy.groovy",
            },
        ],
    }
    try:
        actual = json.loads(JOBS.read_text(encoding="utf-8"))
    except json.JSONDecodeError as error:
        fail(f"{JOBS.relative_to(ROOT)} is invalid JSON: {error}")
    if actual != expected:
        fail(f"{JOBS.relative_to(ROOT)} does not match the exact Jenkins job catalog")


def assert_groovy_structure(path: Path) -> None:
    text = read_required(path)
    context = str(path.relative_to(ROOT))
    require(text, "pipeline {", context)
    require(text, "stages {", context)

    stack: list[tuple[str, int]] = []
    pairs = {"}": "{", ")": "(", "]": "["}
    opening = set(pairs.values())
    index = 0
    quote = ""
    while index < len(text):
        if quote:
            if text.startswith(quote, index):
                index += len(quote)
                quote = ""
                continue
            if len(quote) == 1 and text[index] == "\\":
                index += 2
                continue
            index += 1
            continue
        if text.startswith("//", index):
            newline = text.find("\n", index + 2)
            index = len(text) if newline < 0 else newline + 1
            continue
        if text.startswith("/*", index):
            closing = text.find("*/", index + 2)
            if closing < 0:
                fail(f"{context} has an unterminated block comment")
            index = closing + 2
            continue
        if text.startswith("'''", index) or text.startswith('\"\"\"', index):
            quote = text[index : index + 3]
            index += 3
            continue
        if text[index] in ("'", '\"'):
            quote = text[index]
            index += 1
            continue
        character = text[index]
        if character in opening:
            stack.append((character, index))
        elif character in pairs:
            if not stack or stack[-1][0] != pairs[character]:
                fail(f"{context} has an unbalanced {character!r} at byte {index}")
            stack.pop()
        index += 1
    if quote:
        fail(f"{context} has an unterminated quoted string")
    if stack:
        fail(f"{context} has an unclosed {stack[-1][0]!r} at byte {stack[-1][1]}")


def assert_no_placeholders_or_secret_literals() -> None:
    paths = [*DIRECT_PIPELINES.values(), *SYNC_PIPELINES.values(), HOOK]
    for path in paths:
        text = read_required(path)
        context = str(path.relative_to(ROOT))
        if path.suffix == ".groovy":
            assert_groovy_structure(path)
        reject(text, r"@@|\b(?:TBD|TODO|FIXME)\b", context)
        reject(text, r"(?:password|token|private[_ -]?key)\s*[:=]\s*['\"][^$'\"]+", context)


def main() -> int:
    assert_workflow()
    assert_hook()
    for component, path in DIRECT_PIPELINES.items():
        assert_direct_pipeline(component, path)
    for component, path in SYNC_PIPELINES.items():
        assert_sync_pipeline(component, path)
    assert_jobs()
    assert_no_placeholders_or_secret_literals()
    print("CI/CD structural contract passed.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except AssertionError as error:
        print(f"CI/CD contract failed: {error}", file=sys.stderr)
        raise SystemExit(1)
