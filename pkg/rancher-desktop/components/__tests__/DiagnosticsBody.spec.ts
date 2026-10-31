// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import { jest } from '@jest/globals';
import { flushPromises, mount } from '@vue/test-utils';
import { createStore } from 'vuex';

import { DiagnosticsCategory } from '@pkg/main/diagnostics/types';
import mockModules from '@pkg/utils/testUtils/mockModules';
import { sortableTableGlobal } from '@pkg/utils/testUtils/sortableTable';

const componentStub = { template: '<div />' };

mockModules({
  '@pkg/components/DiagnosticsButtonRun.vue': componentStub,
  '@pkg/components/EmptyState.vue':           componentStub,
  '@pkg/components/form/LabeledTooltip.vue':  componentStub,
  '@pkg/components/form/ToggleSwitch.vue':    componentStub,
});

const { default: DiagnosticsBody } = await import('@pkg/components/DiagnosticsBody.vue');

const ROWS = [
  { id: 'alpha', description: 'Alpha failed', mute: false, category: DiagnosticsCategory.Networking, fixes: [] },
  { id: 'beta', description: 'Beta <code>broke</code> &amp; fixed', mute: true, category: DiagnosticsCategory.Utilities, fixes: [] },
];

function mountBody() {
  const store = createStore({
    getters: {
      'i18n/t':                                    () => (key: string) => key,
      'prefs/get':                                 () => () => 100,
      'resource-fetch/isTooManyItemsToAutoUpdate': () => false,
      'resource-fetch/manualRefreshIsLoading':     () => false,
    },
    modules: { preferences: { namespaced: true, getters: { showMuted: () => true } } },
  });
  const global = sortableTableGlobal();

  delete (global.mocks as Record<string, unknown>).$store;

  return mount(DiagnosticsBody as any, {
    props:  { rows: ROWS },
    global: { ...global, plugins: [store] },
  });
}

async function query(wrapper: ReturnType<typeof mountBody>, text: string) {
  const box = wrapper.find('[data-testid="table-views-query"]');

  box.element.textContent = text;
  await box.trigger('input');
  await flushPromises();
  jest.advanceTimersByTime(400);
  await flushPromises();
}

const shownRows = (wrapper: ReturnType<typeof mountBody>) => wrapper.findAll('tr.main-row').map(row => row.text());

describe('DiagnosticsBody', () => {
  beforeEach(() => {
    localStorage.clear();
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('offers the query box without the View menu', () => {
    const wrapper = mountBody();

    expect(wrapper.find('[data-testid="table-views-query"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="table-views-view-menu"]').exists()).toBe(false);
  });

  it('puts each row under its own category heading', async() => {
    const wrapper = mountBody();

    await flushPromises();

    expect(wrapper.findAll('tr.group-row, tr.main-row').map(row => row.text())).toEqual([
      `${ DiagnosticsCategory.Networking } Mute`,
      'Alpha failed',
      `${ DiagnosticsCategory.Utilities } Mute`,
      'Beta broke & fixed',
    ]);
  });

  it('matches the text that a description shows', async() => {
    const wrapper = mountBody();

    await query(wrapper, 'broke');

    expect(shownRows(wrapper)).toEqual(['Beta broke & fixed']);
  });

  it.each(['code', 'true'])('does not match %s, which no cell shows', async(text) => {
    const wrapper = mountBody();

    await query(wrapper, text);

    expect(shownRows(wrapper)).toEqual([]);
  });
});
