#!/usr/bin/env python3
"""Extract selected, auditable paragraphs of the supplied scenario, not a biography.

No invented historical quotations and no game-master spoilers in public chunks.
Paragraph numbers include non-empty table-cell paragraphs in document order.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import xml.etree.ElementTree as ET
import zipfile

ALL = ["dzhanibekov", "savinykh", "ryazansky", "krikalev"]
C, E, B, N = ALL
# id, heading, paragraph ranges, agents, topic, required visible facts
SECTIONS = [
    ("briefing", "Завязка и общая цель", [(4, 4), (19, 25)], ALL, "briefing", []),
    ("cooperation", "Роли и подтверждение капитана", [(32, 48)], ALL, "roles", []),
    ("resources", "Начальные ресурсы корабля", [(49, 72)], ALL, "resources", []),
    ("cycle", "Расчёт обычного цикла", [(73, 82)], ALL, "resources", []),
    ("captain", "Панель и ответственность капитана", [(30, 31)], [C], "captain", ["shutdown_command"]),
    ("engineer", "Начальные сведения инженера", [(26, 27)], [E], "reactor", ["temperature_spike"]),
    ("biologist", "Начальные сведения биолога", [(28, 29)], [B], "biology", ["no_infection"]),
    ("first-crisis", "Распределение энергии и резерв", [(88, 109)], ALL, "power", []),
    ("archive-cost", "Восстановление архива", [(111, 112)], [C, N], "archive", []),
    ("temperature", "Температурный скачок", [(116, 118)], [E], "evidence", ["temperature_spike"]),
    ("filter", "Найденный фильтр", [(119, 121)], [E], "evidence", ["damaged_filter"]),
    ("sample", "Проба биолога", [(122, 124)], [B], "biology", ["no_infection"]),
    ("chronology", "Восстановленная хронология", [(125, 130)], [C, N], "archive", ["archive_chronology", "impact_record"]),
    ("confirmed-cause", "Подтверждённая причина", [(131, 137)], ALL, "evidence", ["cause_confirmed"]),
    ("course", "Подготовка курса", [(138, 141)], [C, N], "navigation", []),
    ("emergency-plan", "Аварийный запуск", [(145, 147), (157, 158)], [C, E, N], "rescue", []),
    ("ecosystem-plan", "Длительный план", [(148, 150), (159, 160)], ALL, "rescue", []),
    ("combined-plan", "Комбинированный план", [(151, 153)], ALL, "rescue", []),
    ("evacuation", "Аварийный модуль", [(154, 156)], ALL, "rescue", []),
    ("ai-limits", "Проверяемые подсказки и правила ИИ", [(220, 224), (230, 233), (249, 250)], ALL, "help", []),
    ("quest-rules", "Общие правила дополнительных квестов", [(353, 358)], ALL, "quests", []),
    ("leak-alert", "Утечка кислорода", [(390, 391)], [C, E, B], "leak", ["leak_active"]),
    ("leak-inspection", "Осмотр клапанов и устранение утечки", [(392, 405)], [C, E, B], "leak", ["valves_inspected"]),
    ("drone-alert", "Заблудившийся ремонтник", [(406, 407)], [C, E, N], "drone", ["drone_active"]),
    ("drone-route", "Сеть узлов ремонтного дрона", [(408, 421)], [C, E, N], "drone", ["drone_sensor"]),
    ("garden-quest", "Сохранение биологической коллекции", [(423, 438)], [C, B], "garden", ["garden_active"]),
    ("capsule", "Научная капсула", [(439, 453)], [C, B, N], "capsule", ["capsule_active"]),
    ("beacon", "Проверка навигационных сообщений", [(455, 470)], [C, N], "navigation", ["navigation_records"]),
    ("power-quest", "Аудит потребления энергии", [(471, 485)], [C, E, B], "power", ["power_audit"]),
    ("memory-quest", "Проверка вывода ИИ", [(487, 502)], [C, B, N], "memory", ["archive_chronology", "no_infection"]),
    ("door", "Согласование открытия двери", [(503, 517)], [C, E], "door", ["door_active"]),
]


def build(source: Path, output: Path) -> None:
    ns = {"w": "http://schemas.openxmlformats.org/wordprocessingml/2006/main"}
    raw = source.read_bytes()
    with zipfile.ZipFile(source) as archive:
        root = ET.fromstring(archive.read("word/document.xml"))
    paragraphs = []
    for paragraph in root.findall(".//w:body//w:p", ns):
        text = "".join(e.text or "" for e in paragraph.findall(".//w:t", ns)).strip()
        if text:
            paragraphs.append(text)
    if len(paragraphs) != 517 or not paragraphs[1].startswith("Последний экипаж"):
        raise ValueError("Unexpected source revision: update and review paragraph ranges before import")
    output.mkdir(parents=True, exist_ok=True)
    chunks = []
    manifest = []
    for key, heading, ranges, agents, topic, facts in SECTIONS:
        text = "\n".join(paragraphs[i - 1] for start, end in ranges for i in range(start, end + 1))
        assert len(text.encode()) <= 16000, key
        chunk = {
            "id": "scenario-" + key,
            "text": heading + "\n" + text,
            "agents": agents,
            "topics": [topic, "scenario"],
            "source": {"id": "survival-scenario-v1", "title": "Последний экипаж — Протокол выживания, игровой сценарий", "locator": "Абзацы " + ", ".join(f"{a}–{b}" for a, b in ranges)},
        }
        if facts:
            chunk["requires_facts"] = facts
        chunks.append(json.dumps(chunk, ensure_ascii=False))
        manifest.append({"chunk_id": chunk["id"], "paragraph_ranges": ranges, "agents": agents, "requires_facts": facts})
    (output / "scenario.jsonl").write_text("\n".join(chunks) + "\n", encoding="utf-8")
    (output / "scenario-manifest.json").write_text(json.dumps({"source": "sources/scenario.docx", "source_sha256": hashlib.sha256(raw).hexdigest(), "source_paragraphs": len(paragraphs), "kind": "user_game_scenario", "captain_biography_chunks": "not supplied in this conversation", "chunks": manifest}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (output / "sources").mkdir(exist_ok=True)
    destination = output / "sources/scenario.docx"
    if source.resolve() != destination.resolve():
        shutil.copyfile(source, destination)
    print(f"Imported {len(chunks)} scenario chunks; source SHA256={hashlib.sha256(raw).hexdigest()}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("--output", type=Path, default=Path("knowledge"))
    args = parser.parse_args()
    build(args.source, args.output)
