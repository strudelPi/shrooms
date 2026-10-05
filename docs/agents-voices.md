# Shrooms Agents: a natural voice for read-aloud

**Status:** 2026-10-05 — option A is built: a "voice" section in each app
(next to "close" on the phone, beside AGENTS in Basecamp). In Basecamp it sets
Piper and an English voice up with one click; on the phone it says which
engine reads and walks through SherpaTTS — install, pick a voice, make it the
preferred engine — with a button for each step and one to try it. Option B is
not built.

## What there is now

Both apps read the model's replies sentence by sentence, so they can be paused,
skipped and followed (the sentence being read is lit). The voice is whatever
the device offers:

- **Phone:** Android's system speech engine — Google's, unless another is
  chosen in Settings → Text-to-speech. Google's offline voices sound robotic.
- **Basecamp:** the core runs Piper when `~/.local/share/shrooms/piper` holds
  it, otherwise speech-dispatcher (espeak-ng on this laptop: robotic too).

English is what matters; Czech is a bonus.

## The engines worth having

| | quality | speed on CPU | size | runs where |
|---|---|---|---|---|
| **Piper** (VITS, ONNX) | good, clearly human | ~8–9× faster than real time | 20–60 MB a voice | phone, laptop, Pi |
| **Kokoro-82M** (ONNX) | best of the small models | about real time on a modern CPU, below it on small ARM boards | ~80–330 MB | laptop, GPU machine; a phone only just |
| Google / espeak-ng | robotic | instant | built in | anywhere |

Piper is the one that is natural *and* cheap everywhere. Kokoro is the one
that sounds best, at a cost that suits the agent's machine rather than a phone.
Both run through **sherpa-onnx** (k2-fsa), which also runs on Android.

## Two ways to use them

**A. On the device — no change to the apps (recommended now).**

- *Phone:* install **SherpaTTS** (F-Droid: `org.woheller69.ttsengine`), pick an
  English Piper voice in it (e.g. `en_US-lessac` or `en_US-ryan`, medium or
  high), and select it as the default engine in Android's Text-to-speech
  settings. Shrooms Agents uses the system engine, so it reads with Piper from
  then on — offline, nothing sent anywhere, pause/skip/highlight unchanged.
  **VoxSherpa TTS** does the same with Kokoro too, for the better voice, if the
  phone keeps up.
- *Basecamp:* "voice" → SET UP: the core downloads rhasspy/piper's
  standalone build (25 MB, for the machine's architecture) and
  `en_US-lessac-medium` from rhasspy's own voice repository (63 MB) into
  `~/.local/share/shrooms/piper`, and reads with them from then on; "remove"
  goes back to spd-say. The voice is rhasspy's original, not the copy
  repackaged for sherpa-onnx: under this Piper build the repackaged one ran at
  half real time on a 20-core laptop, the original at about eleven times.

**B. On the agent's machine — one voice everywhere (later, if A is not enough).**

The agent synthesises a reply with Kokoro (or Piper) and serves the audio
(`GET /v1/sessions/{name}/speech/{seq}`, Opus, sentence by sentence with their
start times). Both apps play it; the times keep the lit sentence in step.
The same natural voice on every device, the phone does no work, and a machine
with a GPU (jimmy-crib) makes Kokoro fast. Costs: audio over the mesh (Opus at
~24 kbit/s is a few hundred kilobytes a minute), a model on the agent's
machine, and playback code in both apps. Offline, the phone would fall back to
its own engine.

## Recommendation

A first: it costs nothing to try and keeps working offline. If the phone's
Piper voice is still not good enough, B with Kokoro on the agent's machine.

Sources: [SherpaTTS on F-Droid](https://f-droid.org/packages/org.woheller69.ttsengine/),
[VoxSherpa TTS](https://github.com/CodeBySonu95/VoxSherpa-TTS),
[Kokoro vs Piper](https://voicechanger.live/text-to-speech/compare/kokoro-vs-piper).
