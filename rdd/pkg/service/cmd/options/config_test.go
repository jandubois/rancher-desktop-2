// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
package options

import (
	"net/http"
	"testing"

	"gotest.tools/v3/assert"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/server/filters"
)

func TestPassthroughWebSocketsAreLongRunning(t *testing.T) {
	isLongRunning := passthroughWebSocketsAreLongRunning(filters.BasicLongRunningRequestCheck(
		sets.NewString("watch", "proxy"),
		sets.NewString("attach", "exec", "proxy", "log", "portforward"),
	))
	newRequest := func(path string, upgrade bool) (*http.Request, *request.RequestInfo) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1"+path, http.NoBody)
		assert.NilError(t, err)
		if upgrade {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
		}
		return req, &request.RequestInfo{Path: path, Verb: "get"}
	}

	req, info := newRequest("/passthrough/engine/logs/abc", true)
	assert.Assert(t, isLongRunning(req, info), "passthrough WebSocket")

	req, info = newRequest("/passthrough/demo/hello", false)
	assert.Assert(t, !isLongRunning(req, info), "plain passthrough request")

	req, info = newRequest("/healthz", true)
	assert.Assert(t, !isLongRunning(req, info), "WebSocket outside passthrough")

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1/api/v1/pods?watch=true", http.NoBody)
	assert.NilError(t, err)
	info = &request.RequestInfo{IsResourceRequest: true, Path: "/api/v1/pods", Verb: "watch", Resource: "pods"}
	assert.Assert(t, isLongRunning(req, info), "resource watch")
}
