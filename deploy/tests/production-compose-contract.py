#!/usr/bin/env python3
import copy
import json
import sys
import tempfile
from pathlib import Path
from typing import Any


EXPECTED_SERVICES = {"migrate", "api", "worker", "gateway"}
INFRASTRUCTURE_IMAGES = {"mysql", "redis", "nginx"}


def key_boundary_before(line: str, start: int) -> bool:
    position = start - 1
    while position >= 0 and line[position].isspace():
        position -= 1
    return position < 0 or line[position] in "{,"


def colon_after(line: str, end: int) -> bool:
    while end < len(line) and line[end].isspace():
        end += 1
    return end < len(line) and line[end] == ":"


def quoted_scalar(line: str, start: int) -> tuple[str, int]:
    quote = line[start]
    value: list[str] = []
    position = start + 1
    while position < len(line):
        character = line[position]
        if quote == '"' and character == "\\" and position + 1 < len(line):
            value.append(line[position + 1])
            position += 2
            continue
        if character == quote:
            if quote == "'" and position + 1 < len(line) and line[position + 1] == "'":
                value.append("'")
                position += 2
                continue
            return "".join(value), position + 1
        value.append(character)
        position += 1
    return "".join(value), position


def line_has_extends_key(line: str) -> bool:
    position = 0
    while position < len(line):
        character = line[position]
        if character == "#" and (position == 0 or line[position - 1].isspace()):
            return False
        if character in "\"'":
            value, end = quoted_scalar(line, position)
            if value == "extends" and key_boundary_before(line, position) and colon_after(line, end):
                return True
            position = end
            continue
        if line.startswith("extends", position):
            end = position + len("extends")
            if key_boundary_before(line, position) and colon_after(line, end):
                return True
        position += 1
    return False


def validate_source(path: str) -> list[str]:
    errors: list[str] = []
    source = Path(path).read_text(encoding="utf-8")
    for line_number, line in enumerate(source.splitlines(), start=1):
        if line_has_extends_key(line):
            errors.append(f"active extends key at line {line_number}")
    return errors


def require_source_valid(path: str) -> int:
    errors = validate_source(path)
    for error in errors:
        print(f"Production Compose source contract violation: {error}", file=sys.stderr)
    return 1 if errors else 0


def run_source_self_test() -> int:
    rejected_fixtures = {
        "block-style": """services:
  api:
    extends:
      file: ./base.yaml
      service: base-api
""",
        "inline mapping": """services:
  api: { image: example/api:latest, extends: { file: ./base.yaml, service: base-api } }
""",
        "double-quoted key": """services:
  api:
    \"extends\": { file: ./base.yaml, service: base-api }
""",
        "single-quoted key": """services:
  api:
    'extends': { file: ./base.yaml, service: base-api }
""",
    }
    allowed_fixture = """services:
  api:
    # extends:
    image: example/api:latest
    environment:
      LABEL: \"extends:\"
      DETAIL: \"literal # extends:\"
      FLOW_TEXT: \"{ extends: ignored }\"
"""

    with tempfile.TemporaryDirectory() as directory:
        fixture_dir = Path(directory)
        for name, content in rejected_fixtures.items():
            fixture = fixture_dir / f"{name.replace(' ', '-')}.yaml"
            fixture.write_text(content, encoding="utf-8")
            if not validate_source(str(fixture)):
                print(f"Source Compose contract accepted fixture: {name}", file=sys.stderr)
                return 1
            print(f"Source Compose fixture rejected: {name}")

        allowed = fixture_dir / "comments-and-strings.yaml"
        allowed.write_text(allowed_fixture, encoding="utf-8")
        errors = validate_source(str(allowed))
        if errors:
            print("Source Compose contract rejected comments or string values.", file=sys.stderr)
            return 1
        print("Source Compose fixture accepted: comments and string values")
    return 0


def image_repository(image: object) -> str:
    if not isinstance(image, str):
        return ""
    reference = image.split("@", 1)[0]
    return reference.rsplit("/", 1)[-1].split(":", 1)[0].lower()


def validate_compose(model: object) -> list[str]:
    if not isinstance(model, dict) or not isinstance(model.get("services"), dict):
        return ["normalized Compose model must contain a services object"]

    services: dict[str, Any] = model["services"]
    errors: list[str] = []
    actual_services = set(services)
    missing = sorted(EXPECTED_SERVICES - actual_services)
    unexpected = sorted(actual_services - EXPECTED_SERVICES)
    if missing:
        errors.append(f"missing services: {', '.join(missing)}")
    if unexpected:
        errors.append(f"unexpected services: {', '.join(unexpected)}")

    for name, service in sorted(services.items()):
        if not isinstance(service, dict):
            errors.append(f"{name} must be a service object")
            continue
        if str(service.get("network_mode", "")).lower() == "host":
            errors.append(f"{name} uses network_mode=host")
        if service.get("ports"):
            errors.append(f"{name} publishes host ports")

        repository = image_repository(service.get("image"))
        if repository in INFRASTRUCTURE_IMAGES:
            errors.append(f"{name} uses infrastructure image {repository}")

    return errors


def load_model(path: str) -> object:
    if path == "-":
        return json.load(sys.stdin)
    with Path(path).open(encoding="utf-8") as stream:
        return json.load(stream)


def require_valid(model: object) -> int:
    errors = validate_compose(model)
    for error in errors:
        print(f"Production Compose contract violation: {error}", file=sys.stderr)
    return 1 if errors else 0


def run_self_test(baseline: object) -> int:
    baseline_errors = validate_compose(baseline)
    if baseline_errors:
        print("Compose self-test baseline is invalid.", file=sys.stderr)
        return require_valid(baseline)

    def add_db(model: dict[str, Any]) -> None:
        model["services"]["db"] = {"image": "mysql:8.4"}

    def add_cache(model: dict[str, Any]) -> None:
        model["services"]["cache"] = {"image": "redis:7-alpine"}

    def enable_host_mode(model: dict[str, Any]) -> None:
        model["services"]["api"]["network_mode"] = "host"

    def publish_gateway_port(model: dict[str, Any]) -> None:
        model["services"]["gateway"]["ports"] = [
            {"target": 80, "published": "8080", "protocol": "tcp", "mode": "ingress"}
        ]

    def replace_worker_with_mysql(model: dict[str, Any]) -> None:
        model["services"]["worker"]["image"] = "mysql:8.4"

    mutations = (
        ("extra db with mysql image", add_db),
        ("extra cache with redis image", add_cache),
        ("api host network mode", enable_host_mode),
        ("gateway published port", publish_gateway_port),
        ("worker replaced with mysql image", replace_worker_with_mysql),
    )
    for name, mutate in mutations:
        mutant = copy.deepcopy(baseline)
        mutate(mutant)
        if not validate_compose(mutant):
            print(f"Compose contract accepted mutation: {name}", file=sys.stderr)
            return 1
        print(f"Compose mutation rejected: {name}")
    return 0


def main() -> int:
    if len(sys.argv) == 2 and sys.argv[1] == "--source-self-test":
        return run_source_self_test()
    if len(sys.argv) == 3 and sys.argv[1] == "--source":
        return require_source_valid(sys.argv[2])
    if len(sys.argv) == 2:
        return require_valid(load_model(sys.argv[1]))
    if len(sys.argv) == 3 and sys.argv[1] == "--self-test":
        return run_self_test(load_model(sys.argv[2]))
    print(
        f"Usage: {sys.argv[0]} (--source <compose.yaml> | --source-self-test | "
        "[--self-test] <compose.json>)",
        file=sys.stderr,
    )
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
