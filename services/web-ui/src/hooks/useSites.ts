import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { sitesApi } from '@/api/sites';
import { CreateSiteDto } from '@/types';

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
        mutationFn: (data: CreateSiteDto) => sitesApi.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['sites'] });
        },
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => sitesApi.delete(id),
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
    };
};
