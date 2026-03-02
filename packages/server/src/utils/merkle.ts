/**
 * SHA-256 Merkle tree implementation for epoch rollups.
 *
 * Leaf hashes are sorted lexicographically before tree construction.
 * Internal nodes are computed by concatenating the raw bytes (not base64url
 * strings) of their children and hashing the result with SHA-256.
 */

import { sha256Base64url, base64urlDecode } from './crypto.js';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface MerkleTree {
  /** Base64url SHA-256 root hash */
  root: string;
  /** Sorted leaf hashes */
  leaves: string[];
  /** Operation IDs matching sorted leaves */
  leafOps: string[];
  /** All tree layers from leaves (index 0) to root (last index) */
  layers: string[][];
}

export interface MerkleProof {
  /** The leaf hash being proved */
  leaf: string;
  /** Sibling hashes along the path to the root */
  siblings: string[];
  /** Direction of each sibling ('left' if sibling is on the left) */
  directions: ('left' | 'right')[];
  /** The expected root hash */
  root: string;
}

// ---------------------------------------------------------------------------
// Internal helper
// ---------------------------------------------------------------------------

/**
 * Compute the parent hash from two child hashes.
 * Decodes both base64url hashes to raw bytes, concatenates, then SHA-256.
 */
async function hashPair(left: string, right: string): Promise<string> {
  const leftBytes = base64urlDecode(left);
  const rightBytes = base64urlDecode(right);
  const combined = new Uint8Array(leftBytes.length + rightBytes.length);
  combined.set(leftBytes);
  combined.set(rightBytes, leftBytes.length);
  return sha256Base64url(combined);
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

/**
 * Build a Merkle tree from leaf hashes and their corresponding operation IDs.
 *
 * Leaves are sorted lexicographically (with operationIds reordered to match).
 * If a layer has an odd number of nodes the last node is duplicated.
 */
export async function buildMerkleTree(
  leafHashes: string[],
  operationIds: string[],
): Promise<MerkleTree> {
  // Pair up leaves with their operation IDs and sort by leaf hash
  const paired = leafHashes.map((hash, i) => ({
    hash,
    opId: operationIds[i]!,
  }));
  paired.sort((a, b) => (a.hash < b.hash ? -1 : a.hash > b.hash ? 1 : 0));

  const sortedLeaves = paired.map((p) => p.hash);
  const sortedOps = paired.map((p) => p.opId);

  const layers: string[][] = [sortedLeaves];

  let currentLayer = sortedLeaves;
  while (currentLayer.length > 1) {
    const nextLayer: string[] = [];
    // If odd, duplicate the last node
    if (currentLayer.length % 2 !== 0) {
      currentLayer = [...currentLayer, currentLayer[currentLayer.length - 1]!];
    }
    for (let i = 0; i < currentLayer.length; i += 2) {
      nextLayer.push(await hashPair(currentLayer[i]!, currentLayer[i + 1]!));
    }
    layers.push(nextLayer);
    currentLayer = nextLayer;
  }

  return {
    root: currentLayer[0]!,
    leaves: sortedLeaves,
    leafOps: sortedOps,
    layers,
  };
}

// ---------------------------------------------------------------------------
// Proof generation
// ---------------------------------------------------------------------------

/**
 * Generate a Merkle inclusion proof for a given leaf hash.
 *
 * Returns null if the leaf is not found in the tree.
 */
export function getMerkleProof(
  tree: MerkleTree,
  leafHash: string,
): MerkleProof | null {
  let index = tree.leaves.indexOf(leafHash);
  if (index === -1) return null;

  const siblings: string[] = [];
  const directions: ('left' | 'right')[] = [];

  for (let layerIdx = 0; layerIdx < tree.layers.length - 1; layerIdx++) {
    let layer = tree.layers[layerIdx]!;
    // Duplicate last node if odd (mirrors build logic)
    if (layer.length % 2 !== 0) {
      layer = [...layer, layer[layer.length - 1]!];
    }
    const siblingIndex = index % 2 === 0 ? index + 1 : index - 1;
    siblings.push(layer[siblingIndex]!);
    directions.push(index % 2 === 0 ? 'right' : 'left');
    index = Math.floor(index / 2);
  }

  return {
    leaf: leafHash,
    siblings,
    directions,
    root: tree.root,
  };
}

// ---------------------------------------------------------------------------
// Proof verification
// ---------------------------------------------------------------------------

/**
 * Verify a Merkle inclusion proof by recomputing the root.
 */
export async function verifyMerkleProof(proof: MerkleProof): Promise<boolean> {
  let current = proof.leaf;

  for (let i = 0; i < proof.siblings.length; i++) {
    const sibling = proof.siblings[i]!;
    const direction = proof.directions[i]!;
    if (direction === 'left') {
      current = await hashPair(sibling, current);
    } else {
      current = await hashPair(current, sibling);
    }
  }

  return current === proof.root;
}
