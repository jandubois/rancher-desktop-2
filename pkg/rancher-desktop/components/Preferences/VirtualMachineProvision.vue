<script lang="ts" setup>
import _ from 'lodash';
import { computed, onBeforeUnmount } from 'vue';
import { useStore } from 'vuex';

import RdFieldset from '@pkg/components/form/RdFieldset.vue';
import ScriptEditor from '@pkg/components/form/ScriptEditor.vue';

defineOptions({ name: 'preferences-virtual-machine-provision' });

const store = useStore();
const preferences = computed(() => store.getters['preferences/preferences']);
const isPreferenceLocked = computed(() => store.getters['preferences/isPreferenceLocked']);
const provisionScript = computed(() => preferences.value?.virtualMachine?.provisionScript ?? '');
const isLocked = computed(() => isPreferenceLocked.value('virtualMachine.provisionScript'));

// Each preferences/modify call asks rdd to validate the pending changes, so
// wait for typing to pause before sending one. But send the first change
// after a pause at once. Closing Preferences asks whether to discard changes
// only when the store has recorded some, and a change still waiting here is
// not recorded yet.
const setProvisionScript = _.debounce((text: string) => {
  // The editor shows a missing script as empty text. So when the App doesn't
  // have a script, send empty text as a missing script too. Otherwise typing a
  // script and deleting it again would leave a change to apply.
  const committed = store.getters['preferences/committed']?.virtualMachine?.provisionScript;
  const value = text === '' && committed === undefined ? undefined : text;

  store.dispatch('preferences/modify', { key: 'virtualMachine.provisionScript', value });
}, 300, { leading: true });

onBeforeUnmount(() => {
  setProvisionScript.flush();
});
</script>

<template>
  <div
    class="virtual-machine-provision"
    data-testid="provision-script"
  >
    <rd-fieldset
      class="provision-fieldset"
      :legend-text="t('virtualMachine.provision.label')"
      :is-locked="isLocked"
    >
      <p class="description">
        {{ t('virtualMachine.provision.description') }}
      </p>
      <script-editor
        class="editor"
        :model-value="provisionScript"
        :disabled="isLocked"
        :aria-label="t('virtualMachine.provision.label')"
        @update:model-value="setProvisionScript"
        @focusout="setProvisionScript.flush()"
      />
      <p class="restart-notice">
        <i class="icon icon-info-circle" />
        {{ t('virtualMachine.provision.restartNotice') }}
      </p>
    </rd-fieldset>
  </div>
</template>

<style lang="scss" scoped>
  .virtual-machine-provision {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .provision-fieldset {
    flex: 1 1 auto;
    min-height: 0;
    // A fieldset is at least as wide as its content by default, so a long
    // script line would widen the form past the window instead of scrolling
    // inside the editor.
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .editor {
    flex: 1 1 0;
    min-height: 4rem;
  }

  .restart-notice {
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }
</style>
