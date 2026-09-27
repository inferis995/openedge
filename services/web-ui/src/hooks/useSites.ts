import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { sitesApi } from '@/api/sites';
import { CreateSiteDto } from '@/types';
import { showApiError } from '@/lib/api-error-handler';
import i18n from '@/i18n';

export const useSites = (orgId?: number | null) => {
    const queryClient = useQueryClient();

    const query = useQuery({
        queryKey: ['sites', orgId],
        queryFn: () => sitesApi.getAll(orgId || 0), // 0 or undefined, handled by interceptor? No, API needs param.
        // Actually, if orgId is null/undefined, we shouldn't fetch unless we want all sites (admin only).
        // Since we are multi-tenant strict, we should disable query if no orgId.
        enabled: !!orgId,
    });

    const createMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.create_site_failed')),
        mutationFn: (data: CreateSiteDto) => sitesApi.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['sites'] });
        },
    });

    const deleteMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.delete_site_failed')),
        mutationFn: (id: number) => sitesApi.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['sites'] });
        },
    });

    const updateMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.update_site_failed')),
        mutationFn: ({ id, data }: { id: number; data: { name: string } }) => sitesApi.update(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['sites'] });
        },
    });

    return {
        sites: query.data || [],
        isLoading: query.isLoading,
        isError: query.isError,
        create: createMutation.mutateAsync,
        isCreating: createMutation.isPending,
        remove: deleteMutation.mutateAsync,
        isDeleting: deleteMutation.isPending,
        update: updateMutation.mutateAsync,
        isUpdating: updateMutation.isPending,
    };
};

export const useSite = (id?: number | null) => {
    return useQuery({
        queryKey: ['site', id],
        queryFn: () => sitesApi.get(id!),
        enabled: !!id,
    });
};
