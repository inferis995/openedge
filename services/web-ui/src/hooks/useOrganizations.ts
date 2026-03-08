import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { organizationsApi } from '@/api/organizations';
import { CreateOrganizationDto } from '@/types';
import { showApiError, showApiSuccess, formatApiError } from '@/lib/api-error-handler';

export const useOrganizations = () => {
    const queryClient = useQueryClient();

    const query = useQuery({
        queryKey: ['organizations'],
        queryFn: organizationsApi.getAll,
        retry: 1,
        retryDelay: 1000,
    });

    const createMutation = useMutation({
        mutationFn: (data: CreateOrganizationDto) => organizationsApi.create(data),
        onSuccess: (_, variables) => {
            queryClient.invalidateQueries({ queryKey: ['organizations'] });
            showApiSuccess('Organization created', `"${variables.name}" has been created successfully`);
        },
        onError: (error) => {
            showApiError(error, 'Failed to create organization');
        },
    });

    const updateMutation = useMutation({
        mutationFn: ({ id, data }: { id: number; data: CreateOrganizationDto }) =>
            organizationsApi.update(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['organizations'] });
            showApiSuccess('Organization updated', 'The organization has been updated successfully');
        },
        onError: (error) => {
            showApiError(error, 'Failed to update organization');
        },
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => organizationsApi.delete(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['organizations'] });
            showApiSuccess('Organization deleted', 'The organization has been removed');
        },
        onError: (error) => {
            showApiError(error, 'Failed to delete organization');
        },
    });

    // Format error for display
    const error = query.isError ? formatApiError(query.error).message : null;
    const organizations = query.data || [];

    return {
        organizations,
        isLoading: query.isLoading,
        isError: query.isError,
        error,
        create: createMutation.mutateAsync,
        isCreating: createMutation.isPending,
        update: updateMutation.mutateAsync,
        isUpdating: updateMutation.isPending,
        remove: deleteMutation.mutateAsync,
        isDeleting: deleteMutation.isPending,
        refetch: query.refetch,
    };
};
