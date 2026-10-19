<script lang="ts" setup>
import { computed } from 'vue';
import { useStore } from 'vuex';

import EngineSelector from '@pkg/components/EngineSelector.vue';
import RdFieldset from '@pkg/components/form/RdFieldset.vue';
import { IoRancherdesktopAppV1alpha1AppSpecContainerEngineNameEnum as ContainerEngineName } from '@rdd-client';

defineOptions({ name: 'preferences-container-engine-general' });

const store = useStore();
const isPreferenceLocked = computed(() => store.getters['preferences/isPreferenceLocked']);
const preferences = computed(() => store.getters['preferences/preferences']);
const currentContainerEngine = computed(() => preferences.value.containerEngine?.name ?? ContainerEngineName.Moby);

function onChangeEngine(value: ContainerEngineName) {
  store.dispatch('preferences/modify', { key: 'containerEngine.name', value });
}

</script>

<template>
  <div class="container-engine-general">
    <rd-fieldset
      data-test="containerEngine"
      :legend-text="t('containerEngine.label')"
      :is-locked="isPreferenceLocked('containerEngine.name')"
    >
      <template #default="{ isLocked }">
        <engine-selector
          :container-engine="currentContainerEngine"
          :is-locked="isLocked"
          @change="onChangeEngine"
        />
      </template>
    </rd-fieldset>
  </div>
</template>

<style lang="scss" scoped>
.container-engine-general {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
</style>
