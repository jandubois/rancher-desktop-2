// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package v1alpha1

import (
	"encoding/json"
	"testing"

	"gotest.tools/v3/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestOptionalTimestampsAreOmitted guards the omitzero tag on every optional
// timestamp. omitempty alone does not omit a struct, so without omitzero an
// unset metav1.Time reaches the API server as null.
func TestOptionalTimestampsAreOmitted(t *testing.T) {
	tests := []struct {
		name   string
		status any
		fields []string
	}{
		{"ContainerStatus", ContainerStatus{}, []string{"createdAt", "startedAt", "finishedAt"}},
		{"ContainerLastAction", ContainerLastAction{}, []string{"observedAt", "completedAt"}},
		{"ImageStatus", ImageStatus{}, []string{"createdAt"}},
		{"VolumeStatus", VolumeStatus{}, []string{"createdAt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.status)
			assert.NilError(t, err)
			var encoded map[string]json.RawMessage
			assert.NilError(t, json.Unmarshal(data, &encoded))
			for _, field := range tc.fields {
				_, present := encoded[field]
				assert.Assert(t, !present, "%s should be absent when unset, got %s", field, data)
			}
		})
	}
}

func TestSetTimestampIsEncoded(t *testing.T) {
	data, err := json.Marshal(ContainerStatus{StartedAt: metav1.Unix(1700000000, 0)})
	assert.NilError(t, err)
	var encoded struct {
		StartedAt string `json:"startedAt"`
	}
	assert.NilError(t, json.Unmarshal(data, &encoded))
	assert.Equal(t, encoded.StartedAt, "2023-11-14T22:13:20Z")
}
