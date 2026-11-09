import tempfile
import subprocess
import re
import posixpath
import unittest
from pathlib import Path
from release import release_bytes, runtime_files


class DocumentationPackaging(unittest.TestCase):
    def test_current_articles_keep_all_remaining_local_links_inside_the_kit(self):
        root = Path(__file__).resolve().parent.parent
        tracked = subprocess.check_output(
            ['git', 'ls-files', '--cached', '--others', '--exclude-standard'],
            cwd=root, text=True).splitlines()
        included = set(runtime_files(tracked))
        self.assertIn('docs/start.md', included)
        self.assertFalse(any(p.startswith(('docs/internal/', 'docs/design/')) for p in included))
        for name in included:
            if not name.endswith('.md'):
                continue
            data = release_bytes(root, name, included, 'test-revision')
            if name.startswith('skills/'):
                self.assertEqual(data, (root / name).read_bytes())
            for href in re.findall(r'\]\(([^\s)]+)\)', data.decode('utf-8')):
                if re.match(r'[a-zA-Z][a-zA-Z0-9+.-]*:|//', href):
                    continue
                location = href.partition('#')[0]
                target = posixpath.normpath(posixpath.join(posixpath.dirname(name), location)) if location else name
                self.assertIn(target, included, f'{name}: {href}')

    def test_selection_excludes_work_records_assets_and_local_secrets(self):
        self.assertEqual(runtime_files([
            'docs/start.md', 'docs/internal/plan.md', 'docs/design/tui/mockup.fig',
            'docs/design/tui/README.md', '.env', 'skills/browser/SKILL.md',
            'docs/reference/cli.md', 'docs/images/gektor-demo.webp',
        ]), ['docs/images/gektor-demo.webp', 'docs/reference/cli.md',
             'docs/start.md', 'skills/browser/SKILL.md'])

    def test_offline_links_and_frozen_skills(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'docs').mkdir()
            (root / 'docs/start.md').write_text('[Local](reference/cli.md#flags) [Source](../cmd/gektor/options.go)')
            text = release_bytes(root, 'docs/start.md', {'docs/reference/cli.md'}, 'abc123').decode()
            self.assertIn('](reference/cli.md#flags)', text)
            self.assertIn('https://github.com/keshon/gektor/blob/abc123/cmd/gektor/options.go', text)
            (root / 'skills').mkdir()
            (root / 'skills/SKILL.md').write_bytes(b'[Frozen](../missing.md)\r\n')
            self.assertEqual(release_bytes(root, 'skills/SKILL.md', set(), 'abc123'), b'[Frozen](../missing.md)\r\n')


if __name__ == '__main__':
    unittest.main()
