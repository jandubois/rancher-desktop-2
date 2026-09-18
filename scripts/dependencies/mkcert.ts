import {
  DependencyAsset,
  DownloadContext,
  downloadAndHash,
  GitHubDependency,
  GlobalDependency,
  GoArch,
  GUEST_DEP_VERSIONS_PATH,
} from '@/scripts/lib/dependencies';

const ARCHES: readonly GoArch[] = ['amd64', 'arm64'];

/**
 * mkcert, which the distro overlay bakes into the guest image for the
 * image allow list.  rddepman tracks it like any other GitHub dependency, so
 * there is no host install.  The release publishes bare binaries and no
 * checksum file, so nothing upstream cross-checks the digest rddepman records.
 */
export class Mkcert extends GlobalDependency(GitHubDependency) {
  readonly name = 'mkcert';
  readonly githubOwner = 'FiloSottile';
  readonly githubRepo = 'mkcert';
  readonly manifestPath = GUEST_DEP_VERSIONS_PATH;

  download(_context: DownloadContext): Promise<void> {
    return Promise.reject(new Error('mkcert is a guest-only dependency and is not installed by postinstall'));
  }

  getAssets(version: string): Promise<DependencyAsset[]> {
    const baseURL = `https://github.com/${ this.githubOwner }/${ this.githubRepo }/releases/download/v${ version }`;

    return Promise.all(ARCHES.map(async(arch) => {
      const url = `${ baseURL }/mkcert-v${ version }-linux-${ arch }`;

      return {
        platform: 'linux' as const, arch, url, checksum: await downloadAndHash(url), filename: 'mkcert',
      };
    }));
  }
}
