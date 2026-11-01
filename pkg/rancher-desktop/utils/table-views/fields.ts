import day from 'dayjs';

import type { TableViewColumn, TableViewField, TableViewRow, TableViewValueSuggestion } from '@pkg/types/table-views';
import { get } from '@pkg/utils/object';
import { valueFor } from '@pkg/utils/table-columns';

export const LABEL_FIELD_PREFIX = 'label:';

/** Columns the user can't hide: without them a row can't be identified or its health seen */
export const CORE_FIELD_IDS = ['state', 'name'];

/**
 * The columns a table won't let go of: the leading state and name, else the first column, plus the
 * one it's sorted by
 */
export function coreFieldIdsFor(defaultColumnIds?: string[], sortedById?: string | null): string[] {
  const defaults = defaultColumnIds || [];
  const leading: string[] = [];

  for (const id of defaults) {
    if (!CORE_FIELD_IDS.includes(id)) {
      break;
    }

    leading.push(id);
  }

  const out = leading.length ? leading : defaults.slice(0, 1);

  if (sortedById && defaults.includes(sortedById) && !out.includes(sortedById)) {
    out.push(sortedById);
  }

  return out;
}

/** Names that meant "not data" before headers could say so with `tableViews: false`; kept for extensions' headers */
const IGNORED_COLUMNS = ['check', 'actions', 'spacer'];

const SCAN_LIMIT = 1000;

/** Label keys contain dots, which `get` would read as a path */
function safeGet(row: TableViewRow, path: string): unknown {
  if (!row || !path) {
    return undefined;
  }

  try {
    return get(row, path);
  } catch (e) {
    return undefined;
  }
}

/**
 * A column drawn entirely by a formatter has `value: ''`, so grouping and filtering fall back to
 * its sort or search path. Only the first `sort` entry: the rest are tie breakers
 */
function fallbackPath(header: TableViewColumn): string | null {
  if (typeof header.sort === 'string') {
    return header.sort.split(':')[0];
  }

  if (Array.isArray(header.sort) && typeof header.sort[0] === 'string') {
    return header.sort[0].split(':')[0];
  }

  if (typeof header.search === 'string' && header.search) {
    return header.search;
  }

  return null;
}

/**
 * A field's value for a row: what its cell shows first, so a group heading, a filter and the cell
 * always agree
 */
export function fieldValue(row: TableViewRow, field: TableViewField): unknown {
  if (!row || !field) {
    return '';
  }

  if (field.isLabel) {
    return row.metadata?.labels?.[field.labelKey!] ?? '';
  }

  const header = field.header;

  if (!header) {
    return safeGet(row, field.id) ?? '';
  }

  // Some columns genuinely have no path, so no warning
  try {
    const shown = valueFor(row, header, false, { warn: false });

    if (shown !== undefined && shown !== null && shown !== '') {
      return shown;
    }
  } catch (e) {
    return '';
  }

  const path = fallbackPath(header);
  const out = path ? safeGet(row, path) : undefined;

  if (out !== undefined && out !== null && out !== '') {
    return out;
  }

  if (typeof header.getValue === 'function') {
    try {
      return header.getValue(row) ?? '';
    } catch (e) {
      return '';
    }
  }

  return '';
}

export function stringifyValue(value: unknown): string {
  if (value === undefined || value === null) {
    return '';
  }

  if (Array.isArray(value)) {
    return value.map((v) => stringifyValue(v)).filter((v) => !!v).join(', ');
  }

  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? '' : value.toISOString();
  }

  if (typeof value === 'object') {
    // Some columns return render objects, eg `{ label, color }`
    const { label } = value as { label?: unknown };

    if (typeof label === 'string') {
      return label;
    }

    return '';
  }

  return `${ value }`;
}

export function headerFieldId(header: TableViewColumn): string {
  const name = header.name || header.label || '';

  return `${ name }`.replace(/\s+/g, '-').toLowerCase();
}

/** A column that isn't data - see TableViewColumn.tableViews */
export function isIgnoredColumn(header: TableViewColumn): boolean {
  return header?.tableViews === false || IGNORED_COLUMNS.includes(headerFieldId(header));
}

/**
 * Everything a table can be filtered, grouped or shown by: its columns, plus one field per label
 * key on the rows
 */
