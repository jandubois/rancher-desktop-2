// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

import { mount } from '@vue/test-utils';

import { headerFieldId } from '@pkg/utils/table-views/fields';
import mockModules from '@pkg/utils/testUtils/mockModules';

const viewTableStub = {
  name:     'ViewTable',
  props:    { coreColumns: Array, headers: Array },
  template: '<table />',
};

mockModules({ '@pkg/components/TableViews/ViewTable.vue': viewTableStub });

const { default: PortForwarding } = await import('@pkg/components/PortForwarding.vue');

describe('PortForwarding', () => {
  it('keeps the listen port column, which holds the only input for a forward', () => {
    const wrapper = mount(PortForwarding as any, {
      props:  { services: [] },
      global: { mocks: { t: (key: string) => key } },
    });
    const table = wrapper.getComponent(viewTableStub);
    const headers = table.props('headers') as { name: string }[];
    const listenPort = headers.find(header => header.name === 'listenPort')!;

    expect(table.props('coreColumns')).toContain(headerFieldId(listenPort));
  });
});
