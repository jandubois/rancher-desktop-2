# Deep Review: 20260910-012226-guest-deps-downloader

| | |
|---|---|
| **Date** | 2026-09-10 01:22 |
| **Repo** | [rancher-sandbox/rancher-desktop-2](https://github.com/rancher-sandbox/rancher-desktop-2) |
| **Round** | 3 (of PR #711) |
| **Author** | [@jandubois](https://github.com/jandubois) |
| **Branch** | `guest-deps-downloader` |
| **Commits** | `d47fa7dac` rdd: add the guest dependency downloader<br>`3ef059fe3` rddepman: correct the nerdctl staging note |
| **Review SHA** | `3ef059fe3e2a1727024214fb67bdc47871de5643` |
| **Reviewers** | Claude Opus 5 (effort: xhigh), Codex GPT 5.6 Sol (effort: xhigh), Gemini 3.1 Pro (effort: default), Gemini 3.5 Flash (effort: default) |
| **Verdict** | **Merge as-is** — every round-2 fix closes its finding without adding a defect; the four suggestions are optional |
| **Wall-clock time** | `9 h 31 min 41 s` |

## Executive Summary

This is the round that converged. Every round-2 fix closes its finding and none introduces a defect, so the four suggestions below are optional. `go test -race`, `go vet`, and `golangci-lint` are clean. Of 15 mechanisms I broke one at a time, the suite noticed 11. S1 and S2 cover three of the four misses, each with a probe and a test ready to paste. S3 steadies a timing-sensitive test, and S4 corrects a nerdctl comment.

This round reviews the local branch head `3ef059fe3`. The author amended both commits to apply round 2's fixes after round 2 reviewed the pushed head `14b43bf81`, so CI has not run on this SHA.

---

## Critical Issues

None.

---

## Important Issues

None.

---

## Suggestions

S1. **The retry wait's exits can be broken with the suite still green** — `rdd/pkg/guestdeps/stage.go:346-350` [Claude Opus 5, Codex GPT 5.6 Sol]

```go
			select {
			case <-ctx.Done():
				break attempts
			case <-time.After(wait):
			}
```

The round's deadline fix leaves this wait two ways, and no test reaches either. After `break attempts`, `err` still holds the previous attempt's failure, so the `caller` case at line 365 is what makes a Ctrl-C here report cancellation instead of "unexpected status 502". `TestStageCleansUpWhenCancelled` cancels mid-body, where the transport already returns `context.Canceled`, so it passes without that case. The probe disables the `caller` case, then reverts the wait to round 2's `return ctx.Err()`. The current tests stay green through both, and the two proposed tests fail on each.

```bash repro
# Save as probe-retry-wait.sh; run from the repository root: bash probe-retry-wait.sh
# Needs go and perl. Disables each retry-wait exit of download in turn and runs
# the current guestdeps tests, then adds two proposed tests and runs them
# against each change. stage.go is restored and the test file removed on exit.
set -u
cd rdd || exit 1
stage=pkg/guestdeps/stage.go
probe=pkg/guestdeps/retry_wait_probe_test.go
backup=$(mktemp)
cp "$stage" "$backup"
trap 'cp "$backup" "$stage"; rm -f "$backup" "$probe"' EXIT

mutate() {
  FROM=$1 TO=$2 perl -pi -e 's/\Q$ENV{FROM}\E/$ENV{TO}/' "$stage"
  cmp -s "$stage" "$backup" && echo "  change not applied"
}
check() {
  go test -count=1 -run "$1" ./pkg/guestdeps/ 2>&1 |
    grep -E '^(ok|FAIL)[[:space:]]|--- FAIL|probe_test.go:[0-9]+' | head -6
}
cancel_case='case caller.Err() != nil:'
wait_exit='break attempts'

echo "== current tests, caller-cancel case disabled"
mutate "$cancel_case" 'case caller == nil:'; check .; cp "$backup" "$stage"
echo "== current tests, retry wait returns bare ctx.Err()"
mutate "$wait_exit" 'return ctx.Err(); break attempts'; check .; cp "$backup" "$stage"

cat > "$probe" <<'GO'
package guestdeps

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// cancelOnRetry cancels the build as soon as download announces a retry, so the
// cancellation lands in the wait between attempts.
type cancelOnRetry struct{ cancel context.CancelFunc }

func (w cancelOnRetry) Write(p []byte) (int, error) {
	if bytes.HasPrefix(p, []byte("Retrying in ")) {
		w.cancel()
	}
	return len(p), nil
}

func TestStageReportsACancelDuringTheRetryWait(t *testing.T) {
	body := []byte("distro image")
	server := newAssetServer(t, body, http.StatusBadGateway)
	ctx, cancel := context.WithCancel(t.Context())
	stager, _, destPath := stagerFor(t, cancelOnRetry{cancel})

	err := stager.Stage(ctx, server.dependency(body), destPath)
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "downloading "+server.url)
	assert.Equal(t, server.requests.Load(), int32(1))
}

func TestStageReportsADeadlineDuringTheRetryWait(t *testing.T) {
	shortenDownloadTimeout(t, 500*time.Millisecond)
	body := []byte("distro image")
	server := newAssetServer(t, body, http.StatusBadGateway)
	stager, _, destPath := stagerFor(t, nil)

	err := stager.Stage(t.Context(), server.dependency(body), destPath)
	assert.ErrorContains(t, err, "gave up after "+downloadTimeout.String())
	assert.ErrorContains(t, err, "502")
	assert.Equal(t, server.requests.Load(), int32(1))
}
GO
new='DuringTheRetryWait'
echo "== proposed tests, unmodified"
check "$new"
echo "== proposed tests, caller-cancel case disabled"
mutate "$cancel_case" 'case caller == nil:'; check "$new"; cp "$backup" "$stage"
echo "== proposed tests, retry wait returns bare ctx.Err()"
mutate "$wait_exit" 'return ctx.Err(); break attempts'; check "$new"; cp "$backup" "$stage"
```

```text repro-output
$ bash probe-retry-wait.sh
== current tests, caller-cancel case disabled
ok  	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	2.146s
== current tests, retry wait returns bare ctx.Err()
ok  	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	2.101s
== proposed tests, unmodified
ok  	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	0.866s
== proposed tests, caller-cancel case disabled
--- FAIL: TestStageReportsACancelDuringTheRetryWait (0.01s)
    retry_wait_probe_test.go:31: assertion failed: error is "downloading http://127.0.0.1:57514/distro.v0.2.7.amd64.raw.xz: unexpected status 502 Bad Gateway", not "context canceled" (context.Canceled)
FAIL	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	0.882s
== proposed tests, retry wait returns bare ctx.Err()
--- FAIL: TestStageReportsACancelDuringTheRetryWait (0.00s)
    retry_wait_probe_test.go:32: assertion failed: expected error to contain "downloading http://127.0.0.1:57519/distro.v0.2.7.amd64.raw.xz", got "context canceled"
--- FAIL: TestStageReportsADeadlineDuringTheRetryWait (0.50s)
    retry_wait_probe_test.go:43: assertion failed: expected error to contain "gave up after 500ms", got "context deadline exceeded"
FAIL	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	0.883s
```

Fix: add the two tests the probe writes to `stage_test.go`. They pass on the unmodified tree.

S2. **Progress reporting can still be unwired from the log with the suite green** — `rdd/pkg/guestdeps/stage.go:551` [Claude Opus 5]

```go
	r := &progressReader{
		body:   resp.Body,
		total:  resp.ContentLength,
		report: s.logProgress,
		next:   time.Now().Add(progressInterval),
	}
```

Round 2's I2 named deleting the `r.report` call in `Read`, and `TestProgressReaderReportsOnInterval` now catches that. Wiring `report` to a no-op is a neighbouring break with the same symptom, so I judged it fresh, as a Suggestion. Neither progress test goes through `watchTransfer`. One builds its own reader, and the other calls `logProgress` directly. The probe wires `report` to a no-op, then drops the field, and the current tests pass both times. The second break would also panic with a nil dereference on the first real download that runs past `progressInterval`, and no test transfer runs that long. The proposed test fails on both.

```bash repro
# Save as probe-progress-wiring.sh; run from the repository root: bash probe-progress-wiring.sh
# Needs go and perl. Wires watchTransfer's report to a no-op, then drops the
# field altogether, running the current guestdeps tests against each change,
# then adds a proposed test and runs it against both. stage.go is restored and
# the test file removed on exit.
set -u
cd rdd || exit 1
stage=pkg/guestdeps/stage.go
probe=pkg/guestdeps/progress_wiring_probe_test.go
backup=$(mktemp)
cp "$stage" "$backup"
trap 'cp "$backup" "$stage"; rm -f "$backup" "$probe"' EXIT

silence() {
  perl -pi -e 's/report: s\.logProgress,/report: func(int64, int64) {},/' "$stage"
  cmp -s "$stage" "$backup" && echo "  change not applied"
}
drop() {
  perl -ni -e 'print unless /^\s*report:\s+s\.logProgress,$/' "$stage"
  cmp -s "$stage" "$backup" && echo "  change not applied"
}
check() {
  go test -count=1 -run "$1" ./pkg/guestdeps/ 2>&1 |
    grep -E '^(ok|FAIL)[[:space:]]|--- FAIL|panic: |probe_test.go:[0-9]+' | head -6
}

echo "== current tests, report wired to a no-op"
silence; check .; cp "$backup" "$stage"
echo "== current tests, report field dropped"
drop; check .; cp "$backup" "$stage"

cat > "$probe" <<'GO'
package guestdeps

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// A body read through watchTransfer, once the interval has passed, reaches the
// stager's log.
func TestWatchTransferReportsToTheLog(t *testing.T) {
	var log bytes.Buffer
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader(make([]byte, 64))), ContentLength: 128}
	r := (&Stager{Log: &log}).watchTransfer(resp, func() {})
	t.Cleanup(r.stop)
	r.next = time.Now().Add(-time.Hour)

	_, _ = r.Read(make([]byte, 64))
	assert.Assert(t, strings.Contains(log.String(), "(50%)"), "log holds %q", log.String())
}
GO
new='TestWatchTransferReportsToTheLog'
echo "== proposed test, unmodified"
check "$new"
echo "== proposed test, report wired to a no-op"
silence; check "$new"; cp "$backup" "$stage"
echo "== proposed test, report field dropped"
drop; check "$new"; cp "$backup" "$stage"
```

```text repro-output
$ bash probe-progress-wiring.sh
== current tests, report wired to a no-op
ok  	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	2.116s
== current tests, report field dropped
ok  	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	2.109s
== proposed test, unmodified
ok  	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	0.374s
== proposed test, report wired to a no-op
--- FAIL: TestWatchTransferReportsToTheLog (0.00s)
    progress_wiring_probe_test.go:24: assertion failed: expression is false: strings.Contains(log.String(), "(50%)"): log holds ""
FAIL	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	0.364s
== proposed test, report field dropped
--- FAIL: TestWatchTransferReportsToTheLog (0.00s)
panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]
	/var/folders/s2/9q03h0c11_s4fdw169wqz3qc0000gn/T/review-lead-3357488858/rdd/pkg/guestdeps/progress_wiring_probe_test.go:23 +0x25c
FAIL	github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps	0.367s
```

Fix: add `TestWatchTransferReportsToTheLog` from the probe to `stage_test.go`.

S3. **The trickling test shortens a stall window it does not need** — `rdd/pkg/guestdeps/stage_test.go:479-485` [Claude Opus 5]

```go
	shortenStallTimeout(t)
	shortenDownloadTimeout(t, time.Second)

	// A tenth of the stall window leaves the stall timer no chance to fire, so
	// the deadline is what ends this, and it ends the first attempt: any retry
	// here would mean the transfer stalled instead of running out of time.
	server := newTricklingServer(t, stallTimeout/10)
```

The comment says the stall timer has no chance to fire. With a 50 ms window and a byte every 5 ms, a pause over about 45 ms fires it, and the retry that follows fails `requests == 1` at line 495. On Windows the margin is probably no wider, since the runtime's clock there steps on the 15.6 ms tick (reasoned). Nobody has seen it flake. Claude ran 100 iterations under `-race` beside 16 busy loops on 8 cores, I ran 60 the same way, and all passed. The mechanism under test is the deadline, and the default 30 s window cannot fire inside a one-second deadline.

Fix: drop `shortenStallTimeout(t)` at line 479 and trickle at a fixed 10 ms. With this change the test passes in 1.4 s and still fails at its 30 s guard when the deadline is deleted.

```diff
-	// A tenth of the stall window leaves the stall timer no chance to fire, so
-	// the deadline is what ends this, and it ends the first attempt: any retry
-	// here would mean the transfer stalled instead of running out of time.
-	server := newTricklingServer(t, stallTimeout/10)
+	// The stall window is thirty times the deadline, so only the deadline can
+	// end this, and it ends the first attempt.
+	server := newTricklingServer(t, 10*time.Millisecond)
```

S4. **The nerdctl comment says nothing fetches it, but rddepman downloads it to hash it** — `scripts/dependencies/nerdctl.ts:17` [Codex GPT 5.6 Sol]

```ts
/**
 * nerdctl, which the distro overlay will bake into the guest image.  rddepman
 * tracks it like any other GitHub dependency, so there is no host install.
 * Nothing fetches it yet; the Go downloader stages only the distro until the
 * overlay work lands.
 */
```

`getAssets` hashes each artifact through `downloadAndHash`, which downloads it first (`scripts/lib/dependencies.ts:329`).

rddepman calls `getAssets` whenever it bumps or regenerates a dependency (`scripts/rddepman.ts:283`, and lines 333 and 370). What nobody does yet is stage nerdctl for a build or install it on a host. Round 1's fix wrote this sentence, and round 2's fix corrected the rejection message at line 27 below it.

Fix: drop "Nothing fetches it yet;" and start the sentence at "The Go downloader stages only the distro".

---

## Design Observations

### Concerns

- **A throttling 403 is retried only if it names a delay** `(future)` [Claude Opus 5]. `TestStageRetriesAServerThatNamesADelay` stages a 403 with `Retry-After`, and nothing in the tree shows whether GitHub's release CDN sends that header. The GitHub throttling measured on this project in June 2026 slowed each connection to 0.3 to 0.6 MB/s; nobody recorded a 403. One captured throttled response would settle it.
- **A throughput floor would end a trickle within one stall window** `(future)` [Claude Opus 5]. The two-hour deadline bounds a trickling peer but lets it hold a build that long first. If the stall timer reset only after, say, 64 KiB per window, a throttled peer would fail within 30 s and the deadline would stay as a backstop. That floor is about 2 KiB/s, far under the 35.5 KiB/s the deadline already assumes.
- **Any 4xx that names a delay now retries** `(in-scope)`. The reorder that rescues a throttling 403 also retries a 404 or 410 with `Retry-After`. Four attempts and a 30 s ceiling on each wait cap the cost at about 90 s before the same failure, and the comment at line 393 accepts it ("whatever status it sent"). I'd leave it.
- **`missing` stats the cache entry a second time** `(in-scope)` [Claude Opus 5]. Checking whether the `*fs.PathError` from `copyFile`'s open names `cachePath` would identify the vanished entry without the extra `Stat`, and close the window between the failed open and the stat. That window needs a prune and a finished 250 MiB re-download between two system calls, so I'd call this tidiness.

### Strengths

- One deadline covers every attempt while the caller's context stays separate, so a cancelled build and an expired deadline report differently, and the stall timer stays per attempt [Claude Opus 5, Codex GPT 5.6 Sol, Gemini 3.1 Pro].
- The progress tests start `next` an hour in the past instead of timing a transfer, which keeps the Windows clock tick out of both [Claude Opus 5, Gemini 3.5 Flash].
- The new cache name checks out against every neighbour. `instance.Name()` puts a hyphen where the cache name has a dot, the Electron app's caches have their own names, RD1's factory reset deletes exact paths only, and nothing in either tree deletes by a `rancher-desktop*` glob [Claude Opus 5, Codex GPT 5.6 Sol, Gemini 3.1 Pro].

---

## Testing Assessment

From the lead worktree at `3ef059fe3`, `go test -race` and `go vet` pass over both packages, and `golangci-lint` reports 0 issues. Coverage is 88.6% of statements in `pkg/guestdeps` and 38.1% in `cmd/download-guest-deps`, where `main` itself is untested.

The suite now catches what round 2 asked of it. Breaking one mechanism at a time, it caught the shared deadline and its message, the header timeout, the progress report in `Read` and its percentage, the `Retry-After` ordering, the 408 exception, the cache name, the name guard, and both new URL checks.

Gaps, worst first:

1. **A cancel or deadline during the retry wait** (S1).
2. **The `watchTransfer` wiring** (S2).
3. **Round 2's S8 narrowing** [Claude Opus 5, Codex GPT 5.6 Sol]. Restoring the unconditional fallback at `stage.go:131` leaves the suite green. A test needs a destination-side ENOENT while the cache entry exists, which only a race inside `createTemp` or before the rename produces. That is the seam the author declined to add for `createTemp`'s retry in round 2.
4. **CI has not run on this SHA** [Claude Opus 5, Codex GPT 5.6 Sol]. The PR's green checks belong to `14b43bf81`. The new timing tests are least proven on `windows-latest`, which no reviewer can run.

---

## Documentation Assessment

Round 2's two documentation gaps are closed. `progressReader`'s comment now leaves the transfer's bound to `downloadTimeout`, and `touch`'s comment says why it refreshes an entry it never verifies. No prose in the tree still names the old cache path.

1. **`downloadTimeout`'s comment holds only for a transfer that never drops** (`rdd/pkg/guestdeps/stage.go:41`). "No working link reaches it" assumes one uninterrupted transfer. A retry restarts from byte zero, so for the 261,668,428-byte amd64 image a link that drops once near the end needs 71 KiB/s, and one that drops three times needs 142 KiB/s, where the comment names 36. Round 2's resolution notes weighed this trade-off, and the GitHub throttle this project measured (0.3 to 0.6 MB/s) clears even four full transfers, so only the sentence overstates. The 250 MiB figure will also go stale unseen when a distro release outgrows it, because the manifest records no sizes [Claude Opus 5]. Suggested text: "The largest asset the manifest names is 250 MiB, which arrives in time at 36 KiB/s, or at 71 KiB/s when a retry starts over."

---

## Commit Structure

Clean. `d47fa7dac` is the Go tool with its tests, and its body now describes the two-hour bound and the capped `Retry-After` wait. `3ef059fe3` is the nerdctl comment and rejection message, and its body matches that diff.

---

## Acknowledged Limitations

- **No Windows host.** Every Windows timing claim here is reasoned from the 15.6 ms tick. Both packages cross-compile for Windows (Codex ran `go test -c`).

### Declined in prior rounds

- **Concurrent stagers sharing one cache can collide on Windows** — declined round 1: documented on `finishTemp`; a lock file would add its own stale-lock failure mode, and the collision is loud and clears on a re-run. The comment is still at `stage.go:486-489`, and this round leaves that path alone.
- **Nothing records that `rdd/pkg/embedded/` is a build output** — declined round 1: #708 adds the Makefile rule, and the documentation belongs there. At this SHA the directory is absent and no Makefile names `download-guest-deps`.
- **`touch` refreshes a cache entry it never verified** — declined round 2: verifying would cost the hash the fast path exists to avoid. `touch`'s comment at `stage.go:143-147` now says so.
- **Nothing collects a temporary file stranded in the destination directory** — declined round 2: it belongs with #708, which creates `pkg/embedded`.
- **`createTemp`'s ENOENT retry is untested** — declined round 2: no deterministic test can stage the race without an injection seam.
- **`distro.ts` says the build stages the distro** — declined round 2: it is outside this diff, and #708 makes it true. The message is at `scripts/dependencies/distro.ts:29`.

---

## Agent Performance Retro

### [Claude Opus 5]

Claude found three of the four suggestions and was the only reviewer to test the `watchTransfer` wiring. It asked whether the progress tests go through the production constructor, and noticed that neither does. It kept to the prompt's rule on mutations, naming each one for me to run and using `go test -overlay` only to add its proposed tests; all of them passed my runs and caught every mutation it predicted. Its round-2 verdicts each name their evidence and say which claims it reasoned and which it ran. It listed RD1's factory reset as unchecked, and I checked it.

### [Codex GPT 5.6 Sol]

Codex found S4, a sentence the other three reviewers read past, and ranked the retry-wait gap first among its untested scenarios. It reasoned every mutation instead of running one, as the prompt asks, and its predictions match my runs. It ran into two environment problems. `go test ./...` needs the `lima-guestagent.gz` build artifact; the repo context says so, but a line I added for this round said `go test ./...` works. And `gh` returned HTTP 401 for PR comments. Its sandbox also refused an `rm -f`, but it removed its scratch executables another way.

### [Gemini 3.1 Pro]

Gemini Pro approved with no findings and, unlike Flash, gave a verdict with evidence for each round-2 fix. Two of its line ranges, for the deadline and the `Retry-After` check, point 22 to 35 lines above the code; the rest are exact, and every test it cites exists. It called the coverage exhaustive, though the suite missed four of the 15 mutations I ran. Its output file holds a dump of its reasoning and two drafts of the review; the appendix keeps the second.

### [Gemini 3.5 Flash]

Flash approved with one suggestion, `os.ErrNotExist` beside `fs.ErrNotExist`, which I dropped as a style point on a line this round did not touch. It gave no verdict on any named round-2 fix, so its approval rests on nothing I can check. It also wrote that every edge case is thoroughly tested, yet the suite missed four of my 15 mutations, and it cited a main.go under pkg/guestdeps that does not exist. The two Gemini models agree, and I count that as one opinion.

### Summary

| | Claude Opus 5 | Codex GPT 5.6 Sol | Gemini 3.1 Pro | Gemini 3.5 Flash |
|---|---|---|---|---|
| Duration | 20m 42s | 19m 30s | 3m 20s | 4m 11s |
| Findings | 3S | 2S | none | none |
| Tool calls | 43 (Bash 42, Read 1) | — | 8 (run_shell_command 7, read_file 1) | 26 (read_file 11, run_shell_command 8, grep_search 3) |
| Design observations | 5 | 3 | 3 | 3 |
| False positives | 0 | 0 | 0 | 0 |
| Unique insights | 5 | 1 | 0 | 0 |
| Files reviewed | 8 | 8 | 8 | 8 |
| Coverage misses | 0 | 0 | 0 | 0 |
| **Totals** | **3S** | **2S** | **none** | **none** |
| Downgraded | 0 | 0 | 0 | 0 |
| Dropped | 0 | 0 | 0 | 1 |


**Reconciliation.** Codex's untested-scenario note on the retry wait joins Claude's finding as S1, so Codex is credited with it. Claude's own findings map S1 → S3, S2 → S1 and S3 → S2. Flash's only suggestion was dropped as below the round's bar; its claim is accurate. No severity changed.

Claude and Codex between them found everything in this report, and every finding they made passed verification. The wall-clock time includes the overnight wait on the two scope questions before any agent launched; the table's durations do not.

---

## Review Process Notes

### Skill improvements

- When a test builds the unit under test itself, with a struct literal or by calling a helper directly, check that some test still goes through the production constructor. Break the wiring the constructor supplies, a callback or a field assignment, and confirm the suite notices. Coverage cannot show this gap, because both halves are covered.
- Recompute any number a comment gives to justify a limit, under the worst case the code permits. A retry that restarts from zero, a cold cache, or a full queue changes the arithmetic, and checking the comment's own case confirms the sentence without testing it.
- Anchor the reported wall-clock time at the first agent launch, or report the wait before it separately. The start marker is written before the scope questions, so an answer that arrives hours later inflates the time with a wait no agent spent.
- When the review target is a branch with an open PR, count prior rounds under both the branch ID and `pr-<N>`, and say in the report which series it continues. Otherwise a branch-mode round of a PR restarts the count at 1, and the next PR-mode round repeats a round number.

### Repo context updates

- [repo] Decline the pending round-2 suggestion that says `logProgress` and `ResponseHeaderTimeout` have no tests because `progressInterval` is a constant and `httpClient` reads `stallTimeout` once at package init. Tests now cover both, through `newHTTPClient` and a reader built with `next` already past, so a reviewer given that entry would look for a gap that no longer exists.

---

## Appendix: Original Reviews

### [Claude Opus 5]

#### Executive Summary

Round 3 of PR #711 adds a two-hour deadline covering all download attempts. It also retries any status that carries `Retry-After`, plus 408; moves the cache to `rancher-desktop.guest-deps`; tightens manifest validation; and backfills tests. Every round-2 fix closes its finding, and I found no defect that the fixes introduce. What remains is test coverage. Each of two mechanisms can be broken with the suite still green: the retry-wait exits of `download`, and the progress wiring in `watchTransfer`. The trickling test also leans on a scheduling margin of about 45 ms that it doesn't need.

#### Findings

##### Critical Issues

None.

##### Important Issues

None.

##### Suggestions

S1. **The trickling test shortens `stallTimeout`, which it doesn't need, so one scheduling pause can fail it** — `rdd/pkg/guestdeps/stage_test.go:479-485` (suggestion, gap)

```go
	shortenStallTimeout(t)
	shortenDownloadTimeout(t, time.Second)

	// A tenth of the stall window leaves the stall timer no chance to fire, so
	// the deadline is what ends this, and it ends the first attempt: any retry
	// here would mean the transfer stalled instead of running out of time.
	server := newTricklingServer(t, stallTimeout/10)
```

The stall timer can fire. The window is 50 ms and a byte arrives every 5 ms, so the client can go only about 45 ms without a read. A longer pause on a loaded runner causes a stall, then a retry, then a failure at `requests == 1` (line 495). On Windows the 15.6 ms tick rounds the gap up to one tick and the window to 46.9–62.5 ms, so the margin is about the same, not larger (reasoned). The test passed 100 of 100 runs under 2x CPU oversubscription on macOS, so this makes the test sturdier; it doesn't fix an observed flake. The mechanism under test is the deadline, and the default 30 s window cannot fire inside a 1 s deadline.

Fix: keep the default window. Deleting the deadline still turns the test red at the 30 s guard.

```diff
 	shortenRetries(t)
-	shortenStallTimeout(t)
 	shortenDownloadTimeout(t, time.Second)
 
-	// A tenth of the stall window leaves the stall timer no chance to fire, so
-	// the deadline is what ends this, and it ends the first attempt: any retry
-	// here would mean the transfer stalled instead of running out of time.
-	server := newTricklingServer(t, stallTimeout/10)
+	// The stall window is thirty times the deadline, so only the deadline can
+	// end this, and it ends the first attempt.
+	server := newTricklingServer(t, 10*time.Millisecond)
```

S2. **The retry-wait exits of `download` can be broken with the suite green** — `rdd/pkg/guestdeps/stage.go:346-350` (suggestion, gap)

```go
			select {
			case <-ctx.Done():
				break attempts
			case <-time.After(wait):
			}
```

After `break attempts`, `err` still holds the previous attempt's failure. The `caller` case (lines 365-366) is what makes a Ctrl-C during a retry wait report cancellation instead of "unexpected status 502". No test ends the context during a wait. `TestStageCleansUpWhenCancelled` (stage_test.go:447-452) cancels mid-body, where the transport already returns `context.Canceled` (probe, go1.27.1). So the default branch at line 370 satisfies its `ErrorIs` even without the `caller` case.

Two mutations for the consolidator, both expected to leave today's suite green: delete lines 365-366, or restore `return ctx.Err()` at line 348.

Fix: pin both exits. This test passes 3 of 3 runs under `-race` on the unmodified tree (added with `go test -overlay`):

```go
type cancelOnRetry struct{ cancel context.CancelFunc }

func (w cancelOnRetry) Write(p []byte) (int, error) {
	if bytes.HasPrefix(p, []byte("Retrying in ")) {
		w.cancel()
	}
	return len(p), nil
}

func TestStageReportsACancelDuringTheRetryWait(t *testing.T) {
	body := []byte("distro image")
	server := newAssetServer(t, body, http.StatusBadGateway)
	ctx, cancel := context.WithCancel(t.Context())
	stager, _, destPath := stagerFor(t, cancelOnRetry{cancel})

	err := stager.Stage(ctx, server.dependency(body), destPath)
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "downloading "+server.url)
	assert.Equal(t, server.requests.Load(), int32(1))
}
```

A deadline twin also passes 3 of 3. It uses `shortenDownloadTimeout(t, 500*time.Millisecond)` and the same server that fails once with 502, keeps the default 2 s retry delay, and asserts `"gave up after "+downloadTimeout.String()` and `"502"`.

S3. **The link from `watchTransfer` to the log is still unpinned** — `rdd/pkg/guestdeps/stage.go:551` (suggestion, gap)

```go
	r := &progressReader{
		body:   resp.Body,
		total:  resp.ContentLength,
		report: s.logProgress,
		next:   time.Now().Add(progressInterval),
	}
```

Round 2 I2 found that progress reporting could be deleted with the suite green. Part of that remains. `TestProgressReaderReportsOnInterval` builds its own reader (stage_test.go:561-566), and `TestLogProgress` calls `logProgress` directly (line 549), so neither test goes through `watchTransfer`. Replacing line 551 with `report: func(int64, int64) {}` should leave the suite green (a mutation for the consolidator). Dropping the field would also pass every test, because no test transfer lasts `progressInterval`, and would then panic on the first real transfer that does.

Fix: send one read through `watchTransfer`. This passes on the unmodified tree:

```go
func TestWatchTransferReportsToTheLog(t *testing.T) {
	var log bytes.Buffer
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader(make([]byte, 64))), ContentLength: 128}
	r := (&Stager{Log: &log}).watchTransfer(resp, func() {})
	t.Cleanup(r.stop)
	r.next = time.Now().Add(-time.Hour)

	_, _ = r.Read(make([]byte, 64))
	assert.Assert(t, strings.Contains(log.String(), "(50%)"), "log holds %q", log.String())
}
```

#### Round 2 Fix Verdicts

Where a verdict below says a test fails if a mechanism is removed, that outcome is reasoned from the test's assertions. I ran no mutated code.

1. **Overall download deadline: closes round 2 I1; no defect introduced.** I traced every exit from the attempts loop in `stage.go`:
   - **Success** returns at line 357, after `finishTemp` has renamed the temp file into place.
   - **A fatal error** breaks at line 361 and returns at line 370. After a checksum failure, the temp file is removed by the deferred cleanup in `downloadOnce` (lines 413-418). A 4xx returns before `createTemp` runs (line 409).
   - **A stall** cancels only the attempt's context (lines 379 and 554-557). Line 360 sees that `download`'s context is still alive and retries. The fourth stall ends at line 370.
   - **The deadline mid-transfer:** the body read returns `context.DeadlineExceeded` (probe). Line 360 breaks, line 367 reports "gave up after", and the deferred cleanup removes the temp file. `TestStageGivesUpOnATricklingTransfer` passes with one request and no temp files.
   - **The deadline during a retry wait:** line 348 breaks and line 367 wraps the previous attempt's error; that attempt has already removed its temp file. My overlay test confirms this ("gave up after 500ms", "502").
   - **Caller cancellation**, mid-transfer or during a wait, reaches line 365. It is confirmed by the overlay test and by `TestStageCleansUpWhenCancelled`. The suite itself has no test for the wait-path exits (S2).
2. **Untested round-2 mechanisms: mostly closed.**
   - `TestStageGivesUpOnATricklingTransfer`: without the deadline, the trickle runs until the 30 s guard fails the test. It depends on timing (S1).
   - `TestStageFailsAPeerThatNeverSendsHeaders`: without `ResponseHeaderTimeout`, `Do` waits for the two-hour deadline and the 30 s guard fails the test. The timeout it waits for is the event it expects, so a slow runner only delays it. The test is portable.
   - `TestLogProgress`: pins both output formats.
   - `TestProgressReaderReportsOnInterval`: fails if `Read` stops reporting or stops resetting `next`. It reads no clock that the Windows tick can collapse, because `next` starts an hour in the past. The `watchTransfer` wiring is still unpinned (S3).
   - `TestStageRetriesAServerThatNamesADelay` fails in two cases:
     - The `Retry-After` parse moves back below the 4xx check: the 403 becomes fatal.
     - The parse is dropped: 429 logs "Retrying in 1ms", not "10ms".
     
     It measures no elapsed time.
   - `TestStageRetriesARequestTimeout`: fails without the 408 exception.
   - `TestDefaultCacheDirIsNobodyElses` and `TestLoadManifestRejectsAnEscapingDependencyName`: each fails if its change is reverted.
   - `TestRunWarnsWhenTheCacheCannotBePruned`: fails if `run` returns the prune error or drops the warning. It ran here rather than skipping; it skips on Windows and when run as root.
3. **`Retry-After` before the 4xx check, 408 retriable: closes round 2 S1** (lines 395-405). An open question about GitHub's 403s is under Design Observations.
4. **Cache directory moved: the comment's claim holds.**
   - `instance.Name()` is `"rancher-desktop-" + Suffix()` (instance.go:42-43). Every instance directory (`Dir()`, line 54) and the Windows log directory (`Name()+"-logs"`, line 77) therefore has a hyphen where the new name has a dot.
   - The RD2 app cache is named after `basename(instance.Dir())` on macOS and Linux, and sits inside `instance.Dir()` on Windows (paths.ts:123-139). RD1 uses plain `rancher-desktop` (paths.ts:185, 196, 201).
   - `rdd svc delete` removes `instance.Dir()`, the log directory, and `~/.rd<N>` (service.go:529-548).
   - Nothing in the tree deletes by a `rancher-desktop*` glob.
   - Not checked: RD1's factory reset (not in this tree) and electron-updater's own cache (no `updaterCacheDirName` in `packaging/`).
5. **nerdctl rejection message: closes round 2 S3.** `Nerdctl` is absent from `scripts/postinstall.ts`, and its line 95 is the only caller of `download()`. The rejection is therefore an unreachable guard, and the new message is true.
6. **Dependency name guarded: closes round 2 S4** as defence in depth (manifest.go:69-71). The new test fails without the guard.
7. **Hostless URLs and trailing slashes rejected: closes round 2 S7** (manifest.go:115-122). Each new table row fails without its check. The "several retries into a build" rationale is reasoned: `Do` returns "http: no Host in request URL" at stage.go:386-389, and that error is not fatal, so it gets retried.
8. **Vanished-entry fallback narrowed: closes round 2 S8** for the reachable destination-side ENOENT (lines 130-133 and 506-509). One residual race, too narrow for a finding: `missing` looks at the file a second time, after the failed open. An entry that is pruned and then re-downloaded between those two looks would fail the build. Checking the `*fs.PathError` from `os.Open` (its `Path` equals `cachePath`) would remove both the race and the extra stat.
9. **Comment updates: all match the code.** The figures in the `downloadTimeout` comment check out. The largest asset, `distro.v0.2.7.amd64.raw.xz`, is 261,668,428 bytes by HEAD request, which is 249.5 MiB. Spread over 7,200 s that is 35.5 KiB/s, which the comment rounds to 36. The only overstatement is "no chance to fire" in the trickling test (S1).

#### Design Observations

**Concerns**

- **(future) GitHub's throttling 403s may not carry `Retry-After`.** The new ordering rescues a 403 only if the response names a delay. Whether GitHub's throttling 403s on release downloads include `Retry-After` can't be determined from the tree; the test (stage_test.go:589-591) stages a 403 that does. If GitHub signals the delay some other way, the 403 stays fatal, as it was before this round. A single captured throttled response would settle it.
- **(future) A throughput floor would end a trickle much sooner than the deadline.** The two-hour deadline bounds a trickle, but a trickle can still hold CI for two hours before it fails. Instead, `progressReader` could reset the stall timer only after a minimum number of bytes, say 64 KiB per `stallTimeout` window, rather than after any byte. A throttled peer would then fail within one window, and the deadline would remain as a backstop. The floor needs to sit well under the 35.5 KiB/s the deadline already assumes; 64 KiB per 30 s is about 2 KiB/s.

**Strengths**

- One deadline spans all attempts, and the caller's context is kept separately, so a cancelled build and an expired deadline report differently. The stall timer stays per attempt (stage.go:336-338, 377-380).
- The progress tests drive the reader with a `next` already in the past instead of timing a transfer, which takes the Windows tick out of both (stage_test.go:559-579).
- When `missing` cannot answer, it counts the file as present, so an unexpected stat error surfaces instead of triggering a download (stage.go:503-509).

#### Testing Assessment

1. Cancellation or deadline during a retry wait (S2). The fix's stated purpose, telling a cancelled build from an expired deadline, depends on this path.
2. The `watchTransfer` to `logProgress` wiring (S3).
3. The round-2 S8 narrowing: with the old unconditional check restored at line 131, the suite stays green. Staging it would need `createTemp`'s retry to fail twice, which the author already declined to test (see Acknowledged Limitations).
4. A caller context with its own deadline goes through the `caller` case and does not report "gave up after". No caller has such a deadline today.
5. The round-3 tests have not run on windows-latest. GitHub's latest CI run is on `14b43bf81`, where every check is green.

#### Documentation Assessment

- The `downloadTimeout` comment quotes the largest asset's size (250 MiB). The manifest records no sizes, so nothing will flag the figure when a distro release outgrows it. It is accurate today.
- The injected review overview says Go 1.26.0, but `rdd/go.mod` says `go 1.27.0`. That text is not in the working tree (no `CLAUDE.md` or `AGENTS.md` mentions 1.26), so there is nothing to fix in this PR.

#### Acknowledged Limitations

- `finishTemp` can hit a Windows sharing violation (stage.go:486-489; declined in round 1). This round leaves it unchanged.
- `touch` refreshes an entry it never verified (stage.go:143-147; declined in round 2). The comment now records the trade-off.
- `createTemp`'s ENOENT retry has no test (stage.go:465-481; declined in round 2). The same obstacle leaves the S8 narrowing untested.
- `scripts/dependencies/distro.ts:28-29` still says the build stages the distro (declined in round 2; #708 makes it true). It now reads differently from nerdctl's corrected message until #708 lands.
- Nothing collects a temp file stranded in the destination directory, and nothing records that `pkg/embedded/` is build output. Both are deferred to #708.
- `isVersionDir` never collects a version that doesn't start with a digit (stage.go:193-200); the comment documents this.

#### Coverage Summary

| File | Result |
|---|---|
| `rdd/.gitignore` | Trivial. Unchanged this round; ignores the staged distro and its temp files. |
| `rdd/cmd/download-guest-deps/main.go` | Reviewed, no issues. Unchanged this round; the prune warning at lines 72-74 is now pinned. |
| `rdd/cmd/download-guest-deps/main_test.go` | Reviewed, no issues. `seedRun` and the new prune-warning test were checked against `run`. |
| `rdd/pkg/guestdeps/manifest.go` | Reviewed, no issues. |
| `rdd/pkg/guestdeps/manifest_test.go` | Reviewed, no issues. |
| `rdd/pkg/guestdeps/stage.go` | S2, S3 |
| `rdd/pkg/guestdeps/stage_test.go` | S1, S2, S3 |
| `scripts/dependencies/nerdctl.ts` | Reviewed, no issues. |

This round adds a build-time tool under `rdd/cmd/`, a new category for the tree. It is not a controller, and `make test` (`go test ./...`, Makefile:158-159) runs its tests.

Probes and experiments. All ran against the unmodified tree unless noted; I started no rdd instance.

1. `go test -race -count=1 -v ./pkg/guestdeps/ ./cmd/download-guest-deps/` with go1.27.1: all 44 top-level tests pass, and none skipped.
2. Stress test: 100 iterations of `TestStageGivesUpOnATricklingTransfer`, built with `-race`, alongside 16 busy loops on 8 cores. 100 of 100 passed in 102 s.
3. Transport probe in `.scratch/probe` (go1.27.1): a mid-body cancel returns `"context canceled"`, and an expiring parent deadline mid-body returns `"context deadline exceeded"`.
4. Proposed tests added through `go test -overlay`, touching no tree file: the cancel-during-wait and deadline-during-wait tests pass 3 of 3 under `-race`, and the `watchTransfer` test passes.
5. `go tool golangci-lint run` with the repo config on both packages: 0 issues.
6. HEAD requests for the four v0.2.7 distro assets: 261,668,428, 217,409,580, 185,195,056, and 156,577,696 bytes.
7. `gh pr view 711`: the head is `14b43bf81` and all its checks are green. CI has not run on `3ef059fe3`.
8. `git blame` on every line I cite attributes it to `d47fa7dac`.
9. Not run, left for the consolidator per the instructions: the mutations named in S2 and S3.

### [Codex GPT 5.6 Sol]

#### Review: `guest-deps-downloader`

| Field | Value |
|---|---|
| Verdict | **APPROVE** |
| Reviewed SHA | `3ef059fe3e2a1727024214fb67bdc47871de5643` |
| Merge base | `2eb7aa3af1d75288c1e09763faf111ffe18fdd58` |

###### Executive Summary

The branch adds a checksum-verifying, deadline-bounded guest dependency downloader with shared caching and pruning. The round-three fixes close the prior functional findings without introducing a correctness defect. One low-impact wording gap remains: the revised nerdctl comment says nothing fetches the artifact even though rddepman fetches it while resolving the manifest.

###### Findings

###### Critical Issues

None.

###### Important Issues

None.

###### Suggestions

S1. **Distinguish staging from rddepman’s artifact fetch** — `scripts/dependencies/nerdctl.ts:15-18` (suggestion, gap)

```ts
/**
 * nerdctl, which the distro overlay will bake into the guest image.  rddepman
 * tracks it like any other GitHub dependency, so there is no host install.
 * Nothing fetches it yet; the Go downloader stages only the distro until the
 * overlay work lands.
 */
```

The statement at line 17 is still literally inaccurate: `getAssets()` calls `downloadAndHash()` at line 37, whose implementation downloads the artifact at `scripts/lib/dependencies.ts:329`. Rddepman invokes `getAssets()` during local bumps, CI bumps, and regeneration at `scripts/rddepman.ts:283`, `:333`, and `:370`. The intended distinction is that nothing stages or installs nerdctl yet.

Fix: Replace “Nothing fetches it yet” with “Nothing stages or installs it yet.”

###### Round 2 Fix Verification

| Item | Verdict and evidence |
|---|---|
| Overall download deadline | **Closed; no defect found.** The context created at `stage.go:337` spans all attempts at line 342, retry waits at lines 346-350, and transfers at line 355. Caller cancellation takes precedence at lines 365-368. Failure cleanup removes an opened temporary file at lines 413-417. |
| Previously untested mechanisms | **Closed.** All nine named tests execute their intended branches and pass. The timing-focused subset also passed 20 consecutive runs in 26.5 seconds, and both affected packages passed under `-race`. |
| Retry-After ordering and HTTP 408 | **Closed; no defect found.** Retry-After is parsed at lines 395-396 before fatal 4xx classification at lines 401-405; 408 is explicitly retriable at line 403. Tests assert both request counts and selected delay. |
| Cache directory relocation | **Closed; no defect found.** `DefaultCacheDir()` returns the isolated name at `stage.go:93`. `instance.Name()` always inserts `rancher-desktop-` at `instance.go:43`; Electron caches use `rancher-desktop-<suffix>` or `rancher-desktop` at `paths.ts:124`, `:129`, `:134`, `:185`, and `:196`. |
| nerdctl rejection message | **Functionally closed.** The error at `nerdctl.ts:27` now accurately says postinstall does not install it; `postinstall.ts:31-62` contains no Nerdctl registration. The adjacent terminology gap is S1. |
| Dependency-name guard | **Closed; no defect found.** `LoadManifest()` rejects non-elements at `manifest.go:69-70`, and `manifest_test.go:168-179` exercises an escaping map key. |
| Hostless and directory URLs | **Closed; no defect found.** Empty hosts are rejected at `manifest.go:115-116`, and decoded paths ending in `/` are rejected at lines 118-121. Both cases pass their tests. |
| Narrow vanished-cache fallback | **Closed; no defect found.** The condition at `stage.go:130-132` retries only for `ENOENT` when `cachePath` is itself absent; destination-side errors propagate. |
| Updated comments | **Closed.** The timeout comments at `stage.go:32-42` and `:530-533`, cache comment at `:81-87`, touch comment at `:143-147`, and attempt-context comment at `:376-379` match their implementations. |

Mutation sensitivity was reasoned rather than executed because the review explicitly prohibits testing modified code:

- Removing the overall deadline leaves `TestStageGivesUpOnATricklingTransfer` until its 30-second failure guard.
- Removing the response-header timeout makes `TestStageFailsAPeerThatNeverSendsHeaders` hit its 30-second guard.
- Changing progress formatting breaks `TestLogProgress`’s exact strings.
- Removing interval reporting or rescheduling breaks the recorded slices in `TestProgressReaderReportsOnInterval`.
- Ignoring Retry-After makes the forbidden case terminate after one request or use the wrong logged delay.
- Making 408 fatal reduces its test’s request count from two to one.
- Reverting the cache directory breaks the exact path and prefix assertions.
- Removing the dependency-name guard makes the expected manifest error disappear.
- Removing the prune warning breaks the captured-log assertion.

###### Design Observations

###### Strengths

- The separate overall and stall deadlines clearly model two different failure modes while preserving caller cancellation as the primary error.
- Downloads are hashed while streaming and published only after verification through a synced temporary-file rename.
- Cache pruning is constrained to the directory layout created by the downloader, reducing the risk of deleting unrelated cache contents.

###### Testing Assessment

Untested scenarios, ranked by risk:

1. Deadline expiry and caller cancellation during the retry wait at `stage.go:346-350`; current tests exercise both conditions during active transfers.
2. A destination-side `ENOENT` while the verified cache entry remains present, which would pin the negative half of the new condition at `stage.go:130-132`.
3. Runtime execution on Windows. Both packages cross-compile successfully, but no Windows host was available; the permission-based prune-warning test intentionally skips there.

Verification performed:

| Command | Result |
|---|---|
| `go test ./pkg/guestdeps` | Passed |
| `go test ./cmd/download-guest-deps` | Passed |
| `go test -race ./pkg/guestdeps ./cmd/download-guest-deps` | Passed |
| Focused round-three tests with `-count=20` | Passed; 26.5 seconds |
| Focused tests with `-v -count=1` | Every selected test passed, including the prune-warning test without skipping |
| Windows `go test -c` for both packages | Passed; scratch executables removed afterward |
| `go test ./...` | Initially failed on the known absent `lima-guestagent.gz` prerequisite |
| `make test` | Passed after generating that prerequisite; generated artifact removed afterward |
| `git diff --check` | Passed with no diagnostics |

The trickling-transfer test uses a 50 ms stall window, a nominal 5 ms trickle, and a one-second overall deadline. Even with Windows’ approximately 15.6 ms timer granularity, the effective trickle remains below the stall window. The progress tests avoid depending on a measurable sub-tick interval.

###### Documentation Assessment

S1 is the only documentation gap. Exported Go symbols have appropriate Godoc. User-facing build documentation can remain deferred because no Makefile or production path invokes the new command yet.

###### Acknowledged Limitations

- **Code comment:** “On Windows the rename can fail with a sharing violation…” at `rdd/pkg/guestdeps/stage.go:486-489`. The author accepted this loud, rerunnable collision instead of adding stale-lock recovery. The new deadline does not materially change its likelihood.
- **PR comment:** Documentation identifying `rdd/pkg/embedded/` as generated output remains deferred to #708, which will add the Makefile rule. The directory is absent and no current Makefile target calls this command, consistent with the rationale.
- **Code comment:** “Nor does touch verify the entry…” at `rdd/pkg/guestdeps/stage.go:145-147`. Avoiding a full fast-path hash deliberately permits a corrupt unused cache entry to have its TTL refreshed; it is verified when actually needed.
- **PR comment:** Collection of destination-directory temporary files after an abrupt process death remains deferred to #708. Context-driven errors clean up at `stage.go:452-456`; only ungraceful termination leaves this state.
- **PR comment:** Deterministic coverage of `createTemp()`’s `ENOENT` retry remains omitted because reproducing the `MkdirAll`/`CreateTemp` race requires an injection seam. The best-effort retry remains at `stage.go:473-480`.
- **PR comment:** The forward-looking statement in `scripts/dependencies/distro.ts:19-20` remains deferred to #708, which will wire the downloader into the build.
- **Code comment:** Versions not starting with a digit are intentionally never pruned at `stage.go:193-197`. Current manifest versions (`0.2.7` and `2.2.2`) satisfy the pruning grammar.

Live PR-comment retrieval was attempted with `gh pr view` and the REST endpoints, but both returned HTTP 401. The author dismissals supplied in the review prompt are all represented above.

###### Coverage Summary

The new command and Go package were read end-to-end, including manifest selection, cache state transitions, retry classification, cancellation, temporary-file cleanup, pruning, and tests.

- `rdd/.gitignore` — **Trivial**
- `rdd/cmd/download-guest-deps/main.go` — **Reviewed, no issues**
- `rdd/cmd/download-guest-deps/main_test.go` — **Reviewed, no issues**
- `rdd/pkg/guestdeps/manifest.go` — **Reviewed, no issues**
- `rdd/pkg/guestdeps/manifest_test.go` — **Reviewed, no issues**
- `rdd/pkg/guestdeps/stage.go` — **Reviewed, no issues**
- `rdd/pkg/guestdeps/stage_test.go` — **Reviewed, no issues**
- `scripts/dependencies/nerdctl.ts` — **S1**

### [Gemini 3.1 Pro]

Gemini's raw output opens with its reasoning and holds two drafts of the review. This is the second, final one.

#### Executive Summary

This PR adds `download-guest-deps`, a Go tool that parses a dependency manifest and securely downloads, hashes, and stages guest dependencies for the build system. The author's latest commit expertly addresses all round 2 findings, adding comprehensive test coverage, solidifying deadlines, fixing path traversals, and correctly sandboxing the cache directory. The implementation is robust and ready to merge.

**Verdict:** Merge.

#### Findings

##### Critical Issues
None.

##### Important Issues
None.

##### Suggestions
None.

#### Design Observations

**Concerns**
None.

**Strengths**
- The separation of context lifecycles—using an overarching `downloadTimeout` alongside a per-attempt `cancel` fired by an independent `stallTimeout` timer—is a masterclass in bounding both total wall-clock time and immediate network responsiveness without introducing leaks or data races.
- Moving the cache to `rancher-desktop.guest-deps` correctly segregates build tooling from the runtime daemon, preventing `rdd svc delete` from unexpectedly purging developer caches.
- Leveraging `MoveFileEx(MOVEFILE_REPLACE_EXISTING)` semantics for Windows atomic replacement through `os.Rename` correctly avoids lock folklore.

#### Testing Assessment

The test suite provides exceptional coverage of the newly added mechanisms, verifying network timeouts, retries, exponential backoffs, payload corruption, and directory permission edge cases. The test harness cleanly isolates the environment by spoofing HTTP responses (trickling, throttling, failing) and replacing `httpClient` for rapid execution.

#### Documentation Assessment

Inline comments accurately describe the behaviors (such as `createTemp`'s retry loop and `touch`'s deliberate avoidance of full validation) and reflect the updated architectures. The rationale for missing dependencies (`nerdctl.ts`) has been clarified.

#### Prior-Round Verification

1. **Overall download deadline**: Closes the finding. Introduces no defect. Evidence: `rdd/pkg/guestdeps/stage.go:309-338` applies `downloadTimeout = 2 * time.Hour` to a parent context (`ctx, cancel := context.WithTimeout(ctx, downloadTimeout)`) that wraps the loop. If it fires, the loop is broken and `ctx.Err() == context.DeadlineExceeded` routes to a clear error message. Tested with `TestStageGivesUpOnATricklingTransfer`.
2. **Untested round-2 mechanisms**: Closes the finding. Introduces no defect. Evidence: `rdd/pkg/guestdeps/stage_test.go` and `main_test.go` add exhaustive coverage for stalls (`TestStageFailsAPeerThatNeverSendsHeaders`, `TestStageFailsAStalledTransfer`), trickling peers, named delays, request timeouts, and caching behavior. `httpClient` is correctly made dynamic for test isolation.
3. **Retry-After before the fatal-4xx test; 408 retriable**: Closes the finding. Introduces no defect. Evidence: `rdd/pkg/guestdeps/stage.go:360-372` evaluates the `Retry-After` header before checking the 400-499 range. `http.StatusRequestTimeout` is excluded from the fatal list.
4. **Cache directory moved**: Closes the finding. Introduces no defect. Evidence: `rdd/pkg/guestdeps/stage.go:82-89`. `DefaultCacheDir()` correctly uses `os.UserCacheDir()` and suffixes `rancher-desktop.guest-deps`, which sits outside `instance.Name()` (`rancher-desktop-` + suffix) and clears the App's own cache subdirectories (verified against `utils/paths.ts`).
5. **nerdctl rejection message**: Closes the finding. Evidence: `scripts/dependencies/nerdctl.ts:27` updates the error to `'nerdctl is a guest-only dependency and is not installed by postinstall'`.
6. **Dependency name guarded**: Closes the finding. Introduces no defect. Evidence: `rdd/pkg/guestdeps/manifest.go:69` runs `isPathElement(name)` on manifest keys, preventing traversal into the cache directory.
7. **URL with no host, and trailing-slash paths, rejected**: Closes the finding. Introduces no defect. Evidence: `rdd/pkg/guestdeps/manifest.go:114-121` rejects `Hostname() == ""` and trailing slashes.
8. **Vanished-entry fallback narrowed**: Closes the finding. Introduces no defect. Evidence: `rdd/pkg/guestdeps/stage.go:130-132` checks `missing(cachePath)` when `copyFile` returns `fs.ErrNotExist`. A failure due to destination directory limits is appropriately propagated instead of masked.
9. **Comment updates**: Closes the finding. Evidence: Comments on `stallTimeout`, `touch`, `downloadOnce`, and `DefaultCacheDir` match the newly verified behaviors.

#### Coverage Summary

- `rdd/.gitignore`: Reviewed, no issues
- `rdd/cmd/download-guest-deps/main.go`: Reviewed, no issues
- `rdd/cmd/download-guest-deps/main_test.go`: Reviewed, no issues
- `rdd/pkg/guestdeps/manifest.go`: Reviewed, no issues
- `rdd/pkg/guestdeps/manifest_test.go`: Reviewed, no issues
- `rdd/pkg/guestdeps/stage.go`: Reviewed, no issues
- `rdd/pkg/guestdeps/stage_test.go`: Reviewed, no issues
- `scripts/dependencies/nerdctl.ts`: Reviewed, no issues

### [Gemini 3.5 Flash]

| Dimension | Detail |
| :--- | :--- |
| **Verdict** | Approved |

#### Executive Summary

The `guest-deps-downloader` PR introduces a highly secure, parallel-concurrency-aware, and robust downloader and caching stager for guest dependencies in Rancher Desktop. All issues from prior rounds have been beautifully resolved, and all unit tests run and pass cleanly with the race detector enabled. The codebase represents excellent engineering quality with no identified correctness, logic, or performance issues.

---

#### Findings

##### Critical Issues

None.

##### Important Issues

None.

##### Suggestions

###### S1. **Inconsistent Sentinel File-Not-Found Checks** — `rdd/pkg/guestdeps/stage.go:515` (suggestion, gap)

```go
func fileHasChecksum(path, checksum string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
```

In `rdd/pkg/guestdeps/stage.go`, there is a minor style inconsistency when checking for file existence. Line 515 uses the legacy `os.ErrNotExist` sentinel, whereas lines 474 and 508 use the modern `fs.ErrNotExist` sentinel. Since Go 1.16, `os.ErrNotExist` is an alias to `fs.ErrNotExist`, but standardizing on `fs.ErrNotExist` is cleaner and maintains consistent patterns across the package.

Fix: Unify on `fs.ErrNotExist` for all file existence checks.

```diff
diff --git a/rdd/pkg/guestdeps/stage.go b/rdd/pkg/guestdeps/stage.go
index 68fa962..4927ec1 100644
--- a/rdd/pkg/guestdeps/stage.go
+++ b/rdd/pkg/guestdeps/stage.go
@@ -512,7 +512,7 @@ func missing(path string) bool {
 // A missing file is not an error, just not a match.
 func fileHasChecksum(path, checksum string) (bool, error) {
 	f, err := os.Open(path)
-	if errors.Is(err, os.ErrNotExist) {
+	if errors.Is(err, fs.ErrNotExist) {
 		return false, nil
 	}
 	if err != nil {
```

---

#### Design Observations

##### Concerns

None in-scope.

##### Strengths

- **Graceful Context Separation**: The stager cleverly segregates `caller` context from the internal `WithTimeout` download context. This allows distinguishing a network timeout from an intentional caller cancellation, optimizing error messages.
- **Robust Stall Timer Design**: Using `time.AfterFunc` to implement the stall timeout avoids the common channel-draining race conditions associated with standard Go `time.Timer` resets, making the progress reader fully watertight.
- **Flawless File Operations**: The temporary-file creation, flushing, and atomicity pattern via `finishTemp` is perfectly safe. If a rename fails, the deferred callback guarantees no stale or corrupted temp files remain.

---

#### Testing Assessment

The test coverage in `stage_test.go` and `main_test.go` is exceptionally high and exemplary.
1. The trickling/stalled server tests are extremely clever, asserting timeouts and deadlines reliably without flaky sleeps.
2. The progress logger clock-tick portability concern on Windows was resolved elegantly by driving the reader directly without clock reliance.
3. Every potential edge case (such as directory permissions, Retry-After header parsing, backoffs, and concurrent file removals underfoot) is thoroughly tested.

---

#### Documentation Assessment

The Go docstrings on all exported symbols (structures, helper functions, and methods) are comprehensive and highly accurate. They cleanly convey not only the *what* but also the precise *why* behind design trade-offs.

---

#### Commit Structure

The commits are clean, logical, and stand as independent, cohesive units of work with descriptive, high-quality commit messages conforming to the project's repository standards.

---

#### Acknowledged Limitations

The following minor limitations are documented as intentional trade-offs in comments and prior rounds:
- **Fast-path touch verification omission** (`rdd/pkg/guestdeps/stage.go:138`): Hashing a cached asset during a standard `touch` would defeat the fast path's purpose. Any corrupt entry safely gets replaced on the next download attempt.
- **Windows concurrency collision risk** (`rdd/pkg/guestdeps/stage.go:486`): On Windows, concurrent builds sharing a cache may trigger a sharing violation due to lacking `FILE_SHARE_DELETE` support on rename. A lock file was rejected because it introduces a worse failure mode (stale locks); a re-run clears the loud error safely.
- **Staging folder build output registration** (`rdd/pkg/guestdeps/main.go:12`): The `rdd/pkg/embedded` folder and build integration are left to the forthcoming Lima VM PR (#708), keeping this downloader PR focused.

---

#### Coverage Summary

| File Path | Status / Finding |
| :--- | :--- |
| `rdd/.gitignore` | Reviewed, no issues |
| `rdd/cmd/download-guest-deps/main.go` | Reviewed, no issues |
| `rdd/cmd/download-guest-deps/main_test.go` | Reviewed, no issues |
| `rdd/pkg/guestdeps/manifest.go` | Reviewed, no issues |
| `rdd/pkg/guestdeps/manifest_test.go` | Reviewed, no issues |
| `rdd/pkg/guestdeps/stage.go` | **S1** |
| `rdd/pkg/guestdeps/stage_test.go` | Reviewed, no issues |
| `scripts/dependencies/nerdctl.ts` | Reviewed, no issues |
