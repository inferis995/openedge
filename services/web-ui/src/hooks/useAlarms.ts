import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { alarmsApi } from '@/api/alarms';

export const useAlarms = (statusFilter?: string) => {
    const queryClient = useQueryClient();

    const query = useQuery({
        queryKey: ['alarms', statusFilter],
        queryFn: () => alarmsApi.getAll(statusFilter === 'all' ? undefined : statusFilter),
        refetchInterval: 5000, // Poll every 5s for active alarms
    });

    const acknowledgeMutation = useMutation({
        mutationFn: (id: number) => alarmsApi.acknowledge(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['alarms'] });
        },
    });

    return {
        alarms: query.data || [],
        isLoading: query.isLoading,
        isError: query.isError,
        acknowledge: acknowledgeMutation.mutateAsync,
        isAcknowledging: acknowledgeMutation.isPending,
    };
};
