#!/usr/bin/env python3
"""Build the three role-scoped BM25 corpora from reviewed local documents.

No model, network, Python packages, credentials or embeddings are needed here.
Run from any directory: python scripts/build_crew_corpus.py [--check]
The manifest and reviewed Markdown are authoritative. Fetching a newer website
does NOT automatically replace an approved summary or rewrite game knowledge.
"""

import argparse
import collections
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE_ROOT = ROOT / "knowledge/sources/crew"
PERSON_IDS = ("dzhanibekov", "ryazansky", "krikalev")
MAX_BYTES = 9000  # Runtime accepts 16000; leave room for tool context/metadata.
TARGET_CHARS = 2200
MAX_CHARS = 4000


def digest(text):
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def sections(markdown):
    """Headings delimit topics. Never mix two sections or two documents."""
    heading, paragraphs = "Основной текст", []
    for block in re.split(r"\n\s*\n", markdown.strip()):
        block = block.strip()
        if block.startswith("# "):
            continue  # Display title is already indexed through source.title.
        if re.match(r"#{2,6} ", block):
            if paragraphs:
                yield heading, paragraphs
            heading = re.sub(r"^#+\s+", "", block)
            paragraphs = []
        elif block:
            paragraphs.append(block)
    if paragraphs:
        yield heading, paragraphs


def bounded_paragraphs(paragraph):
    """Keep sentences intact, splitting long paragraphs only when necessary."""
    if len(paragraph) <= MAX_CHARS and len(paragraph.encode()) <= MAX_BYTES:
        yield paragraph
        return
    pieces = re.split(r"(?<=[.!?])\s+", paragraph)
    pending = ""
    for piece in pieces:
        # Tables/long URLs may contain no punctuation. A final word boundary
        # split avoids silently truncating the text in these exceptional cases.
        units = piece.split() if len(piece) > MAX_CHARS else [piece]
        for unit in units:
            candidate = (pending + " " + unit).strip()
            if pending and (len(candidate) > MAX_CHARS or len(candidate.encode()) > MAX_BYTES):
                yield pending
                pending = unit
            else:
                pending = candidate
            if len(pending.encode()) > MAX_BYTES:
                raise ValueError("A single token exceeds the chunk budget")
    if pending:
        yield pending


def semantic_chunks(markdown):
    for heading, paragraphs in sections(markdown):
        pending = []
        for paragraph in paragraphs:
            for part in bounded_paragraphs(paragraph):
                candidate = "\n\n".join(pending + [part])
                if pending and (len(candidate) > TARGET_CHARS or len(candidate.encode()) > MAX_BYTES):
                    yield heading, "\n\n".join(pending)
                    pending = []
                pending.append(part)
        if pending:
            yield heading, "\n\n".join(pending)


