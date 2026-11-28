// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package capi is a Cluster API infrastructure provider. CAPI runs in the
// App's k3s cluster, and this package creates a Lima VM for each machine of
// the clusters that CAPI manages.
package capi

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"sigs.k8s.io/yaml"
)

// The bootstrap data holds Rancher's registration token, so the errors in this
// file must never quote any part of it. They name the field and what was
// expected instead.

const (
	// installScriptPath is where Rancher's cloud-config writes its install script.
	installScriptPath = "/usr/local/custom_script/install.sh"
	installRunCmd     = "sh " + installScriptPath
	maxScriptSize     = 1 << 20
)

type cloudConfig struct {
	WriteFiles []cloudConfigFile `json:"write_files"`
	RunCmd     []string          `json:"runcmd"`
}

type cloudConfigFile struct {
	Path        string `json:"path"`
	Encoding    string `json:"encoding"`
	Content     string `json:"content"`
	Permissions string `json:"permissions,omitempty"`
	Owner       string `json:"owner,omitempty"`
}

// InstallScript returns the install script from Rancher's CAPI bootstrap data.
// Rancher generates an optional "## template: jinja" line, a "#cloud-config"
// line, one gzip+b64 write_files entry for the script and a runcmd that runs
// it. InstallScript rejects any other cloud-config, because the provider
// applies only the install script and would otherwise skip the other settings
// without an error. The entry's permissions and owner are accepted but not
// applied. The guard script writes the install script for root only and runs
// it with sh, so neither matters.
func InstallScript(data []byte) ([]byte, error) {
	body, _ := bytes.CutPrefix(data, []byte("## template: jinja\n"))
	if !bytes.HasPrefix(body, []byte("#cloud-config\n")) {
		return nil, errors.New("bootstrap data does not start with a #cloud-config line")
	}
	var config cloudConfig
	// The parse error is dropped because YAML errors can quote the input.
	if err := yaml.UnmarshalStrict(body, &config); err != nil {
		return nil, errors.New("bootstrap data is not a cloud-config with only write_files and runcmd")
	}
	if len(config.WriteFiles) != 1 {
		return nil, fmt.Errorf("bootstrap data has %d write_files entries, expected 1", len(config.WriteFiles))
	}
	file := config.WriteFiles[0]
	if file.Path != installScriptPath {
		return nil, fmt.Errorf("bootstrap write_files path is not %s", installScriptPath)
	}
	if file.Encoding != "gzip+b64" {
		return nil, errors.New("bootstrap write_files encoding is not gzip+b64")
	}
	if len(config.RunCmd) != 1 || config.RunCmd[0] != installRunCmd {
		return nil, fmt.Errorf("bootstrap runcmd is not the single command %q", installRunCmd)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(file.Content))
	if err != nil {
		return nil, errors.New("bootstrap write_files content is not valid base64")
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, errors.New("bootstrap write_files content is not gzip data")
	}
	script, err := io.ReadAll(io.LimitReader(reader, maxScriptSize+1))
	if err != nil {
		return nil, errors.New("bootstrap write_files content is not valid gzip data")
	}
	if len(script) > maxScriptSize {
		return nil, fmt.Errorf("bootstrap install script is larger than %d bytes", maxScriptSize)
	}
	return script, nil
}
