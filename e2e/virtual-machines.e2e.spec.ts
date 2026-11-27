import { ElectronApplication, Page, expect, test } from '@playwright/test';

import { NavPage } from './pages/nav-page';
import { VirtualMachinesPage } from './pages/virtual-machines-page';
import {
  answerMessageBox, CONFIRM_DELETE, getRDDConfig, lastMessageBox, rdd, startSlowerDesktop, teardown,
} from './utils/TestUtils';

import * as RDDClient from '@rdd-client';

let page: Page;

test.describe.configure({ mode: 'serial' });

test.describe('Virtual Machines Tests', () => {
  let electronApp: ElectronApplication;
  let client: RDDClient.LimaRancherdesktopIoV1alpha1Api;
  let rddConfigCleanup = () => {};

  const namespace = 'e2e-vm';
  const vmName = 'test-vm';
  const templateName = 'e2e-vm-source';

  test.beforeAll(async({ colorScheme }, testInfo) => {
    [electronApp, page] = await startSlowerDesktop(testInfo);
    const [rddConfig, cleanup] = await getRDDConfig();

    rddConfigCleanup = cleanup;
    client = rddConfig.makeApiClient(RDDClient.LimaRancherdesktopIoV1alpha1Api);

    const navPage = new NavPage(page);

    await navPage.waitForAppSettled();
  });

  test.afterAll(async({ colorScheme }, testInfo) => {
    await teardown(electronApp, testInfo);
    rddConfigCleanup();
  });

  test('should navigate to virtual machines page', async() => {
    const navPage = new NavPage(page);
    const virtualMachinesPage = await navPage.navigateTo('VirtualMachines');

    await expect(navPage.mainTitle).toHaveText('Virtual Machines');
    await expect(virtualMachinesPage.table).toContainText('There are no virtual machines to show');
  });

  test('should display a new virtual machine', async() => {
    const virtualMachinesPage = new VirtualMachinesPage(page);

    await rdd('ctl', 'create', 'namespace', namespace);
    await rdd('ctl', 'create', 'configmap', templateName, `--namespace=${ namespace }`, '--from-literal=template=images: []');
    await client.createNamespacedLimaVM({
      namespace,
      body: {
        apiVersion: 'lima.rancherdesktop.io/v1alpha1',
        kind:       'LimaVM',
        metadata:   { name: vmName },
        spec:       {
          templateRef: { name: templateName, namespace },
          running:     false,
        },
      },
    });

    const row = virtualMachinesPage.getVirtualMachineRow(vmName);

    await expect(row).toBeVisible({ timeout: 15_000 });
    await expect(virtualMachinesPage.getNamespaceGroup(namespace)).toBeVisible();
    await expect(virtualMachinesPage.getVirtualMachineStatus(vmName)).toHaveText('Pending');
  });

  test('should start and stop a virtual machine', async() => {
    const virtualMachinesPage = new VirtualMachinesPage(page);
    // The mock control plane doesn't run a LimaVM controller, so only the desired state changes.
    const desiredRunning = async() => (await client.readNamespacedLimaVM({ namespace, name: vmName })).spec?.running;

    await virtualMachinesPage.clickVirtualMachineAction(vmName, 'Start');
    await expect.poll(desiredRunning).toBe(true);
    await virtualMachinesPage.clickVirtualMachineAction(vmName, 'Stop');
    await expect.poll(desiredRunning).toBe(false);
  });

  test('should delete a virtual machine', async() => {
    const virtualMachinesPage = new VirtualMachinesPage(page);

    await answerMessageBox(electronApp, CONFIRM_DELETE);
    await virtualMachinesPage.clickVirtualMachineAction(vmName, 'Delete');
    await expect(lastMessageBox(electronApp)).resolves.toMatchObject({ message: 'Delete 1 virtual machine?', detail: `${ namespace }/${ vmName }` });
    await expect(virtualMachinesPage.getVirtualMachineRow(vmName)).toHaveCount(0, { timeout: 15_000 });
    await expect(client.readNamespacedLimaVM({ namespace, name: vmName })).rejects.toMatchObject({ code: 404 });
  });
});
