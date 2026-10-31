// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import { jest } from '@jest/globals';
import { flushPromises, mount } from '@vue/test-utils';
import { Dropdown } from 'floating-vue';
import { createStore } from 'vuex';

import mockModules from '@pkg/utils/testUtils/mockModules';
import { sortableTableGlobal } from '@pkg/utils/testUtils/sortableTable';

const componentStub = { template: '<div />' };

mockModules({
  '@pkg/components/BadgeState.vue':          componentStub,
  '@pkg/components/Banner.vue':              componentStub,
  '@pkg/components/form/Checkbox.vue':       { template: '<div class="checkbox-container" />' },
  '@pkg/components/form/LabeledTooltip.vue': componentStub,
});

const { default: SortableTable } = await import('@pkg/components/SortableTable/index.vue');
const { default: TableViewControls } = await import('@pkg/components/TableViews/TableViewControls.vue');
const { default: TableViewQueryInput } = await import('@pkg/components/TableViews/TableViewQueryInput.vue');
const { default: ViewTable } = await import('@pkg/components/TableViews/ViewTable.vue');

const ROWS = [
  { id: 'web', image: 'nginx', state: 'running', port: 80 },
  { id: 'db', image: 'postgres', state: 'running', port: 5432 },
  { id: 'job', image: 'alpine', state: 'exited', port: 0 },
];

const HEADERS = [
  { name: 'id', label: 'Id', value: 'id', sort: ['id'] },
  { name: 'image', label: 'Image', value: 'image', sort: ['image'] },
  { name: 'status', label: 'Status', value: 'state', sort: ['state'] },
  { name: 'port', label: 'Port', value: (row: any) => `host:${ row.port }` },
];

function mountTable(props: Record<string, unknown> = {}, slots: Record<string, string> = {}) {
  const store = createStore({ getters: { 'i18n/t': () => (key: string) => key } });

  return mount(ViewTable as any, {
    props: {
      tableId: 'test', headers: HEADERS, rows: ROWS, ...props,
    },
    attrs:  { keyField: 'id', rowActions: false, rowsPerPage: 10 },
    slots,
    global: {
      ...sortableTableGlobal(), plugins: [store], components: { VDropdown: Dropdown },
    },
  });
}

/** What the View menu hands back, with the query given time to settle */
async function changeView(wrapper: any, changes: Record<string, unknown>) {
  const controls = wrapper.findComponent(TableViewControls);

  controls.vm.$emit('update:view', { ...controls.props('view'), ...changes });
  await flushPromises();
  jest.advanceTimersByTime(400);
  await flushPromises();
}

const columnNames = (wrapper: any) => wrapper.findComponent(SortableTable).vm.columns.map((column: any) => column.name);

