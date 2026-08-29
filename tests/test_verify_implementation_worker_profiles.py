from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from scripts.verify_implementation_worker_profiles import (
    REQUIRED_PROFILES,
    REQUIRED_TOOLSETS,
    verify_worker_profiles,
)


class VerifyImplementationWorkerProfilesTests(unittest.TestCase):
    def _profile(
        self,
        root: Path,
        name: str,
        *,
        tools: str,
        env: str = "",
        auto_source_bashrc: bool = False,
        shell_init_files: tuple[str, ...] | None = None,
    ) -> None:
        directory = root / name
        directory.mkdir(parents=True, exist_ok=True)
        if shell_init_files is None:
            shell_init_files = (str(directory / "shell" / "profile-env.sh"),)
        shell_init_yaml = "\n".join(f"    - {path}" for path in shell_init_files)
        (directory / "config.yaml").write_text(
            "model:\n"
            "  default: test\n"
            f"toolsets: {tools}\n"
            "terminal:\n"
            "  env_passthrough: []\n"
            "  shell_init_files:\n"
            f"{shell_init_yaml}\n"
            f"  auto_source_bashrc: {str(auto_source_bashrc).lower()}\n",
            encoding="utf-8",
        )
        (directory / ".env").write_text(env, encoding="utf-8")

    def _valid_profiles(self, root: Path) -> None:
        for name in REQUIRED_PROFILES:
            self._profile(
                root,
                name,
                tools=f"[{', '.join(sorted(REQUIRED_TOOLSETS[name]))}]",
                env=(
                    "GH_TOKEN=repository-scoped\nGITHUB_TOKEN=repository-scoped\n"
                    if name == "coding-worker"
                    else "# coordinator environment\n"
                ),
            )

    def test_audits_only_project_manager_and_coding_worker(self) -> None:
        self.assertEqual(("project-manager", "coding-worker"), REQUIRED_PROFILES)

    def test_accepts_role_capabilities_profile_startup_and_approved_env_names(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._valid_profiles(root)

            result = verify_worker_profiles(root)

        self.assertEqual(set(REQUIRED_PROFILES), set(result))

    def test_rejects_unapproved_capability_or_service_secret_without_its_value(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._valid_profiles(root)
            self._profile(
                root,
                "coding-worker",
                tools=f"[{', '.join(sorted(REQUIRED_TOOLSETS['coding-worker'] | {'kanban'}))}]",
                env="NOTION_API_KEY=do-not-print-this-value\n",
            )

            with self.assertRaises(RuntimeError) as raised:
                verify_worker_profiles(root)

        message = str(raised.exception)
        self.assertIn("profile isolation audit failed", message)
        self.assertIn("NOTION_API_KEY", message)
        self.assertNotIn("do-not-print-this-value", message)

    def test_rejects_host_shell_startup_inheritance(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._valid_profiles(root)
            self._profile(
                root,
                "project-manager",
                tools=f"[{', '.join(sorted(REQUIRED_TOOLSETS['project-manager']))}]",
                auto_source_bashrc=True,
            )

            with self.assertRaisesRegex(RuntimeError, "auto_source_bashrc"):
                verify_worker_profiles(root)

    def test_rejects_unapproved_shell_initialization(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._valid_profiles(root)
            self._profile(
                root,
                "coding-worker",
                tools=f"[{', '.join(sorted(REQUIRED_TOOLSETS['coding-worker']))}]",
                shell_init_files=("~/.zshrc",),
            )

            with self.assertRaisesRegex(RuntimeError, "shell_init_files"):
                verify_worker_profiles(root)

    def test_rejects_duplicate_contract_keys(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._valid_profiles(root)
            config = root / "coding-worker" / "config.yaml"
            config.write_text(
                config.read_text(encoding="utf-8")
                + "terminal:\n  auto_source_bashrc: true\n",
                encoding="utf-8",
            )

            with self.assertRaisesRegex(RuntimeError, "duplicate config keys"):
                verify_worker_profiles(root)

    def test_rejects_valueless_or_exported_unapproved_env_names(self) -> None:
        for declaration in ("NOTION_API_KEY\n", "export NOTION_API_KEY=value\n"):
            with self.subTest(declaration=declaration), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                self._valid_profiles(root)
                (root / "coding-worker" / ".env").write_text(
                    declaration, encoding="utf-8"
                )

                with self.assertRaisesRegex(RuntimeError, "NOTION_API_KEY"):
                    verify_worker_profiles(root)


if __name__ == "__main__":
    unittest.main()
