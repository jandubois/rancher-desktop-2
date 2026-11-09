# Deep Review: 20261107-131241-dashboard-language

| | |
|---|---|
| **Date** | 2026-11-07 13:12 |
| **Repo** | [rancher-sandbox/rancher-desktop-2](https://github.com/rancher-sandbox/rancher-desktop-2) |
| **Round** | 1 (of target) |
| **Author** | [@jandubois](https://github.com/jandubois) |
| **PR** | [#1195](https://github.com/rancher-sandbox/rancher-desktop-2/pull/1195) — Follow the app's language in the dashboard |
| **Branch** | `dashboard-language` |
| **Commits** | `d2bbd2694` Follow the app's language in the dashboard<br>`79e561b2b` Show dashboard dates in the dashboard's language |
| **Review SHA** | `79e561b2b8e55e51dd43ef6bc75e192d05d6c248` |
| **Reviewers** | Claude Opus 5.5 (effort: high), Codex GPT 6 Astra (effort: high), Gemini 3.1 Pro (effort: default), Gemini 3.5 Flash (effort: default) |
| **Verdict** | **Merge with fixes**: add I1's spec; the code itself needs no change |
| **Wall-clock time** | `26 min 22 s` |

The PR's branch also has #1191's six commits and #1181's two below them. This review covers only #1195's own two commits, diffed against `42f70e61e`, the head of #1191.

---

## Executive Summary

This PR makes the dashboard follow the app's language. It sets the `R_LOCALE` cookie before the window loads, reloads the dashboard on a language change, adds a zh-hans plugin file, and sets dayjs's locale from the shell's. Traces through the vendored shell and probes confirm each mechanism. The one Important finding is a test gap. Deleting the cookie write leaves the Jest suite green, and I1's patch adds the spec that catches it. Two Suggestions concern the plugin's dayjs code.

---

## Since This Review

The author addressed the findings on 2026-11-07 and amended the fixes into the PR's two commits, which became `1bfd9297c` and `453b76a4e`. On 2026-11-09 the author rebased them onto #1181 and dropped two unused members from the spec's fake window, which a review of the fixes found. That made them `62d7cfbf6` and `58bc909ea`. The code that the findings cite has changed since; the links still point at the reviewed SHA. Each finding ends with its outcome, which names the commit from before the rebase.

---

## Critical Issues

None.

---

## Important Issues

I1. **Nothing tests the dashboard's locale sync** — `pkg/rancher-desktop/window/dashboard.ts:69-72` [Codex GPT 6 Astra, Gemini 3.5 Flash]

```ts
  await setDashboardLocale(window, port);
  if (!window.isDestroyed()) {
    window.loadURL(`http://127.0.0.1:${ port }/c/local/explorer`);
  }
```

No test imports `window/dashboard.ts`, and `e2e/dashboard.e2e.spec.ts` is `test.describe.fixme`, so nothing in CI opens the dashboard. Deleting the cookie write above leaves the suite passing (612 tests passed, 33 skipped), and a user would then get an English dashboard with no error. The same holds for dropping the reload in the locale-change callback, or for moving `windowMapping['dashboard']` below the first await, after which a second click opens a second window. Under the Testing rubric, a wrong result with no error makes the test required, and so does a case that depends on click timing. The plugin's dayjs watcher would need new Jest setup, since `jest.config.js` ignores `dashboard/`, so the PR's live run stays its only check.

```bash repro
# Save as probe-untested-cookie.sh in the repo root; run: bash probe-untested-cookie.sh
# Needs installed node_modules and a Node whose zlib has createZstdDecompress (run here with 26.13.0).
# Deletes the cookie write that precedes the dashboard's first load, runs the whole Jest suite, then restores the file.
file=pkg/rancher-desktop/window/dashboard.ts
cp "$file" "$file.orig"
perl -0pi -e 's/\n  await setDashboardLocale\(window, port\);\n  if \(!window\.isDestroyed\(\)\) \{\n    window\.loadURL/\n  if (!window.isDestroyed()) {\n    window.loadURL/' "$file"
git diff --stat -- "$file"
BROWSERSLIST_IGNORE_OLD_DATA=1 node --experimental-vm-modules node_modules/jest/bin/jest.js 2>&1 | grep -E '^(Tests|Test Suites):'
echo "jest exit: ${PIPESTATUS[0]}"
mv "$file.orig" "$file"
```

```text repro-output
# On the reviewed tree:
$ bash probe-untested-cookie.sh
 pkg/rancher-desktop/window/dashboard.ts | 1 -
 1 file changed, 1 deletion(-)
Test Suites: 1 skipped, 65 passed, 65 of 66 total
Tests:       33 skipped, 612 passed, 645 total
jest exit: 0
# With the patch below applied:
$ bash probe-untested-cookie.sh
 pkg/rancher-desktop/window/dashboard.ts | 1 -
 1 file changed, 1 deletion(-)
Test Suites: 1 failed, 1 skipped, 65 passed, 66 of 67 total
Tests:       3 failed, 33 skipped, 612 passed, 648 total
jest exit: 1
```

Fix: add a spec that drives the real main-process i18n module against a fake `BrowserWindow`. It passes on the reviewed code and fails on each of those mutations, and on removing the closed-window check.

```diff patch
diff --git a/pkg/rancher-desktop/window/__tests__/dashboard.spec.ts b/pkg/rancher-desktop/window/__tests__/dashboard.spec.ts
new file mode 100644
index 000000000..cf4589176
--- /dev/null
+++ b/pkg/rancher-desktop/window/__tests__/dashboard.spec.ts
@@ -0,0 +1,115 @@
+import { EventEmitter } from 'node:events';
+
+import { jest } from '@jest/globals';
+
+import mockModules from '@pkg/utils/testUtils/mockModules';
+
+const port = 9443;
+const onIPCMainProxy = jest.fn((event: unknown, cb: (event: unknown, locale: string) => void) => {});
+const windowMapping: Record<string, number> = {};
+const cookieWrites: { cookie: unknown, resolve: () => void }[] = [];
+
+class FakeWindow extends EventEmitter {
+  static windows: FakeWindow[] = [];
+  static fromId = (id: number) => FakeWindow.windows.find(w => w.id === id) ?? null;
+  id = FakeWindow.windows.length + 1;
+  destroyed = false;
+  loadURL = jest.fn();
+  reload = jest.fn();
+  setTitle = jest.fn();
+  show = jest.fn();
+  isFocused = () => true;
+  isDestroyed = () => this.destroyed;
+  webContents = {
+    session: {
+      cookies: {
+        set: (cookie: unknown) => new Promise<void>((resolve) => {
+          cookieWrites.push({ cookie, resolve });
+        }),
+      },
+    },
+  };
+
+  constructor() {
+    super();
+    FakeWindow.windows.push(this);
+  }
+
+  close() {
+    this.destroyed = true;
+    this.emit('closed');
+  }
+}
+
+mockModules({
+  '@pkg/utils/logging': undefined,
+  '@pkg/main/ipcMain':  {
+    getIpcMainProxy: jest.fn(() => ({
+      on:   onIPCMainProxy,
+      once: jest.fn(),
+      send: jest.fn(),
+    })),
+  },
+  electron:              { BrowserWindow: FakeWindow },
+  '@pkg/backend/steve':  { Steve: { getInstance: () => ({ port }) } },
+  '@pkg/window':         {
+    windowMapping,
+    restoreWindow: (window: FakeWindow | null) => !!window,
+  },
+});
+
+const { initMainI18n } = await import('@pkg/main/i18n');
+const { openDashboard, closeDashboard } = await import('@pkg/window/dashboard');
+
+initMainI18n();
+const changeLocale = onIPCMainProxy.mock.calls.find(args => args[0] === 'i18n/locale-change')![1];
+const settle = () => new Promise(resolve => setTimeout(resolve));
+
+describe('openDashboard', () => {
+  afterEach(() => {
+    closeDashboard();
+    changeLocale(null, 'en-us');
+    delete windowMapping['dashboard'];
+    cookieWrites.length = 0;
+    FakeWindow.windows.length = 0;
+  });
+
+  it('sets the locale cookie before loading, and opens only one window', async() => {
+    const opened = openDashboard();
+    const second = openDashboard();
+
+    expect(FakeWindow.windows).toHaveLength(1);
+    expect(cookieWrites.map(({ cookie }) => cookie)).toEqual([
+      { url: `http://127.0.0.1:${ port }/`, name: 'R_LOCALE', value: 'en-us' },
+    ]);
+    expect(FakeWindow.windows[0].loadURL).not.toHaveBeenCalled();
+    cookieWrites[0].resolve();
+    await Promise.all([opened, second]);
+    expect(FakeWindow.windows[0].loadURL).toHaveBeenCalledWith(`http://127.0.0.1:${ port }/c/local/explorer`);
+  });
+
+  it('writes a new locale to the cookie, then reloads', async() => {
+    const opened = openDashboard();
+
+    cookieWrites[0].resolve();
+    await opened;
+    changeLocale(null, 'de');
+    expect(cookieWrites[1].cookie).toMatchObject({ name: 'R_LOCALE', value: 'de' });
+    expect(FakeWindow.windows[0].reload).not.toHaveBeenCalled();
+    cookieWrites[1].resolve();
+    await settle();
+    expect(FakeWindow.windows[0].reload).toHaveBeenCalled();
+  });
+
+  it('does not reload a window closed during a locale change', async() => {
+    const opened = openDashboard();
+
+    cookieWrites[0].resolve();
+    await opened;
+    changeLocale(null, 'de');
+    FakeWindow.windows[0].close();
+    cookieWrites[1].resolve();
+    await settle();
+    expect(FakeWindow.windows[0].reload).not.toHaveBeenCalled();
+  });
+});
```

**Outcome:** test written, in `1bfd9297c`. The spec has a fourth test, which closes the window during the first cookie write: none of the three above fails when the closed-window check before `loadURL` is removed.

---

## Suggestions

S1. **The dayjs watcher relies on the shell's init order** — `dashboard/pkg/rancher-deskboard/index.ts:21-23` [Claude Opus 5.5]

```ts
  store.watch((_state: any, getters: any) => getters['i18n/current'](), (locale: string) => {
    dayjs.locale(dayjsLocales[locale] ?? locale);
  });
```

Without `immediate`, the callback runs only when `i18n/current` changes. A cold load works today because the vendored shell runs built-in extensions' init during plugin installation and dispatches `i18n/init` only afterwards (`initialize/install-plugins.js`, line 76), so the watcher sees `selected` go from `null` to the cookie's locale. A shell bump that moved `i18n/init` earlier would leave dates in English with no error. With `immediate: true` the callback also runs once at registration with `null`, which `dayjs.locale` ignores. A probe of the reviewed plugin against the real shell `i18n` store gives the same dates with the option set.

Fix:

```diff
   store.watch((_state: any, getters: any) => getters['i18n/current'](), (locale: string) => {
     dayjs.locale(dayjsLocales[locale] ?? locale);
-  });
+  }, { immediate: true });
```

**Outcome:** fixed as proposed, in `453b76a4e`.

S2. **A shell language without a dayjs locale import gets English dates** — `dashboard/pkg/rancher-deskboard/index.ts:3-7` [Claude Opus 5.5, Gemini 3.1 Pro]

```ts
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';

