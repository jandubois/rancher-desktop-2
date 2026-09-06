// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"testing"

	"github.com/containerd/containerd/v2/core/containers"
	"github.com/go-logr/logr"
	"gotest.tools/v3/assert"
)

// rawAny is a typeurl.Any carrying pre-marshalled bytes, which is all
// containerdProcessArgs reads off the record.
type rawAny []byte

//nolint:revive,staticcheck // typeurl.Any declares the method with this name.
func (r rawAny) GetTypeUrl() string { return "test" }
func (r rawAny) GetValue() []byte   { return r }

// specRecord wraps a runtime-spec JSON document the way containerd stores it
// on the container record. Feeding real JSON rather than a marshalled struct
// checks that the production unmarshal target matches the on-disk field names.
func specRecord(spec string) containers.Container {
	return containers.Container{ID: "test", Spec: rawAny(spec)}
}

func TestContainerdIOCreator(t *testing.T) {
	const logURI = "binary:///proc/self/exe?_NERDCTL_INTERNAL_LOGGING=/var/lib/nerdctl/1935db59"

	record := func(terminal bool, logDriver string) containers.Container {
		spec := `{"process":{"terminal":false}}`
		if terminal {
			spec = `{"process":{"terminal":true}}`
		}
		rec := specRecord(spec)
		if logDriver != "" {
			rec.Labels = map[string]string{"nerdctl/log-uri": logDriver}
		}
		return rec
	}

	// The creator is opaque until it builds its IO, and the config it builds
	// is what reaches CreateTaskRequest.
	config := func(t *testing.T, rec containers.Container) (stdout string, terminal bool) {
		t.Helper()
		creator, err := containerdIOCreator(logr.Discard(), rec)
		assert.NilError(t, err)
		io, err := creator("probe")
		assert.NilError(t, err)
		return io.Config().Stdout, io.Config().Terminal
	}

	t.Run("passes the log driver through", func(t *testing.T) {
		stdout, terminal := config(t, record(false, logURI))
		assert.Equal(t, stdout, logURI)
		assert.Equal(t, terminal, false)
	})

	t.Run("carries the terminal flag with the log driver", func(t *testing.T) {
		stdout, terminal := config(t, record(true, logURI))
		assert.Equal(t, stdout, logURI)
		assert.Equal(t, terminal, true)
	})

	t.Run("discards output for a container with no log driver", func(t *testing.T) {
		stdout, terminal := config(t, record(false, ""))
		assert.Equal(t, stdout, "")
		assert.Equal(t, terminal, false)
	})

	t.Run("treats the literal none as no log driver", func(t *testing.T) {
		stdout, _ := config(t, record(false, "none"))
		assert.Equal(t, stdout, "")
	})

	t.Run("ignores an unparsable log driver", func(t *testing.T) {
		stdout, _ := config(t, record(false, "%"))
		assert.Equal(t, stdout, "")
	})

	t.Run("refuses a terminal container with no log driver", func(t *testing.T) {
		_, err := containerdIOCreator(logr.Discard(), record(true, "none"))
		assert.ErrorContains(t, err, "records no log driver")
	})

	t.Run("assumes no terminal when the spec is unreadable", func(t *testing.T) {
		rec := specRecord("not json")
		rec.Labels = map[string]string{"nerdctl/log-uri": logURI}
		_, terminal := config(t, rec)
		assert.Equal(t, terminal, false)
	})
}
