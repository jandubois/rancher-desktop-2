import { jest } from '@jest/globals';
import { mount } from '@vue/test-utils';
import { createStore } from 'vuex';

import VirtualMachineProvision from '@pkg/components/Preferences/VirtualMachineProvision.vue';
import ScriptEditor from '@pkg/components/form/ScriptEditor.vue';
import mockModules from '@pkg/utils/testUtils/mockModules';

mockModules({
  '@pkg/entry/store': {},
});

describe('VirtualMachineProvision', () => {
  function makeStore({ provisionScript = 'echo hi', committed = { provisionScript }, locked = new Set<string>() }: {
    provisionScript?: string;
    committed?:       { provisionScript?: string };
    locked?:          Set<string>;
  } = {}) {
    const modify = jest.fn();
    const store = createStore({
      modules: {
        preferences: {
          namespaced: true,
          getters:    {
            committed:          () => ({ virtualMachine: committed }),
            preferences:        () => ({ virtualMachine: { provisionScript } }),
            isPreferenceLocked: () => (key: string) => locked.has(key),
          },
          actions: { modify },
        },
      },
    });

    return { store, modify };
  }

  function mountProvision(store: ReturnType<typeof makeStore>['store']) {
    const wrapper = mount(VirtualMachineProvision, {
      global: {
        plugins:    [store],
        mocks:      { t: (key: string) => key },
        stubs:      { ScriptEditor: true },
        directives: { cleanTooltip: {} },
      },
    });

    return { wrapper, editor: wrapper.findComponent(ScriptEditor) };
  }

  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  test('passes the stored script to the editor', () => {
    const { store } = makeStore({ provisionScript: '#!/bin/sh\ntrue' });
    const { editor } = mountProvision(store);

    expect(editor.props('modelValue')).toBe('#!/bin/sh\ntrue');
    expect(editor.props('disabled')).toBe(false);
  });

  test('passes an empty script when none is stored', () => {
    const store = createStore({
      modules: {
        preferences: {
          namespaced: true,
          getters:    {
            preferences:        () => ({ virtualMachine: {} }),
            isPreferenceLocked: () => () => false,
          },
        },
      },
    });
    const { editor } = mountProvision(store);

    expect(editor.props('modelValue')).toBe('');
  });

  test('records the first edit at once, so closing Preferences right away asks to discard it', () => {
    const { store, modify } = makeStore();
    const { editor } = mountProvision(store);

    editor.vm.$emit('update:modelValue', 'a');

    expect(modify).toHaveBeenCalledWith(expect.anything(), {
      key:   'virtualMachine.provisionScript',
      value: 'a',
    });
  });

  test('dispatches the last value once typing pauses', () => {
    const { store, modify } = makeStore();
    const { editor } = mountProvision(store);

    editor.vm.$emit('update:modelValue', 'a');
    editor.vm.$emit('update:modelValue', 'ab');
    editor.vm.$emit('update:modelValue', 'abc');
    expect(modify).toHaveBeenCalledTimes(1);

    jest.advanceTimersByTime(300);

    expect(modify).toHaveBeenCalledTimes(2);
    expect(modify).toHaveBeenLastCalledWith(expect.anything(), {
      key:   'virtualMachine.provisionScript',
      value: 'abc',
    });
  });

  test('flushes a pending update when the editor loses focus', async() => {
    const { store, modify } = makeStore();
    const { editor } = mountProvision(store);

    editor.vm.$emit('update:modelValue', 'a');
    editor.vm.$emit('update:modelValue', 'abc');
    await editor.trigger('focusout');

    expect(modify).toHaveBeenLastCalledWith(expect.anything(), {
      key:   'virtualMachine.provisionScript',
      value: 'abc',
    });
  });

  test('flushes a pending update on unmount', () => {
    const { store, modify } = makeStore();
    const { wrapper, editor } = mountProvision(store);

    editor.vm.$emit('update:modelValue', 'a');
    editor.vm.$emit('update:modelValue', 'abc');
    wrapper.unmount();

    expect(modify).toHaveBeenLastCalledWith(expect.anything(), {
      key:   'virtualMachine.provisionScript',
      value: 'abc',
    });
  });

  test('does not record a change when the user clears a script that the App never had', () => {
    const { store, modify } = makeStore({ provisionScript: '', committed: {} });
    const { editor } = mountProvision(store);

    editor.vm.$emit('update:modelValue', 'x');
    jest.advanceTimersByTime(300);
    editor.vm.$emit('update:modelValue', '');
    jest.advanceTimersByTime(300);

    expect(modify).toHaveBeenLastCalledWith(expect.anything(), {
      key:   'virtualMachine.provisionScript',
      value: undefined,
    });
  });

  test('records an empty script when the user clears a stored one', () => {
    const { store, modify } = makeStore({ provisionScript: 'x' });
    const { editor } = mountProvision(store);

    editor.vm.$emit('update:modelValue', '');
    jest.advanceTimersByTime(300);

    expect(modify).toHaveBeenLastCalledWith(expect.anything(), {
      key:   'virtualMachine.provisionScript',
      value: '',
    });
  });

  test('disables the editor when the script is locked', () => {
    const { store } = makeStore({ locked: new Set(['virtualMachine.provisionScript']) });
    const { editor } = mountProvision(store);

    expect(editor.props('disabled')).toBe(true);
  });
});
