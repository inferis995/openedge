import { useMemo, useRef, useCallback } from 'react';
import { useQueries, useQueryClient } from '@tanstack/react-query';
import { historyApi } from '@/api/history';
import { tagsApi } from '@/api/tags';
import {
    TagWithHierarchy,
    TrendDataPoint,
    TimeRange,
    AggregationType,
    getAutoInterval,
    calculateTimeRange,
    TagHierarchyResponse,
    OrganizationHierarchy,
    SiteHierarchy,
} from '@/types/trend';

interface UseTrendDataOptions {
    tagIds: number[];
    timeRange: TimeRange;
    aggregation: AggregationType;
    liveMode: boolean;
    autoRefreshInterval?: number;
    enabled?: boolean;
}

interface UseTrendDataResult {
    // Data
    historyData: Map<number, TrendDataPoint[]>;
    tags: TagWithHierarchy[];
    hierarchy: TagHierarchyResponse | null;

    // Status
    isLoading: boolean;
    isError: boolean;
    error: Error | null;

    // Actions
    refresh: () => void;
}

// Fetch tag hierarchy
export const useTagHierarchy = () => {
    return useQueries({
        queries: [
            {
                queryKey: ['tagHierarchy'],
                queryFn: async (): Promise<TagHierarchyResponse> => {
                    try {
                        return await tagsApi.getHierarchy();
                    } catch {
                        // Fallback: build from flat list
                        const flatTags = await tagsApi.getAllWithHierarchy();
                        return buildHierarchyFromFlat(flatTags);
                    }
                },
                staleTime: 60000, // 1 minute
            },
        ],
    });
};

// Build hierarchy from flat tag list (fallback)
const buildHierarchyFromFlat = (tags: TagWithHierarchy[]): TagHierarchyResponse => {
    const orgMap = new Map<number, { id: number; name: string; sites: Map<number, SiteHierarchy> }>();

    tags.forEach(tag => {
        if (!tag.org_id || !tag.org_name) return;

        if (!orgMap.has(tag.org_id)) {
            orgMap.set(tag.org_id, {
                id: tag.org_id,
                name: tag.org_name,
                sites: new Map(),
            });
        }

        const org = orgMap.get(tag.org_id)!;

        if (tag.site_id && tag.site_name) {
            if (!org.sites.has(tag.site_id)) {
                org.sites.set(tag.site_id, {
                    id: tag.site_id,
                    name: tag.site_name,
                    areas: [],
                });
            }

            const site = org.sites.get(tag.site_id)!;

            if (tag.area_id && tag.area_name) {
                let area = site.areas.find(a => a.id === tag.area_id);
                if (!area) {
                    area = {
                        id: tag.area_id,
                        name: tag.area_name,
                        gateways: [],
                    };
                    site.areas.push(area);
                }

                if (tag.gateway_id && tag.gateway_name) {
                    let gateway = area.gateways.find(g => g.id === tag.gateway_id);
                    if (!gateway) {
                        gateway = {
                            id: tag.gateway_id,
                            name: tag.gateway_name,
                            driver_type: 'Unknown',
                            tags: [],
                        };
                        area.gateways.push(gateway);
                    }
                    gateway.tags.push(tag);
                }
            }
        }
    });

    const organizations: OrganizationHierarchy[] = Array.from(orgMap.values()).map(org => ({
        id: org.id,
        name: org.name,
        sites: Array.from(org.sites.values()),
    }));

    return { organizations };
};

export const useTrendData = (options: UseTrendDataOptions): UseTrendDataResult => {
    const {
        tagIds,
        timeRange,
        aggregation,
        liveMode,
        autoRefreshInterval = 10000,
        enabled = true,
    } = options;

    const queryClient = useQueryClient();
    const refreshTriggerRef = useRef(0);

    // Calculate actual date range
    const { start, end } = useMemo(() => {
        if (timeRange.preset === 'custom' && timeRange.customStart && timeRange.customEnd) {
            // Ensure custom dates are Date objects (they might be strings from JSON/store)
            const startDate = timeRange.customStart instanceof Date
                ? timeRange.customStart
                : new Date(timeRange.customStart);
            const endDate = timeRange.customEnd instanceof Date
                ? timeRange.customEnd
                : new Date(timeRange.customEnd);

            // Validate dates
            if (isNaN(startDate.getTime()) || isNaN(endDate.getTime())) {
                console.warn('Invalid custom dates, falling back to 1h range');
                return calculateTimeRange('1h');
            }

            return { start: startDate, end: endDate };
        }
        return calculateTimeRange(timeRange.preset);
    }, [timeRange]);

    // Calculate auto interval
    const interval = useMemo(() => {
        return getAutoInterval(start, end);
    }, [start, end]);

    // Query historical data for each tag
    const historyQueries = useQueries({
        queries: tagIds.map(tagId => ({
            queryKey: ['history', tagId, start.toISOString(), end.toISOString(), aggregation, interval, refreshTriggerRef.current],
            queryFn: async () => {
                const response = await historyApi.query({
                    tag_id: tagId,
                    start: start.toISOString(),
                    end: end.toISOString(),
                    agg: aggregation,
                    interval,
                });
                return { tagId, data: response };
            },
            enabled: enabled && tagIds.includes(tagId),
            staleTime: 5000,
            refetchInterval: liveMode && timeRange.preset !== 'custom' ? autoRefreshInterval : 0,
        })),
    });

    // Fetch all tags with hierarchy
    const tagsQuery = useQueries({
        queries: [
            {
                queryKey: ['tagsWithHierarchy'],
                queryFn: (): Promise<TagWithHierarchy[]> => tagsApi.getAllWithHierarchy(),
                staleTime: 60000,
            },
        ],
    });

    // Fetch hierarchy
    const hierarchyQuery = useQueries({
        queries: [
            {
                queryKey: ['tagHierarchy'],
                queryFn: async (): Promise<TagHierarchyResponse> => {
                    try {
                        return await tagsApi.getHierarchy();
                    } catch {
                        const flatTags = await tagsApi.getAllWithHierarchy();
                        return buildHierarchyFromFlat(flatTags);
                    }
                },
                staleTime: 60000,
            },
        ],
    });

    // Build history data map
    const historyData = useMemo(() => {
        const map = new Map<number, TrendDataPoint[]>();
        historyQueries.forEach(query => {
            if (query.data) {
                map.set(query.data.tagId, query.data.data);
            }
        });
        return map;
    }, [historyQueries]);

    // Get tags list
    const tags = useMemo(() => {
        return tagsQuery[0].data || [];
    }, [tagsQuery]);

    // Get hierarchy
    const hierarchy = useMemo(() => {
        return hierarchyQuery[0].data || null;
    }, [hierarchyQuery]);

    // Loading state
    const isLoading = historyQueries.some(q => q.isLoading) || tagsQuery[0].isLoading;

    // Error state
    const isError = historyQueries.some(q => q.isError) || tagsQuery[0].isError;
    const error = historyQueries.find(q => q.error)?.error || tagsQuery[0].error || null;

    // Refresh function
    const refresh = useCallback(() => {
        refreshTriggerRef.current++;
        tagIds.forEach(tagId => {
            queryClient.invalidateQueries({
                queryKey: ['history', tagId],
            });
        });
    }, [queryClient, tagIds]);

    return {
        historyData,
        tags,
        hierarchy,
        isLoading,
        isError,
        error: error as Error | null,
        refresh,
    };
};

export default useTrendData;
