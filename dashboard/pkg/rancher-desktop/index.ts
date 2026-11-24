import { IExtension } from '@shell/core/types';
import { LimaProvisioner } from './provisioner';

export default function(plugin: IExtension): void {
  plugin.metadata = require('./package.json');
  plugin.metadata.icon = require('./assets/rancher-desktop.svg');

  plugin.register('provisioner', LimaProvisioner.ID, LimaProvisioner);
  plugin.register('machine-config', LimaProvisioner.ID, () => import('./machine-config/lima.vue'));
  plugin.addL10n('en-us', () => import('./l10n/en-us.yaml'));

  // Lima VMs run on the user's own machine, so the cluster form must not ask
  // for a cloud credential.
  plugin.register('cloud-credential', LimaProvisioner.ID, false);
}
