import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { Branding, BrandingAssetKind, BrandingProfile, BrandingProfileInput } from './types';

export const brandingKeys = {
  active: ['branding', 'active'] as const,
  profiles: ['branding', 'profiles'] as const,
};

// Public endpoint - no auth required, used by ThemeProvider before login too.
export const useBranding = () =>
  useQuery({ queryKey: brandingKeys.active, queryFn: () => api.get<Branding>('/api/v1/branding') });

// The list endpoint is writers-only; pass `enabled: false` for viewers so the
// request (and its guaranteed 403) is never made.
export const useBrandingProfiles = (opts?: { enabled?: boolean }) =>
  useQuery({
    queryKey: brandingKeys.profiles,
    queryFn: () => api.get<{ items: BrandingProfile[] }>('/api/v1/branding/profiles'),
    enabled: opts?.enabled ?? true,
  });

export const useCreateBrandingProfile = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.post<BrandingProfile>('/api/v1/branding/profiles', { name }),
    onSuccess: () => qc.invalidateQueries({ queryKey: brandingKeys.profiles }),
  });
};

export const useUpdateBranding = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: BrandingProfileInput) => api.put<BrandingProfile>(`/api/v1/branding/profiles/${id}`, b),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: brandingKeys.profiles });
      void qc.invalidateQueries({ queryKey: brandingKeys.active });
    },
  });
};

export const useActivateBranding = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.post<BrandingProfile>(`/api/v1/branding/profiles/${id}/activate`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: brandingKeys.profiles });
      void qc.invalidateQueries({ queryKey: brandingKeys.active });
    },
  });
};

export const useDeleteBrandingProfile = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/api/v1/branding/profiles/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: brandingKeys.profiles }),
  });
};

export const useUploadBrandingAsset = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ kind, file }: { kind: BrandingAssetKind; file: File }) => {
      const form = new FormData();
      form.append('file', file);
      return api.post<BrandingProfile>(`/api/v1/branding/profiles/${id}/upload?kind=${kind}`, form);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: brandingKeys.profiles });
      void qc.invalidateQueries({ queryKey: brandingKeys.active });
    },
  });
};
