// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import { jest } from '@jest/globals';
import { mount } from '@vue/test-utils';

import { limaVMStatus } from '@pkg/utils/limaVMStatus';
import mockModules from '@pkg/utils/testUtils/mockModules';
import { sortableTableGlobal } from '@pkg/utils/testUtils/sortableTable';
import { t } from '@pkg/utils/testUtils/translations';

const componentStub = { template: '<div />' };

mockModules({
  '@pkg/components/Banner.vue': componentStub,
  '@pkg/entry/store':           {
    mapTypedActions:   jest.fn(() => ({})),
    mapTypedMutations: jest.fn(() => ({})),
    mapTypedState:     jest.fn(() => ({})),
  },
  '@pkg/utils/ipcRenderer': { ipcRenderer: { invoke: jest.fn() } },
});

const { default: VirtualMachines } = await import('@pkg/pages/VirtualMachines.vue');

function condition(type: string, status: string, reason: string, message = '') {
  return {
    type, status, reason, message, lastTransitionTime: new Date(0),
  };
}

const created = condition('Created', 'True', 'Created');
const appOwner = {
  apiVersion: 'app.rancherdesktop.io/v1alpha1', kind: 'App', name: 'app', uid: 'app', controller: true,
};

function limaVM(namespace: string, name: string, running: boolean, conditions: any[] = [], extra: any = {}): any {
  return {
    metadata: { name, namespace, uid: `${ namespace }/${ name }`, ...extra.metadata },
    spec:     { running },
    status:   { conditions, ...extra.status },
  };
}

const limaVMs = [
  limaVM('b', 'zeta', true, [created, condition('Running', 'True', 'Started')], { status: { cpus: 4, memory: '6Gi', disk: '100Gi' } }),
  limaVM('a', 'wsl', false, [created, condition('Running', 'False', 'Stopped')]),
  limaVM('b', 'broken', true, [created, condition('Running', 'False', 'StartFailed', 'boom')], { status: { cpus: 2, memory: '1536Mi' } }),
];

/** Mount the page with the given LimaVMs, and return its table. */
function mountTable(vms: any[], mocks: Record<string, unknown> = {}) {
  const wrapper = mount(VirtualMachines, {
    global: sortableTableGlobal({
      error: undefined, limaVMs: vms, setHeader: jest.fn(), watchResources: () => Promise.resolve(), ...mocks,
    }),
  });

  return wrapper.findComponent({ name: 'SortableTable' });
}

