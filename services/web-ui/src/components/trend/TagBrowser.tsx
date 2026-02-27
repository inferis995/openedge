import React, { useState, useMemo, useEffect } from 'react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
    Search,
    Star,
    Clock,
    X,
    Tag as TagIcon,
    Loader2,
    AlertTriangle,
} from 'lucide-react';
import { OrganizationNode } from './TagTreeNode';
import { useTrendStore } from '@/stores/useTrendStore';
import { tagsApi } from '@/api/tags';
import { TagWithHierarchy, TagHierarchyResponse, TAG_COLORS, OrganizationHierarchy, SiteHierarchy } from '@/types/trend';
import { toast } from 'sonner';
import { useStaleData, getDataStatus, getStatusClasses } from '@/hooks/useStaleData';

interface TagBrowserProps {
    onAddTagToChart: (tagId: number) => void;
    selectedTagIds: number[];
    realtimeValues?: Map<number, { value: number; timestamp: number; quality: number }>;
}

export const TagBrowser: React.FC<TagBrowserProps> = ({
    onAddTagToChart,
    selectedTagIds,
    realtimeValues,
}) => {
    const [searchQuery, setSearchQuery] = useState('');
    const [hierarchy, setHierarchy] = useState<TagHierarchyResponse | null>(null);
    const [isLoading, setIsLoading] = useState(true);

    const { favoriteTagIds, toggleFavoriteTag, activeChartId, addTagToChart } = useTrendStore();

    // Use the stale data hook for device status
    const { isDeviceOnline } = useStaleData();

    // Fetch tag hierarchy
    useEffect(() => {
        const fetchHierarchy = async () => {
            setIsLoading(true);
            try {
                // Try the hierarchy endpoint first, fall back to flat list
                let data: TagHierarchyResponse;
                try {
                    data = await tagsApi.getHierarchy();
                } catch {
                    // Fallback: build hierarchy from flat list
                    const flatTags = await tagsApi.getAllWithHierarchy();
                    data = buildHierarchyFromFlat(flatTags);
                }
                setHierarchy(data);
            } catch (error) {
                console.error('Failed to load tag hierarchy:', error);
                toast.error('Failed to load tags');
            } finally {
                setIsLoading(false);
            }
        };

        fetchHierarchy();
    }, []);

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

        // Convert maps to arrays
        const organizations: OrganizationHierarchy[] = Array.from(orgMap.values()).map(org => ({
            id: org.id,
            name: org.name,
            sites: Array.from(org.sites.values()),
        }));

        return { organizations };
    };

    // Get all tags from hierarchy for favorites
    const allTags = useMemo(() => {
        if (!hierarchy) return [];
        const tags: TagWithHierarchy[] = [];
        hierarchy.organizations.forEach(org => {
            org.sites.forEach(site => {
                site.areas.forEach(area => {
                    area.gateways.forEach(gateway => {
                        tags.push(...gateway.tags);
                    });
                });
            });
        });
        return tags;
    }, [hierarchy]);

    const favoriteTags = useMemo(() => {
        return allTags.filter(tag => favoriteTagIds.includes(tag.id));
    }, [allTags, favoriteTagIds]);

    const handleTagSelect = (tag: TagWithHierarchy) => {
        if (activeChartId) {
            addTagToChart(activeChartId, tag.id);
            onAddTagToChart(tag.id);
        } else {
            toast.info('Please select or create a chart first');
        }
    };

    // Current values panel for selected tags
    const selectedTagsInfo = useMemo(() => {
        return selectedTagIds.map((id, index) => {
            const tag = allTags.find(t => t.id === id);
            const rv = realtimeValues?.get(id);
            return { id, tag, rv, color: TAG_COLORS[index % TAG_COLORS.length] };
        });
    }, [selectedTagIds, allTags, realtimeValues]);

    return (
        <div className="h-full flex flex-col bg-white border-r">
            {/* Current Values Panel */}
            <div className="p-3 border-b">
                <h2 className="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-2 flex items-center gap-2">
                    <Clock className="w-3.5 h-3.5" />
                    Current Values
                </h2>
                {selectedTagsInfo.length === 0 ? (
                    <p className="text-xs text-gray-400 italic py-2">No tags selected</p>
                ) : (
                    <div className="space-y-1.5 max-h-48 overflow-y-auto">
                        {selectedTagsInfo.map(({ id, tag, rv, color }) => {
                            if (!tag) return null;

                            // Use tag alias as device ID for Sparkplug B status check
                            const deviceId = tag.alias || tag.code;
                            const deviceOnline = isDeviceOnline(deviceId);

                            // Determine data status - use device status if available
                            const status = getDataStatus(rv?.quality, deviceOnline);
                            const statusClasses = getStatusClasses(status);
                            const isBool = tag.data_type === 'BOOL';

                            const displayValue = rv !== undefined
                                ? (isBool
                                    ? (rv.value >= 0.5 ? 'TRUE' : 'FALSE')
                                    : (typeof rv.value === 'number'
                                        ? (Number.isInteger(rv.value) ? rv.value : rv.value.toFixed(2))
                                        : '-'))
                                : '-';

                            return (
                                <div
                                    key={id}
                                    className={`flex items-center justify-between p-2 rounded border ${statusClasses.bg} ${statusClasses.border}`}
                                >
                                    <div className="flex items-center gap-2 min-w-0">
                                        <div
                                            className="w-2.5 h-2.5 rounded-full flex-shrink-0"
                                            style={{ backgroundColor: color }}
                                        />
                                        <span className="text-xs font-medium text-gray-700 truncate">{tag.alias || tag.code}</span>
                                        {deviceOnline === false && (
                                            <span title="Device is offline">
                                                <AlertTriangle className="w-3 h-3 text-red-500 flex-shrink-0" />
                                            </span>
                                        )}
                                    </div>
                                    <div className="text-right flex-shrink-0">
                                        {isBool && rv !== undefined ? (
                                            <span className={`text-xs font-bold font-mono px-2 py-0.5 rounded ${
                                                rv.value >= 0.5
                                                    ? 'bg-green-100 text-green-700'
                                                    : 'bg-red-100 text-red-700'
                                            }`}>
                                                {displayValue}
                                            </span>
                                        ) : (
                                            <div className={`text-sm font-bold font-mono ${statusClasses.text}`}>
                                                {displayValue}
                                            </div>
                                        )}
                                        <div className={`text-[10px] mt-0.5 ${
                                            deviceOnline === false ? 'text-red-500 font-medium' : 'text-gray-400'
                                        }`}>
                                            {rv ? (
                                                new Date(rv.timestamp).toLocaleTimeString('it-IT')
                                            ) : '-'}
                                        </div>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                )}
            </div>

            {/* Tag Browser */}
            <div className="flex-1 flex flex-col overflow-hidden min-h-0">
                <div className="p-3 border-b">
                    <h2 className="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-2">
                        Tag Browser
                    </h2>
                    <div className="relative">
                        <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" />
                        <Input
                            placeholder="Search tags..."
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            className="h-8 pl-7 text-xs"
                        />
                    </div>
                </div>

                <Tabs defaultValue="hierarchy" className="flex-1 flex flex-col min-h-0">
                    <TabsList className="mx-3 mt-2 grid w-[calc(100%-24px)] grid-cols-2 h-8 flex-shrink-0">
                        <TabsTrigger value="hierarchy" className="text-xs">Hierarchy</TabsTrigger>
                        <TabsTrigger value="favorites" className="text-xs">
                            Favorites
                            {favoriteTagIds.length > 0 && (
                                <Badge variant="secondary" className="ml-1 h-4 px-1 text-[10px]">
                                    {favoriteTagIds.length}
                                </Badge>
                            )}
                        </TabsTrigger>
                    </TabsList>

                    <TabsContent value="hierarchy" className="flex-1 overflow-hidden m-0 min-h-0">
                        <ScrollArea className="h-full">
                            {isLoading ? (
                                <div className="flex items-center justify-center py-8">
                                    <Loader2 className="w-5 h-5 animate-spin text-gray-400" />
                                </div>
                            ) : hierarchy ? (
                                <div className="p-2">
                                    {hierarchy.organizations.map((org) => (
                                        <OrganizationNode
                                            key={org.id}
                                            org={org}
                                            onSelectTag={handleTagSelect}
                                            selectedTagIds={selectedTagIds}
                                            favoriteTagIds={favoriteTagIds}
                                            onToggleFavorite={toggleFavoriteTag}
                                            searchQuery={searchQuery}
                                        />
                                    ))}
                                </div>
                            ) : (
                                <div className="text-center py-8 text-gray-400 text-xs">
                                    No tags available
                                </div>
                            )}
                        </ScrollArea>
                    </TabsContent>

                    <TabsContent value="favorites" className="flex-1 overflow-hidden m-0 min-h-0">
                        <ScrollArea className="h-full">
                            {favoriteTags.length === 0 ? (
                                <div className="text-center py-8 text-gray-400 text-xs">
                                    <Star className="w-6 h-6 mx-auto mb-2 text-gray-200" />
                                    <p>No favorite tags yet</p>
                                    <p className="text-[10px] mt-1">Star tags to add them here</p>
                                </div>
                            ) : (
                                <div className="p-2 space-y-0.5">
                                    {favoriteTags.map((tag) => (
                                        <div
                                            key={tag.id}
                                            className={`flex items-center justify-between px-2 py-1.5 rounded cursor-pointer hover:bg-blue-50 ${selectedTagIds.includes(tag.id) ? 'bg-blue-100' : ''
                                                }`}
                                            onClick={() => handleTagSelect(tag)}
                                        >
                                            <div className="flex items-center gap-2 min-w-0">
                                                <TagIcon className="w-3 h-3 text-gray-400" />
                                                <span className="text-xs text-gray-700 truncate">{tag.alias || tag.code}</span>
                                                <Badge variant="secondary" className="text-[10px] h-4">
                                                    {tag.data_type}
                                                </Badge>
                                            </div>
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="h-5 w-5"
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    toggleFavoriteTag(tag.id);
                                                }}
                                            >
                                                <X className="w-3 h-3" />
                                            </Button>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </ScrollArea>
                    </TabsContent>
                </Tabs>
            </div>
        </div>
    );
};

export default TagBrowser;
