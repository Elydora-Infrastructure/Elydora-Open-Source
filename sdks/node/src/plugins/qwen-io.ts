import { AGENT_KEY, type QwenRuntimeContract } from './qwen-contract.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';

export async function qwenRuntimeFilesExist(
  contracts: readonly QwenRuntimeContract[],
): Promise<boolean> {
  for (const contract of contracts) {
    if (await managedRuntimeFilesExist(contract, AGENT_KEY)) return true;
  }
  return false;
}
