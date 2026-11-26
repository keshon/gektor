"""Export the checked-in gallery from GEKTOR's opt-in TUI captures."""
import html
import os
from pathlib import Path
import re
import subprocess
import tempfile

DIRECTORY = Path(__file__).resolve().parent
FRAMES = [
    ("Agent", [
        (f"{style}-{theme}", f"{style.title()} · {theme} · 120 × 30", theme)
        for theme in ("dark", "light") for style in ("commander", "modern")
    ] + [
        ("modern-narrow-dark", "Modern · dark · 60 × 22", "dark"),
        ("modern-narrow-light", "Modern · light · 60 × 22", "light"),
    ]),
    ("Controls and states", [
        ("modern-settings-dark", "Model connection · dark · 120 × 30", "dark"),
        ("modal-picker-light", "Model picker · light · 100 × 32", "light"),
        ("modern-queue", "Queued message · dark · 120 × 30", "dark"),
        ("modern-inspector-wide", "Agent inspector · dark · 120 × 30", "dark"),
        ("workflow-inspector-modern-borderless", "Workflow inspector · dark · 121 × 28", "dark"),
        ("modern-modal-dark", "About dialog · dark · 120 × 30", "dark"),
    ]),
]

CSI = re.compile(r"\x1b\[([0-?]*)([ -/]*)([@-~])")
BASE = "#000000 #cd0000 #00cd00 #cdcd00 #0000ee #cd00cd #00cdcd #e5e5e5 #7f7f7f #ff0000 #00ff00 #ffff00 #5c5cff #ff00ff #00ffff #ffffff".split()


def indexed_color(index):
    if index < 16:
        return BASE[index]
    if index >= 232:
        return "#{0:02x}{0:02x}{0:02x}".format(8 + (index - 232) * 10)
    levels = (0, 95, 135, 175, 215, 255)
    index -= 16
    return "#{:02x}{:02x}{:02x}".format(
        levels[index // 36], levels[index // 6 % 6], levels[index % 6]
    )


def render_ansi(frame, style_classes):
    styles = {}
    fragments = []
    reverse = False

    def append(chunk):
        if not chunk:
            return
        applied = styles.copy()
        if reverse:
            applied["color"] = styles.get("background", "var(--terminal-bg)")
            applied["background"] = styles.get("color", "var(--terminal-fg)")
        classes = "terminal-run"
        if applied:
            declarations = tuple(sorted(applied.items()))
            name = style_classes.setdefault(declarations, f"ansi-{len(style_classes) + 1}")
            classes += f" {name}"

        for part in re.split("(█+|\n)", chunk):
            if not part:
                continue
            if part.startswith("█"):
                fragments.append(f'<span class="{classes} terminal-solid">{part}</span>')
            elif part == "\n" or not applied:
                fragments.append(html.escape(part))
            else:
                fragments.append(f'<span class="{classes}">{html.escape(part)}</span>')

    previous = 0
    for match in CSI.finditer(frame):
        append(frame[previous:match.start()])
        previous = match.end()
        if match[3] != "m" or match[2]:
            raise ValueError(f"Unsupported terminal sequence {match[0]!r}")

        codes = iter(int(code or 0) for code in match[1].split(";"))
        for code in codes:
            if code == 0:
                styles.clear()
                reverse = False
            elif code in (38, 48):
                target = "color" if code == 38 else "background"
                mode = next(codes)
                if mode == 2:
                    styles[target] = "#{:02x}{:02x}{:02x}".format(*(next(codes) for _ in range(3)))
                elif mode == 5:
                    styles[target] = indexed_color(next(codes))
                else:
                    raise ValueError(f"Unsupported terminal color mode {mode}")
            elif 30 <= code <= 37 or 90 <= code <= 97:
                styles["color"] = indexed_color(code - (30 if code < 90 else 82))
            elif 40 <= code <= 47 or 100 <= code <= 107:
                styles["background"] = indexed_color(code - (40 if code < 100 else 92))
            elif code in (7, 27):
                reverse = code == 7
            elif code in (1, 2, 3, 4, 9):
                key, value = {
                    1: ("font-weight", "700"), 2: ("opacity", ".65"),
                    3: ("font-style", "italic"), 4: ("text-decoration", "underline"),
                    9: ("text-decoration", "line-through"),
                }[code]
                styles[key] = value
            elif code in (22, 23, 24, 29, 39, 49):
                keys = {
                    22: ("font-weight", "opacity"), 23: ("font-style",),
                    24: ("text-decoration",), 29: ("text-decoration",),
                    39: ("color",), 49: ("background",),
                }[code]
                for key in keys:
                    styles.pop(key, None)
            else:
                raise ValueError(f"Unsupported terminal style {code}")
    append(frame[previous:])
    rendered = "".join(fragments)
    if "\x1b" in rendered:
        raise ValueError("Unrecognized terminal escape")
    return rendered


def main():
    with tempfile.TemporaryDirectory() as temp:
        subprocess.run(
            ["go", "test", "./internal/tui", "./internal/tui/agent", "./internal/tui/workflow",
             "-run", "^(TestModernPreview|TestModalCleanupPreview|TestModernQueuePreview|"
             "TestModernInspectorPreview|TestWorkflowInspectorStyleMatrixKeepsTabsAndScrollingAligned)$",
             "-count=1"],
            cwd=DIRECTORY.parents[2],
            env={**os.environ, "GEKTOR_TUI_PREVIEW_DIR": temp},
            check=True,
        )
        style_classes = {}
        sections = []
        for heading, frames in FRAMES:
            cards = []
            for name, label, theme in frames:
                frame = (Path(temp) / f"{name}.ansi").read_text(encoding="utf-8")
                cards.append(
                    f'<figure><figcaption id="{name}">{label}</figcaption>'
                    f'<pre class="{theme}" tabindex="0" aria-labelledby="{name}">'
                    f'{render_ansi(frame, style_classes)}</pre></figure>'
                )
            sections.append(f"<section><h2>{heading}</h2>" + "\n".join(cards) + "</section>")
    page = """<!doctype html>
<html lang="en" class="gallery-standalone">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>GEKTOR TUI gallery</title>
<link rel="stylesheet" href="gallery.css">
<link rel="stylesheet" href="gallery-ansi.css">
<header><h1>GEKTOR TUI gallery</h1><p>Terminal captures from the GEKTOR preview tests.</p></header>
<main class="terminal-gallery">
""" + "\n".join(sections) + "\n</main>\n"
    rules = ["/* Generated by export-gallery.py. */"]
    for declarations, name in style_classes.items():
        properties = " ".join(f"{key}: {value};" for key, value in declarations)
        rules.append(f".{name} {{ {properties} }}")
    (DIRECTORY / "gallery-ansi.css").write_text("\n".join(rules) + "\n", encoding="utf-8", newline="\n")
    (DIRECTORY / "gallery.html").write_text(page, encoding="utf-8", newline="\n")


if __name__ == "__main__":
    main()
