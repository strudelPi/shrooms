#!/usr/bin/env python3
"""The shrooms agents mark: the fruiting bodies, and nothing underneath.

Shrooms is the mycelium — the network nobody sees — and its mark is mostly
that. Agents are what grows out of it: the visible part that does something.
So this mark is the mushrooms alone, standing on the soil line with the
network left out entirely: the difference from the shrooms mark is the point,
and a launcher row should never make you look twice to tell them apart.
Spores rise from every cap, not just the tallest: that is the agents at work.

Not a second drawing. It imports logo.py and changes only what differs, so the
two marks share every stroke, glow and colour and cannot drift apart.

    python3 assets/agents_logo.py [outdir]
"""
import os
import random
import sys

import logo

# The soil line near the bottom, and nothing under it: no network, no earth.
logo.HORIZON = 94.0
logo.EARTH = logo.VOID

#   x, cap half-width, cap height, stem height, lean, sway
logo.SHROOMS = [
    (47.0, 27.0, 26.0, 38.0, 0.12, -0.18),
    (85.0, 17.0, 16.5, 26.0, -0.22, 0.24),
    (17.0, 12.5, 12.0, 15.0, 0.20, 0.28),
]

# A pair at small sizes: one fruiting body is the shrooms icon.
logo.SHROOMS_SMALL = [
    (40.0, 26.0, 24.0, 26.0, 0.05, -0.05),
    (82.0, 16.0, 15.0, 15.0, -0.10, 0.10),
]
logo.NODES_SMALL = []
logo.EDGES_SMALL = []


def mycelium():
    """None: the agents mark is what grows above ground."""
    return [], []


def pick_node(foot, tan, nodes):
    """With no network to join, a stem ends where it meets the soil."""
    return foot


def spores(n=14):
    """Spores off every cap, more of them: the agents doing something."""
    rnd = random.Random(11)
    out = []
    for k, (x0, w, h, sh, lean, sway) in enumerate(logo.SHROOMS):
        cx = logo.stem_axis(x0, sh, lean, sway, logo.HORIZON - sh)
        top = logo.HORIZON - sh - h
        for _ in range((7, 4, 3)[k]):
            out.append((cx + rnd.uniform(-0.9, 1.2) * w * 0.7,
                        top + rnd.uniform(-12.0, 2.0),
                        rnd.uniform(0.7, 1.6) * (1.0 if k == 0 else 0.8)))
    return out


logo.pick_node = pick_node
logo.mycelium = mycelium
logo.spores = spores

if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.dirname(os.path.abspath(__file__))
    logo.render_png(os.path.join(out, "agents.png"), 512)
    logo.render_png(os.path.join(out, "agents-48.png"), 48)
    logo.render_png(os.path.join(out, "agents-96.png"), 96)
    print("wrote", out)