// Where dayjs names a locale differently from the dashboard.
const dayjsLocales: Record<string, string> = { 'zh-hans': 'zh-cn' };
```

Given a name it has not loaded, `dayjs.locale()` returns the current locale and changes nothing. The shell offers only en-us and zh-hans today, so nothing breaks. But once #1187 adds languages to the shell, each one also needs its `dayjs/locale/<x>` import here, or its dates stay in English with no error. Each also needs a plugin `l10n/<x>.yaml`, but a missing one shows at once as a raw key in the header. Dates do not stay in the previous language: every app language change reloads the dashboard, which starts dayjs in English, and RD's dashboard does not have a language menu of its own. Only the commit message records the trap.

Fix: say it at the map. Importing every app language's dayjs locale now would also work, but #1187 has not settled which languages the shell gets.

```ts
// Where dayjs names a locale differently from the dashboard. Each language
// the shell offers needs its dayjs locale imported above, or its dates stay
// in English.
```

**Outcome:** fixed in `453b76a4e`. The comment also says why en-us needs neither an import nor a map entry: dayjs falls back on the part of a name before the hyphen, so en-us gets its built-in English.

---

## Design Observations

### Concerns

- (future) Every language change reloads the dashboard. That discards unsaved state, such as an open YAML edit, and it happens even when the dashboard's language stays the same (en-us to de, since the shell falls back to en-us). Language changes are rare, so this is fine for now. Skipping the reload when the shell's resolved locale does not change would need the shell's locale list, which #1187 settles. [Claude Opus 5.5]
  **Outcome:** declined for this PR and kept as follow-up work for #1187.

### Strengths

- (in-scope) `windowMapping['dashboard']` is set before the first await, so a second click during the cookie write finds the window instead of opening another. I1's spec fails if the assignment moves. [Claude Opus 5.5, Codex GPT 6 Astra, Gemini 3.5 Flash]
- (in-scope) The dayjs watcher follows the shell's resolved locale, not the cookie, so a language that the shell lacks (`de` today) gets English dates to match its English text. [Codex GPT 6 Astra]
- (in-scope) A failed cookie write is logged and the dashboard still loads, in the language of any earlier cookie. [Codex GPT 6 Astra]

---

## Testing Assessment

- I1 is required, because the regression that it guards shows the wrong language with no error, and one case depends on click timing.
- I ran the full Jest suite in the review worktree under Node 26.13.0: 612 passed, 33 skipped, 65 of 66 suites. With I1's spec it is 615 passed. Under Node 22.14.0, `i18n.spec.ts` fails at load, because jsdom's bundled undici calls `zlib.createZstdDecompress`.
- I applied each mutation named in I1's Fix line, checked the diff, and ran the spec against it. Removing the cookie write fails 3 tests, removing the reload 1, registering the window after the await 2, and removing the closed-window check 1. The third test also fails on its own under the last mutation, so it does not depend on the order the tests run in.
- A probe that runs the reviewed plugin against the real shell `i18n` store, Vuex and dayjs gives Chinese dates under zh-hans and English under en-us and de. It registers the plugin before dispatching `i18n/init`, as the shell does, so it also shows the watcher firing on a cold load. I re-ran it as written and again with S1's `immediate: true`.
- CI on `79e561b2b`: 50 check runs succeeded and 4 were skipped; none failed, and none opens the dashboard.
- No unit test can reach the plugin, because `jest.config.js` ignores `dashboard/`. The PR's live run (zh-hans before launch, then en-us, de and zh-hans with the dashboard open, plus a control on #1191's head) is its only check.

---

## Documentation Assessment

Both commit messages match the code. In the vendored shell, `prefs/loadCookies` reads `R_LOCALE` before `i18n/init` runs. The plugin's en-us strings merge only inside the branch taken when the selected locale has plugin strings of its own (`store/i18n.js`, line 313), and nothing in the shell calls `dayjs.locale`. The comments in `window/dashboard.ts` and the plugin's `index.ts` are accurate.

---

## Acknowledged Limitations

- Languages other than en-us and zh-hans show English text and dates until #1187 adds them to the shell.
- License headers on the plugin's files: #1191's review raised them and the author declined. Do not re-suggest them.
- Do not re-suggest falling back to dayjs's `en` when `dayjs.locale()` ignores an unloaded name. Every language change reloads the page, which already starts dayjs in English.
- Do not re-suggest a destroyed-window check at the top of the locale-change callback. The window's `closed` event unregisters the callback, and `setDashboardLocale` catches its own errors.
- Do not re-suggest making `openDashboard` return the new window. No caller reads its return value, and the code before this PR returned the same way.

---

## Agent Performance Retro

### [Claude Opus 5.5]

It traced every mechanism through the vendored shell, including the server-side prefs that load only under `isRancher`, and confirmed with `find` that the plugin and the shell share one dayjs. S1 came from it alone. It ruled out a test for `window/dashboard.ts` on the grounds that a mocked `BrowserWindow` would only test the mocks, and marked the file clean. The mutation runs under I1 show such a spec exercises the module's own ordering, so this was a miss. Its session log shows that it ran the dayjs probe it quotes.

### [Codex GPT 6 Astra]

It found I1 and backed it with the round's strongest evidence, a probe that runs the reviewed plugin against the real shell `i18n` store, Vuex and dayjs, and kills three mutants. Because the probe registers the plugin before `i18n/init`, as the shell does, it also showed the watcher firing on a cold load, which Claude argued from source. Its proposed plugin test would need Jest setup for `dashboard/`, so the patch leaves that part out. It did not raise any Suggestions, and it missed both S1 and S2.

### [Gemini 3.1 Pro]

It rated the unloaded-dayjs-locale trap Important on a scenario that cannot happen: switching zh-hans to de with Chinese dates kept. The dashboard reloads on every app language change and does not offer a language menu of its own, so dates fall back to English. Its fix, falling back to `en`, changes nothing a user can reach. It also launched Electron's default app twice to test cookie overwrites. The prompt forbade starting the app but did not name a bare Electron binary. The first run hung until the CLI's five-minute timeout. The second showed an API-set cookie replacing a Secure one, which agrees with the PR's live run. gemini-cli's policy denied four of its heredoc writes.

### [Gemini 3.5 Flash]

Two of its three Suggestions were dropped. The destroyed-window check defends a callback that the `closed` event already unregisters, and the return-value asymmetry predates the PR, with no caller reading the value. Its third, an untested `getLocale`, is part of I1, whose spec drives the real accessor. Its Jest and ESLint runs appear in its session log as described. The two Geminis raised nothing in common.

### Summary

| | Claude Opus 5.5 | Codex GPT 6 Astra | Gemini 3.1 Pro | Gemini 3.5 Flash |
|---|---|---|---|---|
| Duration | 5m 36s | 3m 59s | 13m 21s | 13m 58s |
| Findings | 2S | 1I | 1S | 1I |
| Tool calls | 37 (Bash 35, Read 2) | 15 (shell 12, apply_patch 2, stdin 1) | 50 (run_shell_command 40, write_file 10) | 101 (grep_search 39, read_file 30, run_shell_command 24) |
| Design observations | 2 | 3 | 0 | 1 |
| False positives | 0 | 0 | 0 | 1 |
| Unique insights | 2 | 1 | 0 | 0 |
| Files reviewed | 4 | 4 | 4 | 4 |
| Coverage misses | 1 | 0 | 1 | 0 |
| **Totals** | **2S** | **1I** | **1S** | **1I** |
| Downgraded | 0 | 0 | 1 | 0 |
| Dropped | 0 | 0 | 0 | 2 |


Codex gave the most value, with the only Important finding and a probe that checked the plugin against real shell code. Claude read the shell most accurately and contributed S1. Both Geminis' findings needed the most correction.

**Reconciliation:** Gemini 3.1 Pro I1 (dayjs keeps the previous locale): important → suggestion, merged into S2. Gemini 3.5 Flash S3 (untested `getLocale`): suggestion → part of I1. Gemini 3.5 Flash S1 (destroyed-window check) and S2 (return-value asymmetry): dropped.

---

## Review Process Notes

### Skill improvements

- Name GUI runtimes in the preamble's list of forbidden actions. An agent testing a platform API may launch a framework's bare GUI binary, such as Electron or a browser, from the installed dependencies. That binary writes to a per-user profile outside the worktree and can hang until the CLI's timeout. Forbid launching any GUI process.
- The stacked-PR rule scopes findings to the files that the head commit changes. When a stacked PR has several commits of its own, scope to every commit between the base PR's head and this PR's head. In branch mode, `--commits N` with N set to the PR's own commit count does this; the orchestrator then adds the PR description and linked issue to the prompt, since branch mode fetches neither.

### Repo context updates

- The app's Jest suite needs a Node whose `zlib` has `createZstdDecompress`. jsdom's bundled undici calls it at load, so on Node 22.14 a suite that loads jsdom, such as `i18n.spec.ts`, fails before a test runs. Treat that failure as the toolchain, not the change.
- Main-process window modules under `pkg/rancher-desktop/window/` do not have specs, but `mockModules` with a fake `BrowserWindow` and the real `@pkg/main/i18n` reaches their own sequencing: what they await before `loadURL`, when they reload, and what they register before an await. Such a test exercises the module's own code, so rate a missing one by the Testing rubric instead of dismissing it as a test of mocks.

---

## Appendix: Original Reviews

### [Claude Opus 5.5]

#### Review of PR #1195, "Follow the app's language in the dashboard" (commits `d2bbd2694` and `79e561b2b` on top of `42f70e61e`)

##### Executive Summary

The PR sets the dashboard's `R_LOCALE` cookie to the app's language before the dashboard window loads, and reloads an open dashboard when the language changes. It also adds a zh-hans plugin file and sets dayjs's locale from the shell's `i18n/current` getter. I traced each mechanism through the vendored shell and found no defects. The two Suggestions harden the dayjs watcher: one against a change in shell init order, one against the locales #1187 will add.

##### Findings

###### Critical Issues

None.

###### Important Issues

None.

###### Suggestions

S1. **The dayjs watcher depends on plugin init running before `i18n/init`** — `dashboard/pkg/rancher-deskboard/index.ts:21-23` (suggestion, enhancement)

```ts
  store.watch((_state: any, getters: any) => getters['i18n/current'](), (locale: string) => {
    dayjs.locale(dayjsLocales[locale] ?? locale);
  });
