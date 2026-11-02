from pathlib import Path
from urllib.parse import unquote
import re

root = Path(__file__).resolve().parent.parent
skip = {".git", ".research-server", "node_modules", "dist", "bin"}
broken = []
checked = 0
for file in root.rglob("*.md"):
    if any(part in skip for part in file.relative_to(root).parts):
        continue
    for target in re.findall(r"(?<!!)\[[^]]*\]\(([^)]+)\)", file.read_text(encoding="utf-8")):
        target = target.strip().split()[0].strip("<>")
        if not target or target.startswith(("#", "http://", "https://", "mailto:")):
            continue
        path = unquote(target.split("#", 1)[0])
        if not path:
            continue
        checked += 1
        if not (file.parent / path).resolve().exists():
            broken.append(f"{file.relative_to(root)}: {target}")
print(f"Checked {checked} local Markdown links; broken: {len(broken)}")
if broken:
    print("\n".join(broken))
    raise SystemExit(1)
