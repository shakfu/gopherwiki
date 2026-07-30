# Security Model and Hardening for Computational Pages

This document analyzes the security posture of the Quarto computational-pages
feature, reports what the current implementation actually does (verified against
a running instance, not inferred from the design docs), and gives a ranked,
concrete set of recommendations with a deployment decision matrix.

It supplements Section 7 of [computational-pages.md](computational-pages.md),
which states the trust model as an assumption. This document tests that
assumption against the code and tells you what to do about it.

Last verified: 2026-07-30, against the running dev instance.

---

## 1. The one question that decides everything

**Is write access to this wiki equivalent to shell access on the host?**

Today, for a deployment with computational rendering enabled, the answer is
**yes**. Any principal who can trigger a render can run arbitrary code as the
server user, with that user's full ambient authority. Every recommendation below
is about either (a) making that equivalence acceptable by shrinking who has
write access and what the server user can reach, or (b) breaking the equivalence
so that write access no longer implies code execution.

You do not need all of the recommendations. You need to decide which of two
postures you are in, and apply the matching tier in Section 6:

- **Trusted-team posture.** Everyone with write access is someone you would
  already hand a shell. The design assumes this. If it holds, the residual risk
  is author *mistakes* (runaway loops, accidental `rm`), and the work is
  operational hardening -- confine the blast radius, do not try to contain an
  adversary.
- **Untrusted-author posture.** Anyone who can obtain write access is a
  potential adversary. The current design does **not** cover this. Making it
  safe requires per-execution isolation and a hardened data path -- a real
  project, not a config change.

The most dangerous outcome is being in the second posture while believing you
are in the first. Sections 2-3 exist to stop that.

---

## 2. Current posture, measured

The following were verified empirically by rendering a probe `.qmd` through the
gated `POST /{path}/render` endpoint on the running instance and observing what
the page code could reach. The probe page was removed afterward.

### 2.1 The render subprocess is unconfined

| Probe | Result | Meaning |
|-------|--------|---------|
| Read `$HOME` | `readable, 83 entries` | Page code sees the server user's home directory |
| Read `.wiki.db` | `True` | Page code can read the wiki database (session tokens, password hashes) |
| Outbound HTTPS | `allowed` | Page code can exfiltrate to any host |
| Process UID | `501` (same as server) | No privilege separation from the web server |
| `SECRET_KEY` in env | `False` | The env allowlist works: application secrets do **not** reach the subprocess |

The environment allowlist (`internal/quarto/render.go`, `renderEnvAllowlist`:
`PATH`, `HOME`, `TMPDIR`, `LANG`, `LC_ALL`, plus the `QUARTO_*` interpreter
hints) is the one control that is doing real work -- `SECRET_KEY` and other
process env are correctly withheld. But an allowlisted env is not a sandbox: the
subprocess inherits the filesystem and network of the server user. The render
temp dir (`gopherwiki-render-<n>`) is a working directory, not a boundary --
code trivially escapes it with an absolute path.

**Consequence:** with rendering enabled, "can trigger a render" == "can run code
as the server user, read the wiki DB, and reach the network." This is the core
finding.

### 2.2 The rendered-output iframe hands author JS the wiki origin

Rendered output is embedded same-origin with:

```
sandbox="allow-scripts allow-same-origin allow-popups allow-downloads"
```

`allow-scripts` + `allow-same-origin` on a same-origin frame is not a sandbox at
all -- the framed document can reach `window.parent`, the wiki's DOM, its
non-`HttpOnly` cookies, and issue session-authenticated `fetch()` calls. This is
persistent stored XSS with full origin access, and critically it fires on
**view** by any reader, not on render by an editor. Gating the render action
perfectly does not help: once rendered, the JS ships to everyone who opens the
page.

`allow-same-origin` is *required* for the Observable JS runtime to execute
(its inline `<script type="module">` blocks do not run in an opaque-origin
frame), so it cannot simply be dropped without breaking OJS. See Section 4.2.

