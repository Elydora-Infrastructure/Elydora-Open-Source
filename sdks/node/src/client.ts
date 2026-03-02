import type {
  ElydoraClientConfig,
  CreateOperationParams,
  EOR,
  RegisterAgentRequest,
  RegisterAgentResponse,
  GetAgentResponse,
  ListAgentsResponse,
  UnfreezeAgentResponse,
  DeleteAgentResponse,
  SubmitOperationResponse,
  GetOperationResponse,
  VerifyOperationResponse,
  AuditQueryRequest,
  AuditQueryResponse,
  ListEpochsResponse,
  GetEpochResponse,
  CreateExportRequest,
  CreateExportResponse,
  ListExportsResponse,
  GetExportResponse,
  GetMeResponse,
  IssueApiTokenResponse,
  JWKSResponse,
  HealthResponse,
  AuthRegisterResponse,
  AuthLoginResponse,
  ErrorResponse,
} from './types.js';
import { ElydoraError } from './errors.js';
import {
  jcsCanonicalise,
  computePayloadHash,
  computeChainHash,
  signEd25519,
  derivePublicKey,
  ZERO_CHAIN_HASH,
} from './crypto.js';
import { uuidv7, generateNonce } from './utils.js';

const DEFAULT_BASE_URL = 'https://api.elydora.com';
const DEFAULT_TTL_MS = 30_000;
const DEFAULT_MAX_RETRIES = 3;

export class ElydoraClient {
  private readonly orgId: string;
  private readonly agentId: string;
  private readonly privateKey: string;
  private readonly baseUrl: string;
  private readonly ttlMs: number;
  private readonly maxRetries: number;
  private readonly kid: string;
  private prevChainHash: string;
  private token: string | undefined;

  constructor(config: ElydoraClientConfig) {
    this.orgId = config.orgId;
    this.agentId = config.agentId;
    this.privateKey = config.privateKey;
    this.baseUrl = (config.baseUrl ?? DEFAULT_BASE_URL).replace(/\/+$/, '');
    this.ttlMs = config.ttlMs ?? DEFAULT_TTL_MS;
    this.maxRetries = config.maxRetries ?? DEFAULT_MAX_RETRIES;
    this.kid = config.kid ?? this.agentId + '-key-v1';
    this.prevChainHash = ZERO_CHAIN_HASH;
  }

  /**
   * Set the JWT token used for authenticated API calls.
   */
  setToken(token: string): void {
    this.token = token;
  }

  /**
   * Get the current chain hash (useful for debugging/inspection).
   */
  getChainHash(): string {
    return this.prevChainHash;
  }

  /**
   * Get the public key derived from the configured private key.
   */
  getPublicKey(): string {
    return derivePublicKey(this.privateKey);
  }

  // -------------------------------------------------------------------------
  // Auth (static methods — no instance needed)
  // -------------------------------------------------------------------------

