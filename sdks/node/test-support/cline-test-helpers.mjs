import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import {
  createFixtureRoot,
  distUrl,
  homeEnvironment,
  installConfig as baseInstallConfig,
  runPlugin as basePlugin,
} from './harness.mjs';

export {
  VALID_PRIVATE_KEY,
  cliPath,
  readJson,
  registryModuleUrl,
  runNode,
  runProcess,
  startApiServer,
} from './harness.mjs';

export const pluginModuleUrl = distUrl('plugins/cline.js');
export const contractModuleUrl = distUrl('plugins/cline-contract.js');
export const ioModuleUrl = distUrl('plugins/cline-io.js');
export const installationModuleUrl = distUrl('plugins/cline-installation.js');
const PLUGIN = { exportName: 'clinePlugin', moduleUrl: pluginModuleUrl };

export function environment(fixture, clineDir = fixture.clineDir) {
  return { ...homeEnvironment(fixture), CLINE_DIR: clineDir === null ? undefined : clineDir };
}

export function installConfig(fixture, overrides = {}) {
  return baseInstallConfig('cline', fixture, overrides);
}

export function runPlugin(fixture, method, argument, clineDir = fixture.clineDir) {
  return basePlugin(PLUGIN, fixture, method, argument, { env: environment(fixture, clineDir) });
}

async function writeOptional(filePath, contents) {
  if (contents === undefined) return;
  await mkdir(path.dirname(filePath), { recursive: true });
  await writeFile(filePath, contents, { mode: 0o600 });
}

export async function createFixture({
  agentId = 'agent-1',
  baseUrl = 'http://127.0.0.1:9',
  existingAudit,
  existingGuard,
} = {}) {
  const base = await createFixtureRoot(
    'elydora-cline-',
    "home with spaces and 'quote %CLINE%",
    { agentId },
  );
  const clineDir = path.join(base.rootDir, 'custom-cline-home');
  const hooksDir = path.join(clineDir, 'hooks');
  const guardWrapperPath = path.join(hooksDir, 'PreToolUse.mjs');
  const auditWrapperPath = path.join(hooksDir, 'PostToolUse.mjs');
  await writeOptional(guardWrapperPath, existingGuard);
  await writeOptional(auditWrapperPath, existingAudit);
  return {
    ...base,
    auditWrapperPath,
    baseUrl,
    clineDir,
    guardWrapperPath,
    hooksDir,
    install(overrides = {}, clineDirOverride = this.clineDir) {
      return runPlugin(this, 'install', installConfig(this, overrides), clineDirOverride);
    },
  };
}