```

Without `immediate`, the callback fires only when `state.selected` changes. Today the first load works because of an ordering in the vendored shell:

- `plugins/plugin.js:63` runs `loadBuiltinExtensions()`, which calls this init function, inside the `Promise.all` at `initialize/install-plugins.js:62-72`.
- `initialize/install-plugins.js:76` dispatches `i18n/init` only after that `Promise.all`.
- So the watcher sees `selected` go from `null` to the cookie's locale.

If a shell bump moves `i18n/init` earlier, the first load commits the locale before the watcher exists. Dates would then stay English, with no error.

Fix:
```diff
   store.watch((_state: any, getters: any) => getters['i18n/current'](), (locale: string) => {
     dayjs.locale(dayjsLocales[locale] ?? locale);
-  });
+  }, { immediate: true });
```

S2. **A shell locale without an imported dayjs locale silently keeps the previous date language** — `dashboard/pkg/rancher-deskboard/index.ts:3-7` (suggestion, enhancement)

```ts
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';

// Where dayjs names a locale differently from the dashboard.
const dayjsLocales: Record<string, string> = { 'zh-hans': 'zh-cn' };
```

Calling `dayjs.locale()` with a locale that was never imported returns the current locale and changes nothing. The probe under Coverage Summary shows it: `dayjs.locale('de')` after `zh-cn` returns `zh-cn` and still prints `周六, 11月 7 2026`.

Today the shell offers only en-us and zh-hans (`store/i18n.js:21`), so nothing breaks. Once #1187 adds languages to the shell, each one also needs two things here:

- a `dayjs/locale/<x>` import, since dayjs ships `de`, `es`, `fr`, `it`, `ja`, `ko`, `pt-br` and `vi` (all present in the staged dayjs 1.11.18);
- a plugin `l10n/<x>.yaml` file, without which the header shows the raw key.

Missing either one fails silently. The trap is recorded only in the commit messages.

Fix: say it at the map, e.g. `// Each language the shell offers needs its dayjs locale imported above and a file in l10n/, or dates stay English and the header shows a raw key.`. Another option is to import every dayjs locale the app's `LocaleString` names now.

