import { useQuery } from '@tanstack/react-query';
import { organizationsApi } from '@/api/organizations';

export const orgLogoKey = (orgId: number | null | undefined) => ['org-logo', orgId ?? 0] as const;

/** An organization's logo as a data URL; null when it has none (or no organization). */
export function useOrgLogo(orgId: number | null | undefined) {
    return useQuery({
        queryKey: orgLogoKey(orgId),
        queryFn: () => organizationsApi.getLogo(orgId as number),
        enabled: !!orgId,
        staleTime: 10 * 60 * 1000,
        retry: false,
    });
}
