/**
 * Better Auth configuration for ElydoraOpenSource.
 *
 * Creates a Better Auth instance backed by PostgreSQL. The instance is
 * configured to:
 *
 *   - Use the existing `users` table via field-mapping (no schema change)
 *   - Support legacy PBKDF2-SHA256 password hashes alongside Better Auth's
 *     default scrypt so existing users can sign in without re-hashing
 *   - Expose bearer-token auth for API clients
 *   - Provide organization support with Elydora's 5-role RBAC hierarchy
 *   - Provide admin user-management endpoints
 */

import { betterAuth } from 'better-auth';
import { verifyPassword as verifyScryptPassword } from 'better-auth/crypto';
import { bearer } from 'better-auth/plugins/bearer';
import { organization } from 'better-auth/plugins/organization';
import { admin } from 'better-auth/plugins/admin';
import { createAccessControl } from 'better-auth/plugins/access';

// ---------------------------------------------------------------------------
// Legacy PBKDF2 password verification (matches auth-service.ts format)
// Format: pbkdf2:iterations:salt_hex:hash_hex
// ---------------------------------------------------------------------------

async function verifyLegacyPbkdf2(password: string, stored: string): Promise<boolean> {
  const parts = stored.split(':');
  if (parts.length !== 4 || parts[0] !== 'pbkdf2') return false;

  const iterations = parseInt(parts[1]!, 10);
  const saltHex = parts[2]!;
  const storedHashHex = parts[3]!;

  const salt = new Uint8Array(saltHex.match(/.{2}/g)!.map((b) => parseInt(b, 16)));
  const encoder = new TextEncoder();
  const keyMaterial = await crypto.subtle.importKey(
    'raw',
    encoder.encode(password),
    'PBKDF2',
    false,
    ['deriveBits'],
  );
  const bits = await crypto.subtle.deriveBits(
    { name: 'PBKDF2', salt, iterations, hash: 'SHA-256' },
    keyMaterial,
    256,
  );
  const hashHex = Array.from(new Uint8Array(bits))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');

  // Constant-time comparison
  if (hashHex.length !== storedHashHex.length) return false;
  let result = 0;
  for (let i = 0; i < hashHex.length; i++) {
    result |= hashHex.charCodeAt(i) ^ storedHashHex.charCodeAt(i);
  }
  return result === 0;
}

// ---------------------------------------------------------------------------
// RBAC access control — maps Elydora's 5-role hierarchy to Better Auth
// ---------------------------------------------------------------------------

const elydoraAc = createAccessControl({
  organization: ['create', 'read', 'update', 'delete'],
  member: ['create', 'read', 'update', 'delete'],
  invitation: ['create', 'cancel'],
  agent: ['create', 'read', 'update', 'freeze', 'unfreeze', 'revoke', 'delete'],
  operation: ['create', 'read'],
  audit: ['read', 'export'],
  webhook: ['create', 'read', 'update', 'delete'],
} as const);

const orgOwnerRole = elydoraAc.newRole({
  organization: ['create', 'read', 'update', 'delete'],
  member: ['create', 'read', 'update', 'delete'],
  invitation: ['create', 'cancel'],
  agent: ['create', 'read', 'update', 'freeze', 'unfreeze', 'revoke', 'delete'],
  operation: ['create', 'read'],
  audit: ['read', 'export'],
  webhook: ['create', 'read', 'update', 'delete'],
});

const securityAdminRole = elydoraAc.newRole({
  organization: ['read', 'update'],
  member: ['create', 'read', 'update', 'delete'],
  invitation: ['create', 'cancel'],
  agent: ['create', 'read', 'update', 'freeze', 'unfreeze', 'revoke', 'delete'],
  operation: ['create', 'read'],
  audit: ['read', 'export'],
  webhook: ['create', 'read', 'update', 'delete'],
});

const complianceAuditorRole = elydoraAc.newRole({
  organization: ['read'],
  member: ['read'],
  agent: ['read'],
  operation: ['read'],
  audit: ['read', 'export'],
  webhook: ['read'],
});

const integrationEngineerRole = elydoraAc.newRole({
  organization: ['read'],
  member: ['read'],
  agent: ['create', 'read', 'update'],
  operation: ['create', 'read'],
  audit: ['read'],
  webhook: ['create', 'read', 'update', 'delete'],
});

const readonlyInvestigatorRole = elydoraAc.newRole({
  organization: ['read'],
  member: ['read'],
  agent: ['read'],
  operation: ['read'],
  audit: ['read'],
  webhook: ['read'],
});

