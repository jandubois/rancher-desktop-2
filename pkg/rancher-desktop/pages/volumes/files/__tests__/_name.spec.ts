// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import { jest } from '@jest/globals';

import mockModules from '@pkg/utils/testUtils/mockModules';

const componentStub = { template: '<div />' };

mockModules({
  '@pkg/components/TableViews/ViewTable.vue': componentStub,
  '@pkg/entry/store':                         { mapTypedState: jest.fn(() => ({})) },
  '@pkg/utils/ipcRenderer':                   { ipcRenderer: { on: jest.fn(), send: jest.fn() } },
});

const { default: VolumeFiles } = await import('@pkg/pages/volumes/files/_name.vue');

describe('volume files columns', () => {
  const { methods } = VolumeFiles as any;
  const { headers } = (VolumeFiles as any).data.call({
    ...methods,
    $route: { query: {} },
    t:      (key: string) => key,
  });
  const header = (name: string) => headers.find((h: { name: string }) => h.name === name);

  it('gives the size that the cell shows', () => {
    expect(header('size').value({ isDirectory: true, size: 4096 })).toBe('-');
    expect(header('size').value({ isDirectory: false, size: 1536 })).toBe('1.5 KB');
  });

  it('gives the date that the cell shows', () => {
    const modified = new Date(2026, 0, 2, 3, 4, 5);

    expect(header('modified').value({ modified })).toBe(modified.toLocaleString());
  });
});
