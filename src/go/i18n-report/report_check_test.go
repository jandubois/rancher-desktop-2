// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckSourceReportsUnusedAsFindings(t *testing.T) {
	enUS := "status:\n  checking: Checking...\n"
	de := "status:\n  checking: Wird geprüft…\n"
	dir := setupLocaleTestRepo(t, enUS, de, true)

	// No source file references status.checking, so it is unused.
	err := reportCheckSource(io.Discard, dir)
	if err == nil {
		t.Fatal("expected findings for an unused key")
	}
	if !errors.Is(err, errFindings) {
		t.Errorf("unused key should be a findings error, got: %v", err)
	}
}

func TestCheckSourcePassesWhenKeyReferenced(t *testing.T) {
	enUS := "status:\n  checking: Checking...\n"
	de := "status:\n  checking: Wird geprüft…\n"
	dir := setupLocaleTestRepo(t, enUS, de, true)

	srcDir := filepath.Join(dir, "pkg", "rancher-desktop", "components")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	source := "<template>\n  <span v-t=\"'status.checking'\" />\n</template>\n"
	if err := os.WriteFile(filepath.Join(srcDir, "Sample.vue"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := reportCheckSource(io.Discard, dir); err != nil {
		t.Errorf("expected source gate to pass, got: %v", err)
	}
}

func TestCheckLocaleFailureIsFindings(t *testing.T) {
	enUS := "status:\n  checking: Checking...\n"
	de := "status:\n  checking: Wird geprüft…\n  removed: Veraltet\n"
	dir := setupLocaleTestRepo(t, enUS, de, true)

	err := reportCheckLocale(io.Discard, dir, "de", false)
	if !errors.Is(err, errFindings) {
		t.Errorf("check failure should be a findings error, got: %v", err)
	}
}

func TestCheckLocalePassesWithMissingKeys(t *testing.T) {
	enUS := "status:\n  checking: Checking...\n  done: Done\n"
	// de has only 1 key — missing keys are OK for the structural checks.
	de := "status:\n  checking: Wird geprüft…\n"
	dir := setupLocaleTestRepo(t, enUS, de, true)

	err := reportCheckLocale(io.Discard, dir, "de", false)
	if err != nil {
		t.Errorf("structural checks should pass with missing keys, got: %v", err)
	}
}

func TestCheckStrictFailsMissing(t *testing.T) {
	enUS := "status:\n  checking: Checking...\n  done: Done\n"
	de := "status:\n  checking: Wird geprüft…\n"
	dir := setupLocaleTestRepo(t, enUS, de, true)

	err := reportCheckLocale(io.Discard, dir, "de", true)
	if err == nil {
		t.Error("strict should fail with missing keys")
	}
}

func TestCheckStrictPasses(t *testing.T) {
	enUS := "status:\n  checking: Checking...\n  done: Done\n"
	de := "status:\n  checking: Wird geprüft…\n  done: Fertig\n"
	dir := setupLocaleTestRepo(t, enUS, de, true)

	err := reportCheckLocale(io.Discard, dir, "de", true)
	if err != nil {
		t.Errorf("strict should pass with complete translation, got: %v", err)
	}
}

func TestCheckLocaleFailsStale(t *testing.T) {
	enUS := "status:\n  checking: Checking...\n"
	// de has a stale key not in en-us.
	de := "status:\n  checking: Wird geprüft…\n  removed: Veraltet\n"
	dir := setupLocaleTestRepo(t, enUS, de, true)

	err := reportCheckLocale(io.Discard, dir, "de", false)
	if err == nil {
		t.Error("structural checks should fail with stale keys")
	}
}

func TestCheckStrictFailsDrift(t *testing.T) {
	enUS := "status:\n  checking: Checking...\n"
	de := "status:\n  checking: Wird geprüft…\n"
	dir := setupLocaleTestRepo(t, enUS, de, true)

	// Change English after metadata was generated.
	transDir := filepath.Join(dir, "pkg", "rancher-desktop", "assets", "translations")
	os.WriteFile(filepath.Join(transDir, "en-us.yaml"), []byte("status:\n  checking: Verifying...\n"), 0644)

	err := reportCheckLocale(io.Discard, dir, "de", true)
	if err == nil {
		t.Error("strict should fail with drifted keys")
	}
}

// setupRegistrationTestRepo builds a repo fixture with en-us.yaml (including
// locale display names) and the given locale files.
func setupRegistrationTestRepo(t *testing.T, localeFiles []string) string {
	t.Helper()
	dir := t.TempDir()
	transDir := filepath.Join(dir, "pkg", "rancher-desktop", "assets", "translations")
	os.MkdirAll(transDir, 0o755)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644)

	enUS := "locale:\n  de: German\n  en-us: English\n  fa: Farsi\n"
	os.WriteFile(filepath.Join(transDir, "en-us.yaml"), []byte(enUS), 0o644)

	for _, name := range localeFiles {
		os.WriteFile(filepath.Join(transDir, name), []byte("locale:\n  name: Test\n"), 0o644)
	}
	return dir
}

// writeLocaleStringUnion writes the LocaleString union that
// reportCheckRegistration inspects into a test repo directory.
func writeLocaleStringUnion(t *testing.T, dir, members string) {
	t.Helper()

	unionDir := filepath.Join(dir, "pkg", "rancher-desktop", "utils")
	os.MkdirAll(unionDir, 0o755)
	src := fmt.Sprintf("export type LocaleString = %s;\n", members)
	os.WriteFile(filepath.Join(unionDir, "translationLoader.ts"), []byte(src), 0o644)
}

// deUnion is the TypeScript union body for a fixture with only de.yaml.
const deUnion = "'de' | 'en-us'"

// checkRegistration derives the locale list from the fixture directory and
// runs reportCheckRegistration on it.
func checkRegistration(t *testing.T, dir string) error {
	t.Helper()
	locales, err := translationLocales(dir)
	if err != nil {
		t.Fatal(err)
	}
	return reportCheckRegistration(io.Discard, dir, locales)
}

func TestCheckRegistrationMatch(t *testing.T) {
	dir := setupRegistrationTestRepo(t, []string{"de.yaml"})
	writeLocaleStringUnion(t, dir, deUnion)

	if err := checkRegistration(t, dir); err != nil {
		t.Errorf("registration checks should pass: %v", err)
	}
}

func TestCheckRegistrationMissingFromLocaleString(t *testing.T) {
	dir := setupRegistrationTestRepo(t, []string{"de.yaml", "fa.yaml"})

	// LocaleString is missing "fa".
	writeLocaleStringUnion(t, dir, deUnion)

	if err := checkRegistration(t, dir); !errors.Is(err, errFindings) {
		t.Errorf("registration checks should fail with a finding when LocaleString is missing a locale, got: %v", err)
	}
}

func TestCheckRegistrationLocaleWithoutFile(t *testing.T) {
	dir := setupRegistrationTestRepo(t, []string{"de.yaml"})

	// LocaleString lists "fa", but fa.yaml does not exist.
	writeLocaleStringUnion(t, dir, deUnion+" | 'fa'")

	if err := checkRegistration(t, dir); !errors.Is(err, errFindings) {
		t.Errorf("registration checks should fail with a finding when LocaleString has a locale with no file, got: %v", err)
	}
}

func TestCheckRegistrationMissingDisplayName(t *testing.T) {
	dir := setupRegistrationTestRepo(t, []string{"de.yaml", "pt.yaml"})

	// en-us.yaml has no locale.pt display name.
	writeLocaleStringUnion(t, dir, deUnion+" | 'pt'")

	if err := checkRegistration(t, dir); err == nil {
		t.Error("registration checks should fail when en-us.yaml lacks a locale display name")
	}
}

// withDiscardedStdout runs fn with os.Stdout redirected to the null device, so
// a command that writes its report to os.Stdout leaves test output clean.
func withDiscardedStdout(t *testing.T, fn func()) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = devnull
	defer func() {
		os.Stdout = orig
		devnull.Close()
	}()
	fn()
}