describe('VirtualMachines', () => {
  it('groups the VMs by namespace, sorted by name', () => {
    const table = mountTable(limaVMs);
    const rows = table.findAll('tr.group-row, tr.main-row').map(row => row.findAll('td').map(td => td.text()));

    // A main row starts with its checkbox cell and ends with its action menu cell.
    expect(rows).toEqual([
      ['a'],
      ['', 'wsl', 'Stopped', '—', '—', '—', ''],
      ['b'],
      ['', 'broken', 'Failed', '2', '1.5 GiB', '—', ''],
      ['', 'zeta', 'Running', '4', '6 GiB', '100 GiB', ''],
    ]);
  });

  describe('actions', () => {
    const { methods } = VirtualMachines as any;
    const stopping = [created, condition('Running', 'True', 'Started')];

    it.each([
      ['a stopped VM', limaVM('ns', 'vm', false, [created, condition('Running', 'False', 'Stopped')]), [true, false, true]],
      ['a pending VM', limaVM('ns', 'vm', false), [true, false, true]],
      ['a VM that failed to start', limaVM('ns', 'vm', false, [created, condition('Running', 'False', 'StartFailed')]), [true, false, true]],
      ['a stopping VM', limaVM('ns', 'vm', false, stopping), [true, false, false]],
      ['a running VM', limaVM('ns', 'vm', true, stopping), [false, true, false]],
      ['a starting VM', limaVM('ns', 'vm', true, [created]), [false, true, false]],
      ['a failed VM that should run', limaVM('ns', 'vm', true, [created, condition('Running', 'False', 'StartFailed')]), [false, true, false]],
      ['the App\'s stopped VM', limaVM('rancher-desktop', 'rd', false, [], { metadata: { ownerReferences: [appOwner] } }), [false, false, false]],
      ['the App\'s running VM', limaVM('rancher-desktop', 'rd', true, stopping, { metadata: { ownerReferences: [appOwner] } }), [false, false, false]],
      ['a VM that the App owns without controlling it', limaVM('ns', 'vm', false, [], { metadata: { ownerReferences: [{ ...appOwner, controller: false }] } }), [true, false, true]],
    ])('sets which of Start, Stop and Delete are enabled for %s', (_, vm, expected) => {
      const actions = methods.getVirtualMachineActions.call({ t }, vm, limaVMStatus(vm));

      expect(actions.map((action: any) => action.action)).toEqual(['startVirtualMachine', 'stopVirtualMachine', 'deleteVirtualMachine']);
      expect(actions.map((action: any) => action.enabled)).toEqual(expected);
    });

    it.each([
      ['startVirtualMachine', limaVM('ns', 'vm', false), true],
      ['stopVirtualMachine', limaVM('ns', 'vm', true), false],
    ])('%s sets the VM\'s desired state', async(action, vm, running) => {
      const limaVMSetRunning = jest.fn(() => Promise.resolve());

      await mountTable([vm], { limaVMSetRunning }).props('rows')[0][action]();
      expect(limaVMSetRunning).toHaveBeenCalledWith({ namespace: 'ns', name: 'vm', running });
    });

    it('applies a bulk Stop only to the selected VMs it can stop', () => {
      const limaVMSetRunning = jest.fn(() => Promise.resolve());
      const vms = [
        limaVM('a', 'up', true),
        limaVM('a', 'down', false),
        limaVM('rancher-desktop', 'rd', true, [], { metadata: { ownerReferences: [appOwner] } }),
      ];
      const table = mountTable(vms, { limaVMSetRunning });

      table.vm.update(table.props('rows'), []);
      table.vm.applyTableAction({ action: 'stopVirtualMachine' });
      expect(limaVMSetRunning.mock.calls).toEqual([[{ namespace: 'a', name: 'up', running: false }]]);
    });
  });

  describe('deletion', () => {
    /** Build the row that SortableTable would render for a single LimaVM. */
    function rowFor(vm: any, context: any) {
      const self = {
        limaVMs:                  [vm],
        t,
        getVirtualMachineActions: () => [],
        ...context,
      };

      return (VirtualMachines as any).computed.rows.call(self)[0];
    }

    it('does not delete the VM when the user cancels', async() => {
      const limaVMDelete = jest.fn();
      const confirmDelete = jest.fn(() => Promise.resolve(false));

      await rowFor(limaVM('ns', 'vm', false), { limaVMDelete, confirmDelete }).deleteVirtualMachine();
      expect(confirmDelete).toHaveBeenCalledWith(['ns/vm']);
      expect(limaVMDelete).not.toHaveBeenCalled();
    });

    it('deletes the whole bulk selection once the user confirms', async() => {
      const limaVMDelete = jest.fn();
      const confirmDelete = jest.fn(() => Promise.resolve(true));
      const selection = [rowFor(limaVM('a', 'one', false), {}), rowFor(limaVM('b', 'two', false), {})];

      await rowFor(limaVM('a', 'one', false), { limaVMDelete, confirmDelete }).deleteVirtualMachine(selection);
      expect(confirmDelete).toHaveBeenCalledWith(['a/one', 'b/two']);
      expect(limaVMDelete.mock.calls).toEqual([[{ namespace: 'a', name: 'one' }], [{ namespace: 'b', name: 'two' }]]);
    });
  });
});
