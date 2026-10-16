# Remote workers — plan

Run Kurgu's heavy jobs (drafts, renders) on any number of machines — the VPS, a Mac at
home, a spare Linux box — while the server runs on either the VPS or the home network,
and no machine needs a static IP.

"Worker" below is what the request calls an *agent*. The UI and code keep the word
**worker**: *agent* already means Claude Code (the `Agent` port, the `agent` queue).

## 1. Where we are

`kurgu worker` with `KURGU_REMOTE_URL` + `KURGU_WORKER_TOKEN` already runs `deliverable.draft`
and `deliverable.render` remotely (`internal/adapter/remote`,
`internal/transport/http/worker`). The worker only dials out (claim → begin → fetch work
dir → run → upload → finish), so it never needs an inbound port or a fixed address. What
is missing for a fleet:

| Gap | Effect today |
|---|---|
| One shared token; `owner` comes from the client (`hostname:pid`) | Any worker can heartbeat or complete another's job; nobody can revoke a single machine; the name changes on every restart. |
| No registry | The UI cannot show which workers exist, are online, what they run or what they can do. |
| Claude sessions are local | Feedback resumes `claude --resume <id>`; the transcript lives in `~/.claude/projects/<cwd>/` on the host that wrote the draft. A revision claimed by another worker cannot resume. |
| Work dir is a full tar.gz each way | Stock clips and narration go over the wire on every claim and every revision; an interrupted upload starts again. |
| One render = one job | Many workers help only when several deliverables render at once; one long-form video still renders on one machine. |
| Capabilities are implicit | A worker without a Claude login, a Pexels key or a TTS engine claims a draft and fails it. |
| Polling every 2 s per queue slot | Fine for two machines, noisy for twenty, and up to 2 s of latency. |

## 2. Network topology

Rule: **workers only make outbound HTTPS requests to one stable server URL.** A worker's
own IP may change at any time; its identity is its token, not its address. Only the
server needs a reachable name.

| Server on | Worker on | How the worker reaches the server |
|---|---|---|
| VPS (public HTTPS domain) | VPS, home Mac, anywhere | The public URL. Works today. |
| Home network | Same LAN | `http://<lan-host>:8080` or a mDNS name. |
| Home network, dynamic IP | VPS or elsewhere | **Tailscale** on both (recommended): MagicDNS gives a stable name (`kurgu-home.<tailnet>.ts.net`) through NAT and IP changes, and WireGuard encrypts. Alternative: **Cloudflare Tunnel** (`cloudflared`) publishes the home server at a fixed public hostname, which also gives `KURGU_PUBLIC_URL` and the YouTube OAuth callback a real domain. |
| Anywhere | Laptop that sleeps / roams | Same as above; leases and resumable transfers (§5) absorb the gaps. |

No built-in relay or reverse tunnel: Tailscale and Cloudflare Tunnel solve NAT and dynamic
IPs better than Kurgu could, at no cost. Revisit only if neither is acceptable.

Transport safety: the worker refuses a plain `http://` server URL unless the host is
loopback, an RFC 1918 / ULA address, a `.local` name or in Tailscale's `100.64.0.0/10`
(`KURGU_REMOTE_INSECURE=true` overrides).

## 3. Identity and enrolment

Replace the shared token with one token per worker.

1. An admin opens **System → Workers → Add worker**, names it (`mac-studio`) and gets a
   one-time join code (valid 1 h) and the command to run:
   `kurgu worker join https://kurgu.example.com KJ-7F3Q-...`
2. `join` exchanges the code for a long-lived worker token
   (`POST /api/v1/worker/join`), writes it with the URL to the worker's config file
   (`~/.config/kurgu/worker.json`, mode 0600, or `KURGU_DATA_DIR/worker.json` in Docker),
   and prints how to install the service (§8).
3. The server stores only the token's SHA-256, like sessions and invitations.
4. Every worker request authenticates by token; the server derives the **owner from the
   token** (`worker:<id>`) and ignores any owner in the body. Leases, heartbeats, finish
   and fail check that owner.
5. Revoke deletes the token; the worker's running job loses its lease at the next
   heartbeat and is re-queued.

`KURGU_WORKER_TOKEN` stays accepted as a legacy shared token for one release, shown on
the Workers page as "legacy worker", then is removed.

New aggregate in the **job** context: `Worker` (id, workspace-independent, name, token
hash, created by, created at, revoked at, last seen, last address, version, capabilities,
profile name, state: active / draining / paused / revoked). Domain rules on it:
`w.Drain()`, `w.Revoke(now)`, `w.Seen(at, addr, version, caps)`, `w.CanRun(kind, reqs)`.

## 4. Registry, capabilities and routing

