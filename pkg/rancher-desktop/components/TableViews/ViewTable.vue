<script setup lang="ts">
/**
 * A SortableTable with the query box and the View menu above it. It filters the rows before the
 * table sees them, and passes on what it doesn't declare, so a page uses it as it would a SortableTable.
 */
import debounce from 'lodash/debounce';
import omit from 'lodash/omit';
import {
  computed, nextTick, onBeforeUnmount, ref, useSlots, watch,
} from 'vue';
import { useStore } from 'vuex';

import SortableTable from '@pkg/components/SortableTable/index.vue';
import TableViewControls from '@pkg/components/TableViews/TableViewControls.vue';
import TableViewQueryInput from '@pkg/components/TableViews/TableViewQueryInput.vue';
import { useI18n } from '@pkg/composables/useI18n';
import type {
  TableViewColumn, TableViewField, TableViewRow, TableViewState,
} from '@pkg/types/table-views';
import {
  LABEL_FIELD_PREFIX, coreFieldIdsFor, dateText, fieldValue, fieldsFor, findField, headerFieldId, holdsDates,
  isIgnoredColumn, monthLabel, stringifyValue,
} from '@pkg/utils/table-views/fields';
import { applyQueryExpression } from '@pkg/utils/table-views/filter-rows';
import { parseQuery, parseQueryExpression } from '@pkg/utils/table-views/query';
import { readView, writeView } from '@pkg/utils/table-views/storage';
import { NO_GROUPING } from '@pkg/utils/table-views/views';

/** One of the table's own ways to group, offered beside its columns */
export interface TableViewGrouping {
  value:          string;
  label:          string;
  /** What to group by: a path into the row, or a function given the row */
  field?:         string | ((row: TableViewRow) => string);
  /** A column that this grouping stands for, which Group By then leaves out */
  hideColumn?:    string;
  /** A path to the text that its group rows show, when that isn't the grouped value */
  groupLabelKey?: string;
}

/** Marks a view's grouping as one of the table's own rather than a column */
const TABLE_GROUPING_PREFIX = 'group:';

/** Marks a view's grouping as a date column's, by month */
const MONTH_GROUPING_PREFIX = 'month:';

/** Filtering on every keystroke is too expensive */
const SEARCH_DEBOUNCE = 400;

const props = withDefaults(defineProps<{
  /** Names the table's entry in storage, so it must stay the same between releases */
  tableId:         string,
  headers:         TableViewColumn[],
  rows:            TableViewRow[],
  /** Field ids that can't be hidden. Defaults to the leading state and name, and the sorted column */
  coreColumns?:    string[],
  /** Field ids shown until the user picks. Defaults to every column */
  defaultColumns?: string[],
  groupings?:      TableViewGrouping[],
  /** The grouping id that the view falls back to, eg `group:projectGroup` */
  defaultGroupBy?: string | null,
  /** When false, the table gets the query box alone */
  viewMenu?:       boolean,
}>(), {
  coreColumns:    undefined,
  defaultColumns: undefined,
  groupings:      () => [],
  defaultGroupBy: null,
  viewMenu:       true,
});

const { t } = useI18n(useStore());
const slots = useSlots();

const table = ref<any>(null);

const view = ref<TableViewState>({
  query: '', columns: null, columnOrder: null, labelColumns: [], groupBy: null, ...readView(props.tableId),
});

/** The query that the table acts on, which trails the one being typed */
const settledQuery = ref('');

const settleQuery = debounce((query: string) => {
  settledQuery.value = query;
}, SEARCH_DEBOUNCE);

const updateView = (changed: TableViewState) => {
  view.value = changed;
  writeView(props.tableId, changed);
};

/** A change at the end of the query is typing, so it waits. Clearing or replacing the query applies at once */
watch(() => view.value.query, (neu, old) => {
  const query = neu || '';
  const previous = old || '';

  if (query === settledQuery.value) {
    settleQuery.cancel();

    return;
  }

  settleQuery(query);

  if (!query || !(query.startsWith(previous) || previous.startsWith(query))) {
    settleQuery.flush();
  }
});

onBeforeUnmount(() => settleQuery.cancel());

const viewFields = computed(() => fieldsFor(props.headers, props.rows, t));

