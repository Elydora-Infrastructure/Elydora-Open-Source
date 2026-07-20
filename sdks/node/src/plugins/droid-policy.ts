import fsp from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { inspectPhysicalDirectory, readPhysicalFile, type FileSnapshot } from './managed-files.js';
import { parseStrictJsoncObject, type JsonObject } from './strict-json.js';

const MAX_SOURCE_BYTES = 2 * 1024 * 1024;

interface PolicyLocation {
  readonly filePath: string;
  readonly label: string;
}

interface PolicyLayer extends PolicyLocation {
  readonly snapshot?: FileSnapshot;
  readonly hooksDisabled?: boolean;
  readonly allowManagedHooksOnly?: boolean;
  readonly showHookOutput?: boolean;
}

export interface DroidPolicyPrecondition {
  readonly filePath: string;
  readonly label: string;
  readonly maximumBytes: number;
  readonly original?: FileSnapshot;
}

export interface DroidPolicyOrigin {
  readonly filePath: string;
  readonly label: string;
}

export interface DroidPolicyState {
  readonly allowManagedHooksOnlyBy?: DroidPolicyOrigin;
  readonly hooksDisabled?: boolean;
  readonly hooksDisabledBy?: DroidPolicyOrigin;
  readonly preconditions: readonly DroidPolicyPrecondition[];
}

function hasCode(error: unknown, code: string): boolean {
  return typeof error === 'object' && error !== null && 'code' in error && error.code === code;
}

function optionalBoolean(root: JsonObject, field: string, label: string): boolean | undefined {
  const value = root[field];
  if (value !== undefined && typeof value !== 'boolean') {
    throw new Error(`${label} field "${field}" must be a boolean`);
  }
  return value as boolean | undefined;
}

function managedSettingsPath(): string {
  if (process.platform === 'darwin') {
    return '/Library/Application Support/Factory/settings.json';
  }
  if (process.platform === 'win32') {
    return path.join(process.env.ProgramFiles ?? 'C:\\Program Files', 'Factory', 'settings.json');
  }
  return '/etc/factory/settings.json';
}

async function readLayer(location: PolicyLocation): Promise<PolicyLayer> {
  await inspectPhysicalDirectory(path.dirname(location.filePath), `${location.label} directory`);
  const snapshot = await readPhysicalFile(location.filePath, location.label, MAX_SOURCE_BYTES);
  if (!snapshot) return location;
  const root = parseStrictJsoncObject(snapshot.contents, `${location.label} at ${location.filePath}`);
  return {
    ...location,
    snapshot,
    hooksDisabled: optionalBoolean(root, 'hooksDisabled', location.label),
    allowManagedHooksOnly: optionalBoolean(root, 'allowManagedHooksOnly', location.label),
    showHookOutput: optionalBoolean(root, 'showHookOutput', location.label),
  };
}

async function gitRoot(start: string): Promise<string | undefined> {
  let current = path.resolve(start);
  for (;;) {
    const marker = path.join(current, '.git');
    try {
      const metadata = await fsp.lstat(marker);
      if (metadata.isSymbolicLink() || (!metadata.isDirectory() && !metadata.isFile())) {
        throw new Error(`Factory Droid project marker is not physical: ${marker}`);
      }
      return current;
    } catch (error) {
      if (!hasCode(error, 'ENOENT') && !hasCode(error, 'ENOTDIR')) throw error;
    }
    const parent = path.dirname(current);
    if (parent === current) return undefined;
    current = parent;
  }
}

function projectDirectories(root: string, current: string): string[] {
  const directories = [root];
  const relative = path.relative(root, current);
  if (!relative || relative.startsWith('..') || path.isAbsolute(relative)) return directories;
  let directory = root;
  for (const segment of relative.split(path.sep).filter(Boolean)) {
    directory = path.join(directory, segment);
    directories.push(directory);
  }
  return directories;
}

async function projectLocations(): Promise<PolicyLocation[][]> {
  const current = path.resolve(process.cwd());
  const root = await gitRoot(current) ?? current;
  return projectDirectories(root, current).map((directory, index) => {
    const factory = path.join(directory, '.factory');
    const scope = index === 0 ? 'project' : `folder ${directory}`;
    return [{
      filePath: path.join(factory, 'settings.json'),
      label: `Factory Droid ${scope} settings`,
    }, {
      filePath: path.join(factory, 'settings.local.json'),
      label: `Factory Droid ${scope} local settings`,
    }];
  });
}

function scopeValue(layers: readonly PolicyLayer[], field: 'hooksDisabled'): PolicyLayer | undefined {
  const [settings, local] = layers;
  return local[field] !== undefined ? local : settings[field] !== undefined ? settings : undefined;
}

export async function readDroidPolicy(): Promise<DroidPolicyState> {
  const managedLocation: PolicyLocation = {
    filePath: managedSettingsPath(),
    label: 'Factory Droid system-managed settings',
  };
  const scopes = await projectLocations();
  const [managed, ...projectLayers] = await Promise.all([
    readLayer(managedLocation),
    ...scopes.map(async (scope) => Promise.all(scope.map(readLayer))),
  ]);
  const flattened = [managed, ...projectLayers.flat()];
  const allowManagedHooksOnlyBy = managed.allowManagedHooksOnly === true
    ? { filePath: managed.filePath, label: managed.label }
    : undefined;
  const selected = [managed, ...projectLayers.map((scope) => scopeValue(scope, 'hooksDisabled'))]
    .find((layer) => layer?.hooksDisabled !== undefined);
  return {
    allowManagedHooksOnlyBy,
    hooksDisabled: selected?.hooksDisabled,
    hooksDisabledBy: selected
      ? { filePath: selected.filePath, label: selected.label }
      : undefined,
    preconditions: flattened.map((layer) => ({
      filePath: layer.filePath,
      label: layer.label,
      maximumBytes: MAX_SOURCE_BYTES,
      original: layer.snapshot,
    })),
  };
}