##### Design Observations

**Concerns**

- (future) A reload on a language change throws away dashboard state that has not been saved, such as an open YAML edit. It also happens when the dashboard's displayed language does not change, for example en-us → de, since the shell falls back to en-us (`store/i18n.js:231-238`). The comment at `pkg/rancher-desktop/window/dashboard.ts:56-57` gives a reason for reloading: some strings are set only once. Language changes are rare and deliberate, so this is acceptable. If it matters later, the app could skip the reload when the effective dashboard locale stays the same. That needs the shell's locale list, which only #1187 settles.

**Strengths**

- `windowMapping['dashboard']` is set before the first `await` (`dashboard.ts:50-51`), so a second click finds the window instead of opening another. Both post-await paths check `isDestroyed()` before `loadURL` or `reload` (`dashboard.ts:58`, `dashboard.ts:70`).
- `setDashboardLocale` reads `getLocale()` synchronously when it builds the cookie object, so locale changes that overlap still apply cookie writes in the order they were issued.

##### Testing Assessment

No missing test is kept as a finding.

- A user would see a dashboard in the wrong language (a wrong result), which would normally make a test required.
- Reaching `openDashboard` needs the dashboard e2e spec, which is `test.describe.fixme` and needs steve and Kubernetes in CI. That is new infrastructure, so the drop rule applies.
- A Jest test with a mocked `BrowserWindow` and cookie store would only test the mocks.
- `getLocale()` is a one-line accessor and needs no test.

