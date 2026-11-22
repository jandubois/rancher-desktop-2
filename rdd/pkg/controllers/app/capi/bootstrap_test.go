// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// secret stands in for Rancher's registration token, which errors must never
// contain.
const secret = "SECRET-TOKEN-MARKER"

func gzipBase64(t *testing.T, data []byte) string {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(data)
	assert.NilError(t, err)
	assert.NilError(t, w.Close())
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// cloudConfigFor returns bootstrap data in the form that Rancher generates,
// with fileExtra appended to the write_files list and topExtra to the top
// level.
func cloudConfigFor(header, content, fileExtra, topExtra string) []byte {
	return []byte(header + "#cloud-config\n" +
		"write_files:\n" +
		"- path: /usr/local/custom_script/install.sh\n" +
		"  permissions: \"0755\"\n" +
		"  encoding: gzip+b64\n" +
		"  content: " + content + "\n" +
		fileExtra +
		"runcmd:\n" +
		"- sh /usr/local/custom_script/install.sh\n" +
		topExtra)
}

func TestInstallScriptAccepts(t *testing.T) {
	script := []byte("#!/bin/sh\necho " + secret + "\n")
	content := gzipBase64(t, script)
	for _, header := range []string{"", "## template: jinja\n"} {
		got, err := InstallScript(cloudConfigFor(header, content, "", ""))
		assert.NilError(t, err, "header %q", header)
		assert.Assert(t, bytes.Equal(got, script), "header %q: script differs", header)
	}
}

func TestInstallScriptRejects(t *testing.T) {
	content := gzipBase64(t, []byte("echo "+secret+"\n"))
	valid := string(cloudConfigFor("", content, "", ""))
	tests := map[string]string{
		"missing header": strings.TrimPrefix(valid, "#cloud-config\n"),
		"extra key":      string(cloudConfigFor("", content, "", "users: ["+secret+"]\n")),
		"two write_files": string(cloudConfigFor("", content,
			"- path: /usr/local/custom_script/install.sh\n  encoding: gzip+b64\n  content: "+content+"\n", "")),
		"wrong path":     strings.Replace(valid, "custom_script/install.sh\n  perm", "custom_script/"+secret+"\n  perm", 1),
		"wrong encoding": strings.Replace(valid, "gzip+b64", "b64", 1),
		"wrong runcmd":   strings.Replace(valid, "- sh /usr", "- "+secret+" /usr", 1),
		"not base64":     strings.Replace(valid, content, secret+"!", 1),
		"oversize":       string(cloudConfigFor("", gzipBase64(t, bytes.Repeat([]byte(secret), maxScriptSize/len(secret)+1)), "", "")),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := InstallScript([]byte(data))
			assert.Assert(t, err != nil)
			assert.Assert(t, !strings.Contains(err.Error(), secret), "error leaks the input: %v", err)
		})
	}
}
