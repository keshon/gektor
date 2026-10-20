"""Build self-contained Windows/Linux amd64 distributions: python scripts/release.py v0.1.1."""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import zipfile


def main():
    if len(sys.argv) != 2 or not re.fullmatch(r"v\d+\.\d+\.\d+", sys.argv[1]):
        raise SystemExit("Usage: python scripts/release.py vMAJOR.MINOR.PATCH")
    version = sys.argv[1]
    root = Path(__file__).resolve().parent.parent
    os.chdir(root)
    out = root / "tmp" / f"release-{version}"
    out.mkdir(parents=True, exist_ok=True)
    # Explicit runtime kit. Never glob local files: .env can contain credentials.
    common = ["README.md", "LICENSE", "SECURITY.md", "ARCHITECTURE.md", ".env.example"]
    common += subprocess.check_output(
        ["git", "ls-files", "docs", "skills", "profiles", "eval/README.md"], text=True
    ).splitlines()
    windows = [
        "run-tui.cmd", "run-task.cmd", "run-plan.cmd", "run-resume.cmd",
        "run-server-llama.cmd", "run-server-kobold.cmd", "run-server-openai.cmd",
        "scripts/set-env.ps1", "scripts/ensure-build.cmd",
    ]
    for name in common + windows:
        if not (root / name).is_file():
            raise SystemExit(f"Missing release file: {name}")
    archives = []
    for platform in ("windows", "linux"):
        binary = "gektor.exe" if platform == "windows" else "gektor"
        env = dict(os.environ, GOOS=platform, GOARCH="amd64", CGO_ENABLED="0")
        subprocess.run(
            ["go", "build", "-trimpath", "-o", str(out / binary), "./cmd/gektor"],
            env=env, check=True,
        )
        files = common + (windows if platform == "windows" else [])
        suffix = ".zip" if platform == "windows" else ".tar.gz"
        archive = out / f"gektor-{version}-{platform}-amd64{suffix}"
        if platform == "windows":
            with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as bundle:
                bundle.write(out / binary, binary)
                for name in files:
                    bundle.write(root / name, name)
            with zipfile.ZipFile(archive) as bundle:
                assert bundle.testzip() is None
                assert set(files + [binary]) == set(bundle.namelist())
        else:
            with tarfile.open(archive, "w:gz") as bundle:
                info = bundle.gettarinfo(str(out / binary), arcname=binary)
                info.mode = 0o755
                with (out / binary).open("rb") as stream:
                    bundle.addfile(info, stream)
                for name in files:
                    bundle.add(root / name, arcname=name)
            with tarfile.open(archive) as bundle:
                assert bundle.getmember(binary).mode == 0o755
                assert set(files + [binary]) == set(bundle.getnames())
        archives.append(archive)
        print(archive)
    (out / "SHA256SUMS.txt").write_text("".join(
        f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n"
        for path in archives
    ), encoding="ascii")


if __name__ == "__main__":
    main()
