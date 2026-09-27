import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { gatewaysApi } from '@/api/gateways';
import { CreateGatewayDto } from '@/types';
import { showApiError } from '@/lib/api-error-handler';
import i18n from '@/i18n';

export const useGateways = (areaId?: number | null) => {
    const queryClient = useQueryClient();

    const query = useQuery({
        queryKey: ['gateways', areaId],
        queryFn: () => gatewaysApi.getAll(areaId || undefined),
        enabled: !!areaId,
    });

    // Also fetch all gateways when no areaId is provided
    const allGatewaysQuery = useQuery({
        queryKey: ['gateways', 'all'],
        queryFn: () => gatewaysApi.getAll(),
        enabled: !areaId,
    });

    const createMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.create_gateway_failed')),
        mutationFn: (data: CreateGatewayDto) => gatewaysApi.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['gateways'] });
        },
    });

    const deleteMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.delete_gateway_failed')),
        mutationFn: (id: number) => gatewaysApi.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['gateways'] });
        },
    });

    const updateMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.update_gateway_failed')),
        mutationFn: ({ id, data }: { id: number; data: Partial<CreateGatewayDto> }) => gatewaysApi.update(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['gateways'] });
        },
    });

    const testConnectionMutation = useMutation({
        mutationFn: (id: number) => gatewaysApi.testConnection(id),
    });

    return {
        gateways: query.data || allGatewaysQuery.data || [],
        isLoading: areaId ? query.isLoading : allGatewaysQuery.isLoading,
        isError: query.isError || allGatewaysQuery.isError,
        create: createMutation.mutateAsync,
        isCreating: createMutation.isPending,
        remove: deleteMutation.mutateAsync,
        isDeleting: deleteMutation.isPending,
        update: updateMutation.mutateAsync,
        isUpdating: updateMutation.isPending,
        testConnection: testConnectionMutation.mutateAsync,
        isTesting: testConnectionMutation.isPending,
    };
};
