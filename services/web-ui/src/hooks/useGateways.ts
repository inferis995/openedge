import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { gatewaysApi } from '@/api/gateways';
import { CreateGatewayDto } from '@/types';

export const useGateways = (areaId?: number | null) => {
    const queryClient = useQueryClient();

    const query = useQuery({
        queryKey: ['gateways', areaId],
        queryFn: () => gatewaysApi.getAll(areaId || undefined),
        enabled: !!areaId,
    });

    const createMutation = useMutation({
        mutationFn: (data: CreateGatewayDto) => gatewaysApi.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['gateways'] });
        },
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => gatewaysApi.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['gateways'] });
        },
    });

    const testConnectionMutation = useMutation({
        mutationFn: (id: number) => gatewaysApi.testConnection(id),
    });

    return {
        gateways: query.data || [],
        isLoading: query.isLoading,
        isError: query.isError,
        create: createMutation.mutateAsync,
        isCreating: createMutation.isPending,
        remove: deleteMutation.mutateAsync,
        isDeleting: deleteMutation.isPending,
        testConnection: testConnectionMutation.mutateAsync,
        isTesting: testConnectionMutation.isPending,
    };
};
