# Host-switch flake: root-cause handoff (macOS → Windows)

Written 2026-06-09 on macOS, for continuation on the Windows dev machine.
Branch: `hostswitch-relay-diag` (macOS worktree `~/git/rdd-hostswitch-diag`).

## 1. Problem

`BATS (windows-latest, bats-app)` intermittently fails at `not ok 115 setup_file
failed` in `tests/32-app-controllers/kube-context.bats`: the
`rdd ctl wait --for=condition=KubernetesReady --timeout=900s` expires.
Investigation of a captured failing run identified three stacked defects (§3).
The primary remaining work is a clean-room reproduction of Defect 2 (§5),
which runs best on Windows.

## 2. State of branches, runs, and evidence

### Branch
- Local `hostswitch-relay-diag` tip: `e246bf649` (gates `--trace-packets`
  behind `RDD_TRACE_PACKETS`; append-mode stays on under `RDD_KEEP_LOGS`).
- `origin/hostswitch-relay-diag` is one commit behind (`b8bd02334`); push the
  branch before working on Windows. Commit `e246bf649` is already on origin
  via the throwaway branches `ci-repro-traceoff-{1,2,3}`.
- Full stack on top of `main`: `3647cf275` (#397 recovery) → `60c021e18`
  (relay diagnostics) → `87e37404f` (capture vm-switch.log host-side) →
  `ce07929d5` (log-dir guard) → `9bccf082f` (distro v0.2.5) → `b8bd02334`
  (append+trace under RDD_KEEP_LOGS) → `e246bf649` (trace opt-in).

### CI runs (repo `jandubois/rancher-desktop-daemon`)
- Run `27235240401`: attempt 1 PASSED (tracing on), attempt 2 (rerun) PASSED,
  attempt 3 FAILED — the analyzed repro. Artifact:
  `rdd-logs-windows-latest-bats-app`.
- Three tracing-off runs were in flight at handoff time:
  `27241058805`, `27241058771`, `27241058839` (branches `ci-repro-traceoff-1/2/3`).
  Check their `BATS (windows-latest, bats-app)` conclusions first; a failure
  artifact from these shows the flake without tracing perturbation (but has no
  guest packet trace — tracing-on artifacts are the ones with vm-switch logs).

### Evidence archive
- macOS: `~/rdd-flake-evidence/run-27235240401-attempts1and3-logs.tar.gz`
  (17 MB; contains the passing attempt-1 logs and the failing attempt-3 logs,
  including the 312 MB packet-traced `vm-switch.log`).
- Re-download: `gh run download 27235240401 --repo jandubois/rancher-desktop-daemon
  --name rdd-logs-windows-latest-bats-app` fetches the latest attempt only;
  use the archive for attempt-1/attempt-3 comparison.

## 3. Established findings

Failing window timeline (attempt 3; rdd PID 9728; one fresh rdd process for
kube-context — `svc delete` + `svc create` precede it):

| Boot | Window (UTC) | Fresh vn? | Network state | Outcome |
|---|---|---|---|---|
| A (first after fresh `wsl --import`) | 22:35:15–22:41:30 | yes | Relay up 6m14s; DNS dead all boot (~30× `Resolving timed out`) | k3s install failed ×30 → VM restart |
| B | 22:42:00–22:47:46 | yes | Relay perfect (~500 MB HTTPS, DNS 149/150 < 1 s); k3s healthy, kubeconfig probe satisfied 22:43:42 | Host→guest 6443 dials fail `no route to host` ×30 (22:43:54–22:47:43, every 8 s); hostagent shut down 22:47:44 (initiator unknown) |
| C | 22:48:16–22:49:41 | yes | Quiet; DHCP completed; DNS 1/1; guest held 192.168.127.2 | Same `no route to host` from first dial (22:48:26, 9 s after guest ARP); killed by 900 s cap |

Each boot got a fresh host-switch goroutine and a fresh
`virtualnetwork.New()` (lifecycle log: Handshake succeeded → Host-switch
running → Host-switch stopped, once per boot). Nothing rdd-internal persists
across boots. State that does persist: the WSL2 utility VM (same GUID
`1a59a8d8…` from 22:28 through 22:49, across both rdd processes and the
`svc delete`) and Windows host networking (HNS, vEthernet WSL adapter,
Dnscache).

### Defect 1 — gvisor DNS forwards to an unbounded Windows system resolver
- Code: `gvisor-tap-vsock@v0.8.9/pkg/services/dns/dns.go` — `New()` uses
  `net.Resolver{PreferGo: false}`; every lookup in `addAnswers` runs with
  `context.TODO()` (no timeout, no cancellation). On Windows that is
  `GetAddrInfoW`. Errors map to NXDOMAIN (`RcodeNameError`) — wrong semantics;
  hangs map to silence — the guest's exact symptom
  (`curl: (28) Resolving timed out after 10000 milliseconds`).
- No `TypeAAAA` case exists, so AAAA returns instantly empty (explains the 76
  `ANCount=0` responses in the boot-B trace; normal, not the bug).
- Upstream `main` has the same code. `NewWithUpstreamResolver` exists but
  `virtualnetwork/services.go:80` hardcodes `dns.New`. RD1 (v0.8.8, plain
  `virtualnetwork.New`) has identical exposure.
- Status: code-confirmed mechanism; boot A itself is packet-unconfirmed (the
  first boot after import logs vm-switch to the in-guest default path — the
  systemd drop-in race, §7.3).

### Defect 2 — host→guest dial fails EHOSTUNREACH without attempting ARP
- The 6443 forward is a gvisor `PortsForwarder` proxy inside rdd
  (`tcpproxy:` log lines; error strings `no route to host` /
  `connection was refused` are netstack's, not Windows').
- Failing boots B and C: every dial to `192.168.127.2:6443` returned
  `no route to host`. The guest traces show the gateway **never emitted an ARP
  request** (boot B: 6 ARP frames, all guest-initiated request/reply pairs;
  boot C: exactly 1 guest request + 1 gateway reply). The dial fails inside
  netstack's route/neighbor layer before any wire activity.
- Broadcast delivery works (DHCP replies arrive at `ff:ff:ff:ff:ff:ff`), so an
  emitted ARP request would have reached the guest and been logged.
- Passing run (attempt 1): same dial path produced `connection was refused`
  ×4 (= full ARP+TCP round trip reaching the guest while k3s was still
  starting), then succeeded. Pattern fits dials-while-neighbor-entry-warm,
  kept alive afterwards by continuous API traffic.
- Tension to resolve: boot C's first dial came only **9 s** after the guest's
  ARP request and still failed — pure entry-aging does not explain that.
  Either netstack does not (in this configuration) create a usable neighbor
  entry from a received ARP request, or some other per-boot state breaks
  resolution. This is what Experiment A discriminates.
- Status: evidence-locked behavior; mechanism OPEN.

### Defect 3 — a healthy VM was shut down with no logged initiator
- Boot B reached fully-ready (all lima requirements + kubeconfig probe
  satisfied 22:43:42), then `ha.stderr` shows "Shutting down the host agent" /
  "Shutting down WSL2 VM" at 22:47:44 with no preceding error. rdd's logs show
  only reaction ("Hostagent exiting" 22:47:46 → "Starting Lima instance").
- `lima/pkg/hostagent/hostagent.go:687` (`close()`) logs that message when the
  hostagent stops; the trigger at 22:47:44 (~344 s after boot start) is OPEN.
  Look at lima's WSL2 driver lifecycle/keepalive
  (`lima/pkg/driver/wsl2/vm_windows.go`, `wsl_driver_windows.go:281`) and at
  rdd stop paths that may act without logging
  (`rdd/pkg/controllers/lima/limavm/controllers/limavm_lifecycle.go`).

### Disproven along the way (do not re-derive)
- "Relay flaps / unstable data plane": relay connections last exactly one VM
  boot each (durations 6m14s/5m45s/1m25s, `abnormalCloses=1` per boot =
  teardown EOFs). The old `ci-flake-windows-hostswitch-vsock-dns` memory's
  framing is outdated on this point.
- "Leaked vn owns 127.0.0.1:6443": rdd never calls `Expose`
  (only the guest agent does, via each boot's own gateway API onto its own
  vn); engine-era rdds are separate processes; boot A never had k3s listening.
- "DNS responses too slow for curl": boot B latencies — 147 of 149 < 1 s,
  rest ≤ 2 s, zero unanswered.
- "Packet tracing masks the flake": attempt 3 failed with tracing on.
  (Tracing may still influence which defect manifests; the tracing-off runs
  address that.)

### Why only kube-context fails
1. Only test needing guest DNS/internet during provisioning (in-guest
   `curl get.k3s.io | sh` in `lima-template.yaml`).
2. Only test needing host→guest dialing: KubernetesReady probes
   `127.0.0.1:6443` → gvisor forwarder → netstack dial. Engine tests ride the
   separate 6660 docker-socket vsock bridge (`socketbridge`), which bypasses
   the gvisor netstack.
3. Only test with a hard 900 s readiness deadline.

### Why RD1 rarely hits this (verified in `~/git/rancher-desktop`)
1. k3s bring-up needs no guest network: `install-k3s` installs the binary and
   airgap images from a host-managed cache
   (`pkg/rancher-desktop/assets/scripts/install-k3s`). No `get.k3s.io`
   reference exists in RD1.
2. The 6443 forward (`host-switch.exe --port-forward
   127.0.0.1:6443=192.168.127.2:6443`, `backend/wsl.ts:116`) is armed at
   host-switch start, probed seconds later (warm neighbor state), then kept
   alive by continuous API traffic.
3. RD1's BATS factory-resets as often as ours, so churn frequency is NOT the
   differentiator; the differences above are.

## 4. Key reference data

| Item | Value |
|---|---|
| Subnet / gateway / guest | `192.168.127.0/24` / `.1` / `.2` |
| Gateway MAC / guest tap MAC | `5a:94:ef:e4:0c:dd` / `5a:94:ef:e4:0c:ee` |
| Static DHCP lease | `192.168.127.2 → 5a:94:ef:e4:0c:ee` |
| Static DNS host / NAT | `.254` → `127.0.0.1` |
| vsock ports | handshake 6669, data relay 6656, docker bridge 6660 |
| vn construction | `rdd/pkg/controllers/lima/limavm/controllers/hostswitch_windows.go:238`, config at `:354` |
| Accept loop / relay diag | same file `:278–311` |
| Expose API mux | same file `:254–257` (`/services/forwarder/expose`) |
| gvisor version | `github.com/containers/gvisor-tap-vsock v0.8.9` |
| Forwarder internals | `pkg/services/forwarder/ports.go` (`Expose` at `:70`; listener closed only by Unexpose; `VirtualNetwork` has no Close) |
| AcceptStdio framing | `StdioProtocol` → `hyperkitProtocol` (`pkg/tap/switch.go:310` default case): each frame = 2-byte **little-endian** length prefix + raw Ethernet frame |
| Guest-side exposer | opensuse `src/go/guestagent` (`pkg/forwarder/serviceapi.go`, `pkg/kube/watcher_linux.go`); its logs stay in-guest (capture gap, §7.2) |

## 5. Experiment A (primary): clean-room dial-into-guest repro

**Goal.** Reproduce `no route to host` on a netstack dial into the guest with
zero ARP emission, in a controlled test, then identify the broken state
transition and validate a fix.

**Why Windows.** The netstack layer is pure Go and platform-independent — if
it reproduces anywhere, it reproduces on Windows. If it does NOT reproduce,
the remaining suspects are Windows-only layers (hvsock delivery, utility-VM
state), and the same machine can extend the experiment to a real hvsock
transport and run Experiment B.

**Setup.** Standalone module (suggested: `~/git/rdd-repro/gvisor-dial/` with
its own `go.mod`), pinning `gvisor-tap-vsock v0.8.9`. Components:

1. Build the vn exactly as rdd does — copy `newVirtualNetworkConfig` from
   `hostswitch_windows.go:354` (values in §4).
2. Fake guest on one end of `net.Pipe()`; host side runs
   `go vn.AcceptStdio(ctx, hostSide)`.
   Framing (hyperkit/stdio): read 2-byte LE length, then that many bytes (one
   Ethernet frame); write the same way.
   The fake guest must: reply to ARP requests for `192.168.127.2` (source MAC
   `5a:94:ef:e4:0c:ee`), optionally send an ARP request for `192.168.127.1`
   (this is what seeds the gateway's neighbor cache in production), and log
   every received frame (gopacket makes the ARP/TCP decode trivial).
3. Expose the forward the way production does: POST
   `{"protocol":"tcp","local":"127.0.0.1:16443","remote":"192.168.127.2:6443"}`
   to `/services/forwarder/expose` on `vn.Mux()` (drive it directly with
   `httptest.NewRequest`/`ServeHTTP`; no real HTTP server needed).
4. Dial `127.0.0.1:16443` and record: the error string, and which frames (ARP
   request? SYN?) reached the fake guest within the dial window.

**Variant matrix** (each cell: dial error + frames-at-guest):

| # | Variant | Models |
|---|---|---|
| V1 | Dial before any guest frame | cold stack |
| V2 | Guest ARPs for gateway, dial within 5 s | passing run |
| V3 | Guest ARPs, idle 30/60/120/300 s, dial | aging hypothesis |
| V4 | Guest ARPs, continuous guest→gateway UDP (DNS) during wait, dial | boot C (it had just done DNS) |
| V5 | Heavy bidirectional load during dial | boot B |
| V6 | Disconnect conn, AcceptStdio a new pipe (same MAC), dial | VM-restart analog |

**Readout.**
- `connection was refused`/timeout + SYN visible at guest → resolution works
  in that variant.
- `no route to host` + **no ARP/SYN at guest** → repro. Narrow it: which
  variant flips the behavior, and does inserting a guest-initiated ARP
  immediately before the dial fix it?
- If no variant reproduces → escalate fidelity on the Windows box: replace
  `net.Pipe` with a real hvsock loopback pair, then with a live WSL distro
  running the actual `vm-switch` binary from distro v0.2.5.

**Fix candidates to validate once reproduced:**
1. Static neighbor entry: both IP and MAC are protocol constants; seeding the
   neighbor table at vn creation removes ARP dependence entirely. Needs stack
   access gvisor does not export — fork + `replace` directive (the fork can
   also carry the Defect 1 DNS fix; both are upstreamable).
2. Forwarder dial retry with fresh resolution.
3. Periodic gratuitous ARP from the guest (distro-side change to
   vm-switch/network-setup).
Compare invasiveness after the mechanism is understood.

## 6. Experiment B (Windows-only): Defect 1 resolver-hang repro

**Goal.** Show `net.Resolver{PreferGo:false}.LookupIPAddr` (= `GetAddrInfoW`)
hanging multi-second-to-minutes during WSL churn, and that a context deadline
returns control (validating the timeout fix).

**Procedure.** Two terminals:
1. A Go loop resolving a few names (`get.k3s.io`, `github.com`) every 500 ms,
   logging latency; run each lookup twice — bare `context.TODO()` and
   `context.WithTimeout(4s)`.
2. Churn WSL with a scratch distro: cycles of `wsl --import` /
   `wsl --terminate` / `wsl --unregister` (MSYS2 note: `MSYS_NO_PATHCONV=1`,
   convert paths with `cygpath -w`).

**Readout.** Bare-lookup latency spikes correlated with churn events = trigger
confirmed. The timeout-wrapped call returning at ~4 s while the bare one hangs
= the dns.go patch (timeout + SERVFAIL instead of NXDOMAIN) is sufficient even
when the underlying syscall thread stays stuck.

## 7. Secondary work items (independent of the experiments)

1. **Defect 3**: find what stops a healthy hostagent at ~344 s (§3); make
   every rdd stop path log its reason at default verbosity.
2. **Guest-agent log capture**: expose failures (e.g., EADDRINUSE) are only
   visible in the guest agent's in-guest log. Either persist its journal to
   `/mnt/c` (like vm-switch.log) or log expose requests host-side by wrapping
   `vn.Mux()` in rdd.
3. **First-boot trace gap**: the `network-setup.service.d` drop-ins cannot
   apply before systemd first starts the unit on a freshly imported VM, so
   boot-A-type failures lose their vm-switch trace. Fix: in the provision
   script, `systemctl daemon-reload && systemctl try-restart
   network-setup.service` when the unit's active environment predates the
   drop-ins. Boot A is exactly the boot we keep failing to observe.
4. **Structural hardening (discuss before building)**: host-side k3s
   provisioning with airgap images, RD1-style. Removes guest networking from
   Kubernetes bring-up; also immunizes against GitHub CDN throttling and
   shortens kube-context.

## 8. Reading list for a fresh session

- `rdd/pkg/controllers/lima/limavm/controllers/hostswitch_windows.go` (whole
  file; ~580 lines).
- `gvisor-tap-vsock@v0.8.9`: `pkg/services/dns/dns.go`,
  `pkg/services/forwarder/ports.go`, `pkg/tap/switch.go`,
  `pkg/virtualnetwork/virtualnetwork.go`.
- Memories: `ci-flake-windows-hostswitch-vsock-dns.md` (occurrence history;
  its "unstable relay" framing is superseded by §3),
  `vm-switch-log-capture-wiring.md` (how the capture works),
  `windows-dev-setup.md` (MSYS2 gotchas).
