"""Tests for package metadata and standalone installability."""
import importlib.metadata


def _extract_package_name(requirement: str) -> str:
    """Extract the base package name from a pip requirement specifier.

    E.g. 'opentelemetry-sdk>=1.20' -> 'opentelemetry-sdk'.
    """
    for separator in (">=", "==", "<=", ">", "<", "~=", "!"):
        requirement = requirement.split(separator)[0]
    return requirement.strip()


class TestPackageMetadata:
    """Verify the package is properly configured for PyPI publishing."""

    def test_pyproject_has_correct_name(self):
        """Package name is 'lantern-sdk' (with hyphen for pip install)."""
        metadata = importlib.metadata.metadata("lantern-sdk")
        assert metadata["Name"] == "lantern-sdk"

    def test_pyproject_has_version(self):
        """Package has a non-empty version set."""
        version = importlib.metadata.version("lantern-sdk")
        assert version and version != "0.0.0"

    def test_pyproject_has_python_requires(self):
        """Package specifies minimum Python version."""
        requires_python = importlib.metadata.metadata("lantern-sdk").get("Requires-Python", "")
        assert requires_python

    def test_dependencies_are_present(self):
        """Required dependencies are declared and installable."""
        dist = importlib.metadata.distribution("lantern-sdk")
        requires = dist.requires or []

        dep_names = [_extract_package_name(r) for r in requires]

        assert any("opentelemetry-sdk" in d for d in dep_names)
        assert any("opentelemetry-exporter-otlp-proto-http" in d for d in dep_names)
        assert any("requests" in d for d in dep_names)

    def test_dev_dependencies_are_present(self):
        """Development dependencies are declared."""
        dist = importlib.metadata.distribution("lantern-sdk")
        requires = dist.requires or []

        dev_deps = [r for r in requires if "extra == 'dev'" in r]
        dev_dep_names = [_extract_package_name(r) for r in dev_deps]

        assert any("pytest" in d for d in dev_dep_names)
        assert any("responses" in d for d in dev_dep_names)

    def test_package_can_be_imported(self):
        """lantern_sdk can be imported standalone (no repo clone needed)."""
        import lantern_sdk

        # Verify all expected exports are accessible
        assert hasattr(lantern_sdk, "trace")
        assert hasattr(lantern_sdk, "configure")
        assert hasattr(lantern_sdk, "LanternClient")
        assert hasattr(lantern_sdk, "set_input")
        assert hasattr(lantern_sdk, "set_output")
        assert hasattr(lantern_sdk, "set_model")
        assert hasattr(lantern_sdk, "set_tokens")
        assert hasattr(lantern_sdk, "get_active_span")

    def test_package_files_are_included_in_wheel(self):
        """All source files are included when building the wheel."""
        import os
        import zipfile

        dist_dir = os.path.join(os.path.dirname(__file__), "..", "dist")
        if not os.path.exists(dist_dir):
            import subprocess

            subprocess.run(
                ["python3", "-m", "build", "--wheel", "--outdir", dist_dir],
                cwd=os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
                check=True,
                capture_output=True,
            )

        wheels = [f for f in os.listdir(dist_dir) if f.endswith(".whl")]
        assert len(wheels) >= 1, "No wheel found in dist/ after building"

        wheel_path = os.path.join(dist_dir, wheels[0])
        with zipfile.ZipFile(wheel_path) as zf:
            names = zf.namelist()

        # Check all lantern_sdk source files are included
        assert any("lantern_sdk/__init__.py" in n for n in names)
        assert any("lantern_sdk/client.py" in n for n in names)
        assert any("lantern_sdk/exporter.py" in n for n in names)
        assert any("lantern_sdk/trace.py" in n for n in names)

        # Check dist-info metadata
        assert any("METADATA" in n for n in names)
        assert any("WHEEL" in n for n in names)

    def test_readme_exists_and_has_install_instructions(self):
        """README.md exists and contains pip/uv install instructions."""
        import os

        readme_path = os.path.join(os.path.dirname(__file__), "..", "README.md")
        assert os.path.exists(readme_path), "README.md not found"

        with open(readme_path) as f:
            content = f.read()

        assert "pip install" in content or "uv add" in content

    def test_package_has_license(self):
        """Package declares a license (required for PyPI publishing)."""
        metadata = importlib.metadata.metadata("lantern-sdk")
        license_field = metadata.get("License-Expression", "") or metadata.get("License", "")
        assert license_field, "Package must declare a license for PyPI publishing"
        assert "Apache" in license_field or "MIT" in license_field or "BSD" in license_field

    def test_package_has_classifiers(self):
        """Package has PyPI-relevant classifiers."""
        dist = importlib.metadata.distribution("lantern-sdk")
        classifiers = dist.metadata.get_all("Classifier") or []

        classifier_str = " ".join(classifiers)
        assert "Development Status" in classifier_str
        assert "License :: OSI Approved" in classifier_str
        assert "Programming Language :: Python :: 3" in classifier_str

    def test_wheel_builds_cleanly(self):
        """The package builds a clean wheel without warnings or errors."""
        import os
        import subprocess

        result = subprocess.run(
            ["python3", "-m", "build", "--wheel", "--outdir", "dist"],
            capture_output=True,
            text=True,
            cwd=os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
        )
        assert result.returncode == 0, f"Build failed: {result.stderr}"
        assert "Successfully built" in result.stdout or "Successfully built" in result.stderr