def build():
    documents = json.loads((SOURCE_ROOT / "documents.json").read_text(encoding="utf-8"))
    by_person = {p: [] for p in PERSON_IDS}
    doc_ids, chunk_ids, source_urls = set(), set(), collections.defaultdict(set)
    stats = {p: collections.Counter() for p in PERSON_IDS}
    chunks_metadata = []
    for doc in documents:
        person = doc["person_id"]
        if person not in by_person or doc["document_id"] in doc_ids:
            raise ValueError("Unknown persona or duplicate document ID")
        doc_ids.add(doc["document_id"])
        source_urls[person].add(doc["source_url"])
        path = (SOURCE_ROOT / doc["file"]).resolve()
        if not path.is_relative_to(SOURCE_ROOT.resolve()):
            raise ValueError("Document path escapes the corpus")
        markdown = path.read_text(encoding="utf-8")
        if digest(markdown) != doc["sha256"]:
            raise ValueError(f"Document changed without review: {doc['file']}")
        stats[person]["documents"] += 1
        stats[person][doc["content_mode"] + "_documents"] += 1
        document_chunk_count = 0
        for index, (heading, text) in enumerate(semantic_chunks(markdown), 1):
            chunk_id = f"{doc['document_id']}_c{index:03}"
            if chunk_id in chunk_ids or not text.strip() or len(text.encode()) > MAX_BYTES:
                raise ValueError("Invalid or duplicate chunk")
            chunk_ids.add(chunk_id)
            source = {
                "id": doc["document_id"],
                "title": doc["title"],
                "url": doc["source_url"],
                "locator": (doc.get("source_locator", heading))[:180] + f" · фрагмент {index}",
                "content_mode": doc["content_mode"],
                "relation_to_person": doc["relation_to_person"],
                "language": doc["language"],
            }
            chunk = {"id": chunk_id, "text": text, "agents": [person],
                     "topics": doc["keywords_ru"], "source": source}
            by_person[person].append(chunk)
            chunks_metadata.append({"chunk_id": chunk_id, "document_id": doc["document_id"],
                                    "section": heading, "sha256": digest(text),
                                    "characters": len(text), "utf8_bytes": len(text.encode()),
                                    "word_count": len(text.split()), "token_count": None})
            stats[person]["chunks"] += 1
            stats[person]["characters"] += len(text)
            document_chunk_count += 1
        if document_chunk_count == 0:
            raise ValueError("Empty document")
    report = {"schema_version": 1, "built_from": "knowledge/sources/crew/documents.json",
              "retrieval": "existing role-scoped BM25 with Russian suffix normalization",
              "chunking": {"boundaries": "document > heading > paragraph > sentence",
                           "target_characters": TARGET_CHARS, "max_utf8_bytes": MAX_BYTES,
                           "overlap": 0, "exact_tokenizer": None},
              "personas": {p: dict(stats[p], unique_source_urls=len(source_urls[p])) for p in PERSON_IDS},
              "documents": len(doc_ids), "chunks": len(chunk_ids)}
    for person in PERSON_IDS:
        if stats[person]["documents"] != 20 or len(source_urls[person]) != 20:
            raise ValueError(f"Expected 20 different source documents for {person}")
    outputs = {ROOT / "knowledge" / f"{person}.jsonl":
               "".join(json.dumps(c, ensure_ascii=False) + "\n" for c in by_person[person])
               for person in PERSON_IDS}
    outputs[SOURCE_ROOT / "chunks-metadata.jsonl"] = "".join(
        json.dumps(c, ensure_ascii=False) + "\n" for c in chunks_metadata)
    outputs[SOURCE_ROOT / "build-report.json"] = json.dumps(report, ensure_ascii=False, indent=2) + "\n"
    catalog = ["# Источники трёх персонажей", "",
               "60 документов. Поле «Текст» различает пересказ и извлечённый фрагмент. "
               "Техническая справка других авторов не является личной работой космонавта.", ""]
    labels = {"summary": "Пересказ", "public_domain_excerpt": "Извлечённый текст"}
    relations = {"person": "О человеке / интервью", "mission_context": "Контекст миссии", "role_background": "Справка по специальности"}
    for person in PERSON_IDS:
        catalog.extend(["## " + person, "", "| Документ | Текст | Отношение к персоне |",
                        "|---|---|---|"])
        for doc in documents:
            if doc["person_id"] == person:
                title = doc["title"].replace("|", "—")
                catalog.append(f"| [{title}]({doc['source_url']}) | {labels[doc['content_mode']]} | {relations[doc['relation_to_person']]} |")
        catalog.append("")
    outputs[SOURCE_ROOT / "SOURCES.md"] = "\n".join(catalog) + "\n"
    return outputs, report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Verify committed chunks without writing")
    args = parser.parse_args()
    outputs, report = build()
    for path, content in outputs.items():
        if args.check:
            if not path.exists() or path.read_text(encoding="utf-8") != content:
                raise SystemExit(f"Outdated generated corpus: {path.relative_to(ROOT)}")
        else:
            path.write_text(content, encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