The PR's manual run (zh-hans before launch, then en-us, de, zh-hans while open, with a control run on #1191's tip) covers the runtime paths.

##### Documentation Assessment

Both commit messages are accurate. I checked each claim against the vendored shell:

- The shell reads `R_LOCALE` at load: `prefs/loadCookies` (`store/index.js:1272`) runs from `dashboardClientInit`, before `i18n/init`.
- The plugin's en-us file is merged only inside the `i18nExt` branch (`store/i18n.js:317-328`).
- No `dayjs.locale` call or `dayjs/locale` import exists in the vendored shell.

The comments at `dashboard.ts:13-15`, `dashboard.ts:25` and `index.ts:19-20` are accurate. See S2 for the one fact that lives only in the commit message.

##### Coverage Summary

- `dashboard/pkg/rancher-deskboard/index.ts`: S1, S2.
- `dashboard/pkg/rancher-deskboard/l10n/zh-hans.yaml`: Reviewed, no issues. 仪表板 matches the shell's own zh-hans term (9 uses of 仪表板, 0 of 仪表盘, counted with grep) and the app's 集群仪表板.
- `pkg/rancher-desktop/main/i18n.ts`: Reviewed, no issues. `getLocale` returns the main process's `currentLocale`, which `i18n/locale-change` updates. Persisted preferences go through `preferences/modify`, which patches the app at once, so there is no unsaved preview that could reload the dashboard spuriously.
- `pkg/rancher-desktop/window/dashboard.ts`: Reviewed, no issues. Callers are `background.ts:293` (IPC `dashboard-open`, return value ignored) and the commented-out tray entry. Nothing uses the return value, so the change to a Promise is safe.

**Verification:**

- Executed: a dayjs probe against the staged `dashboard/node_modules` (dayjs 1.11.18), saved at `/private/var/folders/s2/9q03h0c11_s4fdw169wqz3qc0000gn/T/review-claude-pass-1-948243500/.scratch/probe/dayjs-probe.cjs`. Each line shows the requested locale, then what `dayjs.locale()` returned (the locale actually in force), then a formatted date:
  ```
  en-us -> en Sat, Nov 7 2026 11:24:48 am
  zh-cn -> zh-cn 周六, 11月 7 2026 11:24:48 中午
  de -> zh-cn 周六, 11月 7 2026 11:24:48 中午
  none -> zh-cn 周六, 11月 7 2026 11:24:48 中午
  en-us -> en Sat, Nov 7 2026 11:24:48 am
  pt-br -> en Sat, Nov 7 2026 11:24:48 am
  ```
  The `en-us` → `en` mapping works, the zh-cn output matches the PR's table, and unloaded locales stay on the previous locale (S2).
- Executed: `find` for `dayjs` directories in the staged dashboard tree found exactly one copy, so the plugin and the shell share one dayjs instance.
- Reasoned, from the vendored shell source:
  - `R_LOCALE` is read before `i18n/init`.
  - Built-in plugin init runs before `i18n/init` (S1).
  - Server-side prefs (`prefs/loadServer`) load only when `isRancher` (`store/index.js:828`), so they cannot override the cookie under RD's steve.
  - An unknown locale such as `de` falls back to en-us.
