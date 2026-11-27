// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import { jest } from '@jest/globals';
import { mount } from '@vue/test-utils';

import mockModules from '@pkg/utils/testUtils/mockModules';
import { sortableTableGlobal } from '@pkg/utils/testUtils/sortableTable';

const componentStub = { template: '<div />' };

mockModules({
  '@pkg/components/Banner.vue': componentStub,
  '@pkg/entry/store':           {
    mapTypedActions:   jest.fn(() => ({})),
    mapTypedMutations: jest.fn(() => ({})),
    mapTypedState:     jest.fn(() => ({})),
  },
});

const { default: VirtualMachines } = await import('@pkg/pages/VirtualMachines.vue');

function condition(type: string, status: string, reason: string, message = '') {
  return {
    type, status, reason, message, lastTransitionTime: new Date(0),
  };
}

const limaVMs = [
  {
    metadata: { name: 'zeta', namespace: 'b', uid: 'zeta' },
    spec:     { running: true },
    status:   {
      cpus: 4, memory: '6Gi', disk: '100Gi', conditions: [condition('Created', 'True', 'Created'), condition('Running', 'True', 'Started')],
    },
  },
  {
    metadata: { name: 'wsl', namespace: 'a', uid: 'wsl' },
    spec:     { running: false },
    status:   { conditions: [condition('Created', 'True', 'Created'), condition('Running', 'False', 'Stopped')] },
  },
  {
    metadata: { name: 'broken', namespace: 'b', uid: 'broken' },
    spec:     { running: true },
    status:   { cpus: 2, memory: '1536Mi', conditions: [condition('Created', 'True', 'Created'), condition('Running', 'False', 'StartFailed', 'boom')] },
  },
];

describe('VirtualMachines', () => {
  it('shows each VM sorted by namespace, then name', () => {
    const wrapper = mount(VirtualMachines, {
      global: sortableTableGlobal({
        error: undefined, limaVMs, setHeader: jest.fn(), watchResources: () => Promise.resolve(),
      }),
    });
    const cells = wrapper.findAll('tr.main-row').map(row => row.findAll('td').map(td => td.text()));

    expect(cells).toEqual([
      ['wsl', 'a', 'Stopped', '—', '—', '—'],
      ['broken', 'b', 'Failed', '2', '1.5 GiB', '—'],
      ['zeta', 'b', 'Running', '4', '6 GiB', '100 GiB'],
    ]);
  });
});
