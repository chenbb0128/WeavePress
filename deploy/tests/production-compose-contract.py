#!/usr/bin/env python3
import copy
import json
import sys
from pathlib import Path
from typing import Any


EXPECTED_SERVICES = {"migrate", "api", "worker", "gateway"}
INFRASTRUCTURE_IMAGES = {"mysql", "redis", "nginx"}


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
    if len(sys.argv) == 2:
        return require_valid(load_model(sys.argv[1]))
    if len(sys.argv) == 3 and sys.argv[1] == "--self-test":
        return run_self_test(load_model(sys.argv[2]))
    print(f"Usage: {sys.argv[0]} [--self-test] <compose.json>", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
