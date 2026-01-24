import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { tagsApi } from '@/api/tags';
import { CreateTagDto } from '@/types';

export const useTags = (gatewayId?: number | null) => {
    const queryClient = useQueryClient();

    const query = useQuery({
        queryKey: ['tags', gatewayId ?? 'all'],
        queryFn: () => {
            if (gatewayId === null || gatewayId === undefined) {
                return tagsApi.getAllTags();
            }
            return tagsApi.getAll(gatewayId);
        },
        // Always enable - if gatewayId is provided, filter by it; otherwise get all tags
        enabled: true,
    });

    const createMutation = useMutation({
        mutationFn: (data: CreateTagDto) => tagsApi.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
    });

    const updateMutation = useMutation({
        mutationFn: ({ id, data }: { id: number; data: Partial<CreateTagDto> }) =>
            tagsApi.update(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => tagsApi.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
    });

    return {
        tags: query.data || [],
        isLoading: query.isLoading,
        isError: query.isError,
        create: createMutation.mutateAsync,
        isCreating: createMutation.isPending,
        update: updateMutation.mutateAsync,
        isUpdating: updateMutation.isPending,
        remove: deleteMutation.mutateAsync,
        isDeleting: deleteMutation.isPending,
    };
};
