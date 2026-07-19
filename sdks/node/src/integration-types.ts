import { INTEGRATION_TYPES, type IntegrationType } from './types.js';

export function assertIntegrationType(value: unknown): asserts value is IntegrationType {
  const isSupported = typeof value === 'string'
    && INTEGRATION_TYPES.some((integrationType) => integrationType === value);
  if (!isSupported) {
    throw new TypeError(
      `Invalid integration_type "${String(value)}". Expected one of: ${INTEGRATION_TYPES.join(', ')}`,
    );
  }
}
