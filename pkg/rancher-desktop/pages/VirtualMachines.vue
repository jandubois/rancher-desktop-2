<template>
  <div class="virtual-machines">
    <banner
      v-if="errorMessage"
      color="error"
      data-testid="error-banner"
      @close="SET_ERROR(undefined)"
    >
      {{ errorMessage }}
    </banner>
    <SortableTable
      data-testid="virtual-machines-table"
      :headers="headers"
      key-field="metadata.uid"
      :rows="rows"
      no-rows-key="virtualMachines.sortableTables.noRows"
      :row-actions="true"
      :paging="true"
      :rows-per-page="10"
      :has-advanced-filtering="false"
      group-by="metadata.namespace"
      :group-sort="['metadata.namespace']"
      default-sort-by="Name"
      :loading="!limaVMs"
    >
      <template #col:Status="{ row } : { row: RowItem }">
        <td data-testid="virtual-machine-status-cell">
          <virtual-machine-status-badge :vm="row.vm" />
        </td>
      </template>
      <template #col:CPUs="{ row } : { row: RowItem }">
        <td data-testid="virtual-machine-cpus-cell">
          <span
            v-if="row.cpus === undefined"
            class="text-muted"
          >&mdash;</span>
          <template v-else>
            {{ row.cpus }}
          </template>
        </td>
      </template>
      <template #col:Memory="{ row } : { row: RowItem }">
        <td data-testid="virtual-machine-memory-cell">
          <span
            v-if="!row.memoryText"
            class="text-muted"
          >&mdash;</span>
          <template v-else>
            {{ row.memoryText }}
          </template>
        </td>
      </template>
      <template #col:Disk="{ row } : { row: RowItem }">
        <td data-testid="virtual-machine-disk-cell">
          <span
            v-if="!row.diskText"
            class="text-muted"
          >&mdash;</span>
          <template v-else>
            {{ row.diskText }}
          </template>
        </td>
      </template>
    </SortableTable>
  </div>
</template>

<script lang="ts">
import { defineComponent } from 'vue';

import Banner from '@pkg/components/Banner.vue';
import SortableTable from '@pkg/components/SortableTable';
import VirtualMachineStatusBadge from '@pkg/components/VirtualMachineStatusBadge.vue';
import { mapTypedActions, mapTypedMutations, mapTypedState } from '@pkg/entry/store';
import { showDeleteConfirmation } from '@pkg/utils/deleteConfirmation';
import { LimaVMStatus, limaVMStatus } from '@pkg/utils/limaVMStatus';
import parseQuantity from '@pkg/utils/parseQuantity';
import { formatSi } from '@pkg/utils/units';

import type { IoRancherdesktopLimaV1alpha1LimaVM as LimaVM } from '@rdd-client';

interface Action {
  label:       string;
  action:      string;
  enabled:     boolean;
  bulkable:    boolean;
  bulkAction?: string;
}

/**
 * RowItem is one row of the table; `status` replaces the LimaVM's own status
 * with the state derived from it.
 */
interface RowItem {
  metadata:             { uid?: string, name?: string, namespace?: string };
  vm:                   LimaVM;
  status:               LimaVMStatus;
  cpus?:                number;
  memoryBytes:          number;
  memoryText:           string;
  diskBytes:            number;
  diskText:             string;
  availableActions:     Action[];
  startVirtualMachine:  () => Promise<void>;
  stopVirtualMachine:   () => Promise<void>;
  deleteVirtualMachine: (rows?: RowItem[]) => Promise<void>;
  openShell:            () => void;
}

/**
 * Whether an App controls the LimaVM. The App controller starts and stops
 * that VM whenever the App starts and stops, so the page leaves it alone.
 */
function isAppOwned(vm: LimaVM): boolean {
  return (vm.metadata?.ownerReferences ?? []).some(ref => ref.controller && ref.kind === 'App');
}

/** Return a quantity's bytes and its text in binary units; [0, ''] if it is missing, zero or invalid. */
function formatQuantity(quantity: string | number | undefined): [number, string] {
  try {
    const bytes = parseQuantity(quantity);

    if (bytes > 0) {
      return [bytes, formatSi(bytes, { increment: 1024, suffix: 'iB', maxPrecision: 1 })];
    }
  } catch {
    // An unparseable quantity shows as missing.
  }
  return [0, ''];
}

