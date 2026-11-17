"""The Ceph requests Go freezes, as TestTheCollectionValidatesEveryRequestGoldenGoFreezes writes them."""

from __future__ import annotations

import json
from pathlib import Path

REQUESTS = json.loads((Path(__file__).resolve().parent / "goldens/ceph_request_goldens.json").read_text(encoding="utf-8"))["requests"]

# The images budget every Ceph host request carries, which Go derives once.
IMAGES_SECONDS = REQUESTS["host"]["imagesSeconds"]