export function fieldsFor(
  headers: TableViewColumn[],
  rows: TableViewRow[],
  t?: (key: string) => string,
): TableViewField[] {
  const out: TableViewField[] = [];
  const seen: Record<string, boolean> = {};

  const scanRows = (rows || []).slice(0, SCAN_LIMIT);

  (headers || []).forEach((header) => {
    const id = headerFieldId(header);

    if (!id || isIgnoredColumn(header) || seen[id]) {
      return;
    }

    let label = header.label;

    if (!label && header.labelKey && t) {
      label = t(header.labelKey);
    }

    if (!label || !`${ label }`.trim()) {
      return;
    }

    seen[id] = true;
    const field: TableViewField = {
      id, label, isLabel: false, header,
    };

    if (isDateColumn(header) || holdsTimestamps(scanRows, field)) {
      field.isDate = true;
    }

    out.push(field);
  });

  const labelKeys: Record<string, boolean> = {};
  const scan = (rows || []).slice(0, SCAN_LIMIT);

  scan.forEach((row) => {
    const labels = row?.metadata?.labels;

    if (labels) {
      Object.keys(labels).forEach((key) => {
        labelKeys[key] = true;
      });
    }
  });

  Object.keys(labelKeys).sort().forEach((key) => {
    out.push({
      id:       `${ LABEL_FIELD_PREFIX }${ key }`,
      label:    key,
      isLabel:  true,
      labelKey: key,
    });
  });

  return out;
}

export function findField(fields: TableViewField[], id: string): TableViewField | undefined {
  if (!id) {
    return undefined;
  }

  const lower = id.toLowerCase();

  return fields.find((f) => f.id.toLowerCase() === lower);
}

export function valuesInUse(rows: TableViewRow[], field: TableViewField, max = 25): TableViewValueSuggestion[] {
  const counts: Record<string, number> = {};

  (rows || []).slice(0, SCAN_LIMIT).forEach((row) => {
    const raw = fieldValue(row, field);
    const values = Array.isArray(raw) ? raw : [raw];

    values.forEach((entry) => {
      const value = stringifyValue(entry).trim();

      if (value) {
        counts[value] = (counts[value] || 0) + 1;
      }
    });
  });

  return Object.entries(counts)
    .map(([value, count]) => ({ value, count }))
    .sort((a, b) => b.count - a.count || a.value.localeCompare(b.value))
    .slice(0, max);
}

/** Columns drawn as a moment in time. The rest are found by their values - see holdsTimestamps */
const DATE_FORMATTERS = ['LiveDate', 'Date'];

export function isDateColumn(header?: TableViewColumn): boolean {
  return DATE_FORMATTERS.includes(header?.formatter || '');
}

const TIMESTAMP = /^\d{4}-\d{2}-\d{2}T/;

/** A column no definition says is a date, but whose values are timestamps - a resource's own printer column */
function holdsTimestamps(rows: TableViewRow[], field: TableViewField): boolean {
  const values: string[] = [];

  for (const row of rows) {
    const value = stringifyValue(fieldValue(row, field));

    if (value) {
      values.push(value);
    }
    if (values.length === 25) {
      break;
    }
  }

  return values.length > 0 && values.filter((value) => TIMESTAMP.test(value)).length >= values.length * 0.8;
}

/** Does this date column hold any actual dates in the rows in hand, rather than text such as "23m"? */
export function holdsDates(rows: TableViewRow[], field: TableViewField): boolean {
  let seen = 0;

  for (const row of rows) {
    const text = dateText(fieldValue(row, field));

    if (TIMESTAMP.test(text)) {
      return true;
    }
    if (text && ++seen === 25) {
      return false;
    }
  }

  return false;
}

/**
 * A date as the text a date term matches: an ISO timestamp, whether it came as one or as epoch
 * milliseconds. Zero is the models' way of saying there is none. Anything else is left as it reads
 */
export function dateText(value: unknown): string {
  const text = stringifyValue(value).trim();

  if (!text || text === '0') {
    return '';
  }

  if (/^\d{11,13}$/.test(text)) {
    return new Date(Number(text)).toISOString();
  }

  return text;
}

/** A month grouping's key, eg `2026-11`, as it reads: "November 2026". Anything else is left alone */
export function monthLabel(key: string): string {
  return /^\d{4}-\d{2}$/.test(key) ? day(`${ key }-01`).format('MMMM YYYY') : key;
}

/**
 * Dates rolled up into the months they fall in, newest first, and the years above them when there
 * is more than one. A date term matches by prefix, so each of these is a filter in itself
 */
export function dateBuckets(values: TableViewValueSuggestion[]): TableViewValueSuggestion[] {
  const months: Record<string, number> = {};
  const years: Record<string, number> = {};

  values.forEach(({ value, count }) => {
    const date = dateText(value);

    if (TIMESTAMP.test(date)) {
      months[date.slice(0, 7)] = (months[date.slice(0, 7)] || 0) + count;
      years[date.slice(0, 4)] = (years[date.slice(0, 4)] || 0) + count;
    }
  });

  const newestFirst = (counts: Record<string, number>) => Object.keys(counts).sort().reverse().map((value) => ({ value, count: counts[value] }));
  const yearly = newestFirst(years);

  return (yearly.length > 1 ? yearly : []).concat(newestFirst(months));
}
