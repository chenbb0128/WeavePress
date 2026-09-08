#!/usr/bin/env python3
import copy
import json
import sys
import tempfile
from pathlib import Path
from typing import Any


EXPECTED_SERVICES = {"migrate", "api", "worker", "gateway"}
INFRASTRUCTURE_IMAGES = {"mysql", "redis", "nginx"}
APP_DSN = "weavepress_app:replace-with-generated-password@tcp(mysql:3306)/weavepress"
MIGRATION_DSN = (
    "weavepress_migrator:replace-with-generated-password@tcp(mysql:3306)/weavepress"
)
API_APP_ALIAS = "weavepress-api"
CRITICAL_SECRET_KEYS = {
    "WEAVEPRESS_REDIS_PASSWORD",
    "WEAVEPRESS_AUTH_JWT_SECRET",
    "WEAVEPRESS_AUTH_MEDIA_SIGNING_KEY",
}
EXPECTED_NETWORKS = {
    "api": {"app", "infra"},
    "worker": {"app", "infra"},
    "gateway": {"app"},
    "migrate": {"infra"},
}
EXPECTED_TMPFS = {
    "api": {"/tmp"},
    "worker": {"/tmp"},
    "gateway": {"/var/cache/nginx", "/var/run", "/tmp"},
    "migrate": {"/tmp"},
}
EXPECTED_RESOURCES = {
    "api": ("402653184", 0.75, 128),
    "worker": ("402653184", 0.50, 128),
    "gateway": ("134217728", 0.25, 64),
    "migrate": ("268435456", 0.50, 128),
}
EXPECTED_API_HEALTHCHECK = {
    "test": ["CMD", "/app/healthcheck", "-url", "http://127.0.0.1:8080/ready"],
    "interval": "10s",
    "timeout": "5s",
    "retries": 6,
    "start_period": "10s",
}


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


def mapping(value: object) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def string_set(value: object) -> set[str]:
    if not isinstance(value, list):
        return set()
    return {item for item in value if isinstance(item, str)}


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

    for name, expected_networks in EXPECTED_NETWORKS.items():
        service = services.get(name)
        if not isinstance(service, dict):
            continue
        actual_networks = set(mapping(service.get("networks")))
        if actual_networks != expected_networks:
            errors.append(
                f"{name} networks must be exactly {', '.join(sorted(expected_networks))}"
            )

    api = mapping(services.get("api"))
    api_app_network = mapping(mapping(api.get("networks")).get("app"))
    api_aliases = string_set(api_app_network.get("aliases"))
    if API_APP_ALIAS not in api_aliases:
        errors.append(f"api app network must include alias {API_APP_ALIAS}")

    runtime_environments: dict[str, dict[str, Any]] = {}
    for name in ("api", "worker"):
        environment = mapping(mapping(services.get(name)).get("environment"))
        runtime_environments[name] = environment
        if environment.get("WEAVEPRESS_DATABASE_DSN") != APP_DSN:
            errors.append(f"{name} must use the application database DSN")
        migration_keys = sorted(key for key in environment if "MIGRAT" in key.upper())
        if migration_keys:
            errors.append(f"{name} exposes migration environment keys: {', '.join(migration_keys)}")

    if (
        runtime_environments.get("api", {}).get("WEAVEPRESS_DATABASE_DSN")
        != runtime_environments.get("worker", {}).get("WEAVEPRESS_DATABASE_DSN")
    ):
        errors.append("api and worker database DSNs must match")

    migrate = mapping(services.get("migrate"))
    migrate_environment = mapping(migrate.get("environment"))
    if set(migrate_environment) != {"WEAVEPRESS_DATABASE_DSN"}:
        errors.append("migrate environment must contain only WEAVEPRESS_DATABASE_DSN")
    if migrate_environment.get("WEAVEPRESS_DATABASE_DSN") != MIGRATION_DSN:
        errors.append("migrate must use the migration database DSN")

    for secret_key in sorted(CRITICAL_SECRET_KEYS):
        owners = {
            name
            for name, service in services.items()
            if secret_key in mapping(mapping(service).get("environment"))
        }
        if owners != {"api", "worker"}:
            errors.append(f"{secret_key} must be present only in api and worker")

    migrate_volumes = migrate.get("volumes")
    if not isinstance(migrate_volumes, list) or len(migrate_volumes) != 1:
        errors.append("migrate must mount only the production config")
    else:
        config_volume = mapping(migrate_volumes[0])
        if (
            config_volume.get("type") != "bind"
            or config_volume.get("target") != "/app/configs/config.yaml"
            or config_volume.get("read_only") is not True
        ):
            errors.append("migrate config mount must be a read-only bind mount")
    if isinstance(migrate_volumes, list) and any(
        mapping(volume).get("target") == "/app/data" for volume in migrate_volumes
    ):
        errors.append("migrate must not mount application data")

    for name, expected_tmpfs in EXPECTED_TMPFS.items():
        service = services.get(name)
        if not isinstance(service, dict):
            continue
        if service.get("read_only") is not True:
            errors.append(f"{name} root filesystem must be read-only")
        if string_set(service.get("security_opt")) != {"no-new-privileges:true"}:
            errors.append(f"{name} must set only no-new-privileges:true")
        if string_set(service.get("tmpfs")) != expected_tmpfs:
            errors.append(f"{name} tmpfs mounts do not match the production plan")

        expected_memory, expected_cpus, expected_pids = EXPECTED_RESOURCES[name]
        if str(service.get("mem_limit")) != expected_memory:
            errors.append(f"{name} memory limit does not match the production plan")
        if service.get("cpus") != expected_cpus:
            errors.append(f"{name} CPU limit does not match the production plan")
        if service.get("pids_limit") != expected_pids:
            errors.append(f"{name} PID limit does not match the production plan")

    if mapping(api.get("healthcheck")) != EXPECTED_API_HEALTHCHECK:
        errors.append("api healthcheck does not match the production plan")

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


