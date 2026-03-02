import type { ErrorCode } from './types.js';

export class ElydoraError extends Error {
  public readonly code: ErrorCode;
  public readonly requestId: string;
  public readonly statusCode: number;
  public readonly details?: Record<string, unknown>;

  constructor(
    statusCode: number,
    code: ErrorCode,
    message: string,
    requestId: string,
    details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = 'ElydoraError';
    this.statusCode = statusCode;
    this.code = code;
    this.requestId = requestId;
    this.details = details;
  }
}
