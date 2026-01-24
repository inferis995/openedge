import { useQuery } from '@tanstack/react-query';
import { historyApi, HistoryQueryParams, HistoryDataPoint } from '@/api/history';

export const useHistory = (params: HistoryQueryParams, enabled = true) => {
    const query = useQuery({
        queryKey: ['history', params],
        queryFn: () => historyApi.query(params),
        enabled: enabled && !!params.tag_id && !!params.start && !!params.end,
    });

    return {
        data: Array.isArray(query.data) ? query.data : [],
        isLoading: query.isLoading,
        isError: query.isError || (query.data && !Array.isArray(query.data)),
        error: query.error,
        refetch: query.refetch,
    };
};

export type { HistoryQueryParams, HistoryDataPoint };
