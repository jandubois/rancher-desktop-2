<template>
  <badge-state
    v-if="status"
    v-tooltip="{ content: status.message }"
    :color="status.state === 'Running' ? 'bg-success' : 'bg-darker'"
    :icon="status.state === 'Failed' ? 'icon-error' : ''"
    :label="t(`virtualMachines.status.${status.state.toLowerCase()}`)"
    data-testid="virtual-machine-state"
  />
</template>

<script lang="ts">
import { defineComponent, PropType } from 'vue';

import BadgeState from '@pkg/components/BadgeState.vue';
import { mapTypedState } from '@pkg/entry/store';
import { LimaVMStatus, limaVMStatus } from '@pkg/utils/limaVMStatus';

import type { IoRancherdesktopLimaV1alpha1LimaVM as LimaVM } from '@rdd-client';

/**
 * The status badge of the `vm` prop, or else of the LimaVM that matches the
 * route's `namespace` and `name` params. A page header shows the badge
 * without props, so in a header it finds the VM from the route.
 */
export default defineComponent({
  name:       'VirtualMachineStatusBadge',
  components: { BadgeState },
  props:      {
    vm: {
      type:    Object as PropType<LimaVM | undefined>,
      default: undefined,
    },
  },
  computed: {
    ...mapTypedState('rdd', ['limaVMs']),
    currentVM(): LimaVM | undefined {
      if (this.vm) {
        return this.vm;
      }
      const { namespace, name } = this.$route.params;

      return (this.limaVMs ?? []).find(vm => vm.metadata?.namespace === namespace && vm.metadata?.name === name);
    },
    status(): LimaVMStatus | undefined {
      return this.currentVM ? limaVMStatus(this.currentVM) : undefined;
    },
  },
});
</script>
