import { jest } from '@jest/globals';
import { shallowMount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { createStore } from 'vuex';

import VirtualMachineHardware from '@pkg/components/Preferences/VirtualMachineHardware.vue';
import SystemPreferences from '@pkg/components/SystemPreferences.vue';
import mockModules from '@pkg/utils/testUtils/mockModules';

mockModules({
  '@pkg/entry/store': {},
});

describe('VirtualMachineHardware', () => {
  function makeStore({
    memory = '4Gi',
    cpus = 3,
    hostMemoryBytes = 16 * 2 ** 30,
    hostCPUs = 8,
    locked = new Set<string>(['virtualMachine.memory']),
  }: {
    memory?:          string | number;
    cpus?:            number;
    hostMemoryBytes?: number;
    hostCPUs?:        number;
    locked?:          Set<string>;
  } = {}) {
    const modify = jest.fn();
    const watchResources = jest.fn();
    const unwatchResources = jest.fn();

    const store = createStore({
      modules: {
        preferences: {
          namespaced: true,
          getters:    {
            preferences:        () => ({ virtualMachine: { memory, cpus } }),
            isPreferenceLocked: () => (key: string) => locked.has(key),
          },
          actions: {
            modify,
          },
        },
        rdd: {
          namespaced: true,
          state:      () => ({
            hostInfos: [{
              status: {
                memory: hostMemoryBytes,
                cpus:   hostCPUs,
              },
            }],
          }),
          actions: {
            watchResources,
            unwatchResources,
          },
        },
      },
    });

    return { store, actions: { modify, watchResources, unwatchResources } };
  }

  test('passes computed hardware values and lock states to SystemPreferences', () => {
    const { store } = makeStore();
    const wrapper = shallowMount(VirtualMachineHardware, {
      global: { plugins: [store] },
    });

    const systemPrefs = wrapper.findComponent(SystemPreferences);

    expect(systemPrefs.exists()).toBe(true);
    expect(systemPrefs.props('memoryInGB')).toBe(4);
    expect(systemPrefs.props('numberCPUs')).toBe(3);
    expect(systemPrefs.props('availMemoryInGB')).toBe(16);
    expect(systemPrefs.props('availNumCPUs')).toBe(8);
    expect(systemPrefs.props('reservedMemoryInGB')).toBe(6);
    expect(systemPrefs.props('reservedNumCPUs')).toBe(1);
    expect(systemPrefs.props('isLockedMemory')).toBe(true);
    expect(systemPrefs.props('isLockedCpu')).toBe(false);
  });

  test.each([
    ['2560Mi', 2],
    ['5.Gi', 5],
    [3 * (2 ** 30), 3],
  ])('computes memoryInGB from preferences.virtualMachine.memory=%p', (memory, expectedGiB) => {
    const { store } = makeStore({ memory });
    const wrapper = shallowMount(VirtualMachineHardware, {
      global: { plugins: [store] },
    });

    const systemPrefs = wrapper.findComponent(SystemPreferences);

    expect(systemPrefs.props('memoryInGB')).toBe(expectedGiB);
  });

  test('watches hostInfos on mount and unwatches on unmount', () => {
    const { store, actions } = makeStore();

    const wrapper = shallowMount(VirtualMachineHardware, {
      global: { plugins: [store] },
    });

    expect(actions.watchResources).toHaveBeenCalledWith(expect.anything(), ['hostInfos']);

    wrapper.unmount();

    expect(actions.unwatchResources).toHaveBeenCalledWith(expect.anything(), ['hostInfos']);
  });

  test('dispatches preference updates for CPU and memory events', async() => {
    const { store, actions } = makeStore();
    const wrapper = shallowMount(VirtualMachineHardware, {
      global: { plugins: [store] },
    });

    const systemPrefs = wrapper.findComponent(SystemPreferences);

    systemPrefs.vm.$emit('update:cpu', 6);
    systemPrefs.vm.$emit('update:memory', 10);
    await nextTick();

    expect(actions.modify).toHaveBeenCalledWith(expect.anything(), {
      key:   'virtualMachine.cpus',
      value: 6,
    });
    expect(actions.modify).toHaveBeenCalledWith(expect.anything(), {
      key:   'virtualMachine.memory',
      value: '10Gi',
    });
  });
});