- Supplied by the prompt, not repeated: CI green on `79e561b2b`; `i18n.spec.ts` 8/8 under Node 26.
- Not executed: the Electron cookie overwrite after the shell rewrites `R_LOCALE` as a `secure` cookie. That needs the app running, which the prompt forbids. The PR's sequence of changes on an open dashboard (en-us → de → zh-hans) went through exactly that overwrite and worked, and `setDashboardLocale` logs any failure.

### [Codex GPT 6 Astra]

| Review | PR #1195 — `42f70e61e..79e561b2b` |
|---|---|
| Verdict | Add regression coverage before merge |

###### Executive Summary

The change synchronizes the dashboard’s language with the app and localizes dayjs dates. All four changed files were reviewed. Isolated probes confirmed the intended behavior; no functional regression was established. The main concern is missing automated coverage for the new asynchronous window lifecycle and date localization.

###### Findings

###### Critical Issues

None.

###### Important Issues

I1. **Protect the locale synchronization with regression tests** — `pkg/rancher-desktop/window/dashboard.ts:53-59,69-71` (important, gap)

The cookie-before-navigation ordering at lines 69–71 and cookie-before-reload ordering at lines 53–59 have no committed regression coverage. Neither does the dayjs assignment in `dashboard/pkg/rancher-deskboard/index.ts:21-23`. The existing main-process test exercises translation callbacks (`pkg/rancher-desktop/main/__tests__/i18n.spec.ts:53-84`), while the dashboard suite is disabled (`e2e/dashboard.e2e.spec.ts:6`).

Removing these operations silently restores the wrong language or dates. Startup and close timing also affect whether the correct window is loaded. The deciding testing criterion is silent incorrect output and state-dependent behavior.

Fix: Add focused tests with deferred cookie promises for initial navigation, reload, concurrent opens, and closure during a write. Test the plugin with real Vuex/dayjs and fixed Chinese/English expectations. The isolated probes confirmed these assertions detect removal of the cookie wait, reload, and locale assignment.

###### Suggestions

None.

###### Design Observations

**Strengths**

- Recording the window before the first await preserves single-window behavior during concurrent opens.
- Watching the shell’s resolved locale keeps dates consistent with its English fallback for unsupported languages.

###### Testing Assessment

- **I1 — Required:** silent wrong-language output and asynchronous lifecycle ordering decide the severity.
- **Executed:** `.scratch/stage/node26/bin/node .scratch/locale-review.cjs` transpiled the reviewed modules and tested window behavior with mocked Electron. Cookie ordering, concurrent opening, closure during a write, and cookie-error fallback passed.
- **Executed:** the same probe used real Vuex/dayjs and the staged shell’s i18n actions. Chinese dates and the `仪表板` label passed; English and unsupported `de` both produced English output. Removing each of the three load-bearing operations triggered an assertion failure.
- **Executed:** the first probe launch failed because copied dependencies were outside a directory named `node_modules`. After correcting that scratch layout, both subsequent runs passed.
- **Executed:** `git diff --check` passed. Tracked files remain unchanged.
- **Reasoned:** shell initialization registers the plugin before selecting its initial locale, so the watcher receives the startup selection.
- **Supplied by the prompt:** CI succeeded and the existing main-process i18n suite passed 8/8. Those runs were not repeated.
- **Not executed:** Electron, dashboard E2E, services, and VMs, as prohibited. Mocked cookie tests establish ordering, not Chromium cookie behavior.

###### Documentation Assessment

Both commit messages match the implementation. No documentation correction identified.

###### Acknowledged Limitations

- **PR description:** languages other than English and Simplified Chinese intentionally fall back to English; additional translations belong to #1187.
- **Code comment:** “Any earlier cookie stays, so the dashboard keeps that language.” — `pkg/rancher-desktop/window/dashboard.ts:25`. Cookie-write failure deliberately preserves availability; the probe confirmed navigation continues.
- **Code comment:** “The dashboard sets some strings only once, at startup…” — `pkg/rancher-desktop/window/dashboard.ts:56-57`. Reloading is an explicit design choice.
- **Prior author decision supplied in the prompt:** dashboard plugin license headers were declined because the app-tree convention is unsettled. No finding repeats that request.

###### Coverage Summary

| Changed file | Coverage |
|---|---|
| `dashboard/pkg/rancher-deskboard/index.ts` | **I1**; watcher registration and locale mapping reviewed and probed |
| `dashboard/pkg/rancher-deskboard/l10n/zh-hans.yaml` | **Reviewed, no issues**; new locale file supplies the expected header key, verified through shell translation loading |
| `pkg/rancher-desktop/main/i18n.ts` | **Reviewed, no issues**; getter exposes the locale used by main-process translation |
| `pkg/rancher-desktop/window/dashboard.ts` | **I1**; cookie helper, opening sequence, locale callback, and close handling reviewed and probed |

### [Gemini 3.1 Pro]

###### Executive Summary
This change sets the `R_LOCALE` cookie to synchronize the dashboard's language with the app, and reloads the dashboard window when the locale changes. The approach correctly updates the dashboard, but a gap in the dayjs locale implementation means dates will incorrectly remain in a previously selected language if a newly added language lacks a dayjs import.

###### Critical Issues
None.

###### Important Issues
I1. **Missing dayjs locale fallback when translations are missing** — `dashboard/pkg/rancher-deskboard/index.ts:19-21`

```typescript repro
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';

function testDayjsFallback() {
  dayjs.locale('zh-cn');
  const cnMonth = dayjs().format('MMMM');

  // Attempt to switch to an unimported locale (e.g. 'de' after PR #1187 adds it)
  dayjs.locale('de');
  const deMonth = dayjs().format('MMMM');
  
  if (cnMonth === deMonth) {
    console.error(`BUG: dates remain in Chinese (${deMonth}) instead of falling back to English!`);
  }
}
testDayjsFallback();
```

