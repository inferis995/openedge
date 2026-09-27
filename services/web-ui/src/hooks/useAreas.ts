import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { areasApi } from '@/api/areas';
import { CreateAreaDto } from '@/types';
import { showApiError } from '@/lib/api-error-handler';
import i18n from '@/i18n';

export const useAreas = (siteId?: number | null) => {
    const queryClient = useQueryClient();

    const query = useQuery({
        queryKey: ['areas', siteId],
        queryFn: () => areasApi.getAll(siteId || undefined),
        enabled: !!siteId,
    });

    const createMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.create_area_failed')),
        mutationFn: (data: CreateAreaDto) => areasApi.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['areas'] });
        },
    });

    const deleteMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.delete_area_failed')),
        mutationFn: (id: number) => areasApi.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['areas'] });
        },
    });

    const updateMutation = useMutation({
        onError: (e) => showApiError(e, i18n.t('feedback.update_area_failed')),
        mutationFn: ({ id, data }: { id: number; data: { name: string } }) => areasApi.update(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['areas'] });
        },
    });

    return {
        areas: query.data || [],
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
