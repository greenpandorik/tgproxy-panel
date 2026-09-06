import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/lib/api';

import type { Paginated, SiteTemplate, TemplateInput, ValidateResult } from './types';

export const siteKeys = {
  all: ['site-templates'] as const,
  one: (id: string) => ['site-templates', id] as const,
};

export const useSiteTemplates = () =>
  useQuery({ queryKey: siteKeys.all, queryFn: () => api.get<Paginated<SiteTemplate>>('/api/v1/site-templates') });

export const useSiteTemplate = (id: string) =>
  useQuery({ queryKey: siteKeys.one(id), queryFn: () => api.get<SiteTemplate>(`/api/v1/site-templates/${id}`), enabled: !!id });

export const useCreateSiteTemplate = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: TemplateInput) => api.post<SiteTemplate>('/api/v1/site-templates', b),
    onSuccess: () => qc.invalidateQueries({ queryKey: siteKeys.all }),
  });
};

export const useUpdateSiteTemplate = (id: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: TemplateInput) => api.put<SiteTemplate>(`/api/v1/site-templates/${id}`, b),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: siteKeys.all });
      void qc.invalidateQueries({ queryKey: siteKeys.one(id) });
    },
  });
};

export const useDeleteSiteTemplate = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/api/v1/site-templates/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: siteKeys.all }),
  });
};

export const useValidateSiteTemplate = () =>
  useMutation({
    mutationFn: (b: TemplateInput) => api.post<ValidateResult>('/api/v1/site-templates/validate', b),
  });

export function nodeSitePreviewUrl(nodeId: string): string {
  return `/api/v1/nodes/${nodeId}/site/preview`;
}
