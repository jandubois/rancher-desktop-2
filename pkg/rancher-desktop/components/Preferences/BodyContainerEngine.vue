<script lang="ts" setup>
import { computed } from 'vue';
import { useStore } from 'vuex';

import PreferencesContainerEngineAllowedImages from '@pkg/components/Preferences/ContainerEngineAllowedImages.vue';
import PreferencesContainerEngineGeneral from '@pkg/components/Preferences/ContainerEngineGeneral.vue';
import RdTabbed from '@pkg/components/Tabbed/RdTabbed.vue';
import Tab from '@pkg/components/Tabbed/Tab.vue';
import type { TransientPreferencesState } from '@pkg/types/transientPreferences';

import type { Component, ComputedRef } from 'vue';

defineOptions({ name: 'preferences-body-container-engine' });

type tabName = TransientPreferencesState['navigation']['preferences']['container-engine'];

const store = useStore();
const navigation = computed(() => store.state['transient-preferences'].navigation);
const activeTab = computed((): tabName => navigation.value?.preferences?.['container-engine'] || 'general');
const componentFromTab: ComputedRef<Component> = computed(() => {
  return ({
    general:          PreferencesContainerEngineGeneral,
    'allowed-images': PreferencesContainerEngineAllowedImages,
  } as const)[activeTab.value];
});

function tabSelected({ selectedName }: { selectedName: tabName }) {
  if (activeTab.value !== selectedName) {
    store.dispatch('transient-preferences/navigate', { 'preferences.container-engine': selectedName })
      // TODO: Actual error handling
      // https://github.com/rancher-sandbox/rancher-desktop-2/issues/574
      .catch(console.error);
  }
}
</script>

<template>
  <rd-tabbed
    v-bind="$attrs"
    class="action-tabs"
    :no-content="true"
    :default-tab="activeTab"
    @changed="tabSelected"
  >
    <template #tabs>
      <tab
        label-key="preferences.tabs.general"
        name="general"
        :weight="2"
      />
      <!--
      Allowed Images is not implemented yet
      <tab
        label-key="preferences.tabs.allowedImages"
        name="allowed-images"
        :weight="1"
      />
      -->
    </template>
    <div class="container-engine-content">
      <component
        v-bind="$attrs"
        :is="componentFromTab"
      />
    </div>
  </rd-tabbed>
</template>

<style lang="scss" scoped>
  .container-engine-content {
    padding: var(--preferences-content-padding);
  }
</style>