- **Hello.** On start and every 30 s the worker posts `POST /worker/hello` with version,
  OS/arch, CPU count, memory, and probed capabilities:
  `claude` (logged in, model), `chrome` (path, major version), `ffmpeg` (encoders, e.g.
  `h264_videotoolbox`), `speech` engines (`okur`, `ema`, `kokoro`), credentials present
  locally (`pexels`, `elevenlabs`), and a **render fingerprint**: hash of Chrome major,
  ffmpeg major and font set. The reply carries the worker's host profile (replacing the
  separate profile poll), its state (drain / pause) and the server's protocol version.
- **Online** = seen in the last 90 s. The Workers page lists each worker: online dot,
  current job and progress, last seen, version, capabilities, profile, and actions
  (pause, drain, revoke, rename, choose profile).
- **Profiles per worker.** The existing host profile mechanism stays; the Workers page
  picks which profile a worker follows, so "concurrency agent 0 / render 2" is set per
  machine from the UI.
- **Routing.** Each job kind declares requirements (`deliverable.draft` → `claude`,
  `chrome`, the voice engine of its language, `pexels` if stock is enabled;
  `deliverable.render` → `chrome`, `ffmpeg`). `Claim` filters by the claiming worker's
  capabilities, so a job is only leased to a worker that can finish it. A job nobody
  online can run shows "no worker can run this: needs kokoro" on the Jobs page instead of
  waiting silently.
- **Server-side secrets instead of copying keys.** Tools that need an API key
  (`kurgu tool stock`, ElevenLabs narration) call the server through the worker API
  (`POST /worker/jobs/{id}/tool/stock`), scoped to the leased job, instead of reading the
  key from the worker's environment. Keys stay in System → Credentials, and a new worker
  needs only Claude and local binaries.
- **Long-poll claim.** `POST /worker/claim` waits up to 25 s for a matching job, woken by
  the enqueue signal, and takes all of the worker's free slots in one request
  (`{"slots": {"agent": 1, "render": 2}}`). That replaces per-slot polling.

## 5. Transfers

Content-addressed files instead of whole-dir tarballs:

- `GET /worker/jobs/{id}/manifest` → `[{path, sha256, size, mode}]` for the work dir.
- The worker keeps a local cache (`KURGU_DATA_DIR/cache/blobs`, LRU, size cap
  `KURGU_WORKER_CACHE`, default 10 GB) and fetches only missing hashes with
  `GET /worker/blobs/{sha256}` (Range supported, so an interrupted download resumes).
- Upload: `POST /worker/blobs/missing` with the result's hashes → `PUT /worker/blobs/{sha256}`
  for each missing one, resumable with `HEAD` (bytes received) + `Content-Range`. The
  finish call then carries only the manifest and result metadata; the server verifies
  every hash, assembles the work dir from its store and runs the existing `FinishDraft` /
  `FinishRender`.
- With the `s3` blob driver the server may answer with presigned URLs instead, so large
  renders go straight to R2 and never through the VPS's bandwidth.

Revisions re-upload only changed files, and N workers rendering parts of the same video
download the work dir once each from cache-friendly hashes.

Leases: the heartbeat keeps running during transfers. A worker that loses the network
longer than the lease loses the job (re-queued as today); on reconnect its heartbeat gets
`Conflict`, the runner cancels the local run and discards it. Default remote lease rises
to 5 min (heartbeat every 1/3), so a router restart or a Mac waking from sleep does not
cost a render.

## 6. Portable agent sessions

Feedback must resume on any worker.

- After each draft run the worker copies the session transcript
  (`~/.claude/projects/<encoded cwd>/<session>.jsonl`) into the work dir as
  `.kurgu/session.jsonl`; it is uploaded with the rest.
- Before a resume, the worker restores it under its own `~/.claude/projects/<encoded
  local cwd>/` and passes the same session id to `--resume`.
- **Affinity as a preference:** a revision job carries `preferWorker` (the one that
  wrote the last draft). Claim gives it to that worker first for 60 s, then to anyone.
- If a restore fails (transcript format changed between Claude Code versions), the draft
  falls back to a fresh session with the previous script, feedback and notes in the
  prompt, and logs it on the job trace.

Spike first: confirm `--resume` accepts a transcript copied from another machine and
another cwd.

## 7. Distributing one render

Level 1 — **parallel jobs** (no new code beyond §3–4): each deliverable's render is one
job; ten workers render ten videos at once.

Level 2 — **split renders** for long videos. The composition is deterministic per frame
(`window.kurgu.renderAt(t)`), and the renderer already splits frame ranges across tabs
(`render.go`, `Chunks`), so the split moves from tabs to workers:

