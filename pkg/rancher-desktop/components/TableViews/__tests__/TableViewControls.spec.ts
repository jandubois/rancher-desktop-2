// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import { jest } from '@jest/globals';
import { mount } from '@vue/test-utils';
import { Dropdown } from 'floating-vue';
import { createStore } from 'vuex';

import type { TableViewField } from '@pkg/types/table-views';
import mockModules from '@pkg/utils/testUtils/mockModules';

mockModules({
  '@pkg/utils/ipcRenderer': {
    ipcRenderer: {
      on: jest.fn(), once: jest.fn(), send: jest.fn(),
    },
  },
});

const i18n = await import('@pkg/store/i18n');
const { default: TableViewControls } = await import('@pkg/components/TableViews/TableViewControls.vue');

const MARKUP = '<img/src=x/onerror=alert(1)>';

const FIELDS: TableViewField[] = [
  { id: 'state', label: 'State', isLabel: false },
  { id: `label:${ MARKUP }`, label: MARKUP, isLabel: true, labelKey: MARKUP },
];

/** Each problem, with the query that raises it */
const PROBLEMS: [string, string, string][] = [
  ['emptyValue', 'state:', '<b>State</b> has no value to match.'],
  ['trailingOperator', 'nginx and', 'The query ends with <b>and</b>.'],
  ['leadingJoiner', 'or nginx', '<b>or</b> has nothing before it to join.'],
  ['consecutiveOperators', 'nginx and or redis', '<b>or</b> follows another operator.'],
  ['danglingNegation', 'nginx - redis', '<b>-</b> on its own negates nothing.'],
  ['unbalancedQuote', 'name:"nginx', 'A quote is opened and never closed.'],
  ['noTerms', 'and', 'There is nothing to filter by.'],
];

function mountControls(query: string) {
  const store = createStore({ modules: { i18n: { ...i18n, namespaced: true } } as any });

  return mount(TableViewControls, {
    props:  { view: { query, columns: null, labelColumns: [], groupBy: null }, fields: FIELDS },
    global: {
      plugins:    [store],
      components: { VDropdown: Dropdown },
      directives: {
        'clean-html':    (el: HTMLElement, { value }: { value: string }) => {
          el.innerHTML = value;
        },
        'clean-tooltip': {},
      },
    },
  });
}

const notice = (wrapper: any) => wrapper.find('[data-testid="table-views-query-problem"] span').element.innerHTML;

describe('TableViewControls', () => {
  it.each(PROBLEMS)('formats %s with its tags and no raw pattern', (_, query, expected) => {
    expect(notice(mountControls(query))).toBe(expected);
  });

  it('escapes markup typed into a query', () => {
    const wrapper = mountControls(`label:${ MARKUP }:`);

    expect(wrapper.find('[data-testid="table-views-query-problem"] img').exists()).toBe(false);
    expect(notice(wrapper)).toContain('&lt;img');
  });
});