```text repro-output
BUG: dates remain in Chinese (十一月) instead of falling back to English!
```

When `i18n/current` emits a locale that `dayjs` has not statically loaded (such as `de` when it is added by a future PR), `dayjs.locale('de')` silently fails and retains the previous locale. If a user switches from `zh-hans` to `de`, the dashboard text correctly falls back to English, but the dates incorrectly remain in Chinese because the dayjs locale change was ignored.

Fix: Check if the locale change succeeded and explicitly fall back to English if it failed.
```typescript
  store.watch((_state: any, getters: any) => getters['i18n/current'](), (locale: string) => {
    const dayjsLocale = dayjsLocales[locale] ?? locale;
    if (dayjs.locale(dayjsLocale) !== dayjsLocale) {
      dayjs.locale('en');
    }
  });
```
(important, gap)

###### Suggestions
None.

###### Design Observations
None.

###### Testing Assessment
The dashboard end-to-end tests are currently skipped (`test.describe.fixme` in `e2e/dashboard.e2e.spec.ts`), so testing this UI behavior automatically is currently unsupported in the project's CI pipeline.

###### Documentation Assessment
No documentation changes are needed.

###### Acknowledged Limitations
- **Dashboard reload required**: The PR description states "The dashboard sets some strings only once, at startup, so switching it in place would leave them in the old language." Reloading the dashboard window is an acknowledged workaround for this limitation.

###### Coverage Summary
- `dashboard/pkg/rancher-deskboard/index.ts`: I1
- `dashboard/pkg/rancher-deskboard/l10n/zh-hans.yaml`: Reviewed, no issues
- `pkg/rancher-desktop/main/i18n.ts`: Reviewed, no issues
- `pkg/rancher-desktop/window/dashboard.ts`: Reviewed, no issues (verified that `session.cookies.set` safely overwrites the dashboard's cookie, and that the window lifecycle and cleanup are handled correctly).

### [Gemini 3.5 Flash]

##### Executive Summary

PR #1195 configures the cluster dashboard to respect the application's language preference. It writes the `R_LOCALE` cookie on the dashboard session before loading the window, automatically reloads an open dashboard window when the application locale changes, adds the necessary `zh-hans` plugin localization bundle, and updates Day.js to format dates according to the dashboard's active locale. The implementation cleanly addresses the goals of issue #1186 without regressions.

---

##### Findings

###### Critical Issues

None.

###### Important Issues

None.

###### Suggestions

S1. **Missing guard against destroyed window in locale change callback** — `pkg/rancher-desktop/window/dashboard.ts:53-61`

```ts
  const offLocaleChange = onLocaleChange(async() => {
    window.setTitle(t('dashboard.windowTitle'));
    await setDashboardLocale(window, port);
    // The dashboard sets some strings only once, at startup, so switching it
    // in place would leave them in the old language.
    if (!window.isDestroyed()) {
      window.reload();
    }
  });
```

`onLocaleChange` takes an `async` callback. While `window.on('closed', offLocaleChange)` unregisters the callback when the window closes, if the window is destroyed during the execution of earlier callbacks in `localeChangeCallbacks` (or during `setDashboardLocale`), calling `window.setTitle` or accessing `window.webContents` throws `Error: Object has been destroyed`. In an `async` callback, synchronous throws become unhandled promise rejections that bypass `initMainI18n()`'s synchronous `try/catch`. Checking `window.isDestroyed()` at the top of the callback avoids this risk.

Fix:
```diff
--- a/pkg/rancher-desktop/window/dashboard.ts
+++ b/pkg/rancher-desktop/window/dashboard.ts
@@ -53,3 +53,6 @@ export async function openDashboard() {
   const offLocaleChange = onLocaleChange(async() => {
+    if (window.isDestroyed()) {
+      return;
+    }
     window.setTitle(t('dashboard.windowTitle'));
```

---

S2. **Asymmetric return value between window restore and creation** — `pkg/rancher-desktop/window/dashboard.ts:33-35, 70-73`

```ts
  if (restoreWindow(existingWindow)) {
    return existingWindow;
  }
...
  await setDashboardLocale(window, port);
  if (!window.isDestroyed()) {
    window.loadURL(`http://127.0.0.1:${ port }/c/local/explorer`);
  }
