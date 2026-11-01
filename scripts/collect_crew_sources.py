#!/usr/bin/env python3
"""Fetch only the reviewed source URLs and extract HTML/PDF into a local cache.

Optional preparation tool; the game itself uses only the shipped JSONL files.
Install: python -m pip install -r scripts/rag-requirements.txt
Example: python scripts/collect_crew_sources.py --cache ../crew-source-cache
Then review changed documents and run build_crew_corpus.py. A fetched article
never becomes approved game knowledge automatically. Raw cache may contain
copyrighted originals; only reviewed summaries/permitted excerpts are shipped.
"""

import argparse
import concurrent.futures
import hashlib
import json
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / "knowledge/sources/crew/documents.json"
MAX_DOWNLOAD = 24 * 1024 * 1024
SOURCE_HOSTS = {
    "nasa.gov", "www.nasa.gov", "ntrs.nasa.gov", "www.gctc.ru", "gctc.ru",
    "en.wikisource.org", "zelenyikot.com", "www.astronaut.ru", "aif.ru", "rgo.ru",
    "sergey-ryazanskiy.ru", "bombora.ru", "sukhov.com", "www.business-gazeta.ru",
    "cityposter.ru", "www.esa.int", "www.interfax.ru", "www.sobaka.ru",
    "rt-online.ru", "spbdnevnik.ru", "prichod.ru",
}


def public_url(url):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme not in ("https", "http") or parsed.username or parsed.password or not parsed.hostname:
        raise ValueError("Only public HTTP(S) source URLs are supported")
    # Fixed reviewed hosts, including redirect targets. Do not require local DNS:
    # on proxy-only networks name resolution is performed by the HTTP proxy.
    if parsed.hostname.lower() not in SOURCE_HOSTS or parsed.port not in (None, 80, 443):
        raise ValueError("Host is not in the reviewed public-source allowlist")


class CheckedRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        public_url(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def extract(data, url, content_type):
    if data.startswith(b"%PDF"):
        import fitz
        with fitz.open(stream=data, filetype="pdf") as pdf:
            if sum(len(page.get_text().strip()) for page in pdf) < 300:
                raise ValueError("PDF has no substantive text layer; OCR review required")
            texts = [f"## PDF page {n + 1}\n\n" + page.get_text(sort=True).strip()
                     for n, page in enumerate(pdf)]
            result = "\n\n".join(texts)
            empty_pages = [n + 1 for n, page in enumerate(pdf) if not page.get_text().strip()]
            return result, {"format": "pdf", "pages": len(pdf), "empty_text_pages": empty_pages,
                            "ocr_performed": False, "completeness": "text_layer_without_images"}
    if b"<html" not in data[:5000].lower() and "html" not in content_type.lower():
        raise ValueError("Unsupported source format")
    from lxml import html
    tree = html.fromstring(data, base_url=url)
    title = " ".join(tree.xpath("string(//title)").split())
    date = tree.xpath('//meta[@property="article:published_time"]/@content')
    for node in tree.xpath("//script|//style|//nav|//header|//footer|//aside|//form|//noscript"):
        node.drop_tree()
    candidates = tree.xpath("//article") or tree.xpath("//main") or [tree]
    body = max(candidates, key=lambda n: len(n.text_content()))
    blocks = []
    for node in body.xpath(".//h1|.//h2|.//h3|.//h4|.//h5|.//p|.//li|.//td[not(.//p)]"):
        # Do not duplicate a list/table wrapper containing the same paragraphs.
        if node.tag in ("li", "td") and node.xpath(".//p|.//li|.//td"):
            continue
        value = " ".join(node.text_content().split())
        if value:
            block = ("## " if node.tag.startswith("h") else "") + value
            if not blocks or blocks[-1] != block:
                blocks.append(block)
    text = "\n\n".join(blocks)
    if len(text) < 300:
        raise ValueError("No substantive text: possible error page, teaser or JavaScript shell")
    return text, {"format": "html", "page_title": title, "published_at": date[0] if date else None,
                  "completeness": "automatic_extraction_requires_review"}


def fetch(url, cache):
    entry = {"url": url, "status": "failed", "fetched_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
    try:
        public_url(url)
        opener = urllib.request.build_opener(CheckedRedirect())
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0 EducationalRAG/1.0"})
        with opener.open(req, timeout=25) as response:
            data = response.read(MAX_DOWNLOAD + 1)
            resolved = response.url
            content_type = response.headers.get("Content-Type", "")
        if len(data) > MAX_DOWNLOAD:
            raise ValueError("Source exceeds 24 MiB")
        text, metadata = extract(data, resolved, content_type)
        if len(text.strip()) < 300:
            raise ValueError("No extractable text; scanned PDFs require separate OCR review")
        key = hashlib.sha256(url.encode()).hexdigest()[:20]
        suffix = ".pdf" if metadata["format"] == "pdf" else ".html"
        (cache / (key + suffix)).write_bytes(data)
        (cache / (key + ".txt")).write_text(text, encoding="utf-8")
        entry.update(metadata, status="fetched", resolved_url=resolved, text_file=key + ".txt",
                     raw_file=key + suffix, characters=len(text),
                     sha256=hashlib.sha256(text.encode()).hexdigest(),
                     raw_sha256=hashlib.sha256(data).hexdigest())
    except (OSError, ValueError, urllib.error.URLError, ImportError, RuntimeError) as error:
        entry["error"] = f"{type(error).__name__}: {error}"
    return entry


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cache", required=True, type=Path, help="Private local extraction directory")
    parser.add_argument("--person", choices=("dzhanibekov", "ryazansky", "krikalev"))
    parser.add_argument("--workers", type=int, default=4)
    args = parser.parse_args()
    if not 1 <= args.workers <= 6:
        parser.error("--workers must be between 1 and 6")
    cache = args.cache.resolve()
    if cache.is_relative_to(ROOT / "knowledge"):
        parser.error("Keep raw downloads outside the shipped knowledge directory")
    cache.mkdir(parents=True, exist_ok=True)
    docs = json.loads(MANIFEST.read_text(encoding="utf-8"))
    urls = set()
    for doc in docs:
        if args.person and doc["person_id"] != args.person:
            continue
        urls.add(doc.get("download_url") or doc["source_url"])
        if doc.get("transcription_url"):
            urls.add(doc["transcription_url"])
    entries = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        futures = [pool.submit(fetch, url, cache) for url in sorted(urls)]
        for future in concurrent.futures.as_completed(futures):
            entry = future.result()
            entries.append(entry)
            print(entry["status"], entry["url"], entry.get("error", ""), flush=True)
    (cache / "fetch-report.json").write_text(json.dumps(sorted(entries, key=lambda d: d["url"]),
                                                        ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    failed = [d for d in entries if d["status"] != "fetched"]
    print(f"Fetched {len(entries) - len(failed)}/{len(entries)}. Review before indexing.")
    if failed:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
