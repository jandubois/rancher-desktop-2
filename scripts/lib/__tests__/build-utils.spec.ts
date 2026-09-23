import childProcess from 'node:child_process';

import { jest } from '@jest/globals';
import semver from 'semver';

import buildUtils from '../build-utils';

import { EXT_MACRO } from '@pkg/utils/releaseArtifacts';

import type webpack from 'webpack';

describe('build-utils', () => {
  describe('externals', () => {
    /** Ask a config's externals handler how it would treat a request. */
    async function classify(config: Promise<webpack.Configuration>, request: string): Promise<string | undefined> {
      const [handler] = (await config).externals as any[];

      return new Promise((resolve) => {
        handler({ request }, (_error: unknown, result?: string) => resolve(result));
      });
    }

    it.each([
      // Bare dependencies keep their own resolution.
      ['electron-updater', 'electron-updater'],
      // Subpaths need the extension spelled out for Node's ESM loader.
      ['electron-updater/out/MacUpdater', 'electron-updater/out/MacUpdater.js'],
      ['lodash/merge', 'lodash/merge.js'],
      ['lodash/isEqual.js', 'lodash/isEqual.js'],
      // Optional dependencies ship with the app, so they are external too.
      // We currently do not have any cross-platform optional dependencies.
      // Everything else is bundled.
      ['@pkg/utils/logging', undefined],
      ['./relative', undefined],
    ])('externalizes %s', async(request, expected) => {
      await expect(classify(buildUtils.webpackConfig, request)).resolves.toBe(expected);
    });

    it('externalizes the main process as module-import, so dynamic imports stay dynamic', async() => {
      await expect(buildUtils.webpackConfig).resolves.toHaveProperty('externalsType', 'module-import');
    });

    it('should bundle everything into the preload script', async() => {
      // A sandboxed renderer loading it from outside the asar cannot resolve anything.
      await expect(buildUtils.webpackPreloadConfig).resolves.toHaveProperty('externals', []);
      await expect(buildUtils.webpackPreloadConfig).resolves.toHaveProperty('externalsType', 'commonjs2');
    });

    it.each([
      'lodash/no-such-module',
      'electron-updater/out/DoesNotExist.js',
    ])('rejects %s, which would only fail once packaged', async(request) => {
      await expect(classify(buildUtils.webpackConfig, request))
        .rejects.toThrow(`Cannot resolve external "${ request }"`);
    });
  });

  describe('docsUrl', () => {
    it.each([
      ['1.9.0', 'https://docs.rancherdesktop.io/1.9'],
      ['v1.24.187', 'https://docs.rancherdesktop.io/1.24'],
      ['v1.7.68-tech-preview', 'https://docs.rancherdesktop.io/1.7-tech-preview'],
      ['v1.86.37-1234-g56789abc', 'https://docs.rancherdesktop.io/next'],
      ['v1.8.2-fallback', 'https://docs.rancherdesktop.io/next'],
      ['v1.28.94-rc1-1234-g56789abc', 'https://docs.rancherdesktop.io/next'],
      ['invalid-version', 'https://docs.rancherdesktop.io/next'],
    ])('should return the correct docs URL for version %s', async(version, expectedUrl) => {
      jest.spyOn(buildUtils, 'version', 'get').mockResolvedValue(version);
      const docsUrl = await buildUtils.docsUrl;
      expect(docsUrl).toBe(expectedUrl);
    });
  });

  describe('isReleaseVersion', () => {
    afterEach(() => {
      jest.restoreAllMocks();
    });

    it.each([
      ['1.9.0-tech-preview', false],
      ['2.0.0-alpha.1', false],
      ['2.0.0-alpha.1-9-g0106d8077', false],
      ['2.0.0', true],
      ['2.0.0-9-g0106d8077', false],
      ['invalidVersion', false],
    ])('should classify %s as release=%s', async(version, expected) => {
      jest.spyOn(buildUtils, 'version', 'get').mockResolvedValue(version);
      await expect(buildUtils.isReleaseVersion).resolves.toBe(expected);
    });
  });

  describe('computeVersion', () => {
    afterEach(() => {
      jest.restoreAllMocks();
    });

    /**
     * rejectGit is a mock function for execFile that results in a rejection.
     */
    const rejectGit = jest.fn<() => any>().mockRejectedValue(new Error('git command failed'));

    it('should return the mock version when valid', async() => {
      const mockVersion = '1.2.3-mock';
      jest.replaceProperty(process, 'env', { ...process.env, RD_MOCK_VERSION: mockVersion });
      const actual = await buildUtils.computeVersion();
      expect(actual).toBe(mockVersion);
    });

    it('should return git version', async() => {
      const gitVersion = '1.2.3-4-g56789abc';
      // Mock the git describe command
      function execFile(command: string, args: string[], options: childProcess.ExecFileOptions): Promise<{ stdout: string; stderr: string }> {
        expect(command).toBe('git');
        expect(args).toEqual(['describe', '--tags']);
        expect(options).toHaveProperty('cwd');
        return Promise.resolve({ stdout: `v${ gitVersion }\n` }) as any;
      }
      const version = await buildUtils.computeVersion(execFile as any);
      expect(version).toBe(gitVersion);
    });

    it('should return package.json version when git command fails', async() => {
      const version = '1.2.3-package';
      jest.spyOn(buildUtils, 'packageMeta', 'get').mockReturnValue({ version } as any);
      const actual = buildUtils.computeVersion(rejectGit);
      await expect(actual).resolves.toBe(`${ version }-fallback`);
    });

    it('should return fallback version when no version is valid', async() => {
      jest.spyOn(semver, 'valid').mockReturnValue(null);
      const version = await buildUtils.computeVersion(rejectGit);
      expect(version).toBe('0.0.0-fallback');
    });
  });

  describe('checkArchiveArch', () => {
    afterEach(() => {
      jest.restoreAllMocks();
    });

    const artifactName = (arch: string) => `rancher-desktop-2.0.0.windows.${ arch }.${ EXT_MACRO }`;

    it('accepts an archive built for the selected architecture', () => {
      jest.spyOn(buildUtils, 'arch', 'get').mockReturnValue('arm64');
      expect(() => buildUtils.checkArchiveArch(artifactName('aarch64'), '2.0.0', 'win32')).not.toThrow();
    });

    it.each([
      ['x86_64', 'arm64', 'amd64'],
      ['aarch64', 'x64', 'arm64'],
    ] as const)('refuses an %s archive when signing for %s, naming GOARCH=%s', (archiveArch, arch, goarch) => {
      jest.spyOn(buildUtils, 'arch', 'get').mockReturnValue(arch);
      expect(() => buildUtils.checkArchiveArch(artifactName(archiveArch), '2.0.0', 'win32')).toThrow(`set GOARCH=${ goarch }`);
    });

    it('refuses an archive without a release artifact name', () => {
      expect(() => buildUtils.checkArchiveArch(undefined, '2.0.0', 'win32')).toThrow(`Cannot tell the archive's architecture`);
    });
  });
});