def validate_dev_compose(model: object) -> list[str]:
    if not isinstance(model, dict) or not isinstance(model.get("services"), dict):
        return ["normalized Compose model must contain a services object"]

    api = mapping(model["services"].get("api"))
    default_network = mapping(mapping(api.get("networks")).get("default"))
    if API_APP_ALIAS not in string_set(default_network.get("aliases")):
        return [f"api default network must include alias {API_APP_ALIAS}"]
    return []


def require_dev_valid(model: object) -> int:
    errors = validate_dev_compose(model)
    for error in errors:
        print(f"Development Compose contract violation: {error}", file=sys.stderr)
    return 1 if errors else 0


def run_dev_self_test(baseline: object) -> int:
    baseline_errors = validate_dev_compose(baseline)
    if baseline_errors:
        print("Development Compose self-test baseline is invalid.", file=sys.stderr)
        return require_dev_valid(baseline)

    mutant = copy.deepcopy(baseline)
    mutant["services"]["api"]["networks"]["default"]["aliases"] = []
    if not validate_dev_compose(mutant):
        print("Development Compose contract accepted missing api alias", file=sys.stderr)
        return 1
    print("Development Compose mutation rejected: unique api alias removed")
    return 0


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

    def remove_unique_api_alias(model: dict[str, Any]) -> None:
        model["services"]["api"]["networks"]["app"]["aliases"] = []

    def leak_secret_to_migrate(model: dict[str, Any]) -> None:
        model["services"]["migrate"]["environment"]["WEAVEPRESS_AUTH_JWT_SECRET"] = (
            "leaked-placeholder"
        )

    def mount_media_in_migrate(model: dict[str, Any]) -> None:
        model["services"]["migrate"]["volumes"].append(
            {"type": "bind", "source": "/tmp/media", "target": "/app/data"}
        )

    def use_application_dsn_for_migrate(model: dict[str, Any]) -> None:
        model["services"]["migrate"]["environment"]["WEAVEPRESS_DATABASE_DSN"] = APP_DSN

    def expose_migration_dsn_to_api(model: dict[str, Any]) -> None:
        model["services"]["api"]["environment"]["WEAVEPRESS_DB_MIGRATION_DSN"] = (
            MIGRATION_DSN
        )

    def move_migrate_to_app_network(model: dict[str, Any]) -> None:
        model["services"]["migrate"]["networks"]["app"] = None

    def disable_gateway_read_only(model: dict[str, Any]) -> None:
        model["services"]["gateway"]["read_only"] = False

    def remove_api_security_option(model: dict[str, Any]) -> None:
        model["services"]["api"]["security_opt"] = []

    def add_worker_tmpfs(model: dict[str, Any]) -> None:
        model["services"]["worker"]["tmpfs"].append("/run")

    def increase_api_memory(model: dict[str, Any]) -> None:
        model["services"]["api"]["mem_limit"] = "536870912"

    def break_api_healthcheck(model: dict[str, Any]) -> None:
        model["services"]["api"]["healthcheck"]["test"][-1] = (
            "http://127.0.0.1:8080/health"
        )

    mutations = (
        ("extra db with mysql image", add_db),
        ("extra cache with redis image", add_cache),
        ("api host network mode", enable_host_mode),
        ("gateway published port", publish_gateway_port),
        ("worker replaced with mysql image", replace_worker_with_mysql),
        ("unique api alias removed", remove_unique_api_alias),
        ("migrate receives JWT secret", leak_secret_to_migrate),
        ("migrate mounts application data", mount_media_in_migrate),
        ("migrate uses application DSN", use_application_dsn_for_migrate),
        ("api receives migration DSN", expose_migration_dsn_to_api),
        ("migrate joins app network", move_migrate_to_app_network),
        ("gateway read-only disabled", disable_gateway_read_only),
        ("api security option removed", remove_api_security_option),
        ("worker tmpfs expanded", add_worker_tmpfs),
        ("api memory limit changed", increase_api_memory),
        ("api healthcheck changed", break_api_healthcheck),
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
    if len(sys.argv) == 3 and sys.argv[1] == "--dev":
        return require_dev_valid(load_model(sys.argv[2]))
    if len(sys.argv) == 3 and sys.argv[1] == "--dev-self-test":
        return run_dev_self_test(load_model(sys.argv[2]))
    if len(sys.argv) == 2:
        return require_valid(load_model(sys.argv[1]))
    if len(sys.argv) == 3 and sys.argv[1] == "--self-test":
        return run_self_test(load_model(sys.argv[2]))
    print(
        f"Usage: {sys.argv[0]} (--source <compose.yaml> | --source-self-test | "
        "--dev <compose.json> | --dev-self-test <compose.json> | "
        "[--self-test] <compose.json>)",
        file=sys.stderr,
    )
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
