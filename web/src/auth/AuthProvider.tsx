import { createContext, useContext, useEffect } from 'react';
import type { ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';

import { authKeys, useLogin, useLogout, useMe, useTotpVerify } from '@/api/auth';
import { UNAUTHORIZED_EVENT } from '@/lib/api';
import { queryClient } from '@/lib/query';

import type { LoginResult, Me, SecondFactor } from '@/api/types';

interface AuthContextValue {
  user: Me | null;
  loading: boolean;
  /** Resolves to a session (Me) or, when the account has TOTP enrolled, to a challenge. */
  login: (username: string, password: string) => Promise<LoginResult>;
  /** Second step of login: trades the challenge plus a code for a session. */
  verifyTotp: (challenge: string, factor: SecondFactor) => Promise<Me>;
  logout: () => Promise<void>;
  isWriter: boolean;
  isOwner: boolean;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate();
  const meQuery = useMe();
  const loginMutation = useLogin();
  const logoutMutation = useLogout();
  const verifyMutation = useTotpVerify();

  useEffect(() => {
    const onUnauthorized = () => {
      queryClient.setQueryData(authKeys.me, null);
      if (window.location.pathname !== '/login') {
        navigate('/login', { replace: true });
      }
    };
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
  }, [navigate]);

  const user = meQuery.data ?? null;
  const value: AuthContextValue = {
    user,
    loading: meQuery.isLoading,
    login: async (username, password) => loginMutation.mutateAsync({ username, password }),
    verifyTotp: async (challenge, factor) => verifyMutation.mutateAsync({ challenge, ...factor }),
    logout: async () => {
      await logoutMutation.mutateAsync();
      navigate('/login', { replace: true });
    },
    isWriter: user?.role === 'owner' || user?.role === 'admin',
    isOwner: user?.role === 'owner',
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
