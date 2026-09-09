#!/usr/bin/env python3
"""Offline structural contract for the WeavePress CI/CD release chain."""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/ci-and-mirror.yml"
HOOK = ROOT / "deploy/nas/post-receive"
HOOK_BEHAVIOR = ROOT / "deploy/tests/post-receive-contract.sh"
IMAGE_RELEASE_BEHAVIOR = ROOT / "deploy/tests/image-release-contract.sh"
COMPONENT_CURRENT_BEHAVIOR = ROOT / "deploy/tests/component-current-contract.sh"
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
IMMUTABLE_PUBLISH = JENKINS / "publish-immutable-image.sh"
GATEWAY_VERIFY = JENKINS / "verify-gateway-release.sh"
COMPONENT_CURRENT = JENKINS / "assert-component-current.sh"
COMPONENT_CHECKOUT = JENKINS / "checkout-component-source.sh"
ACR_POLICY_VERIFY = JENKINS / "verify-acr-immutable-policy.sh"
DECLARATIVE_LINTER = JENKINS / "validate-declarative-pipelines.sh"
PENDING_DISPATCHER = ROOT / "deploy/nas/dispatch-pending"
PENDING_INSTALLER = ROOT / "deploy/nas/install-dispatch-pending"
GATEWAY_DOCKERFILE = ROOT / "deploy/Dockerfile.gateway"
GATEWAY_CONFIG = ROOT / "deploy/nginx.conf"
SERVER_DOCKERFILE = ROOT / "server/deployments/Dockerfile"
PRODUCTION_CONTRACT = ROOT / "deploy/tests/production-contract.sh"
PINNED_UBUNTU = (
    "ubuntu@sha256:"
    "0e0a0fc6d18feda9db1590da249ac93e8d5abfea8f4c3c0c849ce512b5ef8982"
)
PINNED_NGINX = "nginx@sha256:65645c7bb6a0661892a8b03b89d0743208a18dd2f3f17a54ef4b76fb8e2f2a10"
PINNED_GIT_FLOCK = "golang@sha256:e401dae1bf814e29204a8cb7915682e1780951e609ca0dd8865ee1937f510c48"
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


def indented_section(text: str, heading: str, indent: int) -> str:
    marker = " " * indent + heading
    start = text.find(marker)
    if start < 0:
        fail(f"missing section: {heading}")
    following = text[start + len(marker) :]
    boundary = re.search(rf"^ {{{indent}}}\S", following, re.MULTILINE)
    end = len(text) if boundary is None else start + len(marker) + boundary.start()
    return text[start:end]


def uncommented_lines(text: str, comment_prefix: str) -> list[str]:
    return [
        line
        for line in text.splitlines()
        if not line.lstrip().startswith(comment_prefix)
    ]


def assert_workflow_active_commands(text: str) -> None:
    server = indented_section(text, "server:", 2)
    active = "\n".join(uncommented_lines(server, "#"))
    if not re.search(r"^\s*run:\s+go test \./\.\.\.\s*$", active, re.MULTILINE):
        fail("server job must actively run go test ./...")


def assert_hook_active_dispatch(text: str) -> None:
    active = "\n".join(uncommented_lines(text, "#"))
    for component in ("server", "gateway"):
        pattern = rf"^\s*queue_component {component}\s*$"
        if len(re.findall(pattern, active, re.MULTILINE)) != 1:
            fail(f"NAS hook must actively queue {component} exactly once")


def assert_gateway_deploy_guards(text: str) -> None:
    active = "\n".join(uncommented_lines(text, "//"))
    for stage_name in (
        "stage('发布并确认 Gateway image SHA') {",
        "stage('验证两层 Nginx 公网入口') {",
    ):
        stage = indented_section(active, stage_name, 4)
        if not re.search(
            r"^\s*when\s*\{\s*expression\s*\{\s*return params\.DEPLOY == true\s*\}\s*\}\s*$",
            stage,
            re.MULTILINE,
        ):
            fail(f"{stage_name} must have an active DEPLOY=true guard")


