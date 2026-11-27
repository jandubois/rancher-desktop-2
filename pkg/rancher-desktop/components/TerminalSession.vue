<template>
  <div class="terminal-session-component">
    <banner
      v-if="ended"
      :color="ended.severity"
      data-testid="session-ended"
    >
      <span class="session-message">{{ t(endedMessage.key, endedMessage.args) }}</span>
      <button
        class="btn btn-sm role-secondary session-action"
        data-testid="session-action"
        :disabled="!targetRunning"
        @click="reconnect"
      >
        {{ ended.action === 'reattach' ? t('terminalSession.reattach') : t('terminalSession.startNewShell') }}
      </button>
    </banner>
    <div
      ref="terminalContainer"
      class="terminal-container"
      data-testid="terminal"
      :data-session-active="ready ? 'true' : undefined"
    />
  </div>
</template>

<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, useTemplateRef, watch } from 'vue';

import Banner from '@pkg/components/Banner.vue';
import { usePassthroughURL } from '@pkg/composables/passthrough';
import { useXterm } from '@pkg/composables/xterm';
import { TerminalConnection } from '@pkg/utils/terminalConnection';
import { TERMINAL_SUBPROTOCOL, TerminalSessionEnd } from '@pkg/utils/terminalProtocol';

import type { Terminal } from '@xterm/xterm';

defineOptions({ name: 'TerminalSession' });

const { endpoint, target, targetRunning } = defineProps<{
  /** The passthrough endpoint, such as `exec`. */
  endpoint:      string;
  /** The path after the endpoint that names the session's target. */
  target:        string;
  /**
   * Whether the target is running. rdd ends the target's session when the
   * target stops, and it starts a new one only in a running target. So the
   * banner disables its button while this is false.
   */
  targetRunning: boolean;
}>();

defineExpose({ focus: () => terminal.value?.focus() });

const terminalContainer = useTemplateRef('terminalContainer');
const terminal = shallowRef<Terminal>();
const ready = ref(false);
const ended = ref<TerminalSessionEnd>();
const aborter = new AbortController();
let connection: TerminalConnection | undefined;

const endpointURL = usePassthroughURL(endpoint);
const sessionURL = computed(() => {
  if (!endpointURL.value || !target) {
    return undefined;
  }
  return new URL(target, endpointURL.value).href;
});

const endedMessage = computed((): { key: string; args?: Record<string, unknown> } => {
  switch (ended.value?.kind) {
  case 'exited':
    return ended.value.exitStatus === 0
      ? { key: 'terminalSession.ended.exited' }
      : { key: 'terminalSession.ended.exitedWithStatus', args: { status: ended.value.exitStatus } };
  case 'failed':
    return { key: 'terminalSession.ended.failed', args: { message: ended.value.message } };
  case 'takenOver':
    return { key: 'terminalSession.ended.takenOver' };
  default:
    return { key: 'terminalSession.ended.connectionLost' };
  }
});

/**
 * Open a connection to the target's session; rdd reattaches to a running
 * shell or starts a new one. Until rdd's endpoint and the terminal are
 * available, it returns without connecting, and the banner stays up for
 * another click.
 */
function connect() {
  if (!sessionURL.value || !terminal.value) {
    return;
  }
  connection?.dispose();
  ready.value = false;
  ended.value = undefined;
  connection = new TerminalConnection(new WebSocket(sessionURL.value, TERMINAL_SUBPROTOCOL), terminal.value, {
    onReady: () => {
      ready.value = true;
    },
    onEnd: (end) => {
      ready.value = false;
      ended.value = end;
    },
  });
}

function reconnect() {
  connect();
  terminal.value?.focus();
}

onMounted(async() => {
  if (!terminalContainer.value) {
    return;
  }
  const { terminal: t } = await useXterm(terminalContainer.value, {
    cursorBlink: true,
    convertEol:  false,
    scrollback:  10_000,
  }, aborter.signal);

  if (!aborter.signal.aborted) {
    terminal.value = t;
    // The page calls focus() each time the Shell tab opens, but its first
    // call comes before the terminal exists. So focus the terminal here.
    t.focus();
  }
});

// The component connects on its own only once. A new connection can start a
// new shell or take a shell back from another client, so later ones wait for
// the banner's button.
const stopFirstConnect = watch([sessionURL, terminal], () => {
  if (sessionURL.value && terminal.value) {
    stopFirstConnect();
    connect();
  }
});

onBeforeUnmount(() => {
  // Closing the connection leaves the shell running in rdd.
  connection?.dispose();
  connection = undefined;
  aborter.abort();
});
</script>

<style lang="scss" scoped>
@import '@xterm/xterm/css/xterm.css';

.terminal-session-component {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  overflow: hidden;
  flex: 1;
}

.session-message {
  align-self: center;
}

.session-action {
  margin-left: auto;
}

.terminal-container {
  background: #1a1a1a;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;

  :deep(.xterm) {
    height: 100%;
  }

  :deep(.xterm-selection) {
    overflow: hidden;
  }
}
</style>
