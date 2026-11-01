#!/usr/bin/env python3
"""Build release binaries from vendor when present, otherwise the Go module cache. Run with Go 1.23+ on PATH."""
from pathlib import Path
import os
import subprocess

ROOT = Path(__file__).resolve().parent.parent
MOD = '-mod=vendor' if (ROOT/'vendor').is_dir() else '-mod=readonly'
TARGETS = [('linux','amd64',''), ('windows','amd64','.exe'), ('darwin','arm64','-macos-arm64')]
for system, arch, suffix in TARGETS:
    for app in ('game-server','mcp-tools'):
        source = '.' if app == 'game-server' else './cmd/'+app
        subprocess.run(['go','build',MOD,'-buildvcs=false','-trimpath','-ldflags=-s -w','-o',str(ROOT/'bin'/(app+suffix)),source],cwd=ROOT,env=dict(os.environ,CGO_ENABLED='0',GOOS=system,GOARCH=arch),check=True)
        print(system,arch,app,'built',flush=True)