def assert_comment_bypass_mutations() -> None:
    workflow = read_required(WORKFLOW)
    workflow_mutant = workflow.replace(
        "        run: go test ./...",
        "        run: true\n        # run: go test ./...",
        1,
    )
    try:
        assert_workflow_active_commands(workflow_mutant)
    except AssertionError:
        pass
    else:
        fail("workflow contract accepted go test preserved only in a comment")

    hook = read_required(HOOK)
    hook_mutant = re.sub(
        r"(?m)^(\s*queue_component (?:server|gateway)\s*)$",
        r'# \1',
        hook,
    )
    try:
        assert_hook_active_dispatch(hook_mutant)
    except AssertionError:
        pass
    else:
        fail("hook contract accepted dispatches preserved only in comments")

    gateway = read_required(DIRECT_PIPELINES["gateway"])
    public_stage = indented_section(
        gateway,
        "stage('验证两层 Nginx 公网入口') {",
        4,
    )
    gateway_mutant = gateway.replace(
        public_stage,
        public_stage.replace(
            "      when { expression { return params.DEPLOY == true } }",
            "      // when { expression { return params.DEPLOY == true } }",
            1,
        ),
        1,
    )
    try:
        assert_gateway_deploy_guards(gateway_mutant)
    except AssertionError:
        pass
    else:
        fail("Gateway contract accepted a public verification stage without an active DEPLOY guard")
    print("Comment-bypass mutations rejected for Go tests, hook dispatch, and Gateway deploy guard.")


def bash_binary() -> str:
    configured = os.environ.get("BASH_BIN")
    if configured:
        return configured
    if os.name == "nt":
        git_binary = shutil.which("git")
        if git_binary:
            bundled_bash = Path(git_binary).resolve().parents[1] / "bin/bash.exe"
            if bundled_bash.is_file():
                return str(bundled_bash)
    discovered = shutil.which("bash")
    if not discovered:
        fail("bash is required to validate the NAS hook")
    return discovered


def run_bash_n(path: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [bash_binary(), "-n", str(path)],
        check=False,
        capture_output=True,
        text=True,
    )


def assert_hook_bash_syntax() -> None:
    result = run_bash_n(HOOK)
    if result.returncode != 0:
        fail(f"bash -n rejected {HOOK.relative_to(ROOT)}: {result.stderr.strip()}")

    with tempfile.TemporaryDirectory(prefix="weavepress-hook-syntax-") as temp_dir:
        mutant = Path(temp_dir) / "post-receive"
        mutant.write_text(
            read_required(HOOK) + "\nif true; then\n",
            encoding="utf-8",
            newline="\n",
        )
        mutation_result = run_bash_n(mutant)
        if mutation_result.returncode == 0:
            fail("bash -n accepted a NAS hook mutation with an unterminated if block")
    print("Mutation rejected: NAS hook unterminated if block.")


