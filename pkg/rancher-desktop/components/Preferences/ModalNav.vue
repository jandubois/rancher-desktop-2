<script lang="ts" setup>
import kebabCase from 'lodash/kebabCase';

import NavItem from '@pkg/components/Preferences/ModalNavItem.vue';
import type { preferencesNavItem } from '@pkg/window/preferenceConstants';

type preferencesNavItemName = preferencesNavItem['name'];

defineOptions({ name: 'preferences-nav' });

const { currentNavItem, navItems } = defineProps<{
  currentNavItem: preferencesNavItem['name'];
  navItems:       preferencesNavItem[];
}>();

const emit = defineEmits<{
  'nav-changed': [preferencesNavItemName],
}>();

function navClicked(tabName: preferencesNavItemName) {
  if (tabName !== currentNavItem) {
    emit('nav-changed', tabName);
  }
}

function navToKebab(navItem: preferencesNavItemName): string {
  return `nav-${ kebabCase(navItem) }`;
}

</script>

<template>
  <div class="preferences-nav">
    <nav-item
      v-for="navItem in navItems"
      :key="navItem.name"
      :data-testid="navToKebab(navItem.name)"
      :name="navItem.name"
      :active="currentNavItem === navItem.name"
      @click="navClicked"
    >
      {{ t(navItem.labelKey) }}
    </nav-item>
  </div>
</template>

<style lang="scss" scoped>
  .preferences-nav {
    display: flex;
    flex-direction: column;
    height: 100%;
    border-right: 1px solid var(--header-border);
    padding-top: 0.75rem;
  }
</style>
