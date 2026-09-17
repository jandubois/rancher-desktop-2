import {
  DependencyAsset,
  DownloadContext,
  downloadAndHash,
  fetchUpstreamChecksums,
  GitHubDependency,
  GlobalDependency,
  GoArch,
  GUEST_DEP_VERSIONS_PATH,
  Version,
} from '@/scripts/lib/dependencies';

const ARCHES: readonly GoArch[] = ['amd64', 'arm64'];

/**
 * The newest nerdctl the containerd engine controller can mirror.  2.1.3 drops
 * the nerdctl/ports label sync_containers_containerd.go reads; 2.3.0 tags
 * through containerd's transfer service, which publishes no image event for
 * the watcher.  Close both before raising this.
 */
const MAX_VERSION = '2.1.2';

/**
 * nerdctl, which the distro overlay bakes into the guest image.  rddepman
 * tracks it like any other GitHub dependency, so there is no host install.
 * The overlay needs buildctl and buildkitd too, and only the full tarball
 * has them.
 */
export class Nerdctl extends GlobalDependency(GitHubDependency) {
  readonly name = 'nerdctl';
  readonly githubOwner = 'containerd';
  readonly githubRepo = 'nerdctl';
  readonly manifestPath = GUEST_DEP_VERSIONS_PATH;

  download(_context: DownloadContext): Promise<void> {
    return Promise.reject(new Error('nerdctl is a guest-only dependency and is not installed by postinstall'));
  }

  async getAvailableVersions(): Promise<Version[]> {
    const versions = await super.getAvailableVersions();

    return versions.filter(version => this.rcompareVersions(version, MAX_VERSION) >= 0);
  }

  async getAssets(version: string): Promise<DependencyAsset[]> {
    const baseURL = `https://github.com/${ this.githubOwner }/${ this.githubRepo }/releases/download/v${ version }`;
    const upstream = await fetchUpstreamChecksums(`${ baseURL }/SHA256SUMS`, 'sha256');

    return Promise.all(ARCHES.map(async(arch) => {
      const artifact = `nerdctl-full-${ version }-linux-${ arch }.tar.gz`;
      const url = `${ baseURL }/${ artifact }`;
      const checksum = await downloadAndHash(url, {
        verify: { algorithm: 'sha256', expected: upstream[artifact] },
      });

      return {
        platform: 'linux' as const, arch, url, checksum, filename: 'nerdctl-full.tar.gz',
      };
    }));
  }
}
