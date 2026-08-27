from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from scripts.verify_implementation_worker_profiles import (
    REQUIRED_PROFILES,
    verify_worker_profiles,
)


class VerifyImplementationWorkerProfilesTests(unittest.TestCase):
    def _profile(self, root: Path, name: str, *, tools: str, env: str = "") -> None:
        directory = root / name
        directory.mkdir(parents=True, exist_ok=True)
        (directory / "config.yaml").write_text(
            f"model:\n  default: test\ntoolsets: {tools}\n", encoding="utf-8"
        )
        (directory / ".env").write_text(env, encoding="utf-8")

    def test_accepts_only_minimal_toolsets_and_empty_envs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in REQUIRED_PROFILES:
                self._profile(root, name, tools="[terminal, file, skills]", env="# empty\n")

            result = verify_worker_profiles(root)

        self.assertEqual(set(REQUIRED_PROFILES), set(result))

    def test_rejects_extra_capability_or_service_secret(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in REQUIRED_PROFILES:
                self._profile(root, name, tools="[terminal, file, skills]")
            self._profile(
                root,
                REQUIRED_PROFILES[0],
                tools="[terminal, file, skills, browser]",
                env="PARTIFUL_TOKEN=secret\n",
            )

            with self.assertRaisesRegex(RuntimeError, "profile isolation audit failed"):
                verify_worker_profiles(root)


if __name__ == "__main__":
    unittest.main()
