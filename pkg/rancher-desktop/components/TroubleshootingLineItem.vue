<script lang="ts">
import { defineComponent } from 'vue';

import Card from '@pkg/components/Card.vue';

export default defineComponent({
  name:       'troubleshooting-line-item',
  components: { Card },
});
</script>
<template>
  <card
    class="troubleshooting-line-item"
    :show-highlight-border="false"
  >
    <template #title>
      <slot name="title" />
    </template>
    <template #body>
      <slot name="description" />
      <span class="options">
        <slot name="options" />
      </span>
    </template>
    <template #actions>
      <slot name="actions" />
    </template>
  </card>
</template>

<style lang="scss" scoped>
  .options {
    margin-top: 0.5rem;
  }

  // Override card styles from @pkg/components/Card.vue; we can remove this once the component is refactored.
  .card-container {
    border-radius: 0;
    box-shadow: none;
    margin: 0;
  }
</style>
