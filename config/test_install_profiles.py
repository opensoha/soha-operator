"""Offline schema/image pairing checks; requires kubectl, never contacts a cluster."""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import unittest


CONFIG = Path(__file__).resolve().parent
CRD = "workloads.soha.io_workloadcronjobs.yaml"
RELEASE_SHA256 = "982de6cc22ee4dba0b6e59d6cabd8863b63db211c02d30e6bfb4a29b7e10469c"


class InstallProfileTests(unittest.TestCase):
    def render(self, profile):
        return subprocess.check_output(
            ["kubectl", "kustomize", str(CONFIG / profile)], text=True
        )

    def assert_field(self, text, name):
        self.assertRegex(text, rf"(?m)^ +{name}:")

    def assert_no_field(self, text, name):
        self.assertNotRegex(text, rf"(?m)^ +{name}:")

    def test_release_crd_is_exact_v0_1_1_artifact(self):
        released = (CONFIG / "release/v0.1.1" / CRD).read_bytes()
        self.assertEqual(hashlib.sha256(released).hexdigest(), RELEASE_SHA256)

    def test_default_pairs_release_schema_with_release_image(self):
        kustomization = (CONFIG / "default/kustomization.yaml").read_text(encoding="utf-8")
        releases = re.findall(r"(?m)^[ \t]*-[ \t]+\.\./release/(v[0-9]+\.[0-9]+\.[0-9]+)[ \t]*$", kustomization)
        self.assertEqual(len(releases), 1, "default must select exactly one release CRD")
        tag = releases[0]
        if os.environ.get("GITHUB_REF_TYPE") == "tag":
            self.assertEqual(tag, os.environ["GITHUB_REF_NAME"], "default CRD must match the release tag")

        rendered = self.render("default")
        self.assertIn(f"image: ghcr.io/opensoha/soha-operator:{tag[1:]}", [line.strip() for line in rendered.splitlines()])
        released_crd = self.render(f"release/{tag}").strip()
        self.assertTrue(
            released_crd in [document.strip() for document in rendered.split("\n---\n")],
            f"default must include the CRD from release/{tag}",
        )
        self.assert_field(rendered, "sourceRef")

    def test_development_preserves_current_schema_with_local_image(self):
        rendered = self.render("development")
        self.assertIn("image: ghcr.io/opensoha/soha-operator:local", rendered)
        self.assertNotIn("image: ghcr.io/opensoha/soha-operator:0.1.1", rendered)
        self.assert_field(rendered, "schedulingGroup")
        self.assert_field(rendered, "defaultUser")
        self.assert_field(rendered, "sourceRef")
        self.assert_no_field(rendered, "workloadRef")


if __name__ == "__main__":
    unittest.main()
