import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { alarmsApi } from '@/api/alarms';

export const useAlarms = (
    state?: string,
    start?: string,
    end?: string,
    tagId?: number | null,
    refetchInterval?: number | false
) => {
    const queryClient = useQueryClient();

    const query = useQuery({
        queryKey: ['alarms', state, start, end, tagId],
        queryFn: () => alarmsApi.getAll({
            state: state === 'all' ? undefined : state,
            start,
            end,
            tag_id: tagId,
        }),
        refetchInterval: refetchInterval ?? false,
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
        error: query.error,
        acknowledge: acknowledgeMutation.mutateAsync,
        isAcknowledging: acknowledgeMutation.isPending,
        refetch: query.refetch,
    };
};
