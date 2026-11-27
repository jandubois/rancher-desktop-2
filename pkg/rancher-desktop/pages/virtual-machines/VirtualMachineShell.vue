<template>
  <div
    class="virtual-machine-shell-page"
    data-testid="virtual-machine-shell"
  >
    <banner
      v-if="errorMessage"
      color="error"
      @close="store.commit('rdd/SET_ERROR', undefined)"
    >
      {{ errorMessage }}
    </banner>
    <terminal-session
      class="virtual-machine-shell-component"
      endpoint="shell"
      :target="`${namespace}/${name}`"
      :target-running="isRunning"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeMount, onBeforeUnmount, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useStore } from 'vuex';

import Banner from '@pkg/components/Banner.vue';
import TerminalSession from '@pkg/components/TerminalSession.vue';
import { limaVMStatus } from '@pkg/utils/limaVMStatus';

import type { IoRancherdesktopLimaV1alpha1LimaVM as LimaVM } from '@rdd-client';

const route = useRoute();
const store = useStore();
const t = (key: string, args?: Record<string, unknown>) => store.getters['i18n/t'](key, args);

const namespace = computed(() => route.params.namespace as string);
const name = computed(() => route.params.name as string);

const vm = computed(() => {
  const vms: LimaVM[] = store.state.rdd.limaVMs ?? [];

  return vms.find(item => item.metadata?.namespace === namespace.value && item.metadata?.name === name.value);
});

const isRunning = computed(() => !!vm.value && limaVMStatus(vm.value).state === 'Running');

const errorMessage = computed(() => store.state.rdd.error ? `${ store.state.rdd.error }` : null);

watch([namespace, name], () => {
  store.dispatch('page/setHeader', {
    title:       name.value,
    description: t('virtualMachines.shell.description', { namespace: namespace.value }),
    action:      'VirtualMachineStatusBadge',
  });
}, { immediate: true });

onBeforeMount(() => {
  store.dispatch('rdd/watchResources', ['limaVMs']).catch(error => store.commit('rdd/SET_ERROR', error));
});

onBeforeUnmount(() => {
  store.dispatch('page/setAction', { action: '' });
  store.dispatch('rdd/unwatchResources', ['limaVMs']).catch(error => console.error(error));
});
</script>

<style lang="scss" scoped>
.virtual-machine-shell-page {
  flex: 1;
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  min-height: 0;
}

.virtual-machine-shell-component {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
</style>
