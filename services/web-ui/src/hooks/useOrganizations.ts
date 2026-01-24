import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { organizationsApi } from '@/api/organizations';
import { CreateOrganizationDto } from '@/types';

export const useOrganizations = () => {
    const queryClient = useQueryClient();

    const query = useQuery({
        queryKey: ['organizations'],
        queryFn: organizationsApi.getAll,
    });

    const createMutation = useMutation({
        mutationFn: (data: CreateOrganizationDto) => organizationsApi.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['organizations'] });
        },
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => organizationsApi.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['organizations'] });
        },
    });

    return {
        organizations: query.data || [],
        isLoading: query.isLoading,
        isError: query.isError,
        create: createMutation.mutateAsync,
        isCreating: createMutation.isPending,
        remove: deleteMutation.mutateAsync,
        isDeleting: deleteMutation.isPending,
    };
};
