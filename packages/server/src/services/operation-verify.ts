import type { EOR, EAR, AgentKey, Operation, Receipt, VerifyOperationResponse } from '../shared/index.js';
import {
  base64urlDecode,
  verifyEd25519Signature,
  jcsCanonicalise,
  computeChainHash,
  computeReceiptHash,
} from '../utils/crypto.js';
import { AppError } from '../middleware/error-handler.js';
import { getMerkleProof, verifyMerkleProof } from '../utils/merkle.js';
import type { Database, ObjectStore } from '../adapters/interfaces.js';
import { buildSignableEOR } from './operation-eor.js';

export async function verifyOperation(
  db: Database,
  r2: ObjectStore,
  operationId: string,
  orgId: string,
): Promise<VerifyOperationResponse> {
  const errors: string[] = [];
  let signatureCheck = false;
  let chainCheck = false;
  let receiptCheck = false;

  // Load the operation
  const operation = await db
    .prepare('SELECT * FROM operations WHERE operation_id = ? AND org_id = ?')
    .bind(operationId, orgId)
    .first<Operation>();

  if (!operation) {
    throw new AppError(404, 'NOT_FOUND', { key: 'operation.notFound', params: { id: operationId } });
  }

  // Load the EOR from R2
  if (!operation.r2_payload_key) {
    errors.push('Operation has no R2 payload key.');
    return { valid: false, checks: { signature: false, chain: false, receipt: false }, errors };
  }
  const eorObject = await r2.get(operation.r2_payload_key);
  if (!eorObject) {
    errors.push('EOR evidence not found in R2.');
    return { valid: false, checks: { signature: false, chain: false, receipt: false }, errors };
  }

  let eor: EOR;
  try {
    eor = JSON.parse(await eorObject.text()) as EOR;
  } catch {
    errors.push('Failed to parse EOR evidence from R2.');
    return { valid: false, checks: { signature: false, chain: false, receipt: false }, errors };
  }

  // ---- Signature verification ----
  try {
    const agentKey = await db
      .prepare('SELECT * FROM agent_keys WHERE kid = ? AND agent_id = ?')
      .bind(eor.agent_pubkey_kid, eor.agent_id)
      .first<AgentKey>();

    if (agentKey) {
      const signableEOR = buildSignableEOR(eor);
      const canonicalData = jcsCanonicalise(signableEOR);
      const dataBytes = new TextEncoder().encode(canonicalData);
      const signatureBytes = base64urlDecode(eor.signature);
      const publicKeyBytes = base64urlDecode(agentKey.public_key);

      signatureCheck = await verifyEd25519Signature(publicKeyBytes, signatureBytes, dataBytes);
      if (!signatureCheck) {
        errors.push('Ed25519 signature verification failed.');
      }
    } else {
      errors.push(`Signing key "${eor.agent_pubkey_kid}" not found.`);
    }
  } catch (e) {
    errors.push(`Signature check error: ${e instanceof Error ? e.message : String(e)}`);
  }

  // ---- Chain hash verification ----
  try {
    const expectedChainHash = await computeChainHash(
      operation.prev_chain_hash,
      operation.payload_hash,
      operation.operation_id,
      operation.issued_at,
    );

    chainCheck = expectedChainHash === operation.chain_hash;
    if (!chainCheck) {
      errors.push(`Chain hash mismatch: expected "${expectedChainHash}", stored "${operation.chain_hash}".`);
    }

    // Also verify the prev_chain_hash links correctly
    if (operation.seq_no > 1) {
      const prevOp = await db
        .prepare('SELECT chain_hash FROM operations WHERE agent_id = ? AND seq_no = ?')
        .bind(operation.agent_id, operation.seq_no - 1)
        .first<{ chain_hash: string }>();

      if (prevOp && prevOp.chain_hash !== operation.prev_chain_hash) {
        chainCheck = false;
        errors.push(`prev_chain_hash does not match previous operation's chain_hash.`);
      }
    }
  } catch (e) {
    errors.push(`Chain check error: ${e instanceof Error ? e.message : String(e)}`);
  }

  // ---- Receipt verification ----
  try {
    const receiptRow = await db
      .prepare('SELECT * FROM receipts WHERE operation_id = ?')
      .bind(operationId)
      .first<Receipt>();

    if (receiptRow) {
      const receiptObject = await r2.get(receiptRow.r2_receipt_key);
      if (receiptObject) {
        const ear = JSON.parse(await receiptObject.text()) as EAR;

        // Recompute receipt hash
        const receiptFields = {
          receipt_version: ear.receipt_version,
          receipt_id: ear.receipt_id,
          operation_id: ear.operation_id,
          org_id: ear.org_id,
          agent_id: ear.agent_id,
          server_received_at: ear.server_received_at,
          seq_no: ear.seq_no,
          chain_hash: ear.chain_hash,
          queue_message_id: ear.queue_message_id,
        };

        const expectedHash = await computeReceiptHash(receiptFields);
        if (expectedHash === ear.receipt_hash) {
          receiptCheck = true;
        } else {
          errors.push('Receipt hash does not match recomputed value.');
        }
      } else {
        errors.push('Receipt evidence not found in R2.');
      }
    } else {
      errors.push('No receipt found for this operation.');
    }
  } catch (e) {
    errors.push(`Receipt check error: ${e instanceof Error ? e.message : String(e)}`);
  }

  // ---- Merkle inclusion verification ----
  let merkleCheck: boolean | undefined;

  // Find epoch by operation's created_at timestamp
  const epochRecord = await db
    .prepare('SELECT * FROM epochs WHERE org_id = ? AND start_time <= ? AND end_time > ?')
    .bind(orgId, operation.created_at, operation.created_at)
    .first<{ epoch_id: string; org_id: string; start_time: number; end_time: number; root_hash: string; leaf_count: number; r2_epoch_key: string; created_at: number }>();

  if (epochRecord) {
    try {
      const epochObj = await r2.get(epochRecord.r2_epoch_key);
      if (epochObj) {
        const epochData = JSON.parse(await epochObj.text()) as {
          merkle: { leafOps: string[]; layers: string[][] };
        };
        // Check if operation's chain_hash is in the leaf layer
        const leaves = epochData.merkle.layers[0];
        if (leaves && leaves.includes(operation.chain_hash)) {
          // Verify the tree root matches the epoch's root_hash
          const tree = {
            root: epochRecord.root_hash,
            leaves,
            leafOps: epochData.merkle.leafOps,
            layers: epochData.merkle.layers,
          };
          const proof = getMerkleProof(tree, operation.chain_hash);
          if (proof) {
            merkleCheck = await verifyMerkleProof(proof);
            if (!merkleCheck) {
              errors.push('Merkle inclusion proof verification failed.');
            }
          } else {
            merkleCheck = false;
            errors.push('Operation chain_hash found in leaves but proof generation failed.');
          }
        } else {
          merkleCheck = false;
          errors.push('Operation chain_hash not found in epoch Merkle tree leaves.');
        }
      }
    } catch {
      // If R2 read fails, leave merkle as undefined (pending)
    }
  }
  // If no epoch found, merkleCheck remains undefined (pending)

  const valid = signatureCheck && chainCheck && receiptCheck;

  return {
    valid,
    checks: {
      signature: signatureCheck,
      chain: chainCheck,
      receipt: receiptCheck,
      ...(merkleCheck !== undefined ? { merkle: merkleCheck } : {}),
    },
    errors: errors.length > 0 ? errors : undefined,
  };
}