/** The headers that a table shows until the user picks */
const defaultHeaders = computed(() => {
  const ids = props.defaultColumns;

  return ids ? props.headers.filter((header) => isIgnoredColumn(header) || ids.includes(headerFieldId(header))) : props.headers;
});

const defaultColumnIds = computed(() => props.defaultColumns ?? props.headers.filter((header) => !isIgnoredColumn(header)).map(headerFieldId));

const coreColumnIds = computed(() => {
  const sorted = props.headers.find((header: TableViewColumn & { defaultSort?: boolean }) => header.defaultSort);

  return props.coreColumns ?? coreFieldIdsFor(defaultColumnIds.value, sorted ? headerFieldId(sorted) : null);
});

/** The columns that the table can sort by, plus the labels, less the dates */
const viewSortableFields = computed(() => viewFields.value.filter((field) => {
  if (field.isLabel) {
    return true;
  }

  const { header } = field;

  if (!header?.sort || field.isDate) {
    return false;
  }

  return typeof header.value === 'function' || !!header.value ||
    typeof header.sort === 'string' || (Array.isArray(header.sort) && typeof header.sort[0] === 'string');
}));

/** The date columns, which the query offers by year and month. Only those holding dates, not text such as "23m" */
const viewDateFields = computed(() => viewFields.value.filter((field) => field.isDate && field.header?.sort && holdsDates(props.rows, field)));

const viewDateFieldIds = computed(() => viewDateFields.value.map((field) => field.id));

/** A date column as the Group By entry that gathers its rows by month */
const monthGrouping = (field: TableViewField): TableViewField => ({
  ...field, id: `${ MONTH_GROUPING_PREFIX }${ field.id }`, byMonth: true,
});

/** The table's own groupings, the columns it can sort by less those that a grouping covers, and the dates by month */
const viewGroupFields = computed((): TableViewField[] => {
  const own = props.groupings.map((option) => ({
    id: `${ TABLE_GROUPING_PREFIX }${ option.value }`, label: option.label, isLabel: false,
  }));
  const covered: unknown[] = props.groupings.flatMap((option) => [option.hideColumn, option.field]).filter(Boolean);
  const sortable = viewSortableFields.value.filter((field) => {
    return field.isLabel || !(covered.includes(field.header?.name) || covered.includes(field.header?.value));
  });
  const dates = viewDateFields.value;

  return own.concat(viewFields.value
    .filter((field) => sortable.includes(field) || dates.includes(field))
    .map((field) => (dates.includes(field) ? monthGrouping(field) : field)));
});

/** The sortable columns and the dates. Labels are left to typing, because their keys are too many to suggest */
const viewFilterFields = computed(() => {
  const offered = [...viewSortableFields.value, ...viewDateFields.value];

  return viewFields.value.filter((field) => !field.isLabel && offered.includes(field));
});

const viewTerms = computed(() => parseQuery(settledQuery.value, viewFields.value));

const viewRows = computed(() => {
  if (!viewTerms.value.length) {
    return props.rows;
  }

  return applyQueryExpression(props.rows, parseQueryExpression(settledQuery.value, viewFields.value), viewFields.value);
});

/** The grouping in force: the view's, else the table's default */
const groupId = computed(() => (view.value.groupBy === NO_GROUPING ? null : view.value.groupBy || props.defaultGroupBy));

const viewTableGrouping = computed(() => props.groupings.find((option) => `${ TABLE_GROUPING_PREFIX }${ option.value }` === groupId.value) || null);

/** The column that a view's grouping names, or a date column taken by month */
const viewGroupField = computed(() => {
  const id = groupId.value;

  if (!id || viewTableGrouping.value) {
    return null;
  }

  if (id.startsWith(MONTH_GROUPING_PREFIX)) {
    const date = findField(viewDateFields.value, id.slice(MONTH_GROUPING_PREFIX.length));

    return date ? monthGrouping(date) : null;
  }

  return findField(viewFields.value, id) || null;
});

/** A column's grouping can be any field, including a label, so its key is a function */
const computedGroupBy = computed(() => {
  const field = viewGroupField.value;

  if (!field) {
    return viewTableGrouping.value?.field ?? null;
  }

  const empty = t('tableViews.group.empty');

  if (field.byMonth) {
    return (row: TableViewRow) => {
      const date = dateText(fieldValue(row, field));

      return /^\d{4}-\d{2}/.test(date) ? date.slice(0, 7) : empty;
    };
  }

  return (row: TableViewRow) => stringifyValue(fieldValue(row, field)) || empty;
});

