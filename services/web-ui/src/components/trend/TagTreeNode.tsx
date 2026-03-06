import React, { useState } from 'react';
import { ChevronRight, ChevronDown, Tag as TagIcon, Building2, MapPin, Server, Star } from 'lucide-react';
import { TagWithHierarchy, GatewayHierarchy, AreaHierarchy, SiteHierarchy, OrganizationHierarchy } from '@/types/trend';
import { cn } from '@/lib/utils';

interface TagNodeProps {
    tag: TagWithHierarchy;
    onSelect: (tag: TagWithHierarchy) => void;
    isSelected?: boolean;
    isFavorite?: boolean;
    onToggleFavorite?: () => void;
}

const TagNode: React.FC<TagNodeProps> = ({ tag, onSelect, isSelected, isFavorite, onToggleFavorite }) => {
    return (
        <div
            className={cn(
                'flex items-center justify-between px-2 py-1 rounded cursor-pointer group',
                'hover:bg-muted/50',
                isSelected && 'bg-primary/20 text-primary'
            )}
            onClick={() => onSelect(tag)}
        >
            <div className="flex items-center gap-1.5 flex-1 min-w-0 pr-2">
                <TagIcon className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                <span className={cn("text-xs truncate", isSelected ? 'text-primary font-medium' : 'text-foreground')}>{tag.alias || tag.code}</span>
                <span className="text-[10px] text-muted-foreground flex-shrink-0">{tag.data_type}</span>
            </div>
            <button
                onClick={(e) => {
                    e.stopPropagation();
                    onToggleFavorite?.();
                }}
                className={cn(
                    'flex-shrink-0 ml-1 transition-opacity',
                    isFavorite ? 'opacity-100' : 'opacity-20 group-hover:opacity-100'
                )}
                title={isFavorite ? 'Rimuovi dai preferiti' : 'Aggiungi ai preferiti'}
            >
                <Star
                    className={cn(
                        'w-3.5 h-3.5',
                        isFavorite ? 'fill-amber-400 text-amber-400' : 'text-gray-500 hover:text-amber-400'
                    )}
                />
            </button>
        </div>
    );
};

interface GatewayNodeProps {
    gateway: GatewayHierarchy;
    onSelectTag: (tag: TagWithHierarchy) => void;
    selectedTagIds: number[];
    favoriteTagIds: number[];
    onToggleFavorite: (tagId: number) => void;
    searchQuery: string;
}

const GatewayNode: React.FC<GatewayNodeProps> = ({
    gateway,
    onSelectTag,
    selectedTagIds,
    favoriteTagIds,
    onToggleFavorite,
    searchQuery,
}) => {
    const [isOpen, setIsOpen] = useState(true); // Start expanded

    const filteredTags = gateway.tags.filter(tag => {
        if (!searchQuery) return true;
        const query = searchQuery.toLowerCase();
        return (
            tag.alias?.toLowerCase().includes(query) ||
            tag.code.toLowerCase().includes(query)
        );
    });

    if (searchQuery && filteredTags.length === 0) return null;

    return (
        <div>
            <div
                className="flex items-center gap-1.5 px-2 py-1 cursor-pointer hover:bg-muted/50 rounded"
                onClick={() => setIsOpen(!isOpen)}
            >
                {isOpen ? (
                    <ChevronDown className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                ) : (
                    <ChevronRight className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                )}
                <Server className="w-3.5 h-3.5 text-purple-500 flex-shrink-0" />
                <span className="text-xs font-medium text-foreground truncate flex-1 min-w-0">{gateway.name}</span>
                <span className="text-[10px] text-muted-foreground ml-auto flex-shrink-0">{gateway.tags.length}</span>
            </div>
            {isOpen && (
                <div className="ml-4 border-l border-border pl-1">
                    {filteredTags.map((tag) => (
                        <TagNode
                            key={tag.id}
                            tag={tag}
                            onSelect={onSelectTag}
                            isSelected={selectedTagIds.includes(tag.id)}
                            isFavorite={favoriteTagIds.includes(tag.id)}
                            onToggleFavorite={() => onToggleFavorite(tag.id)}
                        />
                    ))}
                </div>
            )}
        </div>
    );
};

interface AreaNodeProps {
    area: AreaHierarchy;
    onSelectTag: (tag: TagWithHierarchy) => void;
    selectedTagIds: number[];
    favoriteTagIds: number[];
    onToggleFavorite: (tagId: number) => void;
    searchQuery: string;
}

const AreaNode: React.FC<AreaNodeProps> = ({
    area,
    onSelectTag,
    selectedTagIds,
    favoriteTagIds,
    onToggleFavorite,
    searchQuery,
}) => {
    const [isOpen, setIsOpen] = useState(true); // Start expanded

    const hasMatchingTags = area.gateways.some(g =>
        g.tags.some(t => {
            if (!searchQuery) return true;
            const query = searchQuery.toLowerCase();
            return t.alias?.toLowerCase().includes(query) || t.code.toLowerCase().includes(query);
        })
    );

    if (searchQuery && !hasMatchingTags) return null;

    return (
        <div>
            <div
                className="flex items-center gap-1.5 px-2 py-1 cursor-pointer hover:bg-muted/50 rounded"
                onClick={() => setIsOpen(!isOpen)}
            >
                {isOpen ? (
                    <ChevronDown className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                ) : (
                    <ChevronRight className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                )}
                <MapPin className="w-3.5 h-3.5 text-green-500 flex-shrink-0" />
                <span className="text-xs font-medium text-foreground truncate flex-1 min-w-0">{area.name}</span>
                <span className="text-[10px] text-muted-foreground ml-auto flex-shrink-0">
                    {area.gateways.reduce((sum, g) => sum + g.tags.length, 0)}
                </span>
            </div>
            {isOpen && (
                <div className="ml-4 border-l border-border">
                    {area.gateways.map((gateway) => (
                        <GatewayNode
                            key={gateway.id}
                            gateway={gateway}
                            onSelectTag={onSelectTag}
                            selectedTagIds={selectedTagIds}
                            favoriteTagIds={favoriteTagIds}
                            onToggleFavorite={onToggleFavorite}
                            searchQuery={searchQuery}
                        />
                    ))}
                </div>
            )}
        </div>
    );
};