export default defineComponent({
  name:       'VirtualMachines',
  components: { Banner, SortableTable, VirtualMachineStatusBadge },
  computed:   {
    ...mapTypedState('rdd', ['error', 'limaVMs']),
    headers() {
      return [
        {
          name:   'Name',
          label:  this.t('virtualMachines.table.header.name'),
          value:  'metadata.name',
          search: 'metadata.name',
          sort:   ['metadata.name', 'metadata.namespace'],
        },
        {
          name:   'Status',
          label:  this.t('virtualMachines.table.header.status'),
          search: 'status.state',
          sort:   ['status.state', 'metadata.namespace', 'metadata.name'],
        },
        {
          name:   'CPUs',
          label:  this.t('virtualMachines.table.header.cpus'),
          search: false,
          sort:   ['cpus', 'metadata.namespace', 'metadata.name'],
        },
        {
          name:   'Memory',
          label:  this.t('virtualMachines.table.header.memory'),
          search: false,
          sort:   ['memoryBytes', 'metadata.namespace', 'metadata.name'],
        },
        {
          name:   'Disk',
          label:  this.t('virtualMachines.table.header.disk'),
          search: false,
          sort:   ['diskBytes', 'metadata.namespace', 'metadata.name'],
        },
      ];
    },
    rows(): RowItem[] {
      return (this.limaVMs ?? []).map((vm) => {
        const [memoryBytes, memoryText] = formatQuantity(vm.status?.memory);
        const [diskBytes, diskText] = formatQuantity(vm.status?.disk);
        const status = limaVMStatus(vm);
        const namespace = vm.metadata?.namespace ?? '';
        const name = vm.metadata?.name ?? '';

        return {
          metadata: {
            uid: vm.metadata?.uid, name, namespace,
          },
          vm,
          status,
          cpus:                 vm.status?.cpus,
          memoryBytes,
          memoryText,
          diskBytes,
          diskText,
          availableActions:     this.getVirtualMachineActions(vm, status),
          startVirtualMachine:  () => this.limaVMSetRunning({ namespace, name, running: true }),
          stopVirtualMachine:   () => this.limaVMSetRunning({ namespace, name, running: false }),
          openShell:            () => {
            this.$router.push({ name: 'virtual-machine-shell', params: { namespace, name } });
          },
          deleteVirtualMachine: async(rows?: RowItem[]) => {
            // The context menu passes the table's live selection, and the
            // dialog leaves the table usable. Copy it so that the delete
            // matches what the dialog lists. The page shows every namespace
            // at once, so the dialog lists each VM with its namespace.
            const targets = rows?.length ? rows.map(row => row.metadata) : [{ namespace, name }];

            if (await this.confirmDelete(targets.map(target => `${ target.namespace }/${ target.name }`))) {
              await Promise.all(targets.map(target => this.limaVMDelete({
                namespace: target.namespace ?? '', name: target.name ?? '',
              })));
            }
          },
        };
      });
    },
    errorMessage(): string | null {
      return this.error ? `${ this.error }` : null;
    },
  },
  beforeMount() {
    this.watchResources(['limaVMs']).catch(error => this.SET_ERROR(error));
  },
  mounted() {
    this.setHeader({ titleKey: 'virtualMachines.title' });
  },
  beforeUnmount() {
    this.unwatchResources(['limaVMs']);
  },
  methods: {
    ...mapTypedActions('page', ['setHeader']),
    ...mapTypedActions('rdd', ['limaVMDelete', 'limaVMSetRunning', 'watchResources', 'unwatchResources']),
    ...mapTypedMutations('rdd', ['SET_ERROR']),
    /**
     * Return the row menu and bulk actions for a LimaVM. Start and Stop set
     * `spec.running`, so each is enabled only when it would change that
     * field. Delete also needs a VM that has stopped, has failed or has not
     * been created yet. These three leave the App's own VM alone. Shell
     * does not change the VM, so it needs only a running VM, the App's too.
     */
    getVirtualMachineActions(vm: LimaVM, status: LimaVMStatus): Action[] {
      const editable = !isAppOwned(vm);
      const running = !!vm.spec?.running;
      const atRest = ['Stopped', 'Pending', 'Failed'].includes(status.state);

      return [
        {
          label:    this.t('virtualMachines.action.start'),
          action:   'startVirtualMachine',
          enabled:  editable && !running,
          bulkable: true,
        },
        {
          label:    this.t('virtualMachines.action.stop'),
          action:   'stopVirtualMachine',
          enabled:  editable && running,
          bulkable: true,
        },
        {
          label:      this.t('virtualMachines.action.delete'),
          action:     'deleteVirtualMachine',
          enabled:    editable && !running && atRest,
          bulkable:   true,
          bulkAction: 'deleteVirtualMachine',
        },
        {
          label:    this.t('virtualMachines.action.shell'),
          action:   'openShell',
          enabled:  status.state === 'Running',
          bulkable: false,
        },
      ];
    },
    /**
     * Ask the user to confirm deleting the named virtual machines.
     */
    async confirmDelete(names: string[]): Promise<boolean> {
      return await showDeleteConfirmation({
        title:   this.t('virtualMachines.confirmDelete.title'),
        message: this.t('virtualMachines.confirmDelete.message', { count: names.length }),
        confirm: this.t('virtualMachines.confirmDelete.confirm'),
        cancel:  this.t('generic.cancel'),
        names,
      });
    },
  },
});
</script>
