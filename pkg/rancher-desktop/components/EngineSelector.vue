<script lang="ts" setup>
import { computed } from 'vue';
import { useStore } from 'vuex';

import RadioGroup from '@pkg/components/form/RadioGroup.vue';
import { IoRancherdesktopAppV1alpha1AppSpecContainerEngineNameEnum as ContainerEngineName } from '@rdd-client';

defineOptions({ name: 'engine-selector' });

const store = useStore();
const t = computed(() => store.getters['i18n/t']);

withDefaults(defineProps<{
  containerEngine: ContainerEngineName;
  row?:            boolean;
  isLocked?:       boolean;
}>(), {
  row:      false,
  isLocked: false,
});

const emit = defineEmits({
  change: (_value: ContainerEngineName) => true,
});

const options = computed(() => Object.values(ContainerEngineName)
  .map(value => ({
    label:       t.value(`containerEngine.options.${ value }.label`),
    value,
    description: t.value(`containerEngine.options.${ value }.description`),
  })));
</script>

<template>
  <div
    class="engine-selector"
    :data-value="containerEngine"
  >
    <radio-group
      name="containerEngine"
      class="container-engine"
      :class="{ 'locked-radio': isLocked }"
      :value="containerEngine"
      :options="options"
      :row="row"
      :disabled="isLocked"
      @update:value="emit('change', $event)"
    >
      <template #label>
        <slot name="label" />
      </template>
    </radio-group>
  </div>
</template>

<style lang="scss" scoped>
.container-engine :deep(label) {
  color: var(--input-label);
}
</style>
