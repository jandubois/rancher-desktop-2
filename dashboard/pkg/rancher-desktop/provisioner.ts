import { IClusterProvisioner, ClusterProvisionerContext, ClusterProvisionerDetailTabs } from '@shell/core/types';
import { DEFAULT_WORKSPACE } from '@shell/config/types';
import { normalizeName } from '@shell/utils/kube';
import { set } from '@shell/utils/object';
import isEqual from 'lodash/isEqual';

// Rancher names the machine provider after the first pool's machine template
// kind, and the dashboard looks up the provisioner by that name in lower case.
export const PROVIDER = 'limamachinetemplate';
export const LIMA_MACHINE_TEMPLATE_SCHEMA = 'infrastructure.cluster.x-k8s.io.limamachinetemplate';
export const LIMA_CLUSTER_SCHEMA = 'infrastructure.cluster.x-k8s.io.limacluster';
export const LIMA_API_VERSION = 'infrastructure.cluster.x-k8s.io/v1alpha1';

// Defaults for a new pool's VM; they match the provider's own defaults for an
// unset field. Disk stays unset, so Lima picks its default size.
const DEFAULT_CPUS = 2;
const DEFAULT_MEMORY = '3Gi';

export class LimaProvisioner implements IClusterProvisioner {
  static ID = PROVIDER;

  constructor(private context: ClusterProvisionerContext) {}

  get id(): string {
    return LimaProvisioner.ID;
  }

  // The cluster form reads the kind, group and version of a pool's
  // machineConfigRef from this schema's attributes.
  get machineConfigSchema(): { [key: string]: any } {
    return this.context.getters['management/schemaFor'](LIMA_MACHINE_TEMPLATE_SCHEMA);
  }

  get icon(): any {
    return require('./assets/rancher-desktop.svg');
  }

  get group(): string {
    return 'capi';
  }

  get label(): string {
    return this.context.t('lima.label');
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

  async createMachinePoolMachineConfig(): Promise<{ [key: string]: any }> {
    return await this.context.dispatch('management/create', {
      type:     LIMA_MACHINE_TEMPLATE_SCHEMA,
      metadata: { namespace: DEFAULT_WORKSPACE },
      spec:     { template: { spec: { cpus: DEFAULT_CPUS, memory: DEFAULT_MEMORY } } },
    });
  }

  // Each pool gets a template of its own, because the dashboard maps a
  // MachineDeployment to its pool by template name. Rancher also points the
  // MachineDeployment at its template by name, so a changed template is saved
  // under a new name. The new name makes Rancher roll out new machines for the
  // pool. Updating the template in place would not.
  //
  // Removed pools are left out of the cluster spec here. After the save, the
  // cluster form's default cleanup deletes their templates.
  async saveMachinePoolConfigs(pools: any[], cluster: any): Promise<void> {
    const finalPools = [];

    for (const entry of pools) {
      if (entry.remove) {
        continue;
      }

      entry.pool.name = normalizeName(entry.pool.name) || 'pool';
      const generateName = `nc-${ `${ cluster.metadata.name }-${ entry.pool.name }`.slice(0, 50).toLowerCase() }-`;

      try {
        if (entry.create) {
          if (!entry.config.metadata?.name) {
            entry.config.metadata.generateName = generateName;
          }
          entry.config = await entry.config.save();
          entry.pool.machineConfigRef.name = entry.config.metadata.name;
          entry.create = false;
          entry.update = true;
        } else if (entry.update && entry.config && await this.machineConfigWasModified(entry.config)) {
          const copy = await this.context.dispatch('management/clone', { resource: entry.config });

          delete copy.id;
          delete copy.links;
          delete copy.metadata.name;
          delete copy.metadata.resourceVersion;
          delete copy.metadata.uid;
          delete copy.metadata.creationTimestamp;
          copy.metadata.generateName = generateName;

          entry.config = await copy.save();
          entry.pool.machineConfigRef.name = entry.config.metadata.name;
        }
      } catch (e) {
        throw new Error(`${ this.context.t('lima.errors.savingMachineConfig') } ${ entry.pool.name }: ${ errorMessage(e) }`);
      }

      finalPools.push(entry.pool);
    }

    set(cluster, 'spec.rkeConfig.machinePools', finalPools);
  }

  // Compares the form's template spec with the server's copy. A template that
  // cannot be read counts as modified, so the pool gets a new one.
  private async machineConfigWasModified(config: any): Promise<boolean> {
    let server;

    try {
      server = await this.context.dispatch('management/find', {
        type: LIMA_MACHINE_TEMPLATE_SCHEMA,
        id:   config.id,
        opt:  { force: true, watch: false },
      });
    } catch (e) {
      return true;
    }

    return !isEqual(server?.spec?.template?.spec || {}, config.spec?.template?.spec || {});
  }

  // A new cluster needs a LimaCluster as its infrastructure cluster. Its spec
  // stays empty, because its only field is the control plane endpoint, and the
  // provider's CRD gives that field a default. Editing an existing cluster
  // leaves its LimaCluster alone.
  registerSaveHooks(registerBeforeHook: (fn: () => Promise<void>, name: string, priority?: number) => void): void {
    const context = this.context;

    registerBeforeHook(async function(this: any) {
      if (this.isEdit) {
        return;
      }

      const cluster = this.value;
      const { name, namespace } = cluster.metadata;

      try {
        // A failed cluster save leaves the LimaCluster behind, so a second
        // attempt reuses it.
        const existing = await context.dispatch('management/find', {
          type: LIMA_CLUSTER_SCHEMA,
          id:   `${ namespace }/${ name }`,
          opt:  { force: true, watch: false },
        }).catch(() => undefined);

        if (!existing) {
          const limaCluster = await context.dispatch('management/create', {
            type:     LIMA_CLUSTER_SCHEMA,
            metadata: { name, namespace },
            spec:     {},
          });

          await limaCluster.save();
        }
      } catch (e) {
        throw new Error(`${ context.t('lima.errors.savingInfrastructureCluster') }: ${ errorMessage(e) }`);
      }

      set(cluster, 'spec.rkeConfig.infrastructureRef', {
        kind:       'LimaCluster',
        apiVersion: LIMA_API_VERSION,
        name,
        namespace,
      });
    }, 'save-lima-cluster', 3);
  }
}

function errorMessage(e: any): string {
  return e?.message || e?.data?.message || `${ e }`;
}
