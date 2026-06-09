// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
package xz

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ulikunitz "github.com/ulikunitz/xz"
	"gotest.tools/v3/assert"
)

// sample.txt.xz is produced by the xz CLI (xz -9), so this verifies
// compatibility with real xz output rather than only our own round-trip.
func TestDecompressFixture(t *testing.T) {
	compressed := readFixture(t, "sample.txt.xz")

	var got bytes.Buffer
	assert.NilError(t, Decompress(context.Background(), bytes.NewReader(compressed), &got))
	assert.DeepEqual(t, got.Bytes(), fixturePlaintext())
}

func TestDecompressRoundTrip(t *testing.T) {
	// Larger than bufSize so the buffered input path is exercised.
	want := bytes.Repeat([]byte("rancher-desktop xz round-trip payload\n"), 200_000)

	var compressed bytes.Buffer
	w, err := ulikunitz.NewWriter(&compressed)
	assert.NilError(t, err)
	_, err = w.Write(want)
	assert.NilError(t, err)
	assert.NilError(t, w.Close())

	var got bytes.Buffer
	assert.NilError(t, Decompress(context.Background(), bytes.NewReader(compressed.Bytes()), &got))
	assert.Equal(t, got.Len(), len(want))
	assert.Assert(t, bytes.Equal(got.Bytes(), want))
}

func TestDecompressTruncated(t *testing.T) {
	compressed := readFixture(t, "sample.txt.xz")

	var got bytes.Buffer
	err := Decompress(context.Background(), bytes.NewReader(compressed[:len(compressed)/2]), &got)
	assert.Assert(t, err != nil, "truncated xz stream must fail")
}

func TestDecompressFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sample.txt.xz")
	assert.NilError(t, os.WriteFile(src, readFixture(t, "sample.txt.xz"), 0o644))
	want := fixturePlaintext()

	dst := filepath.Join(dir, "image")
	assert.NilError(t, DecompressFile(context.Background(), src, dst))

	got, err := os.ReadFile(dst)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, want)
	assertNoTempFiles(t, dir)
}

func TestDecompressFileLeavesNoPartialOutput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "truncated.xz")
	fixture := readFixture(t, "sample.txt.xz")
	assert.NilError(t, os.WriteFile(src, fixture[:len(fixture)/2], 0o644))

	dst := filepath.Join(dir, "image")
	assert.Assert(t, DecompressFile(context.Background(), src, dst) != nil)

	_, err := os.Stat(dst)
	assert.Assert(t, os.IsNotExist(err), "dst must not exist after a failed decode")
	assertNoTempFiles(t, dir)
}

// A canceled context must abort the decode and leave neither a partial output
// nor a leftover temp file behind.
func TestDecompressFileCanceledContext(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sample.txt.xz")
	assert.NilError(t, os.WriteFile(src, readFixture(t, "sample.txt.xz"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dst := filepath.Join(dir, "image")
	err := DecompressFile(ctx, src, dst)
	assert.Assert(t, errors.Is(err, context.Canceled), "want context.Canceled, got %v", err)

	_, statErr := os.Stat(dst)
	assert.Assert(t, os.IsNotExist(statErr), "dst must not exist after a canceled decode")
	assertNoTempFiles(t, dir)
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	assert.NilError(t, err)
	return data
}

// fixturePlaintext reconstructs the content compressed into sample.txt.xz. It is
// built from \n literals, so the expectation does not depend on how git checks
// out line endings on the test host.
func fixturePlaintext() []byte {
	const block = "rancher-desktop-daemon in-process xz decoder fixture.\n" +
		"The quick brown fox jumps over the lazy dog.\n"
	return bytes.Repeat([]byte(block), 200)
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	assert.NilError(t, err)
	for _, e := range entries {
		assert.Assert(t, !strings.Contains(e.Name(), ".tmp-"), "leftover temp file: %s", e.Name())
	}
}
