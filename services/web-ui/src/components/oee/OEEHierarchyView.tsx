import { useQuery } from '@tanstack/react-query';
import { Building2, MapPin, Gauge, Loader2 } from 'lucide-react';

import { oeeApi, OEEHierarchyNode } from '@/api/dashboard';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

// OEEHierarchyView — albero Site → Area → Profile con rollup OEE
// per ogni livello. Read-only (la struttura si edita via Aree/Siti).

const oeeBand = (v: number) => {
    if (v >= 85) return { color: 'text-emerald-500', bg: 'bg-emerald-500/10' };
    if (v >= 65) return { color: 'text-amber-500',   bg: 'bg-amber-500/10' };
    if (v >= 40) return { color: 'text-orange-500',  bg: 'bg-orange-500/10' };
    return { color: 'text-red-500', bg: 'bg-red-500/10' };
};

const ICONS = {
    site:    <Building2 size={16} />,
    area:    <MapPin size={14} />,
    profile: <Gauge size={12} />,
};

const HierarchyNode = ({ node, level = 0 }: { node: OEEHierarchyNode; level?: number }) => {
    const band = oeeBand(node.snapshot.oee);
    const sizeClass =
        node.node_type === 'site'    ? 'text-xl font-bold'   :
        node.node_type === 'area'    ? 'text-base font-semibold' :
                                       'text-sm';
    return (
        <div className="space-y-1">
            <div
                className={`flex items-center gap-3 p-2 rounded border ${band.bg}`}
                style={{ marginLeft: level * 20 }}
            >
                <span className={band.color}>{ICONS[node.node_type]}</span>
                <span className={`flex-1 truncate ${sizeClass}`}>{node.name}</span>
                <div className="flex items-center gap-3 font-mono text-xs">
                    <span className={`text-base font-bold ${band.color}`}>
                        {node.snapshot.oee.toFixed(1)}%
                    </span>
                    <div className="hidden sm:flex items-center gap-2 text-muted-foreground">
                        <span>A {node.snapshot.availability.toFixed(0)}</span>
                        <span>P {node.snapshot.performance.toFixed(0)}</span>
                        <span>Q {node.snapshot.quality.toFixed(0)}</span>
                    </div>
                </div>
            </div>
            {node.children && node.children.length > 0 && (
                <div className="space-y-1">
                    {node.children.map((c) => (
                        <HierarchyNode key={`${c.node_type}-${c.id}`} node={c} level={level + 1} />
                    ))}
                </div>
            )}
        </div>
    );
};

export const OEEHierarchyView = () => {
    const { data, isLoading } = useQuery({
        queryKey: ['oee-hierarchy'],
        queryFn: oeeApi.hierarchy,
        refetchInterval: 60_000,
    });

    return (
        <Card>
            <CardHeader className="pb-3">
                <CardTitle className="text-base flex items-center gap-2">
                    <Building2 size={16} /> Gerarchia Site → Area → Profile
                </CardTitle>
            </CardHeader>
            <CardContent>
                {isLoading ? (
                    <div className="py-8 text-center text-sm text-muted-foreground">
                        <Loader2 className="inline animate-spin mr-2" />Caricamento…
                    </div>
                ) : !data || ((data.sites?.length ?? 0) === 0 && !data.unassigned) ? (
                    <div className="py-12 text-center text-sm text-muted-foreground border border-dashed rounded-md">
                        Nessun profilo abilitato. Crea profili e assegnali a un'area
                        per popolare la gerarchia.
                    </div>
                ) : (
                    <div className="space-y-2">
                        {(data.sites ?? []).map((s) => (
                            <HierarchyNode key={s.id} node={s} />
                        ))}
                        {data.unassigned && (
                            <>
                                <div className="text-xs text-muted-foreground mt-4 mb-1">Profili senza area:</div>
                                <HierarchyNode node={data.unassigned} />
                            </>
                        )}
                    </div>
                )}
            </CardContent>
        </Card>
    );
};
