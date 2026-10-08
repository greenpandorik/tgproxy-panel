#!/usr/bin/env python3
"""Validate the assembled public site without dependencies or network access."""
import json
import re
import sys
import xml.etree.ElementTree as ET
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urljoin, urlparse
from urllib.robotparser import RobotFileParser

ORIGIN = "https://tgproxypanel.com"


def schema_objects(value):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from schema_objects(child)
    elif isinstance(value, list):
        for child in value:
            yield from schema_objects(child)


class Page(HTMLParser):
    def __init__(self, source):
        super().__init__(convert_charrefs=True)
        self.links, self.meta, self.refs, self.ids = [], {}, [], set()
        self.h1, self.lang, self.title = 0, None, ""
        self.json_ld, self.script_type, self.script_text = [], None, ""
        self.in_title = False
        self.analytics = []
        self.feed(source)

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if tag == "html":
            self.lang = a.get("lang")
        if tag == "h1":
            self.h1 += 1
        if tag == "title":
            self.in_title = True
        if a.get("id"):
            self.ids.add(a["id"])
        if tag == "link":
            self.links.append(a)
        if tag == "meta":
            self.meta[a.get("name", a.get("property"))] = a.get("content", "")
        if tag == "script":
            self.script_type, self.script_text = a.get("type"), ""
            if "data-website-id" in a:
                self.analytics.append(a)
        for key in ("href", "src"):
            if a.get(key):
                self.refs.append(a[key])

    def handle_data(self, data):
        if self.in_title:
            self.title += data
        if self.script_type == "application/ld+json":
            self.script_text += data

    def handle_endtag(self, tag):
        if tag == "title":
            self.in_title = False
        if tag == "script":
            if self.script_type == "application/ld+json":
                self.json_ld.append(json.loads(self.script_text))
            self.script_type = None