```

`openDashboard()` returns `existingWindow` (`BrowserWindow`) when restoring an existing window (line 34), but returns `undefined` when creating a new window (falling off line 73). Consequently, TypeScript infers the return type as `Promise<BrowserWindow | void>`. Returning `window` at the end makes the return value consistently `Promise<BrowserWindow | undefined>`.

Fix:
```diff
--- a/pkg/rancher-desktop/window/dashboard.ts
+++ b/pkg/rancher-desktop/window/dashboard.ts
@@ -70,3 +70,5 @@ export async function openDashboard() {
   if (!window.isDestroyed()) {
     window.loadURL(`http://127.0.0.1:${ port }/c/local/explorer`);
+    return window;
   }
```

---

S3. **Missing unit test coverage for exported `getLocale`** — `pkg/rancher-desktop/main/i18n.ts:110-112`

```ts
export function getLocale(): LocaleString {
  return currentLocale;
}
```

`getLocale()` was added and exported for `dashboard.ts` to query `currentLocale`, but `pkg/rancher-desktop/main/__tests__/i18n.spec.ts` does not test it. Adding assertions for `getLocale()` before and after IPC `i18n/locale-change` events verifies that it stays synchronized with the loaded translation set.

Fix:
In `pkg/rancher-desktop/main/__tests__/i18n.spec.ts`, import `getLocale` and assert `expect(getLocale()).toEqual('en-us')` initially, `expect(getLocale()).toEqual('de')` after the switch, and `expect(getLocale()).toEqual('en-us')` after revert.

---

##### Design Observations

###### Strengths
- Setting the `R_LOCALE` cookie directly on Electron's session prior to calling `window.loadURL()` leverages the dashboard shell's native cookie preference loading (`store/prefs.js`) without requiring custom preload scripts or query string hacks.
- Recording `windowMapping['dashboard'] = window.id` immediately after `new BrowserWindow` prevents duplicate window instances if the user clicks "Open Dashboard" repeatedly before `await setDashboardLocale` completes.
- Storing translations in `dashboard/pkg/rancher-deskboard/l10n/zh-hans.yaml` addresses the dashboard extension loader's requirement where fallback strings for plugins only load if a translation bundle exists for the active locale.

###### Concerns
- `setDashboardLocale(window: BrowserWindow, port: number)` binds cookie setting to `window.webContents.session`. Because `new BrowserWindow` uses the default session, `session.defaultSession.cookies.set(...)` could be used directly, decoupling cookie management from the window lifecycle. `(future)`

---

##### Testing Assessment

- S3 (Missing unit test for `getLocale()`): Optional gap (Suggestion severity) decided by Check #3 (the one-line accessor is low risk and fails visibly if broken). An experimental probe in `.scratch/` confirmed Jest passes when testing `getLocale()` across `i18n/locale-change`.
- Pre-existing note: `e2e/dashboard.e2e.spec.ts` remains skipped with `test.describe.fixme`, so end-to-end dashboard rendering is not currently exercised in automated CI runs.

---

##### Documentation Assessment

- All new functions (`getLocale`, `setDashboardLocale`) include descriptive JSDoc comments accurately describing their behavior and rationale.

---

##### Commit Structure

The branch consists of two distinct, well-scoped commits that each encapsulate a single logical responsibility:
- `d2bbd2694`: Wires `R_LOCALE` cookie persistence, auto-reloading on locale change, and the `zh-hans` plugin bundle.
- `79e561b2b`: Integrates Day.js locale synchronization within the dashboard plugin.

Commit messages are detailed, accurately document rationale, and reference the corresponding issue.

---

##### Acknowledged Limitations

- **Text for other locales**: The PR description and issue #1186 note that until PR #1187 lands, the dashboard vendor shell only includes text for `en-us` and `zh-hans`. Selecting other locales (such as `de` or `fr`) correctly falls back to English text and date formatting.
- **Date format ordering**: Commit `79e561b2b` notes that Day.js locale changes localize month and day names, while the date format pattern (`DATE_FORMAT`) remains a separate user preference that keeps its default US ordering.
- **SPDX license headers**: Settled during PR #1191 review (author declined adding SPDX headers to dashboard plugin files).

---

##### Coverage Summary

| File | Status | Notes |
| :--- | :--- | :--- |
| `dashboard/pkg/rancher-deskboard/index.ts` | Reviewed, no issues | Verified `store.watch` reactivity against `vendor-shell/store/i18n.js` and confirmed Day.js locale mapping logic (`zh-hans` to `zh-cn`). |
| `dashboard/pkg/rancher-deskboard/l10n/zh-hans.yaml` | Reviewed, no issues | Verified YAML structure matches `en-us.yaml` and provides `rancher-desktop.label`. |
| `pkg/rancher-desktop/main/i18n.ts` | S3 | Reviewed `getLocale()` implementation; noted missing unit test assertion in `i18n.spec.ts`. |
| `pkg/rancher-desktop/window/dashboard.ts` | S1, S2 | Evaluated cookie setting, window lifecycle, and error handling across locale reload events. |

###### Verification Evidence
- **Day.js locale mapping probe**: Executed via Node 26 against `.scratch/stage/dashboard/node_modules/dayjs`:
  - Verified `dayjs.locale('en-us')` resolves to built-in `en` (`Saturday, November 7, 2026`).
  - Verified `import 'dayjs/locale/zh-cn'` does not mutate global locale until `dayjs.locale('zh-cn')` is invoked (`星期六, 十一月 7, 2026`).
  - Verified `dayjs.locale(null)` and `dayjs.locale('unknown')` safely return current locale without throwing.
- **`getLocale` Jest unit test probe**: Created a probe modifying `pkg/rancher-desktop/main/__tests__/i18n.spec.ts` in `.scratch/` and ran Jest under Node 26.13.0; verified 8 of 8 tests pass with `getLocale()` assertions.
- **Lint verification**: Ran `eslint` on `pkg/rancher-desktop/main/i18n.ts` and `pkg/rancher-desktop/window/dashboard.ts`; 0 errors, 0 warnings.

## Resolution

| | |
|---|---|
| **Addressed** | 2026-11-07 |
| **Commit** | `1bfd9297c Follow the app's language in the dashboard`, `453b76a4e Show dashboard dates in the dashboard's language` (fixups amended into the PR's two commits; force-pushed 2026-11-07); `62d7cfbf6`, `58bc909ea` rebased onto #1181, with two unused test members removed |

| # | Finding | Action | Reason |
|---|---------|--------|--------|
| 1 | Important #1: Nothing tests the dashboard's locale sync | Test written | |
| 2 | Suggestion #1: The dayjs watcher relies on the shell's init order | Fixed | |
| 3 | Suggestion #2: A shell language without a dayjs locale import gets English dates | Fixed | |
| 4 | Design Concern #1: Every language change reloads the dashboard | Skipped | Language changes are rare; skipping a same-language reload needs the shell's locale list, which #1187 settles; tracked in pending-work |