const computedGroupRef = computed(() => {
  const field = viewGroupField.value;
  const groupBy = computedGroupBy.value;

  if (!field || typeof groupBy !== 'function') {
    return viewTableGrouping.value?.groupLabelKey ?? null;
  }

  return field.byMonth ? (row: TableViewRow) => monthLabel(groupBy(row)) : groupBy;
});

/**
 * The column's first sort path, without any `:desc`, so that groups follow the column's values.
 * Later `sort` entries only break ties, and a function can't be a sort path.
 */
const viewGroupSort = computed(() => {
  const sort = viewGroupField.value?.header?.sort;
  const first = Array.isArray(sort) ? sort[0] : sort;

  return typeof first === 'string' && first ? [first.split(':')[0]] : null;
});

const viewHeaders = computed(() => {
  const { columns, columnOrder, labelColumns } = view.value;
  let out = defaultHeaders.value;

  if (columns) {
    // From every header, so a column left out by default can be added. Core columns always stay
    out = props.headers.filter((header) => {
      const id = headerFieldId(header);

      return isIgnoredColumn(header) || coreColumnIds.value.includes(id) ||
        !viewFields.value.find((field) => !field.isLabel && field.id === id) || columns.includes(id);
    });
  }

  if (columnOrder?.length) {
    // Only data columns move; `check` and the like stay put
    const movable = out.filter((header) => !isIgnoredColumn(header) && columnOrder.includes(headerFieldId(header)));
    const sorted = movable.slice().sort((a, b) => columnOrder.indexOf(headerFieldId(a)) - columnOrder.indexOf(headerFieldId(b)));
    let next = 0;

    out = out.map((header) => (movable.includes(header) ? sorted[next++] : header));
  }

  return out.concat((labelColumns || []).map((key) => ({
    name:   `${ LABEL_FIELD_PREFIX }${ key }`,
    label:  key,
    value:  (row: TableViewRow) => row?.metadata?.labels?.[key] || '',
    sort:   false,
    search: false,
  })));
});

/**
 * The table falls back to its default order by itself but stays descending, so hiding the sorted
 * column has to return it to its default sort
 */
watch(viewHeaders, async() => {
  await nextTick();

  const sortable = table.value;

  if (!sortable || viewHeaders.value.some((header) => header.name === sortable.sortBy)) {
    return;
  }

  if (sortable.sortBy !== sortable._defaultSortBy || sortable.descending) {
    sortable.changeSort(sortable._defaultSortBy, false);
  }
});

/** `title` is drawn below with the controls. A page's own group heading is for its own grouping, not a column's */
const passthroughSlots = computed(() => {
  const { title, ...rest } = slots;

  return viewGroupField.value ? omit(rest, 'group-by', 'group-row') : rest;
});
</script>

<template>
  <SortableTable
    ref="table"
    :headers="viewHeaders"
    :rows="viewRows"
    :search="false"
    :group-by="computedGroupBy"
    :group-ref="computedGroupRef"
    :group-sort="viewGroupSort"
    :queried="viewTerms.length > 0 && rows.length > 0"
  >
    <template #title>
      <TableViewControls
        v-if="viewMenu"
        :view="view"
        :fields="viewFields"
        :group-fields="viewGroupFields"
        :default-group-by="defaultGroupBy"
        :filter-fields="viewFilterFields"
        :date-fields="viewDateFieldIds"
        :rows="rows"
        :default-columns="defaultColumnIds"
        :core-columns="coreColumnIds"
        @update:view="updateView"
      />
      <TableViewQueryInput
        v-else
        :value="view.query"
        :fields="viewFields"
        :filter-fields="viewFilterFields"
        :date-fields="viewDateFieldIds"
        :rows="rows"
        @update:value="updateView({ ...view, query: $event })"
      />
      <slot name="title" />
    </template>

    <template
      v-for="(_, name) of passthroughSlots"
      :key="name"
      #[name]="scope"
    >
      <slot
        :name="name"
        v-bind="scope"
      />
    </template>
  </SortableTable>
</template>