The rendered-output CSP is necessarily permissive (`unsafe-inline`,
`unsafe-eval`, `data:`/`blob:`, and by default the Observable/jsDelivr CDNs)
because Quarto's self-contained HTML inlines scripts and some libraries `eval`.
So CSP is not a meaningful containment layer for this document either.

### 2.3 Render is gated only by write permission -- which defaults to anonymous

- `POST /{path}/render` is protected by `RequireWrite` -- the *same* gate as
  editing a page. There is no separate "may execute code" permission.
- The default access config is `WriteAccess=ANONYMOUS` with `AutoApproval=true`
  (`internal/config/config.go`). Out of the box, write -- and therefore the
  execution gate -- is open to unauthenticated users.

The one thing standing between "anonymous write default" and "anonymous code
execution" today is incidental: the web UI has no path to *create* a `.qmd` file
(the editor always saves `.md`), so an anonymous user cannot currently plant a
computational page through the browser. This is an accident of missing UI, not a
control. It disappears the moment a `.qmd`-authoring UI is added, or if the
attacker has any filesystem/git-push route to the repo. **Do not rely on it.**

### 2.4 What is already correct

Credit where due -- these invariants are enforced in code, not left to flags:

- Reader page-views never execute; a cache miss serves the placeholder.
- Export runs with `--no-execute`, so it is not a backdoor around the gate.
- Application secrets never enter the render environment (Section 2.1).
- A per-render wall-clock timeout exists (`RENDER_TIMEOUT_SECONDS`, default 120)
  and render concurrency is capped (`RENDER_CONCURRENCY`, default 2).
- The shipped `Dockerfile` already runs the app as a non-root user (uid 1000).

The gaps are: no CPU/memory/pids limits, no filesystem or network confinement of
the subprocess, no origin isolation for rendered output, no execution-specific
permission, and no rate-limiting or audit trail on the execute action.

---

## 3. Threat model

| # | Threat | Who | Enabled by | Current exposure |
|---|--------|-----|-----------|------------------|
| T1 | Arbitrary code execution as server user | Anyone with write | Unconfined subprocess (2.1) | Full: FS + network + wiki DB |
| T2 | Credential/data exfiltration from render | Author of a `.qmd` | Network egress + DB readable (2.1) | Full |
| T3 | Stored XSS against readers/admins | Author of a `.qmd` | Same-origin iframe (2.2) | Full origin access on view |
| T4 | Resource exhaustion (CPU/mem/fork bomb) | Author, or accident | No cgroup limits | Partial: wall-clock + concurrency cap only |
| T5 | Privilege escalation via write-default | Unauthenticated user | `WriteAccess=ANONYMOUS` (2.3) | Gated today only by missing `.qmd` UI |
| T6 | Supply-chain: OJS pulls CDN libs at view | Passive | Default CDN mode (4.2) | Third-party JS runs in reader browsers |

T1-T3 are the load-bearing risks. T4 is a mistake-not-malice risk that matters
even in the trusted-team posture. T5 is a misconfiguration waiting to become
T1. T6 is a distinct offline/air-gap concern with an existing mitigation.

---

## 4. Recommendations, ranked

Ordered by risk removed per unit effort. Each item names the threats it closes.

### 4.1 (Do first) Confine the render subprocess -- closes T1, T2, bounds T4

This is the single highest-value change: it breaks "write == shell." Options in
increasing order of strength:

**A. Run renders in a throwaway container (recommended baseline).**
Instead of `exec`ing `quarto` in-process, run each render in a short-lived
container with no ambient authority:

```
docker run --rm \
  --network=none \                 # closes T2 egress
  --read-only \                    # FS is immutable...
  --tmpfs /work:rw,size=256m,noexec \   # ...except a bounded scratch dir
  --memory=512m --cpus=1 --pids-limit=256 \  # closes T4
  --cap-drop=ALL --security-opt=no-new-privileges \
  --user 1000:1000 \
  -v "$RENDER_INPUT:/work/input:ro" \
  gopherwiki-render:latest \
  quarto render /work/input/page.qmd --to html --embed-resources
```