def assert_hook_behavior() -> None:
    if not HOOK_BEHAVIOR.is_file():
        fail(f"missing NAS hook behavior contract: {HOOK_BEHAVIOR.relative_to(ROOT)}")
    result = subprocess.run(
        [bash_binary(), str(HOOK_BEHAVIOR)],
        cwd=ROOT,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        fail(f"NAS hook behavior contract failed: {detail}")
    print(result.stdout.strip())


def assert_image_release_behavior() -> None:
    if not IMAGE_RELEASE_BEHAVIOR.is_file():
        fail(f"missing image release behavior contract: {IMAGE_RELEASE_BEHAVIOR.relative_to(ROOT)}")
    result = subprocess.run(
        [bash_binary(), str(IMAGE_RELEASE_BEHAVIOR)],
        cwd=ROOT,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        fail(f"image release behavior contract failed: {detail}")
    print(result.stdout.strip())


def assert_component_current_behavior() -> None:
    if not COMPONENT_CURRENT_BEHAVIOR.is_file():
        fail(f"missing component freshness behavior contract: {COMPONENT_CURRENT_BEHAVIOR.relative_to(ROOT)}")
    result = subprocess.run(
        [bash_binary(), str(COMPONENT_CURRENT_BEHAVIOR)],
        cwd=ROOT,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        fail(f"component freshness behavior contract failed: {detail}")
    print(result.stdout.strip())


def git_binary() -> str:
    configured = os.environ.get("GIT_BIN")
    if configured:
        return configured
    discovered = shutil.which("git")
    if not discovered:
        fail("git is required to validate NUL-delimited changed paths")
    return discovered


def run_git(
    arguments: list[str],
    *,
    git_dir: Path | None = None,
    input_bytes: bytes | None = None,
    environment: dict[str, str] | None = None,
    strip_output: bool = True,
) -> bytes:
    command = [git_binary()]
    if git_dir is not None:
        command.extend(["--git-dir", str(git_dir)])
    command.extend(arguments)
    result = subprocess.run(
        command,
        input=input_bytes,
        check=False,
        capture_output=True,
        env=environment,
    )
    if result.returncode != 0:
        fail(
            f"git {' '.join(arguments)} failed during pathname self-test: "
            f"{result.stderr.decode('utf-8', errors='replace').strip()}"
        )
    return result.stdout.strip() if strip_output else result.stdout


def make_tree(git_dir: Path, entries: list[tuple[bytes, bytes, bytes, bytes]]) -> bytes:
    payload = b"".join(
        mode + b" " + object_type + b" " + object_id + b"\t" + name + b"\0"
        for mode, object_type, object_id, name in entries
    )
    return run_git(["mktree", "-z"], git_dir=git_dir, input_bytes=payload)


def assert_git_pathname_semantics() -> None:
    with tempfile.TemporaryDirectory(prefix="weavepress-git-paths-") as temp_dir:
        git_dir = Path(temp_dir) / "objects.git"
        run_git(["init", "--bare", str(git_dir)])
        blob = run_git(
            ["hash-object", "-w", "--stdin"],
            git_dir=git_dir,
            input_bytes=b"same content for rename detection\n",
        )
        old_name = b"leaving\nsource.txt"
        new_name = b"arriving\tdestination.txt"
        old_server = make_tree(git_dir, [(b"100644", b"blob", blob, old_name)])
        new_admin = make_tree(git_dir, [(b"100644", b"blob", blob, new_name)])
        old_root = make_tree(git_dir, [(b"040000", b"tree", old_server, b"server")])
        new_root = make_tree(git_dir, [(b"040000", b"tree", new_admin, b"admin")])
        git_environment = os.environ.copy()
        git_environment.update(
            {
                "GIT_AUTHOR_NAME": "CI contract",
                "GIT_AUTHOR_EMAIL": "ci-contract@example.invalid",
                "GIT_COMMITTER_NAME": "CI contract",
                "GIT_COMMITTER_EMAIL": "ci-contract@example.invalid",
            }
        )
        old_commit = run_git(
            ["commit-tree", old_root.decode("ascii"), "-m", "old"],
            git_dir=git_dir,
            environment=git_environment,
        )
        new_commit = run_git(
            [
                "commit-tree",
                new_root.decode("ascii"),
                "-p",
                old_commit.decode("ascii"),
                "-m",
                "new",
            ],
            git_dir=git_dir,
            environment=git_environment,
        )
        changed = run_git(
            [
                "diff",
                "--no-renames",
                "--name-only",
                "-z",
                old_commit.decode("ascii"),
                new_commit.decode("ascii"),
                "--",
            ],
            git_dir=git_dir,
            strip_output=False,
        ).split(b"\0")
        changed_paths = {path for path in changed if path}
        expected_changed = {b"server/" + old_name, b"admin/" + new_name}
        if changed_paths != expected_changed:
            fail("--no-renames -z did not preserve both special rename pathnames")

        initial = run_git(
            [
                "ls-tree",
                "-r",
                "-z",
                "--name-only",
                new_commit.decode("ascii"),
                "--",
            ],
            git_dir=git_dir,
            strip_output=False,
        ).split(b"\0")
        if {path for path in initial if path} != {b"admin/" + new_name}:
            fail("ls-tree -z did not preserve the initial special pathname")
    print("Git pathname self-test passed: rename source/target and NUL framing preserved.")


def assert_workflow() -> None:
    text = read_required(WORKFLOW)
    context = str(WORKFLOW.relative_to(ROOT))
    try:
        workflow_model = yaml.load(text, Loader=yaml.BaseLoader)
    except yaml.YAMLError as error:
        fail(f"{context} is invalid YAML: {error}")
    if not isinstance(workflow_model, dict) or not isinstance(workflow_model.get("jobs"), dict):
        fail(f"{context} must parse as a workflow mapping with jobs")
    assert_workflow_active_commands(text)
    require(text, "push:\n    branches: [master]", context)
    require(text, "workflow_dispatch:", context)
    require(text, "concurrency:\n  group: weavepress-ci-and-mirror-${{ github.ref }}\n  cancel-in-progress: true", context)

    jobs_section = text.split("\njobs:\n", 1)
    if len(jobs_section) != 2:
        fail(f"{context} must define a jobs mapping")
    jobs = re.findall(r"^  ([a-z][a-z0-9-]*):\s*$", jobs_section[1], re.MULTILINE)
    expected_jobs = ["server", "admin", "web", "production-contract", "mirror"]
    if jobs != expected_jobs:
        fail(f"{context} jobs must be exactly and in order: {expected_jobs}; got {jobs}")

    require(text, "needs: [server, admin, web, production-contract]", context)
    require(text, "if: github.ref == 'refs/heads/master'", context)
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
        "test \"$GITHUB_REF\" = 'refs/heads/master'",
        'test "$head_sha" = "$GITHUB_SHA"',
        "git ls-remote origin refs/heads/master",
        'test "$origin_sha" = "$GITHUB_SHA"',
        "git ls-remote nas refs/heads/master",
        'git push --force-with-lease="refs/heads/master:$nas_sha" nas HEAD:refs/heads/master',
    ):
        require(text, literal, context)

    pull = f"docker pull {PINNED_UBUNTU}"
    inspect = f"docker image inspect {PINNED_UBUNTU}"
    run_contract = "bash deploy/tests/production-contract.sh"
    require_before(text, pull, inspect, context)
    require_before(text, inspect, run_contract, context)
    nginx_pull = f"docker pull {PINNED_NGINX}"
    nginx_inspect = f"docker image inspect {PINNED_NGINX}"
    require_before(text, nginx_pull, nginx_inspect, context)
    require_before(text, nginx_inspect, run_contract, context)
    flock_pull = f"docker pull {PINNED_GIT_FLOCK}"
    flock_inspect = f"docker image inspect {PINNED_GIT_FLOCK}"
    require_before(text, flock_pull, flock_inspect, context)
    require_before(text, flock_inspect, run_contract, context)
    reject(text, r"ubuntu:[0-9]", context)
    reject(text, r"golang:[0-9]", context)
    reject(text, r"docker run[^\n]*(?!.*--network none)", context)

    production_contract = read_required(PRODUCTION_CONTRACT)
    require(production_contract, "--pull never", str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    require(production_contract, "--network none", str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    require(production_contract, PINNED_UBUNTU, str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    require(production_contract, PINNED_NGINX, str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    require(production_contract, PINNED_GIT_FLOCK, str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    require(production_contract, "--require-real-flock", str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    require(production_contract, '"$docker_repo_root:/repo:ro"', str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    reject(production_contract, r"ubuntu:[0-9]", str(PRODUCTION_CONTRACT.relative_to(ROOT)))
    reject(production_contract, r"golang:[0-9]", str(PRODUCTION_CONTRACT.relative_to(ROOT)))


def assert_hook() -> None:
    text = read_required(HOOK)
    context = str(HOOK.relative_to(ROOT))
    assert_hook_active_dispatch(text)
    for literal in (
        "set -Eeuo pipefail",
        'MASTER_REF="refs/heads/master"',
        'STATE_DIR="/volume1/docker/weavepress-git/release-state"',
        'DISPATCHER="/volume1/docker/weavepress-git/bin/dispatch-pending"',
        "JENKINS_REQUEST_MAX_SECONDS=20",
        "LOCK_WAIT_SECONDS=180",
        "HEARTBEAT_MAX_AGE_SECONDS=180",
        'ENABLE_FILE="/volume1/docker/weavepress-git/.enable-auto-deploy"',
        'SECRETS_DIR="/volume1/docker/weavepress-git/.secrets"',
        'LOG_FILE="/volume1/docker/weavepress-git/post-receive.log"',
        'LOCK_FILE="/volume1/docker/weavepress-git/post-receive.lock"',
        'exec 9>>"$LOCK_FILE"',
        'flock -w "$LOCK_WAIT_SECONDS" 9',
        'git rev-parse --verify "$MASTER_REF"',
        'git ls-tree -r -z --name-only "$target" --',
        'git diff --no-renames --name-only -z "$baseline" "$target" --',
        'component_has_changes "$component" "$baseline" "$live_master"',
        'write_state_sha "$STATE_FILE" "$target"',
        "component baseline changed before compare-and-swap",
        "dispatcher.heartbeat",
        "while IFS= read -r -d '' path",
        "server:server/*|server:deploy/production/*|server:deploy/jenkins/assert-component-current.sh|server:deploy/jenkins/checkout-component-source.sh|server:deploy/jenkins/publish-immutable-image.sh|server:deploy/jenkins/verify-acr-immutable-policy.sh)",
        "gateway:admin/*|gateway:web/*|gateway:.dockerignore|gateway:deploy/Dockerfile.gateway|gateway:deploy/Dockerfile.gateway.dockerignore|gateway:deploy/nginx.conf|gateway:deploy/jenkins/assert-component-current.sh|gateway:deploy/jenkins/checkout-component-source.sh|gateway:deploy/jenkins/publish-immutable-image.sh|gateway:deploy/jenkins/verify-acr-immutable-policy.sh|gateway:deploy/jenkins/verify-gateway-release.sh)",
        "WeavePress/WeavePressServer",
        "WeavePress/WeavePressGateway",
        'read_secret "$SECRETS_DIR/zdzq-hook-user"',
        'read_secret "$SECRETS_DIR/zdzq-hook-token"',
        'chmod 0600 "$curl_config" "$curl_headers"',
        'curl --config "$curl_config"',
        "--max-redirs 0",
        "--connect-timeout 5",
        '--max-time "$JENKINS_REQUEST_MAX_SECONDS"',
        "--write-out '%{http_code}'",
        '[[ "$http_code" != 201 ]]',
        'response Location',
        '--data-urlencode "BRANCH=master"',
        '--data-urlencode "APP_SHA=${app_sha}"',
        '--data-urlencode "DEPLOY=true"',
        '[[ "$curl_status" -eq 7 ]]',
        "automatic retry disabled",
        'queue_component server',
        'queue_component gateway',
        '"$DISPATCHER" --hook',
        "--dispatch-pending",
    ):
        require(text, literal, context)
    require_before(text, 'if [[ ! -f "$ENABLE_FILE" ]]', 'queue_component server', context)
    require_before(text, 'if [[ ! -f "$ENABLE_FILE" ]]', 'queue_component gateway', context)
    if text.count("queue_component server") != 1 or text.count("queue_component gateway") != 1:
        fail(f"{context} must queue each component once per receive")
    reject(text, r"last-processed-master", context)
    reject(text, r"refs/weavepress/", context)
    reject(text, r"trigger_job_with_retry", context)
    reject(text, r"set\s+-x", context)
    reject(text, r"curl[^\n]*--user", context)
    reject(text, r"curl[^\n]*--location", context)
    reject(text, r"changed_paths=\"\$\(git\s+(?:ls-tree|diff)", context)
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
        "yy-edusystem-tencent-prod-ssh",
        "aliyun-acr-zdzq",
        "StrictHostKeyChecking=yes",
        "git clone --no-checkout --branch master --single-branch --no-tags",
        "git -C source show 'refs/remotes/origin/master:deploy/jenkins/checkout-component-source.sh'",
        "bash checkout-component-source.sh source",
        "bash component-current.sh source",
        'test "$actual_sha" = "$APP_SHA"',
        "docker logout registry.cn-hangzhou.aliyuncs.com",
        "DOCKER_CONFIG",
        "bash source/deploy/jenkins/publish-immutable-image.sh",
        "sha256:[0-9a-f]{64}",
        "set +x",
        f'"$APP_SHA --component={component}"',
        "verify-acr-immutable-policy.sh",
    ):
        require(text, literal, context)
    if text.count(SHA_RE) < 1:
        fail(f"{context} must validate a complete lowercase 40-character APP_SHA")
    require_before(text, "docker login", "publish-immutable-image.sh", context)
    require_before(text, "verify-acr-immutable-policy.sh", "docker login", context)
    require_before(text, "trap cleanup EXIT", "publish-immutable-image.sh", context)
    reject(text, r"StrictHostKeyChecking=accept-new", context)
    reject(text, r"test -f source/deploy/jenkins/assert-component-current\.sh", context)
    reject(text, r"docker\s+login[^\n]*--password(?:\s|=)(?!-stdin)", context)
    reject(text, r"(?:ACR_PASSWORD|GITHUB_TOKEN)\s*=\s*['\"][^$'\"]", context)

    if component == "server":
        require(text, 'bash checkout-component-source.sh source server "$APP_SHA" component-current.sh', context)
        require(text, 'bash component-current.sh source server "$APP_SHA"', context)
        builds = (
            "docker build -f source/server/deployments/Dockerfile --build-arg APP_SHA=$APP_SHA --build-arg TARGET=api -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-api:$APP_SHA source/server",
            "docker build -f source/server/deployments/Dockerfile --build-arg APP_SHA=$APP_SHA --build-arg TARGET=worker -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-worker:$APP_SHA source/server",
            "docker build -f source/server/deployments/Dockerfile --build-arg APP_SHA=$APP_SHA --build-arg TARGET=migrate -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-migrate:$APP_SHA source/server",
        )
        for command in builds:
            require(text, command, context)
        if text.count("docker build -f source/server/deployments/Dockerfile") != 3:
            fail(f"{context} must build exactly API, Worker, and Migrate images")
        for gate in ("migration completion", "API healthy", "Server image SHA"):
            require(text, gate, context)
        require(text, "printf '%s\\n%s\\n%s\\n%s\\n%s\\n'", context)
        for digest_file in ("api.digest", "worker.digest", "migrate.digest"):
            require(text, digest_file, context)
    else:
        require(text, 'bash checkout-component-source.sh source gateway "$APP_SHA" component-current.sh', context)
        require(text, 'bash component-current.sh source gateway "$APP_SHA"', context)
        require(
            text,
            "docker build -f source/deploy/Dockerfile.gateway --build-arg APP_SHA=$APP_SHA -t registry.cn-hangzhou.aliyuncs.com/zdzq/weavepress-gateway:$APP_SHA source",
            context,
        )
        if text.count("docker build -f source/deploy/Dockerfile.gateway") != 1:
            fail(f"{context} must build exactly one Gateway image")
        for endpoint in ("https://wp.pdurl.cn/ready", "https://wp.pdurl.cn/"):
            require(text, endpoint, context)
        require(text, "when { expression { return params.DEPLOY == true } }", context)
        require(text, "Gateway image SHA", context)
        require(text, 'source/deploy/jenkins/verify-gateway-release.sh "$APP_SHA"', context)
        require(text, "printf '%s\\n%s\\n%s\\n'", context)
        require(text, "gateway.digest", context)
        assert_gateway_deploy_guards(text)


def assert_image_release_contract() -> None:
    publish = read_required(IMMUTABLE_PUBLISH)
    context = str(IMMUTABLE_PUBLISH.relative_to(ROOT))
    for literal in (
        "docker manifest inspect",
        "manifest unknown|no such manifest",
        "org.opencontainers.image.revision",
        "docker pull",
        "docker push",
        "timeout 900 docker push",
        "RepoDigests",
        "sha256:[0-9a-f]{64}",
    ):
        require(publish, literal, context)
    require_before(publish, "docker manifest inspect", "docker push", context)
    reject(publish, r"manifest unknown\|no such manifest\|not found", context)

    server_dockerfile = read_required(SERVER_DOCKERFILE)
    require(server_dockerfile, "ARG APP_SHA", str(SERVER_DOCKERFILE.relative_to(ROOT)))
    require(server_dockerfile, "org.opencontainers.image.revision=$APP_SHA", str(SERVER_DOCKERFILE.relative_to(ROOT)))
    require(server_dockerfile, "gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab", str(SERVER_DOCKERFILE.relative_to(ROOT)))

    gateway_dockerfile = read_required(GATEWAY_DOCKERFILE)
    gateway_context = str(GATEWAY_DOCKERFILE.relative_to(ROOT))
    for literal in ("ARG APP_SHA", "^[0-9a-f]{40}$", "org.opencontainers.image.revision=$APP_SHA", "__WEAVEPRESS_APP_SHA__"):
        require(gateway_dockerfile, literal, gateway_context)
    require(gateway_dockerfile, "docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32", gateway_context)
    require(gateway_dockerfile, "node:24-alpine@sha256:e67514e5d0f6c46656005e1b693b2ec9d52e80b641307de684d4a015ba7a4eaf", gateway_context)
    gateway_config = read_required(GATEWAY_CONFIG)
    require(gateway_config, "X-WeavePress-Release", str(GATEWAY_CONFIG.relative_to(ROOT)))
    require(gateway_config, "__WEAVEPRESS_APP_SHA__", str(GATEWAY_CONFIG.relative_to(ROOT)))

    verify = read_required(GATEWAY_VERIFY)
    verify_context = str(GATEWAY_VERIFY.relative_to(ROOT))
    for literal in ("--max-redirs 0", "--connect-timeout 5", "--max-time 20", "--write-out '%{http_code}'", '[[ "$http_code" == 200 ]]', "X-WeavePress-Release"):
        require(verify, literal, verify_context)
    reject(verify, r"curl[^\n]*--location", verify_context)

    freshness = read_required(COMPONENT_CURRENT)
    freshness_context = str(COMPONENT_CURRENT.relative_to(ROOT))
    for literal in (
        'git -C "$repository" fetch --no-tags origin',
        'merge-base --is-ancestor "$app_sha" "$master_sha"',
        'git -C "$repository" diff --no-renames --name-only -z "$app_sha" "$master_sha" --',
        "server:server/*|server:deploy/production/*|server:deploy/jenkins/assert-component-current.sh|server:deploy/jenkins/checkout-component-source.sh|server:deploy/jenkins/publish-immutable-image.sh|server:deploy/jenkins/verify-acr-immutable-policy.sh)",
        "gateway:admin/*|gateway:web/*|gateway:.dockerignore|gateway:deploy/Dockerfile.gateway|gateway:deploy/Dockerfile.gateway.dockerignore|gateway:deploy/nginx.conf|gateway:deploy/jenkins/assert-component-current.sh|gateway:deploy/jenkins/checkout-component-source.sh|gateway:deploy/jenkins/publish-immutable-image.sh|gateway:deploy/jenkins/verify-acr-immutable-policy.sh|gateway:deploy/jenkins/verify-gateway-release.sh)",
    ):
        require(freshness, literal, freshness_context)


def assert_sync_pipeline(component: str, path: Path) -> None:
    text = read_required(path)
    context = str(path.relative_to(ROOT))
    direct_job = (
        "/WeavePress/WeavePressServer"
        if component == "server"
        else "/WeavePress/WeavePressGateway"
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
        "git push nas HEAD:refs/heads/master",
        "UNCHANGED=false",
        "UNCHANGED=true",
        direct_job,
        "if (!params.DEPLOY_WHEN_UNCHANGED)",
        "wait: true, propagate: true",
        "booleanParam(name: 'DEPLOY', value: true)",
    ):
        require(text, literal, context)
    require_before(text, "if [ \"$github_sha\" = \"$nas_sha\" ]", "git clone", context)
    require_before(
        text,
        "git push nas HEAD:refs/heads/master",
        "UNCHANGED=false",
        context,
    )
    require_before(text, "UNCHANGED=true", "params.DEPLOY_WHEN_UNCHANGED", context)
    if text.count("build job:") != 1:
        fail(f"{context} must trigger the direct job only in the unchanged branch")
    reject(text, r"git push\s+--mirror", context)
    reject(text, r"git push[^\n]*(?:--force|--force-with-lease)", context)
    reject(text, r"git push[^\n]*\|\|\s*true", context)
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
    paths = [
        *DIRECT_PIPELINES.values(),
        *SYNC_PIPELINES.values(),
        HOOK,
        IMMUTABLE_PUBLISH,
        GATEWAY_VERIFY,
        COMPONENT_CURRENT,
        COMPONENT_CHECKOUT,
        ACR_POLICY_VERIFY,
        DECLARATIVE_LINTER,
        PENDING_DISPATCHER,
        PENDING_INSTALLER,
    ]
    for path in paths:
        text = read_required(path)
        context = str(path.relative_to(ROOT))
        if path.suffix == ".groovy":
            assert_groovy_structure(path)
        elif path.suffix == ".sh" or path in (HOOK, PENDING_DISPATCHER, PENDING_INSTALLER):
            result = run_bash_n(path)
            if result.returncode != 0:
                fail(f"bash -n rejected {context}: {result.stderr.strip()}")
        reject(text, r"@@|\b(?:TBD|TODO|FIXME)\b", context)
        reject(text, r"(?:password|token|private[_ -]?key)\s*[:=]\s*['\"][^$'\"]+", context)


def main() -> int:
    assert_workflow()
    assert_hook_bash_syntax()
    assert_git_pathname_semantics()
    assert_hook_behavior()
    assert_image_release_behavior()
    assert_component_current_behavior()
    assert_hook()
    for component, path in DIRECT_PIPELINES.items():
        assert_direct_pipeline(component, path)
    for component, path in SYNC_PIPELINES.items():
        assert_sync_pipeline(component, path)
    assert_image_release_contract()
    assert_jobs()
    assert_no_placeholders_or_secret_literals()
    assert_comment_bypass_mutations()
    print("CI/CD structural contract passed.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except AssertionError as error:
        print(f"CI/CD contract failed: {error}", file=sys.stderr)
        raise SystemExit(1)