interface SiteNodeProps {
    site: SiteHierarchy;
    onSelectTag: (tag: TagWithHierarchy) => void;
    selectedTagIds: number[];
    favoriteTagIds: number[];
    onToggleFavorite: (tagId: number) => void;
    searchQuery: string;
}

const SiteNode: React.FC<SiteNodeProps> = ({
    site,
    onSelectTag,
    selectedTagIds,
    favoriteTagIds,
    onToggleFavorite,
    searchQuery,
}) => {
    const [isOpen, setIsOpen] = useState(true); // Start expanded

    const hasMatchingTags = site.areas.some(a =>
        a.gateways.some(g =>
            g.tags.some(t => {
                if (!searchQuery) return true;
                const query = searchQuery.toLowerCase();
                return t.alias?.toLowerCase().includes(query) || t.code.toLowerCase().includes(query);
            })
        )
    );

    if (searchQuery && !hasMatchingTags) return null;

    return (
        <div>
            <div
                className="flex items-center gap-1.5 px-2 py-1 cursor-pointer hover:bg-muted/50 rounded"
                onClick={() => setIsOpen(!isOpen)}
            >
                {isOpen ? (
                    <ChevronDown className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                ) : (
                    <ChevronRight className="w-3 h-3 text-muted-foreground flex-shrink-0" />
                )}
                <Building2 className="w-3.5 h-3.5 text-blue-500 flex-shrink-0" />
                <span className="text-xs font-medium text-foreground truncate flex-1 min-w-0">{site.name}</span>
                <span className="text-[10px] text-muted-foreground ml-auto flex-shrink-0">
                    {site.areas.reduce((sum, a) => sum + a.gateways.reduce((s, g) => s + g.tags.length, 0), 0)}
                </span>
            </div>
            {isOpen && (
                <div className="ml-4 border-l border-border">
                    {site.areas.map((area) => (
                        <AreaNode
                            key={area.id}
                            area={area}
                            onSelectTag={onSelectTag}
                            selectedTagIds={selectedTagIds}
                            favoriteTagIds={favoriteTagIds}
                            onToggleFavorite={onToggleFavorite}
                            searchQuery={searchQuery}
                        />
                    ))}
                </div>
            )}
        </div>
    );
};

interface OrganizationNodeProps {
    org: OrganizationHierarchy;
    onSelectTag: (tag: TagWithHierarchy) => void;
    selectedTagIds: number[];
    favoriteTagIds: number[];
    onToggleFavorite: (tagId: number) => void;
    searchQuery: string;
}

export const OrganizationNode: React.FC<OrganizationNodeProps> = ({
    org,
    onSelectTag,
    selectedTagIds,
    favoriteTagIds,
    onToggleFavorite,
    searchQuery,
}) => {
    const [isOpen, setIsOpen] = useState(true);

    const hasMatchingTags = org.sites.some(s =>
        s.areas.some(a =>
            a.gateways.some(g =>
                g.tags.some(t => {
                    if (!searchQuery) return true;
                    const query = searchQuery.toLowerCase();
                    return t.alias?.toLowerCase().includes(query) || t.code.toLowerCase().includes(query);
                })
            )
        )
    );

    if (searchQuery && !hasMatchingTags) return null;

    return (
        <div>
            <div
                className="flex items-center gap-1.5 px-2 py-1.5 cursor-pointer hover:bg-muted/50 rounded"
                onClick={() => setIsOpen(!isOpen)}
            >
                {isOpen ? (
                    <ChevronDown className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                ) : (
                    <ChevronRight className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                )}
                <Building2 className="w-4 h-4 text-indigo-500 flex-shrink-0" />
                <span className="text-sm font-semibold text-foreground truncate flex-1 min-w-0">{org.name}</span>
                <span className="text-[10px] text-muted-foreground ml-auto flex-shrink-0">
                    {org.sites.reduce((sum, s) => sum + s.areas.reduce((ss, a) => ss + a.gateways.reduce((sss, g) => sss + g.tags.length, 0), 0), 0)}
                </span>
            </div>
            {isOpen && (
                <div className="ml-3 border-l-2 border-border">
                    {org.sites.map((site) => (
                        <SiteNode
                            key={site.id}
                            site={site}
                            onSelectTag={onSelectTag}
                            selectedTagIds={selectedTagIds}
                            favoriteTagIds={favoriteTagIds}
                            onToggleFavorite={onToggleFavorite}
                            searchQuery={searchQuery}
                        />
                    ))}
                </div>
            )}
        </div>
    );
};

export default OrganizationNode;
