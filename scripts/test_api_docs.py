"""Regression checks for the generated endpoint reference (stdlib only)."""
import copy
import unittest

from render_api_docs import route_rows, replace_catalog


class APIDocsTest(unittest.TestCase):
    def setUp(self):
        self.doc = {
            "openapi": "3.0.3",
            "paths": {
                "/api/v1/nodes/{id}": {
                    "get": {
                        "summary": "Get <node>",
                        "x-required-scope": "nodes:read",
                        "x-owner-only": False,
                    }
                }
            },
        }

    def test_renders_all_routes_and_escapes_html(self):
        rows = route_rows(self.doc, "en")
        self.assertIn("nodes:read", rows)
        self.assertIn("/api/v1/nodes/{id}", rows)
        self.assertIn("Get &lt;node&gt;", rows)
        self.assertNotIn("Get <node>", rows)

    def test_rejects_route_without_scope(self):
        del self.doc["paths"]["/api/v1/nodes/{id}"]["get"]["x-required-scope"]
        with self.assertRaises(ValueError):
            route_rows(self.doc, "en")

    def test_missing_or_duplicate_catalog_markers_fail(self):
        marker = "<!-- API_ROUTES_START --><!-- API_ROUTES_END -->"
        self.assertIn("nodes:read", replace_catalog(marker, self.doc, "ru"))
        for source in ("", marker + marker):
            with self.assertRaises(ValueError):
                replace_catalog(source, self.doc, "en")

    def test_owner_only_permission_is_visible(self):
        doc = copy.deepcopy(self.doc)
        doc["paths"]["/api/v1/nodes/{id}"]["get"]["x-owner-only"] = True
        self.assertIn("Owner only", route_rows(doc, "en"))
        self.assertIn("Только владелец", route_rows(doc, "ru"))

    def test_russian_operations_are_translated(self):
        self.doc["paths"]["/api/v1/nodes/{id}"]["get"]["summary"] = "Get node"
        self.assertNotIn("Get node", route_rows(self.doc, "ru"))
        self.assertIn("Get node", route_rows(self.doc, "en"))


if __name__ == "__main__":
    unittest.main()