1. `deliverable.render` becomes the planner when the video is longer than
   `render.split_min` (default 120 s) **and** at least two online workers can render.
   It reads the frame count, picks `n = min(eligible workers' render slots,
   frames / (render.segment_seconds × fps))` with `render.segment_seconds` default 30,
   and enqueues:
   - `deliverable.render.segment` × n — payload: revision, scale, frame range, encoder
     spec, render fingerprint; unique key `rev/scale/from-to`.
   - `deliverable.render.music` — the Tone.js score, also offloadable.
2. A segment job renders its range with the existing `encodeRange` into a silent MP4 and
   uploads it as a blob (§5).
3. Each segment or music completion checks, in one transaction, whether all parts of the
   render are done and if so enqueues `deliverable.render.assemble` (unique key, so it is
   enqueued once).
4. Assemble (any worker with ffmpeg, or the server's own worker on the `io` queue) runs
   the ffmpeg concat demuxer with stream copy, then the existing `Mix` and `Poster`, then
   `FinishRender`.

Segments must be bit-compatible to concat without re-encoding:

- Segment jobs pin the encoder: `libx264`, fixed preset/CRF/profile/level, `yuv420p`,
  closed GOP, keyframe at the first frame. Hardware encoders (`h264_videotoolbox`) are
  for single-worker renders only.
- All segments of one render go to workers with the **same render fingerprint** (Chrome
  major, ffmpeg major, fonts), recorded by the first segment claimed; text and anti-
  aliasing otherwise differ visibly at the cut. Shipping the Docker worker image (§8)
  gives every Linux machine the same fingerprint; fonts used by templates are bundled in
  the work dir rather than taken from the system.
- A failed segment retries alone. After its attempts run out the whole render fails with
  the segment's error. A newer revision makes the remaining segments no-ops (the existing
  `d.Revision() != task.Revision` check).

Shorts (≤ 60 s) stay single-job: fetching the work dir and starting Chrome per segment
costs more than it saves.

## 8. Installing a worker

| Host | Install |
|---|---|
| Linux / VPS | `kurgu-worker` Docker image (the existing `worker` target) + a compose snippet taking `KURGU_REMOTE_URL` and the join code; or the binary with `kurgu worker install` writing a systemd unit. |
| macOS | Binary (Homebrew tap later) + `kurgu worker install` writing a launchd agent in `~/Library/LaunchAgents` (runs as the user, so the Claude login and VideoToolbox work). |
| Same host as the server | Unchanged: `kurgu all`, or the compose `worker` service; it registers as the built-in worker and appears in the list. |

`kurgu worker status` prints server, identity, online state, capabilities and the current
job. `kurgu worker doctor` runs the capability probes and a test claim.

Version skew: every request sends `Kurgu-Worker-Version`; the server refuses claims from
a worker whose protocol version is too old (the Workers page says "update needed"), and
jobs whose payload format changed declare a minimum worker version.

## 9. Phases

Each phase ships on its own, with tests (conformance for the new repository, `httptest`
through the router for the API, fakes for the remote client).

1. **Identity** — `Worker` aggregate, migration, join codes, per-worker tokens, owner from
   token, revoke; legacy token still accepted. Fixes the owner spoofing gap.
2. **Registry and UI** — hello/heartbeat, capability probes, Workers page (list, pause,
   drain, revoke, profile), job page shows which worker ran it. Profile served in hello.
3. **Routing** — kind requirements, capability-filtered claim, "no worker can run this",
   long-poll multi-slot claim.
4. **Portable sessions** — spike, transcript in the work dir, restore, affinity, fallback.
5. **Transfers** — manifest + blob cache + resumable uploads; presigned S3 URLs; longer
   remote lease.
6. **Server-side tool proxy** — stock search/download and ElevenLabs through the server.
7. **Split renders** — segment / music / assemble jobs, fingerprint pinning, bundled
   fonts, settings `render.split_min` and `render.segment_seconds`.
8. **Packaging** — `worker install` (launchd, systemd), `status`, `doctor`, published
   worker image, version checks.

Phases 1–3 make "many workers" safe and visible; 4 makes revisions work on any of them;
5 makes home connections tolerable; 7 is the only one that speeds up a single video.

## 10. Open questions

- Should workers be scoped to a workspace (someone else's machine rendering only their
  workspace), or stay instance-wide? Instance-wide is assumed above.
- Is a Claude subscription login on several machines acceptable under the current
  terms, or should drafts stay on one or two designated workers and only renders fan out?
- Assemble on the server's own worker (always reachable, no transfer of segments back
  out) or on any worker (keeps the VPS idle)? Server-side is simpler; pick by VPS size.

When a phase lands, update the *Remote workers* row and §3 of `PLAN.md`.
