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
      :row-actions="false"
      :table-actions="false"
      :paging="true"
      :rows-per-page="10"
      :has-advanced-filtering="false"
      default-sort-by="Namespace"
      :loading="!limaVMs"
    >
      <template #col:Status="{ row } : { row: RowItem }">
        <td data-testid="virtual-machine-status-cell">
          <badge-state
            v-tooltip="{ content: row.status.message }"
            :color="row.status.state === 'Running' ? 'bg-success' : 'bg-darker'"
            :icon="row.status.state === 'Failed' ? 'icon-error' : ''"
            :label="t(`virtualMachines.status.${row.status.state.toLowerCase()}`)"
          />
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

import BadgeState from '@pkg/components/BadgeState.vue';
import Banner from '@pkg/components/Banner.vue';
import SortableTable from '@pkg/components/SortableTable';
import { mapTypedActions, mapTypedMutations, mapTypedState } from '@pkg/entry/store';
import { LimaVMStatus, limaVMStatus } from '@pkg/utils/limaVMStatus';
import parseQuantity from '@pkg/utils/parseQuantity';
import { formatSi } from '@pkg/utils/units';

/**
 * RowItem is one row of the table; `status` replaces the LimaVM's own status
 * with the state derived from it.
 */
interface RowItem {
  metadata:    { uid?: string, name?: string, namespace?: string };
  status:      LimaVMStatus;
  cpus?:       number;
  memoryBytes: number;
  memoryText:  string;
  diskBytes:   number;
  diskText:    string;
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
  components: { BadgeState, Banner, SortableTable },
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
          name:   'Namespace',
          label:  this.t('generic.namespace'),
          value:  'metadata.namespace',
          search: 'metadata.namespace',
          sort:   ['metadata.namespace', 'metadata.name'],
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

        return {
          metadata: {
            uid: vm.metadata?.uid, name: vm.metadata?.name, namespace: vm.metadata?.namespace,
          },
          status: limaVMStatus(vm),
          cpus:   vm.status?.cpus,
          memoryBytes,
          memoryText,
          diskBytes,
          diskText,
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
    ...mapTypedActions('rdd', ['watchResources', 'unwatchResources']),
    ...mapTypedMutations('rdd', ['SET_ERROR']),
  },
});
</script>
