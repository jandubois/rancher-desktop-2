import { IClusterProvisioner, ClusterProvisionerContext, ClusterProvisionerDetailTabs } from '@shell/core/types';

// Rancher names the machine provider after the first pool's machine template
// kind, and the dashboard looks up the provisioner by that name in lower case.
export const PROVIDER = 'limamachinetemplate';
export const LIMA_MACHINE_TEMPLATE_SCHEMA = 'infrastructure.cluster.x-k8s.io.limamachinetemplate';

export class LimaProvisioner implements IClusterProvisioner {
  static ID = PROVIDER;

  constructor(private context: ClusterProvisionerContext) {}

  get id(): string {
    return LimaProvisioner.ID;
  }

  get machineConfigSchema(): { [key: string]: any } {
    return { id: LIMA_MACHINE_TEMPLATE_SCHEMA };
  }

  get icon(): any {
    return require('./assets/rancher-desktop.svg');
  }

  get group(): string {
    return 'capi';
  }

  get label(): string {
    return 'Rancher Desktop';
  }

  // Only offer the tile when rdd has installed the Lima provider's CRDs.
  get hidden(): boolean {
    return !this.context.getters['management/schemaFor'](LIMA_MACHINE_TEMPLATE_SCHEMA);
  }

  get detailTabs(): ClusterProvisionerDetailTabs {
    return {
      machines:     true,
      logs:         true,
      registration: false,
      snapshots:    true,
      related:      true,
      events:       false,
      conditions:   true,
      autoscaler:   false,
    };
  }

  get showImport(): boolean {
    return false;
  }

  get isUpstreamCAPIProvider(): boolean {
    return true;
  }
}
