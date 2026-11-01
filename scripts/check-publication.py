#!/usr/bin/env python3
"""Check publishable files without printing suspected secret values.

With --staged, inspect the exact Git index, including unchanged tracked files.
Without Git, use ripgrep's ignore-aware file list. This is a focused preflight,
not a guarantee that every possible personal identifier or secret is detected.
"""
import argparse
import io
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import zipfile
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parent.parent
BLOCKED_DIRS = {
    '.git', '.cache', '.aws', '.ssh', '.gnupg', '.codex', '.agents', '.vscode',
    '.idea', 'data', '.preview-data', 'node_modules', '__pycache__', '.venv', 'bin',
    'LastCrew_Interior_References', 'LastCrew_NPC_References',
}
BLOCKED_NAMES = {
    '.DS_Store', '.netrc', '.npmrc', '.pypirc', 'credentials.json',
    'RELEASE_MANIFEST.json', 'lastcrew', 'game-server', 'mcp-tools',
}
BLOCKED_SUFFIXES = {'.zip', '.sqlite', '.db', '.pem', '.key', '.p12', '.pfx', '.log', '.har', '.exe'}
PATTERNS = {
    'private key': r'-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----',
    'GitHub token': r'\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{50,})\b',
    'AWS access key': r'\b(?:AKIA|ASIA)[A-Z0-9]{16}\b',
    'API token': r'\bsk-(?:proj-|ant-)?[A-Za-z0-9_-]{25,}',
    'Slack token': r'\bxox[baprs]-[A-Za-z0-9-]{20,}',
    'Google API key': r'\bAIza[A-Za-z0-9_-]{35}\b',
    'JWT': r'\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}',
    'personal home path': r'(?:/(?:Users|home)/[^/\s"<>]+|[A-Z]:\\Users\\[^\\\s"<>]+)',
}
EMAIL = re.compile(r'\b[A-Za-z0-9._%+-]+@([A-Za-z0-9.-]+\.[A-Za-z]{2,})\b')


def run(args, check=True):
    return subprocess.run(args, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=check)


def files(staged):
    if staged:
        for entry in run(['git', 'ls-files', '--stage', '-z']).stdout.split(b'\0'):
            if not entry:
                continue
            metadata, path = entry.split(b'\t', 1)
            mode, oid, stage = metadata.decode().split()
            if stage != '0':
                raise ValueError('Unresolved merge entries in index')
            name = path.decode()
            if mode not in {'100644', '100755'}:
                yield name, None
            else:
                yield name, run(['git', 'cat-file', 'blob', oid]).stdout
    else:
        for name in run(['rg', '--files', '--hidden', '--no-require-git', '-0']).stdout.split(b'\0'):
            if not name:
                continue
            path = ROOT / name.decode()
            yield name.decode(), None if path.is_symlink() else path.read_bytes()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--staged', action='store_true')
    args = parser.parse_args()
    findings = []
    personal = []
    # Use local identity only for comparison; never store it in the report.
    for key in ('user.name', 'user.email'):
        value = run(['git', 'config', '--get', key], check=False).stdout.decode().strip()
        if value and len(value) >= 6 and 'noreply.github.com' not in value:
            personal.append(value.casefold())
    count = total = 0

    def report(name, reason):
        findings.append({'file': name, 'reason': reason})

    def scan_text(name, data):
        text = data.decode('utf-8', errors='replace')
        for reason, pattern in PATTERNS.items():
            if re.search(pattern, text):
                report(name, reason)
        folded = text.casefold()
        if any(value in folded for value in personal):
            report(name, 'local Git identity')
        for match in EMAIL.finditer(text):
            domain = match.group(1).lower()
            if domain not in {'example.com', 'example.org', 'example.net'} and not domain.endswith(('.example', '.invalid', '.test', 'users.noreply.github.com')):
                report(name, 'email address requires review')

    for name, data in files(args.staged):
        count += 1
        path = PurePosixPath(name)
        if any(part in BLOCKED_DIRS for part in path.parts) or path.name in BLOCKED_NAMES or path.suffix in BLOCKED_SUFFIXES or (path.name.startswith('.env') and path.name != '.env.example') or re.search(r'\.(?:sqlite|db)-', path.name):
            report(name, 'local/private artifact included')
        if data is None:
            report(name, 'symlink or submodule requires review')
            continue
        total += len(data)
        if len(data) >= 100 * 1024 * 1024:
            report(name, 'file reaches GitHub size limit')
        if path.suffix == '.docx':
            with zipfile.ZipFile(io.BytesIO(data)) as archive:
                for member in archive.namelist():
                    if member.endswith('.xml'):
                        body = archive.read(member)
                        scan_text(name + ':' + member, body)
                        if member == 'docProps/core.xml':
                            for element in ET.fromstring(body).iter():
                                if element.tag.rsplit('}', 1)[-1] in {'creator', 'lastModifiedBy'} and (element.text or '').strip():
                                    report(name, 'document author metadata requires review')
        elif path.suffix in {'.png', '.jpg', '.jpeg', '.woff2'}:
            # Existing images also receive manual visual review before publication.
            if path.suffix != '.woff2' and (b'Exif\x00\x00' in data or b'eXIf' in data or b'http://ns.adobe.com/xap/' in data):
                report(name, 'image EXIF/XMP metadata requires review')
        elif b'\0' in data[:8192]:
            report(name, 'unexpected binary file requires review')
        else:
            scan_text(name, data)

    if not count:
        report('(file list)', 'no files selected')
    print(json.dumps({'passed': not findings, 'mode': 'Git index' if args.staged else 'working files',
                      'files': count, 'bytes': total, 'findings': findings}, indent=2))
    return 1 if findings else 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, subprocess.CalledProcessError, zipfile.BadZipFile) as error:
        print('Publication check could not complete: ' + type(error).__name__, file=sys.stderr)
        sys.exit(2)