The wiki server mounts only the single page source in, reads the rendered HTML
out, and the container is destroyed. The subprocess can no longer see `$HOME`,
`.wiki.db`, or the network. `--network=none` is the key line -- it turns T2 from
"full exfiltration" into "no exfiltration," and also neutralizes the OJS CDN
dependency (you must then use `OJS_LIBS_DIR`, see 4.2).

**B. Non-root, resource-limited, no-network local process (if you cannot add a
container runtime).** Run the render under a dedicated unprivileged user, in a
network namespace with no interfaces, with `RLIMIT_AS`/`RLIMIT_CPU`/`RLIMIT_NPROC`
set and a read-only bind of everything except a scratch tmpfs. On Linux this is
`unshare --net --map-root-user` + `setrlimit` in a `SysProcAttr` wrapper, or a
`systemd` transient unit (see 5.2). Weaker than a container (shared kernel, more
moving parts) but removes the network and the ambient FS.

**C. microVM / gVisor / nsjail (untrusted-author posture only).** If you must
accept genuinely untrusted pages, per-execution isolation with a syscall barrier
(gVisor, Firecracker, nsjail with seccomp) is the floor. This is the "separate,
substantial project" the design doc warns about; do not pretend a Docker
`--cap-drop` is equivalent to it for hostile input.

Implementation note: the code already has the right seam. `Runner` in
`internal/quarto/render.go` is an interface over process execution; a
`containerRunner` implementing it is a localized change, not a rewrite.

### 4.2 (Do first, cheap) Serve rendered output from a separate origin -- closes T3

The XSS (2.2) is not fixed by sandboxing the render; the malicious JS is in the
*output*, served to readers. Two routes:

**Preferred: separate serving origin.** Serve `/{path}/rendered` from a distinct
origin (e.g. `render.wiki.example.com`, or a per-page/random subdomain), and
drop `allow-same-origin` from the iframe. Now the framed document is
cross-origin: OJS still runs (it has its own origin to be same-origin *with*),
but author JS can no longer reach the wiki's DOM, cookies, or authenticated
endpoints. This is the standard pattern (GitHub's `githubusercontent.com`,
Google's `googleusercontent.com`) and the correct fix.

**Weaker fallback if a second origin is impossible:** keep same-origin but make
wiki session cookies `HttpOnly` + `SameSite=Strict` (blunts cookie theft and
cross-site use) and set `Content-Security-Policy: sandbox` semantics you can
actually enforce. This does not stop DOM access to the parent and is a
mitigation, not a fix. Prefer the separate origin.

For the CDN concern (T6): set `OJS_LIBS_DIR` to a local mirror
(`scripts/mirror-ojs-libs.sh`). This is mandatory if you adopt `--network=none`
renders, and desirable anyway -- it removes the runtime third-party JS fetch and
lets the rendered-output CSP drop the CDN allowance (`renderedCSPLocal`).

### 4.3 (Do first, cheap) Split execution from write permission -- closes T5, shrinks T1/T2

Introduce a distinct capability -- call it `PermissionRender` -- and gate
`POST /{path}/render` on it instead of `RequireWrite`. This lets you run the
common and valuable configuration "many editors, few renderers": a large group
may author `.qmd` pages, but only a small trusted set may execute them.

This is only possible because rendering is a *distinct action*. It is the
strongest argument for keeping render manual rather than firing it implicitly on
save -- an implicit save-time render can never be given a higher privilege than
the save itself. Design the UI as an explicit "Render" control, not
render-on-save, precisely so this permission has something to attach to.

Do this even in the trusted-team posture: it converts the anonymous-write
default (T5) from "latent code execution" into "latent page editing," which is
the risk you actually signed up for.

### 4.4 (Do) Lock down the access defaults for any compute-enabled instance

If `COMPUTATIONAL_PAGES_ENABLED=1`, the permissive defaults are actively
dangerous. Recommended baseline for any instance with execution on:

```
WRITE_ACCESS=REGISTERED         # never ANONYMOUS with compute on
AUTO_APPROVAL=false             # an admin approves each new author
DISABLE_REGISTRATION=true       # if the author set is fixed
```

Consider having `setupRenderService` refuse to enable execution when
`WriteAccess=ANONYMOUS` (fail closed with a clear error), so the two dangerous
settings cannot be combined by accident. This is a small, high-value guardrail.

### 4.5 (Do) Add hard resource ceilings -- closes T4 fully

The wall-clock timeout and concurrency cap are necessary but not sufficient: a
render can still exhaust memory or fork-bomb within the timeout. If you adopt
4.1.A the container flags (`--memory`, `--cpus`, `--pids-limit`) cover this. If
you stay in-process, set `RLIMIT_AS`, `RLIMIT_CPU`, `RLIMIT_NPROC`, and
`RLIMIT_FSIZE` on the child via `SysProcAttr`. Keep `RENDER_CONCURRENCY` low
(2 is a sane default) so total resource use is bounded by
`concurrency x per-render-limit`.

### 4.6 (Do) Audit and rate-limit the execute action

Execution is the highest-privilege action in the system and currently leaves no
trace beyond a git commit of the source. Add:

- A structured audit log entry per render: who, which page, source revision,
  duration, success/failure. You already log render failures; extend to an
  explicit audit event on success.
- A per-user rate limit on `POST /render` (there is no rate limiting anywhere in
  the app today). This bounds both accidental render storms and deliberate abuse
  of the render host as compute.

### 4.7 (Consider) Defense in depth on the wiki's own responses

Independent of compute, and cheap: make session cookies `HttpOnly` +
`SameSite=Strict`, keep the strict app-wide CSP (already present outside the
rendered iframe), and set HSTS when served over TLS. These reduce the value of a
T3 compromise even before 4.2 lands. Note the app already sets
`X-Frame-Options: SAMEORIGIN` and `X-Content-Type-Options: nosniff` on normal
responses -- good; the gap is specifically the rendered-output document.

---

## 5. Concrete deployment hardening

### 5.1 Container image and compose

The shipped `Dockerfile` runs as non-root (good) but bundles no Quarto/Python/R
toolchain -- computational rendering is not possible in that image as-is. When
you add the toolchain, do **not** simply install it alongside the web server and
render in-process; that co-locates the code-execution surface with the database
and the git repo. Prefer a two-image split:

- `gopherwiki` (web server): no Quarto, no interpreters. Talks to a render
  worker.
- `gopherwiki-render` (worker): Quarto + Jupyter/knitr, run per-render with the
  isolation flags in 4.1.A, `--network=none`, non-root, all caps dropped.

Compose sketch for the isolated worker pattern (single-host):

```yaml
services:
  wiki:
    image: gopherwiki:latest
    read_only: true
    tmpfs: [/tmp]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    user: "1000:1000"
    environment:
      COMPUTATIONAL_PAGES_ENABLED: "1"
      WRITE_ACCESS: "REGISTERED"
      AUTO_APPROVAL: "false"
      OJS_LIBS_DIR: "/ojs-libs"     # offline OJS, no CDN egress
    volumes:
      - wiki-data:/app-data
      - ojs-libs:/ojs-libs:ro
  # render worker is invoked per-render (docker run --rm ...), not a long-lived
  # service; see 4.1.A. Give it its own network=none and no volume access to
  # wiki-data.
```

If you run renders as a long-lived worker service instead of `docker run --rm`
per render, give that service `network_mode: none`, its own scratch volume, and
**no** mount of `wiki-data` (it receives page source over the wire, not via the
shared DB/repo).

### 5.2 systemd (non-container hosts)

If you deploy on bare systemd, wrap the render in a transient, sandboxed unit
rather than letting the server `fork/exec` Quarto directly. Relevant directives:

```
# gopherwiki.service (web server) -- and mirror the tight ones onto the
# transient render unit spawned per render:
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true               # render cannot read the server user's home
NoNewPrivileges=true
RestrictAddressFamilies=AF_UNIX   # render unit: drop AF_INET/AF_INET6 => no network
IPAddressDeny=any                 # render unit
MemoryMax=512M
CPUQuota=100%
TasksMax=256
CapabilityBoundingSet=            # drop all
SystemCallFilter=@system-service
```

`ProtectHome=true` + `IPAddressDeny=any` on the render unit alone closes T1's FS
reach and T2's egress without any container runtime.

### 5.3 The minimum viable trusted-team hardening

If you are firmly in the trusted-team posture and want the smallest change that
still removes the sharp edges, do exactly these four:

1. `WRITE_ACCESS=REGISTERED`, `AUTO_APPROVAL=false` (4.4).
2. `OJS_LIBS_DIR` set to a local mirror, and `--network=none` on the render
   (4.1.A / 4.2) -- removes exfiltration and the CDN dependency in one move.
3. Container or systemd resource limits on the render (4.5).
4. Run the render host with nothing sensitive on it (no shared secrets, DB on a
   separate service account).

This leaves T3 (same-origin XSS) accepted-by-policy, which is defensible only if
every author is trusted not to ship hostile JS. If that is not certain, add 4.2.

---

## 6. Decision matrix

| Deployment | Posture | Required controls | Optional |
|-----------|---------|-------------------|----------|
| Personal / single-author | Trusted | 4.4 (lock write), 4.5 (limits) | rest |
| Small trusted team | Trusted | 4.4, 4.5, 4.1.A `--network=none`, 4.2 CDN->local | 4.3, 4.6 |
| Larger org, some untrusted editors | Mixed | All of the above **plus** 4.2 separate origin, 4.3 split permission, 4.6 audit+rate-limit | 4.7 |
| Public / open authoring | Untrusted | Do not enable compute until 4.1.C (microVM/gVisor/nsjail) + 4.2 separate origin + 4.3 + 4.6 are all in place | -- |

The bottom row is a deliberate stop sign: the current design explicitly does not
cover untrusted authoring, and no combination of flags makes it safe. That must
be a conscious product decision with real isolation work behind it, not an
emergent default.

---

## 7. Recommendation (summary)

1. **Decide the posture** (Section 1). Write it down. Most GopherWiki
   deployments are trusted-team; if yours is, say so explicitly so the
   same-origin XSS tradeoff (T3) is an accepted decision and not a surprise.

2. **For any compute-enabled instance, regardless of posture, do these five:**
   - Lock the access defaults: `WRITE_ACCESS=REGISTERED`, `AUTO_APPROVAL=false`
     (4.4), ideally with a fail-closed guard against `ANONYMOUS` + compute.
   - Confine the render: container or systemd unit with `--network=none` / no
     network, read-only FS + scratch tmpfs, dropped caps, memory/CPU/pids limits
     (4.1.A, 4.5). This alone converts "write == shell" into "write == sandboxed
     compute."
   - Mirror OJS libraries locally and set `OJS_LIBS_DIR` (4.2 / T6), which is a
     prerequisite for the no-network render anyway.
   - Split `PermissionRender` out from write and make the UI an explicit Render
     action, never render-on-save (4.3).
   - Add audit logging and a rate limit on the execute action (4.6).

3. **If any author is not fully trusted, additionally** serve rendered output
   from a separate origin and drop `allow-same-origin` (4.2 / T3). This is the
   only fix for the stored-XSS surface.

4. **If authoring is open/untrusted, do not enable compute** until real
   per-execution isolation (4.1.C) is built. Treat that as a project.

The implementation cost is modest for the high-value items: the `Runner`
interface already isolates process execution (drop in a container runner), the
route already has a permission gate (change which permission), and the OJS
local-libs path already exists (just turn it on). The origin split (item 3) is
the one genuinely new piece of infrastructure, and it is only required for the
untrusted-author case.
