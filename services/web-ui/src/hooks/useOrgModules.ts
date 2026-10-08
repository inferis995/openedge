import { useQuery } from '@tanstack/react-query';
import { organizationsApi } from '@/api/organizations';

export const orgModulesKey = (orgId: number | null | undefined) => ['org-modules', orgId ?? 0] as const;

/**
 * The set of module keys the organization may use. Null while loading or
 * for global admins (who see everything): callers treat null as "all on".
 */
export function useOrgModules(orgId: number | null | undefined): Set<string> | null {
    const { data } = useQuery({
        queryKey: orgModulesKey(orgId),
        queryFn: () => organizationsApi.getModules(orgId as number),
        enabled: !!orgId,
        staleTime: 10 * 60 * 1000,
        retry: false,
    });
    if (!orgId || !data) return null;
    return new Set(data.filter((m) => m.enabled).map((m) => m.key));
}
