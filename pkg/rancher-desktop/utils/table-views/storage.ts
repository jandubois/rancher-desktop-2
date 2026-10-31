// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import isPlainObject from 'lodash/isPlainObject';
import pick from 'lodash/pick';

import type { TableViewState } from '@pkg/types/table-views';

/** The one key, holding every table's entry: `{ <tableId>: { view: { ... } } }` */
const STORAGE_KEY = 'tableViews';

/** The query stays out, because a filter restored after a restart would make rows look missing */
const STORED_FIELDS = ['columns', 'columnOrder', 'labelColumns', 'groupBy'] as const;

export type StoredView = Partial<Pick<TableViewState, typeof STORED_FIELDS[number]>>;

function isStringList(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((entry) => typeof entry === 'string');
}

const IS_VALID: Record<keyof StoredView, (value: unknown) => boolean> = {
  columns:      (value) => value === null || isStringList(value),
  columnOrder:  (value) => value === null || isStringList(value),
  labelColumns: isStringList,
  groupBy:      (value) => value === null || typeof value === 'string',
};

function asObject(value: unknown): Record<string, any> {
  return isPlainObject(value) ? value as Record<string, any> : {};
}

function readAll(): Record<string, any> {
  try {
    return asObject(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}'));
  } catch {
    return {};
  }
}

/** What is stored for a table. A field that is missing or of the wrong kind is left out */
export function readView(tableId: string): StoredView {
  const stored = asObject(asObject(readAll()[tableId]).view);
  const fields = STORED_FIELDS.filter((field) => field in stored && IS_VALID[field](stored[field]));

  return pick(stored, fields);
}

/**
 * Replaces the fields given and keeps the rest, including those that this version doesn't know,
 * because a table entry can hold more than its view
 */
export function writeView(tableId: string, view: Partial<TableViewState>): void {
  const all = readAll();
  const entry = asObject(all[tableId]);
  const stored = { ...asObject(entry.view), ...pick(view, STORED_FIELDS) };

  localStorage.setItem(STORAGE_KEY, JSON.stringify({ ...all, [tableId]: { ...entry, view: stored } }));
}
