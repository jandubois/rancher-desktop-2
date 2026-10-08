import path from 'node:path';

import { Dependency, DownloadContext } from '@/scripts/lib/dependencies';
import { simpleSpawn } from '@/scripts/simple_process';

export class RDD implements Dependency {
  readonly name = 'rdd';
  async download(context: DownloadContext): Promise<void> {
    const importDir = import.meta.dirname;
    const rddDir = path.join(importDir, '..', '..', 'rdd');
    // The rdd-forwarder is a Windows-only shim; build it alongside rdd there.
    const targets = ['build-rdd', 'build-mock-controller'];

    if (context.goPlatform === 'windows') {
      targets.push('build-forwarder');
    }

    await simpleSpawn('make', targets, {
      cwd: rddDir,
      env: {
        ...process.env,
        GOOS:   context.goPlatform,
        GOARCH: context.goArch,
      },
    });
  }
}