describe('ViewTable', () => {
  beforeEach(() => {
    localStorage.clear();
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('takes a row a query hides out of the selection', async() => {
    const wrapper = mountTable();
    const table: any = wrapper.findComponent(SortableTable).vm;

    table.update([ROWS[0], ROWS[1]], []);
    await changeView(wrapper, { query: 'postgres' });

    expect(table.selectedRows.map((row: any) => row.id)).toStrictEqual(['db']);
  });

  it('shows the no-results message when a query matches nothing', async() => {
    const wrapper = mountTable();

    await changeView(wrapper, { query: 'nothing-like-this' });

    expect(wrapper.find('tr.no-results').exists()).toBe(true);
    expect(wrapper.find('tr.no-rows').exists()).toBe(false);
  });

  it('shows the no-rows message for an empty table, with a query too', async() => {
    const wrapper = mountTable({ rows: [] });

    expect(wrapper.find('tr.no-rows').exists()).toBe(true);

    await changeView(wrapper, { query: 'nginx' });

    expect(wrapper.find('tr.no-rows').exists()).toBe(true);
    expect(wrapper.find('tr.no-results').exists()).toBe(false);
  });

  it('brings hidden and reordered columns and the grouping back after a remount, without the query', async() => {
    const first = mountTable();

    await changeView(first, {
      columns: ['id', 'port', 'status'], columnOrder: ['id', 'port', 'status'], groupBy: 'status', query: 'nginx',
    });
    first.unmount();

    const second = mountTable();

    expect(second.findComponent(TableViewControls).props('view')).toMatchObject({
      columns: ['id', 'port', 'status'], columnOrder: ['id', 'port', 'status'], groupBy: 'status', query: '',
    });
    expect(columnNames(second)).toStrictEqual(['id', 'port', 'status']);
    expect(second.findAll('tr.group-row')).toHaveLength(2);
    expect(second.findAll('tr.main-row')).toHaveLength(ROWS.length);
  });

  it('returns to the default sort, ascending, when the sorted column is hidden', async() => {
    const wrapper = mountTable();
    const table: any = wrapper.findComponent(SortableTable).vm;

    table.changeSort('image', true);
    await changeView(wrapper, { columns: ['id', 'status', 'port'] });

    expect(table.sortBy).toBe('id');
    expect(table.descending).toBe(false);
  });

  it('finds the text a cell shows when the header value is a function', async() => {
    const wrapper = mountTable();

    await changeView(wrapper, { query: 'host:5432' });

    expect(wrapper.findAll('tr.main-row')).toHaveLength(1);
    expect(wrapper.find('tr.main-row').text()).toContain('postgres');
  });

  it('groups by a column with a function, and hides no column', async() => {
    const wrapper = mountTable();
    const table = wrapper.findComponent(SortableTable);

    await changeView(wrapper, { groupBy: 'status' });

    expect(table.props('groupBy')).toBeInstanceOf(Function);
    expect(wrapper.findAll('tr.group-row').map((row) => row.text())).toStrictEqual(['exited', 'running']);
    expect(columnNames(wrapper)).toStrictEqual(HEADERS.map((header) => header.name));
  });

  it('groups by a label, which has no sort of its own', async() => {
    const rows = [
      { id: 'web', metadata: { labels: { tier: 'front' } } },
      { id: 'db', metadata: { labels: { tier: 'back' } } },
    ];
    const wrapper = mountTable({ rows });

    await changeView(wrapper, { groupBy: 'label:tier' });

    expect(wrapper.findAll('tr.group-row').map((row) => row.text())).toStrictEqual(['back', 'front']);
  });

  it('keeps a table grouping\'s group-row slot, and drops it for a column grouping', async() => {
    const groupings = [{ value: 'state', label: 'State', field: 'state' }];
    const wrapper = mountTable({ groupings, defaultGroupBy: 'group:state' }, { 'group-row': '<tr class="own-group-row" />' });

    expect(wrapper.findAll('tr.own-group-row')).toHaveLength(2);

    await changeView(wrapper, { groupBy: 'image' });

    expect(wrapper.find('tr.own-group-row').exists()).toBe(false);
    expect(wrapper.findAll('tr.group-row')).toHaveLength(ROWS.length);
  });

  it('renders the query box and no View menu without viewMenu', () => {
    const wrapper = mountTable({ viewMenu: false });

    expect(wrapper.find('[data-testid="table-views-query"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="table-views-view-menu"]').exists()).toBe(false);
  });

  it('filters by the query box alone without viewMenu', async() => {
    const wrapper = mountTable({ viewMenu: false });

    wrapper.findComponent(TableViewQueryInput).vm.$emit('update:value', 'postgres');
    await flushPromises();
    jest.advanceTimersByTime(400);
    await flushPromises();

    expect(wrapper.findAll('tr.main-row')).toHaveLength(1);
    expect(wrapper.find('tr.main-row').text()).toContain('postgres');
  });

  it('draws a page\'s title after the controls, and leaves $el the table\'s', () => {
    const wrapper = mountTable({}, { title: '<h2 class="own-title" />' });
    const title = wrapper.find('.own-title').element;
    const controls = wrapper.find('[data-testid="table-views-controls"]').element;

    expect(controls.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(wrapper.element).toBe(wrapper.findComponent(SortableTable).element);
  });
});
