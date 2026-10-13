// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package tracker

import (
	"maps"
	"sync"

	"github.com/Masterminds/log-go"

	"github.com/rancher-sandbox/rancher-desktop/src/ports"
)

// portStorage is responsible for storing all the port mappings.
type portStorage struct {
	// container ID is the key for both docker and containerd
	portmap map[string]ports.PortMap
	mutex   sync.Mutex
}

func newPortStorage() *portStorage {
	return &portStorage{
		portmap: make(map[string]ports.PortMap),
	}
}

func (p *portStorage) add(containerID string, portMap ports.PortMap) {
	p.mutex.Lock()
	p.portmap[containerID] = portMap
	p.mutex.Unlock()
	log.Debugf("portStorage add status: %+v", p.portmap)
}

func (p *portStorage) get(containerID string) ports.PortMap {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	log.Debugf("portStorage get status: %+v", p.portmap)

	if portMap, ok := p.portmap[containerID]; ok {
		return portMap
	}

	return nil
}

func (p *portStorage) removeAll() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	for containerID, portMap := range p.portmap {
		log.Debugf("removing the following container [%s] port binding: %+v", containerID, portMap)
		delete(p.portmap, containerID)
	}
}

func (p *portStorage) getAll() map[string]ports.PortMap {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	portMappings := make(map[string]ports.PortMap, len(p.portmap))

	for k, v := range p.portmap {
		portMappings[k] = maps.Clone(v)
	}

	return portMappings
}

func (p *portStorage) remove(containerID string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.portmap, containerID)
	log.Debugf("portStorage remove status: %+v", p.portmap)
}