// setupCheckRepo builds a full fixture runCheck can run against: en-us with two
// display names and a content key, a partial de translation, and the
// registration surfaces. When referenced is true, a component references every
// en-us key so the source gate passes; otherwise every key is unused and the
// source gate reports findings.
func setupCheckRepo(t *testing.T, referenced bool) string {
	t.Helper()
	dir := t.TempDir()
	transDir := filepath.Join(dir, "pkg", "rancher-desktop", "assets", "translations")
	os.MkdirAll(transDir, 0o755)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644)

	enUS := "locale:\n  de: German\n  en-us: English\nstatus:\n  running: Running\n"
	os.WriteFile(filepath.Join(transDir, "en-us.yaml"), []byte(enUS), 0o644)
	os.WriteFile(filepath.Join(transDir, "de.yaml"), []byte("status:\n  running: Läuft\n"), 0o644)
	bootstrapSource(t, dir)

	if referenced {
		compDir := filepath.Join(dir, "pkg", "rancher-desktop", "components")
		os.MkdirAll(compDir, 0o755)
		src := "<template>\n  <span v-t=\"'locale.de'\" />\n  <span v-t=\"'locale.en-us'\" />\n  <span v-t=\"'status.running'\" />\n</template>\n"
		os.WriteFile(filepath.Join(compDir, "Sample.vue"), []byte(src), 0o644)
	}

	writeLocaleStringUnion(t, dir, deUnion)
	return dir
}

// TestRunCheckRejectsBadLocaleAsOperational verifies that a misused --locale is
// an operational error, not a findings error. drift already reports a bad
// locale operationally; check must agree, so CI can tell a broken invocation
// from a real translation problem.
func TestRunCheckRejectsBadLocaleAsOperational(t *testing.T) {
	dir := setupCheckRepo(t, true)
	t.Chdir(dir)

	cases := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{"strict without locale", []string{"--strict"}, "--strict requires --locale"},
		{"source locale", []string{"--locale=en-us"}, "source locale"},
		{"unknown locale", []string{"--locale=xx"}, "no translation file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			withDiscardedStdout(t, func() { err = runCheck(tc.args) })
			if err == nil {
				t.Fatal("expected an operational error, got nil")
			}
			if errors.Is(err, errFindings) {
				t.Errorf("a misused --locale must be operational, not a finding: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

// TestRunCheckSourceFindingFailsOverall verifies that a source-gate finding
// fails the whole check even when registration and the per-locale checks pass.
func TestRunCheckSourceFindingFailsOverall(t *testing.T) {
	dir := setupCheckRepo(t, false)
	t.Chdir(dir)

	var err error
	withDiscardedStdout(t, func() { err = runCheck([]string{"--locale=de"}) })
	if !errors.Is(err, errFindings) {
		t.Errorf("a source-gate finding should fail the check as a finding, got: %v", err)
	}
}
