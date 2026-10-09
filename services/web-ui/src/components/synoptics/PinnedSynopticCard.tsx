import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { synopticsApi, type SynopticWidget } from '@/api/synoptics';
import { SynopticWidgetView, type LiveValue } from './SynopticWidget';
import { useRealtime } from '@/hooks/useRealtime';
import { useNavigationStore } from '@/stores/useNavigationStore';

// Every tag id a canvas reads: main tags, secondary bindings, html props.
export function synopticTagIds(layout: SynopticWidget[] | undefined): Set<number> {
    const ids = new Set<number>();
    for (const w of layout ?? []) {
        if (w.tagId != null) ids.add(w.tagId);
        const cfg = w.config;
        if (!cfg) continue;
        for (const key of ['tagSecondary', 'tagFlow', 'tagPosition', 'tagBinding'] as const) {
            const v = cfg[key];
            if (typeof v === 'number') ids.add(v);
        }
        if (w.type === 'html' && Array.isArray(cfg.bindings)) {
            for (const b of cfg.bindings as Array<{ tagId?: unknown }>) {
                if (typeof b?.tagId === 'number') ids.add(b.tagId);
            }
        }
    }
    return ids;
}

// One pinned synoptic, live, scaled to fit. Click opens it big.
export default function PinnedSynopticCard({
    synopticId,
    height = 220,
    onOpen,
    showTitle = true,
}: {
    synopticId: number;
    height?: number;
    onOpen?: () => void;
    showTitle?: boolean;
}) {
    const { selectedOrgId } = useNavigationStore();
    const { data: synoptic } = useQuery({
        queryKey: ['synoptic', synopticId],
        queryFn: () => synopticsApi.get(synopticId),
        staleTime: 60_000,
    });

    const tagIds = useMemo(() => synopticTagIds(synoptic?.layout), [synoptic]);
    const { values: liveValues } = useRealtime(
        selectedOrgId ?? undefined,
        tagIds.size > 0 ? tagIds : undefined,
    );

    const liveMap = useMemo(() => {
        const m = new Map<number, LiveValue>();
        for (const [id, v] of liveValues) {
            m.set(id, { value: v.value, quality: v.quality, timestamp: v.timestamp });
        }
        // Bound but silent: BAD-quality null, so widgets show "—", never a
        // stale number that looks live.
        for (const id of tagIds) {
            if (!m.has(id)) m.set(id, { value: null, quality: 2 });
        }
        return m;
    }, [liveValues, tagIds]);

    if (!synoptic) return null;
    const scale = Math.min(height / synoptic.canvas_h, 3);
    const w = synoptic.canvas_w * scale;

    const liveOf = (tagId?: number | null): LiveValue | undefined =>
        tagId == null ? undefined : liveMap.get(tagId);

    return (
        <div
            className={onOpen ? 'cursor-pointer group' : undefined}
            onClick={onOpen}
            style={{ width: w }}
        >
            {showTitle && (
                <div className="flex items-center justify-between mb-1 gap-2">
                    <span className="text-xs font-semibold truncate">{synoptic.name}</span>
                </div>
            )}
            <div
                className="relative overflow-hidden rounded-md border group-hover:border-primary/60 transition-colors"
                style={{ width: w, height, background: synoptic.background_color }}
            >
                <div style={{
                    width: synoptic.canvas_w, height: synoptic.canvas_h,
                    transform: `scale(${scale})`, transformOrigin: 'top left',
                    pointerEvents: 'none',
                }}>
                    {(synoptic.layout || []).map(wdg => (
                        <div key={wdg.id} className="absolute" style={{
                            left: wdg.x, top: wdg.y, width: wdg.w, height: wdg.h,
                            transform: wdg.rotation ? `rotate(${wdg.rotation}deg)` : undefined,
                        }}>
                            <SynopticWidgetView widget={wdg} live={liveOf(wdg.tagId)} liveMap={liveMap} />
                        </div>
                    ))}
                </div>
            </div>
        </div>
    );
}
