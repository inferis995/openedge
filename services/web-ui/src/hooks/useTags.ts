import { useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { tagsApi } from '@/api/tags';
import { CreateTagDto } from '@/types';
import { showApiError } from '@/lib/api-error-handler';
import i18n from '@/i18n';

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
        onError: (e) => showApiError(e, i18n.t('feedback.create_tag_failed')),
        mutationFn: (data: CreateTagDto) => tagsApi.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
    });

    const updateMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.update_tag_failed')),
        mutationFn: ({ id, data }: { id: number; data: Partial<CreateTagDto> }) =>
            tagsApi.update(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
    });

    const deleteMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.delete_tag_failed')),
        mutationFn: (id: number) => tagsApi.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['tags'] });
        },
    });

    const tags = useMemo(() => Array.isArray(query.data) ? query.data : [], [query.data]);

    return {
        tags,
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
