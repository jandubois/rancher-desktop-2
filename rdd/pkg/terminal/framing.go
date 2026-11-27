// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package terminal

import (
	"encoding/json"
	"net/http"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/remotecommand"
)

// maxInput is the largest input payload a client may send in one message.
const maxInput = 64 << 10

// frame returns a message for the channel with the payload.
func frame(channel byte, payload []byte) []byte {
	msg := make([]byte, len(payload)+1)
	msg[0] = channel
	copy(msg[1:], payload)
	return msg
}

// termSize is a terminal size as channel 4 sends it.
type termSize struct {
	Width  int
	Height int
}

// parseSize decodes a resize payload, and reports false for one that is
// malformed or has a dimension outside 1 to 65535.
func parseSize(payload []byte) (termSize, bool) {
	var size termSize
	if err := json.Unmarshal(payload, &size); err != nil {
		return size, false
	}
	valid := func(n int) bool { return n > 0 && n <= 65535 }
	return size, valid(size.Width) && valid(size.Height)
}

// exitStatus returns the status for a shell that ended with the code and
// error that Wait returned.
func exitStatus(code int, err error) *metav1.Status {
	switch {
	case err != nil:
		return errorStatus(err)
	case code == 0:
		return &metav1.Status{Status: metav1.StatusSuccess}
	default:
		return &metav1.Status{
			Status:  metav1.StatusFailure,
			Reason:  remotecommand.NonZeroExitCodeReason,
			Message: "command terminated with non-zero exit code " + strconv.Itoa(code),
			Details: &metav1.StatusDetails{
				Causes: []metav1.StatusCause{{
					Type:    remotecommand.ExitCodeCauseType,
					Message: strconv.Itoa(code),
				}},
			},
		}
	}
}

// errorStatus returns the status for a shell that failed to start or ended
// without an exit status.
func errorStatus(err error) *metav1.Status {
	return &metav1.Status{
		Status:  metav1.StatusFailure,
		Message: err.Error(),
	}
}

// conflictStatus returns the status for a client whose session another client
// took over.
func conflictStatus() *metav1.Status {
	return &metav1.Status{
		Status:  metav1.StatusFailure,
		Reason:  metav1.StatusReasonConflict,
		Code:    http.StatusConflict,
		Message: "another client attached to the session",
	}
}

// statusFrame returns the channel-3 message for the status.
func statusFrame(status *metav1.Status) []byte {
	data, err := json.Marshal(status)
	if err != nil {
		// metav1.Status always marshals.
		panic(err)
	}
	return frame(remotecommand.StreamErr, data)
}
