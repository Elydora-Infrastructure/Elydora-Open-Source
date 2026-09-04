/** Role-based access control (RBAC) middleware. */

import type { MiddlewareHandler } from 'hono';
import type { RbacRole } from '../shared/index.js';
import type { Env, AppVariables } from '../types.js';
import { AppError } from './error-handler.js';

/** Privilege level map. */
const ROLE_LEVEL: Record<RbacRole, number> = {
  readonly_investigator: 10,
  integration_engineer: 20,
  compliance_auditor: 30,
  security_admin: 40,
  org_owner: 50,
};

/** Check if a role has at least the given privilege level. */
function hasMinimumRole(userRole: RbacRole, requiredRole: RbacRole): boolean {
  return ROLE_LEVEL[userRole] >= ROLE_LEVEL[requiredRole];
}

/** Create an RBAC middleware that requires a minimum role. */
export function requireRole(minimumRole: RbacRole): MiddlewareHandler<{
  Bindings: Env;
  Variables: AppVariables;
}> {
  return async (c, next) => {
    const userRole = c.get('role');

    if (!userRole) {
      throw new AppError(401, 'UNAUTHORIZED', { key: 'rbac.noRole' });
    }

    if (!hasMinimumRole(userRole, minimumRole)) {
      throw new AppError(
        403,
        'FORBIDDEN',
        { key: 'rbac.insufficientPermissions' },
      );
    }

    await next();
  };
}