// ---------------------------------------------------------------------------
// Singleton — creates a Better Auth instance for the server process
// ---------------------------------------------------------------------------

export function createAuth(databaseUrl: string, secret: string, baseUrl: string, allowedOrigins: string) {
  return betterAuth({
    database: {
      type: 'postgres',
      url: databaseUrl,
    },
    secret,
    baseURL: baseUrl,
    trustedOrigins: allowedOrigins
      ? allowedOrigins.split(',').map((s: string) => s.trim()).filter(Boolean)
      : [],

    emailAndPassword: {
      enabled: true,
      minPasswordLength: 8,
      maxPasswordLength: 256,
      password: {
        verify: async ({ hash, password }) => {
          // Handle legacy PBKDF2 hashes from the existing auth system
          if (hash.startsWith('pbkdf2:')) {
            return verifyLegacyPbkdf2(password, hash);
          }
          // For BA-native scrypt hashes (new registrations, password resets)
          return verifyScryptPassword({ hash, password });
        },
      },
    },

    user: {
      modelName: 'users',
      fields: {
        name: 'display_name',
        email: 'email',
        emailVerified: 'email_verified',
        image: 'image',
        createdAt: 'created_at',
        updatedAt: 'updated_at',
      },
      additionalFields: {
        org_id: {
          type: 'string',
          required: false,
        },
        role: {
          type: 'string',
          required: true,
          defaultValue: 'org_owner',
        },
        status: {
          type: 'string',
          required: true,
          defaultValue: 'active',
        },
        password_hash: {
          type: 'string',
          required: false,
        },
        onboarding_completed: {
          type: 'number',
          required: true,
          defaultValue: 0,
        },
      },
    },

    databaseHooks: {
      user: {
        create: {
          before: async (user, _context) => {
            const now = new Date();
            return {
              data: {
                ...user,
                createdAt: now,
                updatedAt: now,
                role: 'org_owner',
              },
            };
          },
          after: async (_user) => {
            // No-op: new users start with org_id = NULL and
            // onboarding_completed = 0 (DB default). The onboarding
            // flow handles org creation.
          },
        },
      },
    },

    account: {
      modelName: 'ba_accounts',
      fields: {
        createdAt: 'created_at',
        updatedAt: 'updated_at',
        userId: 'user_id',
        providerId: 'provider_id',
        accountId: 'account_id',
        accessToken: 'access_token',
        refreshToken: 'refresh_token',
        idToken: 'id_token',
        accessTokenExpiresAt: 'access_token_expires_at',
        refreshTokenExpiresAt: 'refresh_token_expires_at',
      },
    },

    verification: {
      modelName: 'ba_verifications',
      fields: {
        createdAt: 'created_at',
        updatedAt: 'updated_at',
        expiresAt: 'expires_at',
      },
    },

    session: {
      modelName: 'ba_sessions',
      expiresIn: 604800, // 7 days in seconds
      updateAge: 86400,  // refresh session if older than 1 day
      fields: {
        createdAt: 'created_at',
        updatedAt: 'updated_at',
        userId: 'user_id',
        expiresAt: 'expires_at',
        ipAddress: 'ip_address',
        userAgent: 'user_agent',
      },
    },

    advanced: {
      cookiePrefix: 'elydora',
      defaultCookieAttributes: {
        httpOnly: true,
        secure: true,
        sameSite: 'lax' as const,
        path: '/',
      },
    },

    plugins: [
      bearer(),
      organization({
        ac: elydoraAc,
        creatorRole: 'org_owner',
        roles: {
          org_owner: orgOwnerRole,
          security_admin: securityAdminRole,
          compliance_auditor: complianceAuditorRole,
          integration_engineer: integrationEngineerRole,
          readonly_investigator: readonlyInvestigatorRole,
        },
        schema: {
          organization: {
            modelName: 'ba_organizations',
            fields: {
              createdAt: 'created_at',
              updatedAt: 'updated_at',
            },
          },
          member: {
            modelName: 'ba_members',
            fields: {
              createdAt: 'created_at',
              organizationId: 'organization_id',
              userId: 'user_id',
            },
          },
          invitation: {
            modelName: 'ba_invitations',
            fields: {
              createdAt: 'created_at',
              organizationId: 'organization_id',
              inviterId: 'inviter_id',
              expiresAt: 'expires_at',
            },
          },
        },
      }),
      admin(),
    ],
  });
}

export type ElydoraAuth = ReturnType<typeof createAuth>;
