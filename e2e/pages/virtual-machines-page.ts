import type { Locator, Page } from '@playwright/test';

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
}
