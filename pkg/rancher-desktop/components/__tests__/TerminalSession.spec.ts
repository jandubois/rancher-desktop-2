import { jest } from '@jest/globals';
import { flushPromises, mount } from '@vue/test-utils';
import { nextTick, ref } from 'vue';

import cleanHtmlDirective from '@pkg/plugins/clean-html-directive';
import mockModules from '@pkg/utils/testUtils/mockModules';

const passthroughURL = ref<string | undefined>('http://rdd.test/passthrough/mock/exec/');
const terminal = {
  cols:     80,
  rows:     24,
  write:    () => {},
  reset:    () => {},
  onData:   () => ({ dispose: () => {} }),
  onResize: () => ({ dispose: () => {} }),
  focus:    jest.fn(),
};

mockModules({
  '@pkg/composables/passthrough': { usePassthroughURL: () => passthroughURL },
  '@pkg/composables/xterm':       { useXterm: () => Promise.resolve({ terminal }) },
});

const { default: TerminalSession } = await import('../TerminalSession.vue');

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  binaryType = 'blob';
  listeners:        Record<string, ((event?: any) => void)[]> = {};
  constructor() {
    FakeWebSocket.instances.push(this);
  }

  send() {}
  close() {}
  addEventListener(type: string, listener: (event?: any) => void) {
    (this.listeners[type] ??= []).push(listener);
  }

  emit(type: string) {
    this.listeners[type]?.forEach(listener => listener());
  }
}

describe('TerminalSession.vue', () => {
  const realWebSocket = globalThis.WebSocket;

  beforeEach(() => {
    globalThis.WebSocket = FakeWebSocket as any;
  });
  afterEach(() => {
    globalThis.WebSocket = realWebSocket;
  });

  const mountOptions = {
    props:  { endpoint: 'exec', target: 'container' },
    global: {
      plugins: [cleanHtmlDirective], mocks: { t: (key: string) => key }, stubs: { t: true },
    },
  };

  it('focuses the terminal once it exists', async() => {
    terminal.focus.mockClear();
    mount(TerminalSession, mountOptions);
    await flushPromises();
    expect(terminal.focus).toHaveBeenCalledTimes(1);
  });

  it('connects again only when the user asks', async() => {
    FakeWebSocket.instances = [];
    const wrapper = mount(TerminalSession, mountOptions);

    await flushPromises();
    expect(FakeWebSocket.instances).toHaveLength(1);
    FakeWebSocket.instances[0].emit('close');
    await nextTick();

    // The passthrough endpoint goes away and comes back.
    passthroughURL.value = undefined;
    await flushPromises();
    passthroughURL.value = 'http://rdd.test/passthrough/mock/exec/';
    await flushPromises();
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(wrapper.find('[data-testid="session-ended"]').exists()).toBe(true);

    // A click before the endpoint is back keeps the banner, so the user can click again.
    passthroughURL.value = undefined;
    await flushPromises();
    await wrapper.get('[data-testid="session-action"]').trigger('click');
    expect(wrapper.find('[data-testid="session-ended"]').exists()).toBe(true);
    passthroughURL.value = 'http://rdd.test/passthrough/mock/exec/';
    await flushPromises();
    expect(FakeWebSocket.instances).toHaveLength(1);

    await wrapper.get('[data-testid="session-action"]').trigger('click');
    expect(FakeWebSocket.instances).toHaveLength(2);
  });
});
