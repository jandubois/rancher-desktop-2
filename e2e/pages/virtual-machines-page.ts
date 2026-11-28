import { expect } from '@playwright/test';

import type { Locator, Page } from '@playwright/test';

type ActionString = 'Start' | 'Stop' | 'Delete';

export class VirtualMachinesPage {
  readonly page:            Page;
  readonly table:           Locator;
  readonly virtualMachines: Locator;

  constructor(page: Page) {
    this.page = page;
    this.table = page.getByTestId('virtual-machines-table');
    this.virtualMachines = this.table.locator('tr.main-row');
  }

  getVirtualMachineRow(name: string) {
    return this.virtualMachines.filter({ hasText: name });
  }

  getVirtualMachineStatus(name: string) {
    return this.getVirtualMachineRow(name).getByTestId('virtual-machine-status-cell');
  }

  getNamespaceGroup(namespace: string) {
    return this.table.locator('tr.group-row').filter({ hasText: namespace });
  }

  /**
   * Choose an action from the VM's row menu. The menu lists only the actions
   * enabled when it opened, so reopen it until the page has caught up with
   * the VM's last change.
   */
  async clickVirtualMachineAction(name: string, action: ActionString) {
    const menu = this.page.getByTestId('actionmenu');

    await expect(async() => {
      if (await menu.isVisible()) {
        // Clicking outside the menu closes it.
        await menu.locator('.background').click({ position: { x: 1, y: 1 } });
      }
      await this.getVirtualMachineRow(name).locator('.btn.role-multi-action.actions').click();
      await menu.getByText(action, { exact: true }).click({ timeout: 1_000 });
    }).toPass({ timeout: 15_000 });
  }
}
