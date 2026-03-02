'use client';

import { createContext, useContext, useState, useEffect, useCallback } from 'react';
import type { ReactNode } from 'react';
import type { RbacRole } from '@elydora/shared';

interface AuthUser {
  sub: string;
  org_id: string;
  role: RbacRole;
  display_name?: string;
  email?: string;
}

interface AuthContextValue {
  user: AuthUser | null;
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  login: (token: string, user?: { display_name?: string; email?: string }) => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function decodeJWTPayload(token: string): AuthUser | null {
  try {
    const parts = token.split('.');
    if (parts.length !== 3) return null;
    // base64url decode
    let base64 = parts[1]!.replace(/-/g, '+').replace(/_/g, '/');
    while (base64.length % 4 !== 0) base64 += '=';
    const json = atob(base64);
    const payload = JSON.parse(json);
    // Check expiration
    const nowSeconds = Math.floor(Date.now() / 1000);
    if (typeof payload.exp === 'number' && payload.exp < nowSeconds) return null;
    if (!payload.sub || !payload.org_id || !payload.role) return null;
    return { sub: payload.sub, org_id: payload.org_id, role: payload.role };
  } catch {
    return null;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const stored = localStorage.getItem('elydora_token');
    if (stored) {
      const decoded = decodeJWTPayload(stored);
      if (decoded) {
        setToken(stored);
        // Try to fetch full user profile from localStorage
        const displayName = localStorage.getItem('elydora_display_name');
        const email = localStorage.getItem('elydora_email');
        if (displayName) decoded.display_name = displayName;
        if (email) decoded.email = email;
        setUser({ ...decoded });
      } else {
        // Token expired or invalid, clear it
        localStorage.removeItem('elydora_token');
        localStorage.removeItem('elydora_display_name');
        localStorage.removeItem('elydora_email');
      }
    }
    setIsLoading(false);
  }, []);

  const login = useCallback((newToken: string, userInfo?: { display_name?: string; email?: string }) => {
    localStorage.setItem('elydora_token', newToken);
    const decoded = decodeJWTPayload(newToken);
    if (decoded) {
      if (userInfo?.display_name) {
        decoded.display_name = userInfo.display_name;
        localStorage.setItem('elydora_display_name', userInfo.display_name);
      }
      if (userInfo?.email) {
        decoded.email = userInfo.email;
        localStorage.setItem('elydora_email', userInfo.email);
      }
      setUser(decoded);
    }
    setToken(newToken);
  }, []);

  const logout = useCallback(() => {
    localStorage.removeItem('elydora_token');
    localStorage.removeItem('elydora_display_name');
    localStorage.removeItem('elydora_email');
    setUser(null);
    setToken(null);
  }, []);

  return (
    <AuthContext.Provider value={{ user, token, isAuthenticated: !!user, isLoading, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
