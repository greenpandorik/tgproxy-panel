#!/usr/bin/env python3
"""Render static, crawlable endpoint tables from the generated OpenAPI policy."""
import html
import json
import re
import sys
from pathlib import Path

START = "<!-- API_ROUTES_START -->"
END = "<!-- API_ROUTES_END -->"
RESOURCES = {
    "nodes": "Серверы", "users": "Пользователи", "monitoring": "Мониторинг",
    "sites": "Сайты", "branding": "Оформление", "settings": "Настройки", "audit": "Аудит",
}
RU_LABELS = json.loads(Path(__file__).with_name("api_route_labels_ru.json").read_text())


def route_rows(doc, lang):
    if doc.get("openapi") != "3.0.3" or not doc.get("paths"):
        raise ValueError("missing OpenAPI 3.0.3 endpoint catalog")
    grouped = {}
    for path, item in sorted(doc["paths"].items()):
        for method, operation in sorted(item.items()):
            if method not in ("get", "post", "put", "patch", "delete", "head", "options"):
                continue
            scope = operation.get("x-required-scope", "")
            resource, _, action = scope.partition(":")
            if not path.startswith("/api/v1/") or resource not in RESOURCES or action not in ("read", "write"):
                raise ValueError(f"{method} {path}: missing or unknown automation scope")
            owner = ("Только владелец" if lang == "ru" else "Owner only") if operation.get("x-owner-only") else ""
            row = f'<tr><td><code>{method.upper()}</code></td><td><code>{html.escape(path)}</code></td><td><code>{html.escape(scope)}</code>'
            if owner:
                row += f'<br><span>{owner}</span>'
            summary = operation["summary"]
            if lang == "ru":
                summary = RU_LABELS.get(summary, summary)
            row += f'</td><td>{html.escape(summary)}</td></tr>'
            grouped.setdefault(resource, []).append(row)
    sections = []
    for resource in RESOURCES:
        if resource not in grouped:
            continue
        title = RESOURCES[resource] if lang == "ru" else resource.capitalize()
        headings = ("Метод", "Маршрут", "Разрешение", "Операция") if lang == "ru" else ("Method", "Endpoint", "Permission", "Operation")
        head = "".join(f"<th>{value}</th>" for value in headings)
        sections.append(f'<h3>{title}</h3><div class="table-scroll api-routes" role="region" aria-label="{title}" tabindex="0"><table><thead><tr>{head}</tr></thead><tbody>{"".join(grouped[resource])}</tbody></table></div>')
    return "\n".join(sections)


def replace_catalog(source, doc, lang):
    if source.count(START) != 1 or source.count(END) != 1:
        raise ValueError("expected exactly one API catalog insertion region")
    pattern = re.escape(START) + r".*?" + re.escape(END)
    replacement = START + "\n" + route_rows(doc, lang) + "\n" + END
    result, count = re.subn(pattern, lambda _: replacement, source, flags=re.S)
    if count != 1:
        raise ValueError("invalid API catalog marker order")
    return result


def render(root):
    doc = json.loads((root / "api/openapi.json").read_text())
    summaries = {
        operation["summary"]
        for item in doc["paths"].values()
        for method, operation in item.items()
        if method in ("get", "post", "put", "patch", "delete", "head", "options")
    }
    missing = summaries - RU_LABELS.keys()
    if missing:
        raise ValueError("missing Russian operation labels: " + ", ".join(sorted(missing)))
    for lang, rel in (("ru", "api/index.html"), ("en", "en/api/index.html")):
        path = root / rel
        path.write_text(replace_catalog(path.read_text(), doc, lang))


if __name__ == "__main__":
    try:
        render(Path(sys.argv[1]))
    except (OSError, ValueError, KeyError, IndexError) as exc:
        sys.exit(f"API documentation build failed: {exc}")
