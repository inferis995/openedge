/**
 * Where to show a gateway that has no position yet.
 *
 * A gateway created without coordinates used to be missing from the map until
 * someone placed it. Instead it is shown where it most likely is — the
 * centre of its area when the area has a boundary, otherwise the middle of
 * the gateways of the same area already placed — marked as provisional until
 * confirmed or moved. Several at the same point are spread on a small spiral
 * so each can be picked. A gateway with neither stays off the map.
 */
export interface GatewayLike {
    id: number;
    area_id: number;
    latitude?: number | null;
    longitude?: number | null;
}

export interface Position {
    latitude: number;
    longitude: number;
}

const placed = (g: GatewayLike): g is GatewayLike & { latitude: number; longitude: number } =>
    typeof g.latitude === 'number' && typeof g.longitude === 'number';

/** Spacing of the spiral, in degrees (about 250 m at these latitudes). */
const STEP = 0.0025;
const GOLDEN = Math.PI * (3 - Math.sqrt(5));

export function predictPositions(
    gateways: GatewayLike[],
    areaCenters: Map<number, [number, number]>,
): Map<number, Position> {
    const byArea = new Map<number, GatewayLike[]>();
    for (const g of gateways) {
        const list = byArea.get(g.area_id) ?? [];
        list.push(g);
        byArea.set(g.area_id, list);
    }
    const out = new Map<number, Position>();
    for (const [areaId, list] of byArea) {
        const missing = list.filter((g) => !placed(g)).sort((a, b) => a.id - b.id);
        if (missing.length === 0) continue;
        let base: [number, number] | undefined = areaCenters.get(areaId);
        if (!base) {
            const known = list.filter(placed);
            if (known.length === 0) continue;
            base = [
                known.reduce((s, g) => s + g.longitude, 0) / known.length,
                known.reduce((s, g) => s + g.latitude, 0) / known.length,
            ];
        }
        const [lon0, lat0] = base;
        missing.forEach((g, k) => {
            // The first sits on the point when nothing else of the area is
            // there yet; the rest (and every one, when the area already has
            // placed gateways around its middle) step out on the spiral.
            const n = list.some(placed) ? k + 1 : k;
            const r = n === 0 ? 0 : STEP * Math.sqrt(n);
            out.set(g.id, {
                longitude: lon0 + r * Math.cos(n * GOLDEN),
                latitude: lat0 + r * Math.sin(n * GOLDEN),
            });
        });
    }
    return out;
}
