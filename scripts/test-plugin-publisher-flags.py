#!/usr/bin/env python3
"""Boundary and build-entrypoint checks; all client IDs here are fixtures."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('publisher', ROOT / 'scripts/plugin-publisher-flags.py')
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)


class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.config = {'schema_version': 1, 'github': {'owner': 'daoleno', 'client_id': 'fixture.github'}, 'slack': {'workspace_id': 'T123ABC', 'client_id': '123.456'}}

    def test_public_only_and_missing_registration(self):
        self.assertIn('GitHubPublicClientID=fixture.github', publisher.publisher_flags(self.config, ['github']))
        self.config['github']['client_id'] = None
        self.assertNotIn('GitHubPublicClientID', publisher.publisher_flags(self.config, []))
        with self.assertRaisesRegex(ValueError, 'cannot publish'):
            publisher.publisher_flags(self.config, ['github'])

    def test_reject_secrets_unknown_owner_and_linker_injection(self):
        for path, value in [(('github', 'client_secret'), 'secret'), (('github', 'owner'), 'paul-freeride'), (('slack', 'workspace_id'), None), (('github', 'client_id'), 'x -X main.Version=evil'), (('github', 'client_id'), '$(touch bad)'), (('github', 'client_id'), '')]:
            with self.subTest(path=path, value=value):
                config = copy.deepcopy(self.config)
                config[path[0]][path[1]] = value
                with self.assertRaises(ValueError):
                    publisher.publisher_flags(config, [])

    def test_every_official_daemon_build_embeds_same_public_ids(self):
        with tempfile.TemporaryDirectory(prefix='zen-publisher-build-') as directory:
            root = Path(directory)
            for name in ['scripts', 'release', 'daemon', 'app', 'fake-bin']:
                (root / name).mkdir()
            for name in ['plugin-publisher-flags.py', 'build-zen-local.sh', 'build-daemon-linux.sh']:
                shutil.copy(ROOT / 'scripts' / name, root / 'scripts' / name)
            (root / 'release/plugin-publishers.json').write_text(json.dumps(self.config))
            (root / 'app/app.base.json').write_text('{"expo":{"version":"0.0.0"}}')
            fake_go = root / 'fake-bin/go'
            fake_go.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
with open(os.environ['BUILD_CAPTURE'], 'a') as f:
    f.write(json.dumps({'args':sys.argv[1:], 'os':os.environ.get('GOOS'), 'arch':os.environ.get('GOARCH')})+'\\n')
pathlib.Path(sys.argv[sys.argv.index('-o')+1]).touch()
''')
            fake_go.chmod(0o755)
            capture = root / 'calls.jsonl'
            env = dict(os.environ, PATH=str(fake_go.parent) + os.pathsep + os.environ['PATH'], BUILD_CAPTURE=str(capture))
            subprocess.run(['bash', str(root / 'scripts/build-zen-local.sh')], env=env, check=True, capture_output=True)
            subprocess.run(['bash', str(root / 'scripts/build-daemon-linux.sh')], env=env, check=True, capture_output=True)
            calls = [json.loads(line) for line in capture.read_text().splitlines()]
            self.assertEqual(len(calls), 4)
            self.assertEqual([(c['os'], c['arch']) for c in calls[1:]], [('linux','amd64'), ('linux','arm64'), ('darwin','arm64')])
            for call in calls:
                flags = next(a for a in call['args'] if a.startswith('-ldflags='))
                self.assertIn('connections.GitHubPublicClientID=fixture.github', flags)
                self.assertIn('connections.SlackPublicClientID=123.456', flags)


if __name__ == '__main__':
    unittest.main()
