// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package mock

import (
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

var containerIDValidator = regexp.MustCompile(`^[0-9a-fA-F]+$`)

const (
	// logWriterLabel is the label key used to specify which log writing function
	// to use for a container.
	logWriterLabel = "io.rancherdesktop.mock.logs.writer"
	// logWriterLabels is the value for the logWriterLabel that indicates that the
	// log writing function should write the labels of the container.
	logWriterLabels = "labels"
	// logWriterTen is the value for the logWriterLabel that indicates that the
	// log writing function should write 10 lines of logs.
	logWriterTen = "ten"
	// logWriterHundred is the value for the logWriterLabel that indicates that
	// the log writing function should write 100 lines of logs.
	logWriterHundred = "hundred"
	// logWriterLoop is the value for the logWriterLabel that indicates that the
	// log writing function should write logs in an infinite loop with a one
	// second delay between each line.
	logWriterLoop = "loop"
)

// handleLogs is the handler for the `/passthrough/mock/logs/{container}`
// endpoint.  It exists for providing fake logs for testing and screenshots.
func (c *controller) handleLogs(w http.ResponseWriter, r *http.Request) {
	log := ctrl.LoggerFrom(r.Context())
	containerID, _, _ := strings.Cut(strings.TrimLeft(r.URL.Path, "/"), "/")
	log.Info("Handling logs request", "container", containerID)

	if !containerIDValidator.MatchString(containerID) {
		log.V(5).Info("Invalid container ID", "container", containerID)
		http.Error(w, "Invalid container ID", http.StatusBadRequest)
		return
	}

	var container v1alpha1.Container
	err := c.mgr.GetClient().Get(r.Context(), types.NamespacedName{
		Namespace: apiNamespace,
		Name:      containerID,
	}, &container)
	if err != nil {
		log.V(5).Info("Failed to get container", "error", err)
		http.Error(w, "Failed to get container", http.StatusNotFound)
		return
	}

	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.V(5).Info("Failed to upgrade to WebSocket", "error", err)
		return
	}
	defer conn.Close()
	defer func() {
		message := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Closing connection")
		err := conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(time.Second))
		if err != nil {
			log.V(5).Info("Failed to close WebSocket", "error", err)
		}
	}()

	// Handle WebSocket control messages
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.V(5).Info("WebSocket closed", "error", err)
				_ = conn.Close()
				return
			}
		}
	}()

	// Write a message per line to simulate the real logs better.
	write := func(line string) error {
		err := conn.WriteMessage(websocket.BinaryMessage, []byte(line))
		if err != nil {
			log.V(5).Info("Failed to write WebSocket message", "error", err)
		}
		return err
	}

	logWriterMap := map[string]func(func(string) error, *v1alpha1.Container){
		logWriterLabels:  c.writeLogsLabels,
		logWriterTen:     c.writeLogsCount(10),
		logWriterHundred: c.writeLogsCount(100),
		logWriterLoop:    c.writeLogsLoop,
	}

	logWriter, ok := logWriterMap[container.Status.Labels[logWriterLabel]]
	if !ok {
		logWriter = c.writeLogsLabels
	}

	logWriter(write, &container)
}

// writeLogsLabels writes the labels of the container to the provided write function.
// This is used as the fallback when the log writing function is not specified.
func (c *controller) writeLogsLabels(write func(string) error, container *v1alpha1.Container) {
	if err := write(fmt.Sprintf("Logs for container %s\n", container.ObjectMeta.Name)); err != nil {
		return
	}
	keys := slices.Sorted(maps.Keys(container.Status.Labels))
	for _, k := range keys {
		err := write(fmt.Sprintf("Label: %s\t%s\n", k, container.Status.Labels[k]))
		if err != nil {
			return
		}
	}
}

// writeLogsCount writes lines of "Line %d" to the provided write function,
// where %d is the line number.  The logs are written immediately.
func (c *controller) writeLogsCount(n int) func(write func(string) error, _ *v1alpha1.Container) {
	return func(write func(string) error, _ *v1alpha1.Container) {
		for i := 1; i <= n; i++ {
			err := write(fmt.Sprintf("[Line %d]\n", i))
			if err != nil {
				return
			}
		}
	}
}

// writeLogsLoop writes lines of "Line %d" to the provided write function,
// where %d is the line number.  The logs are written with a one second delay
// between each line.  This does not terminate.
func (c *controller) writeLogsLoop(write func(string) error, _ *v1alpha1.Container) {
	for i := 1; ; i++ {
		err := write(fmt.Sprintf("[Line %d]\n", i))
		if err != nil {
			return
		}
		time.Sleep(time.Second)
	}
}
