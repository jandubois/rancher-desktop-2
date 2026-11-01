// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import { readView, writeView } from '@pkg/utils/table-views/storage';

const KEY = 'tableViews';

describe('table views storage', () => {
  beforeEach(() => localStorage.clear());

  it.each([
    ['malformed JSON', '{not json'],
    ['a value of the wrong kind', '["containers"]'],
    ['a table of the wrong kind', '{"containers":3}'],
    ['a view of the wrong kind', '{"containers":{"view":"x"}}'],
  ])('gives the defaults for %s', (_, stored) => {
    localStorage.setItem(KEY, stored);

    expect(readView('containers')).toStrictEqual({});
  });

  it('drops a field of the wrong kind and keeps the others', () => {
    localStorage.setItem(KEY, '{"containers":{"view":{"columns":"name","groupBy":"state"}}}');

    expect(readView('containers')).toStrictEqual({ groupBy: 'state' });
  });

  it('writes over malformed JSON', () => {
    localStorage.setItem(KEY, '{not json');
    writeView('containers', { groupBy: 'state' });

    expect(readView('containers')).toStrictEqual({ groupBy: 'state' });
  });

  it('keeps unknown fields at both levels, the fields not given, and other tables', () => {
    localStorage.setItem(KEY, JSON.stringify({
      containers: { views: ['saved'], view: { pinned: true, columns: ['name'] } },
      images:     { view: { groupBy: 'tag' } },
    }));

    writeView('containers', { groupBy: 'state' });

    expect(JSON.parse(localStorage.getItem(KEY)!)).toStrictEqual({
      containers: { views: ['saved'], view: { pinned: true, columns: ['name'], groupBy: 'state' } },
      images:     { view: { groupBy: 'tag' } },
    });
  });

  it('never stores the query', () => {
    writeView('containers', { query: 'nginx', groupBy: null });

    expect(localStorage.getItem(KEY)).not.toContain('nginx');
  });
});
