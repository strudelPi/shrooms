# Faster speech-to-text for voice notes

**Status:** measured 2026-10-03; not switched yet — a decision below.

Voice notes to agents are transcribed on the agent's machine
([docs/agents.md](agents.md)). The first engine, whisper.cpp with
large-v3-turbo, managed about 1.7× real time on the laptop at best, and 1.0×
under the balanced power profile: a minute of speech took a minute. Too slow
for something you wait on.

## Measured

Laptop: i7-13700H, 12 threads, balanced power profile (≈1.4 GHz average
during the runs), CPU only. Best of two runs. "Voice" is a real 75-second
voice note recorded on a phone, in English, with filler words and a false
start; JFK is whisper.cpp's 11-second sample; "long" is it five times.

| engine / model | size | JFK 11 s | voice 75 s | long 55 s | × real time |
|---|---|---|---|---|---|
| whisper large-v3-turbo q5_0 (`-l en`, clip-sized `-ac`) | 574 MB | 11.0 s | 73.5 s | 47.3 s | **1.0–1.2×** |
| whisper small q5_1 | 190 MB | 3.2 s | 23.3 s | 15.3 s | 3.2–3.6× |
| whisper base q5_1 | 60 MB | 1.2 s | 9.1 s | 5.6 s | 8–10× |
| **parakeet-tdt-0.6b-v3 q4_k** | 416 MB | 1.8 s | 10.5 s | 7.0 s | **6–8×** |
| parakeet-tdt-0.6b-v3 q8_0 | 669 MB | 2.0 s | 12.1 s | 8.4 s | 5.6–6.5× |

Quality on the real voice note:

- **turbo** — the cleanest: punctuation, "Logos Basecamp" spelled right,
  filler words dropped. Slowest by far.
- **parakeet v3** — the same words, "Logos Basecamp" right, but verbatim: it
  keeps "uh", "um" and the false start ("it would di uh deploy"). For text
  sent to a model, harmless; for text a person reads back, slightly noisy.
- **small** — good, but "logos base camp", fewer sentence breaks.
- **base** — fastest, and wrong in places ("a configured idea local AI model",
  "run on an hardware").

## What Parakeet changes besides speed

- **No language choice.** Parakeet v3 detects the language itself, with no
  extra pass — Whisper's auto-detection doubled its time, which is why the
  apps grew a CS/EN/AUTO switch. That switch could go.
- **Czech is supported** (v3 covers 25 European languages). Not measured yet:
  needs a Czech voice note.
- It runs in whisper.cpp's own build (`parakeet-cli`, upstream since
  2026-10), with ggml models from `ggml-org/parakeet-GGUF` on Hugging Face —
  no new dependency.

## Faster still, not tried

- **The performance power profile** — everything above ran at about 1.4 GHz.
- **Vulkan on the Iris Xe** — whisper.cpp (and llama.cpp) build with it; the
  headers are installed, `glslc` is not (`sudo apt install glslc`).

## Decision

**Switch shrooms-agent to Parakeet v3 q4_k by default**, keeping Whisper as a
configured alternative, and drop the language switch once Czech is confirmed?
Measured 6–8× real time against 1–1.2×, the same words, and one less thing to
set. The cost is verbatim filler words.
