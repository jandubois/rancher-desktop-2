import { jest } from '@jest/globals';
import { flushPromises, shallowMount } from '@vue/test-utils';
import { createStore } from 'vuex';

import PreferencesAlert from '@pkg/components/Preferences/Alert.vue';
import mockModules from '@pkg/utils/testUtils/mockModules';
import { t } from '@pkg/utils/testUtils/translations';

mockModules({
  '@pkg/utils/ipcRenderer': {
    ipcRenderer: {
      send: jest.fn(),
    },
  },
});

const { default: FirstRun } = await import('@pkg/pages/FirstRun.vue');

/**
 * Build a store with just enough of `preferences`, `rdd`, and
 * `rdd-connection` for `FirstRun.vue` to mount and run its lifecycle hooks.
 */
function makeStore({
  preferences = { containerEngine: { name: 'moby' } },
  commit = jest.fn(() => Promise.resolve(true)),
  ensureAppExists = jest.fn(() => Promise.resolve(true)),
}: {
  preferences?:     Record<string, unknown>;
  commit?:          jest.Mock;
  ensureAppExists?: jest.Mock;
} = {}) {
  const watchResources = jest.fn(() => Promise.resolve());
  const unwatchResources = jest.fn(() => Promise.resolve());
  const fetchConfig = jest.fn(() => Promise.resolve());

  const store = createStore({
    getters: {
      'i18n/t': () => t,
    },
    modules: {
      preferences: {
        namespaced: true,
        state:      () => ({ errorStatus: undefined }),
        mutations:  {
          SET_ERROR_STATUS(state, errorStatus) {
            state.errorStatus = errorStatus;
          },
        },
        getters: {
          preferences:        () => preferences,
          isPreferenceLocked: () => () => false,
        },
        actions: { commit },
      },
      rdd: {
        namespaced: true,
        actions:    { watchResources, unwatchResources, ensureAppExists },
      },
      'rdd-connection': {
        namespaced: true,
        actions:    { fetchConfig },
      },
    },
  });

  return {
    store,
    actions: {
      commit, ensureAppExists, watchResources, unwatchResources, fetchConfig,
    },
  };
}

describe('FirstRun.vue', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('shows the preferences error alert and keeps the window open when preferences/commit fails', async() => {
    jest.spyOn(window, 'close').mockImplementation(() => {});

    const commit = jest.fn(({ commit }: any) => {
      commit('SET_ERROR_STATUS', { message: 'failed to patch app' });
      return Promise.resolve(false);
    });
    const { store } = makeStore({ commit });

    const wrapper = shallowMount(FirstRun, { global: { plugins: [store] } });
    await flushPromises();

    expect(wrapper.findComponent(PreferencesAlert).exists()).toBe(false);

    const acceptBtn = wrapper.find('[data-testid="accept-btn"]');
    expect(acceptBtn.element).not.toHaveProperty('disabled', true);
    await acceptBtn.trigger('click');
    await flushPromises();

    // A regression that silences this error (e.g. by closing the window
    // regardless of the result of `preferences/commit`, or by never
    // rendering the alert) must fail this assertion.
    expect(wrapper.findComponent(PreferencesAlert).exists()).toBe(true);
    expect(window.close).not.toHaveBeenCalled();
    // The button must be re-enabled so the user can retry.
    expect(acceptBtn.element).not.toHaveProperty('disabled', true);
  });

  it('leaves the accept button disabled when rdd/ensureAppExists fails to create the app', async() => {
    const ensureAppExists = jest.fn(() => Promise.resolve(false));
    // No preferences are available until the `App` object exists, so
    // `preferences/preferences` stays empty when `ensureAppExists` fails.
    const { store, actions } = makeStore({ preferences: {}, ensureAppExists });

    const wrapper = shallowMount(FirstRun, { global: { plugins: [store] } });
    await flushPromises();

    expect(actions.ensureAppExists).toHaveBeenCalled();

    // A regression that re-enables the button despite the App object never
    // having been created must fail this assertion.
    const acceptBtn = wrapper.find('[data-testid="accept-btn"]');
    expect(acceptBtn.element).toHaveProperty('disabled', true);
    // ...and there is no visible indication of the failure for the user.
    expect(wrapper.findComponent(PreferencesAlert).exists()).toBe(false);
  });
});
