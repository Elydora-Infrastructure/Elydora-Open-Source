import fsp from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { asError, errorMessage, hasCode, samePath } from './common.js';
import {
  AGENT_KEY,
  LEGACY_EVENTS,
  STABLE_EVENTS,
  createKimiDocument,
  parseKimiDocument,
  type KimiConfigDocument,
  type KimiContract,
  type KimiRuntimeContract,
} from './kimi-contract.js';
import { inspectPhysicalDirectory, readPhysicalFile } from './managed-files.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';

async function pathEntryExists(filePath: string, label: string): Promise<boolean> {
  try {
    await fsp.lstat(filePath);
    return true;
  } catch (error) {
    if (hasCode(error, 'ENOENT') || hasCode(error, 'ENOTDIR')) return false;
    throw new Error(`Inspect ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

function stableContract(configPath: string): KimiContract {
  return {
    generation: 'stable',
    runtimeName: 'Kimi Code',
    label: 'Kimi Code hooks config',
    directoryLabel: 'Kimi Code home directory',
    configPath,
    events: STABLE_EVENTS,
  };
}

function legacyContract(configPath: string): KimiContract {
  return {
    generation: 'legacy',
    runtimeName: 'kimi-cli',
    label: 'kimi-cli legacy hooks config',
    directoryLabel: 'kimi-cli legacy home directory',
    configPath,
    events: LEGACY_EVENTS,
  };
}

export async function resolveKimiContracts(): Promise<KimiContract[]> {
  const home = os.homedir();
  const configuredHome = process.env.KIMI_CODE_HOME;
  const explicitHome = configuredHome === undefined || configuredHome === ''
    ? undefined
    : path.resolve(configuredHome);
  const stableHome = explicitHome ?? path.join(home, '.kimi-code');
  const legacyHome = path.join(home, '.kimi');
  const stable = stableContract(path.join(stableHome, 'config.toml'));
  const legacy = legacyContract(path.join(legacyHome, 'config.toml'));
  if (samePath(stable.configPath, legacy.configPath)) return [stable];

  const stableDetected = explicitHome !== undefined
    || await pathEntryExists(stableHome, 'Kimi Code home');
  const legacyDetected = await pathEntryExists(legacyHome, 'kimi-cli legacy home');
  if (legacyDetected && !stableDetected) return [legacy];
  return legacyDetected ? [stable, legacy] : [stable];
}

async function readKimiDocument(contract: KimiContract): Promise<KimiConfigDocument> {
  await inspectPhysicalDirectory(path.dirname(contract.configPath), contract.directoryLabel);
  const snapshot = await readPhysicalFile(contract.configPath, contract.label);
  return snapshot
    ? parseKimiDocument(contract, snapshot.contents)
    : createKimiDocument(contract);
}

export async function readKimiDocuments(): Promise<KimiConfigDocument[]> {
  const contracts = await resolveKimiContracts();
  const documents: KimiConfigDocument[] = [];
  for (const contract of contracts) documents.push(await readKimiDocument(contract));
  return documents;
}

export async function kimiRuntimeFilesExist(
  contracts: readonly KimiRuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    if (await managedRuntimeFilesExist(contract, AGENT_KEY)) return true;
  }
  return false;
}