  static async register(
    baseUrl: string,
    email: string,
    password: string,
    displayName?: string,
    orgName?: string,
  ): Promise<AuthRegisterResponse> {
    const url = `${baseUrl.replace(/\/+$/, '')}/v1/auth/register`;
    const body: Record<string, unknown> = { email, password };
    if (displayName !== undefined) body.display_name = displayName;
    if (orgName !== undefined) body.org_name = orgName;

    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });

    return handleResponse<AuthRegisterResponse>(res);
  }

  static async login(
    baseUrl: string,
    email: string,
    password: string,
  ): Promise<AuthLoginResponse> {
    const url = `${baseUrl.replace(/\/+$/, '')}/v1/auth/login`;
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    });

    return handleResponse<AuthLoginResponse>(res);
  }

  // -------------------------------------------------------------------------
  // Auth (instance methods — require token)
  // -------------------------------------------------------------------------

  async getMe(): Promise<GetMeResponse> {
    return this.request<GetMeResponse>('GET', '/v1/auth/me');
  }

  async issueApiToken(ttlSeconds?: number | null): Promise<IssueApiTokenResponse> {
    return this.request<IssueApiTokenResponse>('POST', '/v1/auth/token', { ttl_seconds: ttlSeconds ?? null });
  }

  // -------------------------------------------------------------------------
  // Agent management
  // -------------------------------------------------------------------------

  async registerAgent(request: RegisterAgentRequest): Promise<RegisterAgentResponse> {
    return this.request<RegisterAgentResponse>('POST', '/v1/agents/register', request);
  }

  async getAgent(agentId: string): Promise<GetAgentResponse> {
    return this.request<GetAgentResponse>('GET', `/v1/agents/${encodeURIComponent(agentId)}`);
  }

  async freezeAgent(agentId: string, reason: string): Promise<void> {
    await this.request<unknown>('POST', `/v1/agents/${encodeURIComponent(agentId)}/freeze`, { reason });
  }

  async listAgents(): Promise<ListAgentsResponse> {
    return this.request<ListAgentsResponse>('GET', '/v1/agents');
  }

  async unfreezeAgent(agentId: string, reason: string): Promise<UnfreezeAgentResponse> {
    return this.request<UnfreezeAgentResponse>('POST', `/v1/agents/${encodeURIComponent(agentId)}/unfreeze`, { reason });
  }

  async deleteAgent(agentId: string): Promise<DeleteAgentResponse> {
    return this.request<DeleteAgentResponse>('DELETE', `/v1/agents/${encodeURIComponent(agentId)}`);
  }

  async revokeKey(agentId: string, kid: string, reason: string): Promise<void> {
    await this.request<unknown>('POST', `/v1/agents/${encodeURIComponent(agentId)}/revoke`, { kid, reason });
  }

  // -------------------------------------------------------------------------
  // Operations (core)
  // -------------------------------------------------------------------------

  /**
   * Construct and sign an Elydora Operation Record (EOR) locally.
   *
   * 1. Generate UUIDv7 for operation_id
   * 2. Generate random nonce (16 bytes, base64url)
   * 3. Compute payload_hash (SHA-256 of JCS-canonicalized payload)
   * 4. Compute chain_hash from prev_chain_hash
   * 5. Construct canonical EOR (minus signature)
   * 6. Sign JCS-canonicalized EOR with Ed25519
   * 7. Update internal prev_chain_hash
   */
  createOperation(params: CreateOperationParams): EOR {
    const operationId = uuidv7();
    const issuedAt = Date.now();
    const nonce = generateNonce();
    const payload = params.payload ?? null;

    // Compute payload hash
    const payloadHash = computePayloadHash(payload);

    // Compute chain hash
    const chainHash = computeChainHash(
      this.prevChainHash,
      payloadHash,
      operationId,
      issuedAt,
    );

    // Construct the EOR without signature for signing
    const eorWithoutSig: Omit<EOR, 'signature'> = {
      op_version: '1.0',
      operation_id: operationId,
      org_id: this.orgId,
      agent_id: this.agentId,
      issued_at: issuedAt,
      ttl_ms: this.ttlMs,
      nonce,
      operation_type: params.operationType,
      subject: params.subject,
      action: params.action,
      payload,
      payload_hash: payloadHash,
      prev_chain_hash: this.prevChainHash,
      agent_pubkey_kid: this.kid,
    };

    // Sign the JCS-canonicalized EOR (without signature field)
    const canonical = jcsCanonicalise(eorWithoutSig);
    const signature = signEd25519(this.privateKey, Buffer.from(canonical, 'utf-8'));

    // Update internal chain state
    this.prevChainHash = chainHash;

    return {
      ...eorWithoutSig,
      signature,
    };
  }

  async submitOperation(eor: EOR): Promise<SubmitOperationResponse> {
    return this.request<SubmitOperationResponse>('POST', '/v1/operations', eor);
  }

  async getOperation(operationId: string): Promise<GetOperationResponse> {
    return this.request<GetOperationResponse>('GET', `/v1/operations/${encodeURIComponent(operationId)}`);
  }

  async verifyOperation(operationId: string): Promise<VerifyOperationResponse> {
    return this.request<VerifyOperationResponse>('POST', `/v1/operations/${encodeURIComponent(operationId)}/verify`);
  }

  // -------------------------------------------------------------------------
  // Audit
  // -------------------------------------------------------------------------

  async queryAudit(params: AuditQueryRequest): Promise<AuditQueryResponse> {
    return this.request<AuditQueryResponse>('POST', '/v1/audit/query', params);
  }

  // -------------------------------------------------------------------------
  // Epochs
  // -------------------------------------------------------------------------

  async listEpochs(): Promise<ListEpochsResponse> {
    return this.request<ListEpochsResponse>('GET', '/v1/epochs');
  }

  async getEpoch(epochId: string): Promise<GetEpochResponse> {
    return this.request<GetEpochResponse>('GET', `/v1/epochs/${encodeURIComponent(epochId)}`);
  }

  // -------------------------------------------------------------------------
  // Exports
  // -------------------------------------------------------------------------

  async createExport(params: CreateExportRequest): Promise<CreateExportResponse> {
    return this.request<CreateExportResponse>('POST', '/v1/exports', params);
  }

  async listExports(): Promise<ListExportsResponse> {
    return this.request<ListExportsResponse>('GET', '/v1/exports');
  }

  async getExport(exportId: string): Promise<GetExportResponse> {
    return this.request<GetExportResponse>('GET', `/v1/exports/${encodeURIComponent(exportId)}`);
  }

  async downloadExport(exportId: string): Promise<ArrayBuffer> {
    const url = `${this.baseUrl}/v1/exports/${encodeURIComponent(exportId)}/download`;
    const headers: Record<string, string> = {};

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    const res = await fetch(url, { method: 'GET', headers });

    if (!res.ok) {
      return handleResponse<never>(res);
    }

    return res.arrayBuffer();
  }

  // -------------------------------------------------------------------------
  // JWKS
  // -------------------------------------------------------------------------

  async getJWKS(): Promise<JWKSResponse> {
    const url = `${this.baseUrl}/.well-known/elydora/jwks.json`;
    const res = await fetch(url, {
      method: 'GET',
      headers: { 'Accept': 'application/json' },
    });
    return handleResponse<JWKSResponse>(res);
  }

  // -------------------------------------------------------------------------
  // Health
  // -------------------------------------------------------------------------

  async health(): Promise<HealthResponse> {
    const url = `${this.baseUrl}/v1/health`;
    const res = await fetch(url, {
      method: 'GET',
      headers: { 'Accept': 'application/json' },
    });
    return handleResponse<HealthResponse>(res);
  }

  // -------------------------------------------------------------------------
  // Internal HTTP helpers
  // -------------------------------------------------------------------------

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
  ): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const headers: Record<string, string> = {
      'Accept': 'application/json',
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }

    let lastError: Error | undefined;

    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      try {
        const res = await fetch(url, {
          method,
          headers,
          body: body !== undefined ? JSON.stringify(body) : undefined,
        });

        // Only retry on 429 or 5xx
        if (attempt < this.maxRetries && (res.status === 429 || res.status >= 500)) {
          const retryAfter = res.headers.get('Retry-After');
          const delayMs = retryAfter
            ? parseInt(retryAfter, 10) * 1000
            : Math.min(1000 * 2 ** attempt, 10_000);
          await sleep(delayMs);
          continue;
        }

        return await handleResponse<T>(res);
      } catch (err) {
        lastError = err instanceof Error ? err : new Error(String(err));

        // Only retry on network errors, not API errors
        if (err instanceof ElydoraError) {
          throw err;
        }

        if (attempt < this.maxRetries) {
          await sleep(Math.min(1000 * 2 ** attempt, 10_000));
          continue;
        }
      }
    }

    throw lastError ?? new Error('Request failed after retries');
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async function handleResponse<T>(res: Response): Promise<T> {
  if (res.ok) {
    // 204 No Content
    if (res.status === 204) {
      return undefined as T;
    }
    return (await res.json()) as T;
  }

  let errorBody: ErrorResponse | undefined;
  try {
    errorBody = (await res.json()) as ErrorResponse;
  } catch {
    // Response body was not valid JSON
  }

  if (errorBody?.error) {
    throw new ElydoraError(
      res.status,
      errorBody.error.code,
      errorBody.error.message,
      errorBody.error.request_id,
      errorBody.error.details,
    );
  }

  throw new ElydoraError(
    res.status,
    'INTERNAL_ERROR',
    `HTTP ${res.status}: ${res.statusText}`,
    'unknown',
  );
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
