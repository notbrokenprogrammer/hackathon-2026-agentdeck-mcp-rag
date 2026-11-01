#!/usr/bin/env python3
"""Regression checks for the publication guard; no real credentials or Git writes."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location('publication', Path(__file__).with_name('check-publication.py'))
publication = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publication)


class PublicationTest(unittest.TestCase):
    def check(self, entries, identity=''):
        output = io.StringIO()
        command = subprocess.CompletedProcess([], 0, stdout=identity.encode(), stderr=b'')
        with patch.object(publication, 'files', return_value=iter(entries)), patch.object(publication, 'run', return_value=command), patch('sys.argv', ['check-publication.py']), contextlib.redirect_stdout(output):
            code = publication.main()
        return code, json.loads(output.getvalue())

    def test_source_and_example_addresses_are_allowed(self):
        code, result = self.check([('README.md', b'Example: student@example.com'), ('.env.example', b'HOST_ADDR=localhost:8080')])
        self.assertEqual(code, 0)
        self.assertEqual(result['files'], 2)

    def test_local_files_fail_even_if_forced_into_index(self):
        for name in ['data/account.json', 'internal/game/data/cache.json', '.env.local', '.ssh/config', 'FIZMA.zip', 'file.sqlite-wal', '.cache/report.json', 'session.har']:
            with self.subTest(name=name):
                self.assertEqual(self.check([(name, b'{}')])[0], 1)

    def test_tokens_paths_and_identity_are_redacted(self):
        token = 'gh' + 'p_' + 'A' * 36
        address = 'private.person' + '@mail.testhost'
        home_path = '/' + 'Users' + '/private-person/source'
        for value in [token, address, home_path]:
            with self.subTest(kind=value[:2]):
                code, result = self.check([('notes.txt', value.encode())], identity=address)
                self.assertEqual(code, 1)
                self.assertNotIn(value, json.dumps(result))

    def test_document_author_and_image_exif_are_flagged(self):
        data = io.BytesIO()
        with zipfile.ZipFile(data, 'w') as archive:
            archive.writestr('docProps/core.xml', '<props><creator>Private Author</creator></props>')
        self.assertEqual(self.check([('source.docx', data.getvalue())])[0], 1)
        self.assertEqual(self.check([('image.jpg', b'Exif\x00\x00metadata')])[0], 1)

    def test_empty_selection_and_symlinks_fail(self):
        self.assertEqual(self.check([])[0], 1)
        self.assertEqual(self.check([('linked-file', None)])[0], 1)

    def test_staged_mode_reads_blob_from_index(self):
        calls = []

        def fake_run(args, check=True):
            calls.append(args)
            body = b'100644 aabbcc 0\tREADME.md\x00' if args[1] == 'ls-files' else b'staged content'
            return subprocess.CompletedProcess(args, 0, stdout=body, stderr=b'')

        with patch.object(publication, 'run', side_effect=fake_run):
            self.assertEqual(list(publication.files(True)), [('README.md', b'staged content')])
        self.assertIn(['git', 'cat-file', 'blob', 'aabbcc'], calls)

    def test_conflicted_index_is_rejected(self):
        command = subprocess.CompletedProcess([], 0, stdout=b'100644 aabbcc 2\tREADME.md\x00', stderr=b'')
        with patch.object(publication, 'run', return_value=command), self.assertRaises(ValueError):
            list(publication.files(True))


if __name__ == '__main__':
    unittest.main()
