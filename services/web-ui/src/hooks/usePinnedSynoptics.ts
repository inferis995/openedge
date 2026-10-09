import { useCallback, useState } from 'react';
import { useNavigationStore } from '@/stores/useNavigationStore';

// Synoptics pinned on the dashboard, per org, on this device. A TV on the
// shop floor pins the pages it rotates through; an office PC pins its own.
const keyFor = (orgId: number | null) => `openedge.pinnedSynoptics.${orgId ?? 'global'}`;

const read = (orgId: number | null): number[] => {
    try {
        const v = JSON.parse(localStorage.getItem(keyFor(orgId)) ?? '[]');
        return Array.isArray(v) ? v.filter(n => typeof n === 'number') : [];
    } catch {
        return [];
    }
};

export function usePinnedSynoptics() {
    const { selectedOrgId } = useNavigationStore();
    const [pins, setPins] = useState<number[]>(() => read(selectedOrgId));

    const refresh = useCallback(() => setPins(read(selectedOrgId)), [selectedOrgId]);

    const toggle = useCallback((id: number) => {
        setPins(prev => {
            const next = prev.includes(id) ? prev.filter(x => x !== id) : [...prev, id];
            try {
                localStorage.setItem(keyFor(selectedOrgId), JSON.stringify(next));
            } catch { /* private mode: pins just don't survive reload */ }
            return next;
        });
    }, [selectedOrgId]);

    const isPinned = useCallback((id: number) => pins.includes(id), [pins]);

    return { pins, toggle, isPinned, refresh };
}