def validate(root):
    errors, pages = [], {}

    def check(ok, message):
        if not ok:
            errors.append(message)

    def target(url):
        path = root / unquote(urlparse(url).path).lstrip("/")
        return path / "index.html" if path.is_dir() else path

    titles, descriptions = set(), set()
    for file in sorted(root.rglob("*.html")):
        rel = file.relative_to(root).as_posix()
        url = ORIGIN + "/" + (rel[:-10] if rel.endswith("index.html") else rel)
        try:
            source = file.read_text()
            p = Page(source)
        except (ValueError, OSError) as e:
            errors.append(f"{rel}: cannot parse HTML/JSON-LD: {e}")
            continue
        pages[url] = p
        check("greenpandorik.github.io" not in source, f"{rel}: obsolete public domain")
        check(p.lang in ("ru", "en"), f"{rel}: missing supported language")
        check(p.h1 == 1, f"{rel}: expected one h1")
        check(bool(p.title.strip()), f"{rel}: missing title")
        check(bool(p.meta.get("description")), f"{rel}: missing description")
        check("width=device-width" in p.meta.get("viewport", ""), f"{rel}: missing responsive viewport")
        if rel == "404.html":
            check("noindex" in p.meta.get("robots", ""), "404.html: must not be indexed")
            continue
        check(p.title not in titles, f"{rel}: duplicate title")
        titles.add(p.title)
        desc = p.meta.get("description")
        check(desc not in descriptions, f"{rel}: duplicate description")
        descriptions.add(desc)
        canonical = [a.get("href") for a in p.links if a.get("rel") == "canonical"]
        check(canonical == [url], f"{rel}: canonical must be {url}")
        alternates = {a.get("hreflang"): a.get("href") for a in p.links if a.get("rel") == "alternate"}
        check(set(alternates) == {"ru", "en", "x-default"}, f"{rel}: incomplete hreflang")
        check(alternates.get(p.lang) == url, f"{rel}: language does not reference itself")
        check(p.meta.get("og:url") == url, f"{rel}: wrong Open Graph URL")
        check(p.meta.get("og:image", "").startswith(ORIGIN + "/"), f"{rel}: wrong preview domain")
        check(bool(p.json_ld), f"{rel}: missing structured data")
        for graph in p.json_ld:
            check(graph.get("@context") == "https://schema.org", f"{rel}: invalid schema context")
            for item in schema_objects(graph):
                if "@id" in item:
                    check(item["@id"].startswith(ORIGIN + "/"), f"{rel}: noncanonical schema entity")
                if "url" in item:
                    check(item["url"].startswith(ORIGIN + "/"), f"{rel}: noncanonical schema URL")
                    p.refs.append(item["url"])
                if item.get("@type") in ("WebPage", "TechArticle"):
                    check(item.get("url") == url, f"{rel}: schema page URL disagrees with canonical")
                    check(item.get("inLanguage") == p.lang, f"{rel}: schema language disagrees with page")
                for key in ("image", "screenshot", "mainEntityOfPage", "item"):
                    if isinstance(item.get(key), str):
                        check(item[key].startswith(ORIGIN + "/"), f"{rel}: noncanonical schema {key}")
                        p.refs.append(item[key])
        check(bool(p.analytics), f"{rel}: missing analytics")
        for a in p.analytics:
            check("tgproxypanel.com" in a.get("data-domains", "").split(","), f"{rel}: analytics rejects production domain")

    for url, p in pages.items():
        for ref in p.refs + [p.meta.get("og:image", "")]:
            resolved = urljoin(url, ref)
            parsed = urlparse(resolved)
            if parsed.netloc != "tgproxypanel.com":
                continue
            check(target(resolved).is_file(), f"{url}: missing resource {ref}")
            if parsed.fragment and resolved.split("#")[0] in pages:
                check(unquote(parsed.fragment) in pages[resolved.split("#")[0]].ids, f"{url}: missing anchor {ref}")
        alternates = {a.get("hreflang"): a.get("href") for a in p.links if a.get("rel") == "alternate"}
        for lang in ("ru", "en"):
            other = pages.get(alternates.get(lang))
            if other:
                back = {a.get("hreflang"): a.get("href") for a in other.links if a.get("rel") == "alternate"}
                check(back.get(p.lang) == url, f"{url}: hreflang is not reciprocal")

    for file in root.rglob("*.css"):
        for ref in re.findall(r"url\(['\"]?([^)'\"]+)", file.read_text()):
            if not ref.startswith("data:"):
                check((file.parent / ref).is_file(), f"{file.name}: missing CSS resource {ref}")
    sitemap = root / "sitemap.xml"
    try:
        tree = ET.parse(sitemap)
        records = tree.findall("{http://www.sitemaps.org/schemas/sitemap/0.9}url")
        locations = [e.findtext("{http://www.sitemaps.org/schemas/sitemap/0.9}loc") for e in records]
        listed = set(locations)
        check(len(listed) == len(locations), "sitemap.xml: duplicate canonical page")
        expected = {u for u in pages if "noindex" not in pages[u].meta.get("robots", "")}
        check(listed == expected, "sitemap.xml: must list every indexable page exactly on the canonical domain")
        for e in records:
            url = e.findtext("{http://www.sitemaps.org/schemas/sitemap/0.9}loc")
            if url in pages:
                expected_alternates = {a.get("hreflang"): a.get("href") for a in pages[url].links if a.get("rel") == "alternate"}
                actual_alternates = {a.get("hreflang"): a.get("href") for a in e.findall("{http://www.w3.org/1999/xhtml}link")}
                check(actual_alternates == expected_alternates, f"sitemap.xml: wrong language alternates for {url}")
    except (OSError, ET.ParseError):
        errors.append("sitemap.xml: missing or invalid")
    robots = root / "robots.txt"
    if robots.is_file():
        text = robots.read_text()
        parser = RobotFileParser()
        parser.parse(text.splitlines())
        check(f"Sitemap: {ORIGIN}/sitemap.xml" in text, "robots.txt: missing sitemap")
        for bot in ("Googlebot", "bingbot", "OAI-SearchBot", "ChatGPT-User"):
            for url in pages:
                check(parser.can_fetch(bot, url), f"robots.txt: blocks {bot} on {url}")
    else:
        errors.append("robots.txt: missing")
    for name in ("llms.txt",):
        file = root / name
        check(file.is_file(), f"{name}: missing project/documentation index")
        if file.is_file():
            check("greenpandorik.github.io" not in file.read_text(), f"{name}: obsolete public domain")
            for ref in re.findall(r"\]\((https://tgproxypanel\.com/[^)]*)\)", file.read_text()):
                check(target(ref).is_file(), f"{name}: missing page {ref}")
    return errors, len(pages)


if __name__ == "__main__":
    errors, count = validate(Path(sys.argv[1] if len(sys.argv) > 1 else "_site"))
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        sys.exit(1)
    print(f"Site checks passed: {count} HTML pages, canonical URLs, languages, assets, sitemap and crawler access")
