export { ElydoraClient } from './client.js';
export { ElydoraError } from './errors.js';

export {
  jcsCanonicalise,
  sha256Base64url,
  computeChainHash,
  computePayloadHash,
  signEd25519,
  derivePublicKey,
  ZERO_CHAIN_HASH,
} from './crypto.js';

export {
  uuidv7,
  generateNonce,
  base64urlEncode,
  base64urlDecode,
} from './utils.js';

export type {
  // Enums
  AgentStatus,
  KeyStatus,
  ExportStatus,
  RbacRole,
  ErrorCode,

  // Entities
  Agent,
  AgentKey,
  Operation,
  Receipt,
  Epoch,
  Organization,
  User,
  Export,

  // Protocol
  EOR,
  EAR,

  // API request/response
  ElydoraClientConfig,
  CreateOperationParams,
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
  GetEpochResponse,
  ListEpochsResponse,
  CreateExportRequest,
  CreateExportResponse,
  GetExportResponse,
  ListExportsResponse,
  GetMeResponse,
  IssueApiTokenResponse,
  JWK,
  JWKSResponse,
  HealthResponse,
  AuthRegisterResponse,
  AuthLoginResponse,
  ErrorResponse,
} from './types.js';
