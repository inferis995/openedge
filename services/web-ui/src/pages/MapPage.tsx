import 'maplibre-gl/dist/maplibre-gl.css';
import { startTransition, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { Map as MapGL, Marker, Popup, NavigationControl, Source, Layer, type MapRef } from 'react-map-gl/maplibre';
import type { MapGeoJSONFeature } from 'maplibre-gl';
import type { LineLayerSpecification, FillLayerSpecification, StyleSpecification } from 'maplibre-gl';
import { useNavigationStore } from '@/stores/useNavigationStore';
import { formatLiveValue, agoParts, shortTagNames } from '@/lib/format';
import { predictPositions } from '@/lib/predictPosition';
import { useThemeStore } from '@/stores/useThemeStore';
import { useAuthStore } from '@/stores/useAuthStore';
import { useGateways } from '@/hooks/useGateways';
import { useAreas } from '@/hooks/useAreas';
import { useSites } from '@/hooks/useSites';
import { useRealtime } from '@/hooks/useRealtime';
import { tagsApi } from '@/api/tags';
import type { TagValue } from '@/api/tags';
import type { Tag, Gateway, Area } from '@/types';
import { gatewaysApi } from '@/api/gateways';
import { areasApi } from '@/api/areas';
import { parseCoords, formatCoord, searchPlace, type PlaceResult } from '@/lib/coords';
import { Battery, History, Loader2, MapPin, Search } from 'lucide-react';
import TagHistoryChart from '@/components/TagHistoryChart';

// Basemap Esri in versione raster: le tile PNG si vedono ovunque
// (mobile incluso). Chiara col tema chiaro, scura col tema scuro.
function basemapStyle(dark: boolean): StyleSpecification {
    const base = dark ? 'World_Dark_Gray_Base' : 'World_Light_Gray_Base';
    return {
        version: 8,
        sources: {
            cartobase: {
                type: 'raster',
                tiles: [
                    `https://server.arcgisonline.com/ArcGIS/rest/services/Canvas/${base}/MapServer/tile/{z}/{y}/{x}`,
                ],
                tileSize: 256,
                maxzoom: 16,
                attribution: '© Esri, HERE, Garmin, © OpenStreetMap contributors',
            },
        },
        layers: [{ id: 'basemap', type: 'raster', source: 'cartobase' }],
    };
}
// Centro iniziale: Calabria.
const CALABRIA_CENTER: [number, number] = [16.6, 39.3];
// Oltre questo zoom il gateway apre la vista dettaglio con le pill dei tag.
const DETAIL_ZOOM = 13;
// Pill visibili prima del «+N altri».
const MAX_PILLS = 7;

// Ombra di un tag: ultimo valore noto, dal batch /tags/shadows.
interface TagShadow {
    tag_id: number;
    value: TagValue;
    quality: number;
    ts: number;
    source: string;
}

// Stato vivo di un tag nella vista dettaglio: valore corrente (ombre + realtime)
// e ultimi valori visti, per la micro-freccia di tendenza.
interface TagLive {
    value: TagValue;
    quality: number;
    ts: number;
    history: TagValue[];
}

// Colore del pin per stato di connessione.
function statusColor(status?: string): string {
    if (status === 'online') return '#10B981'; // verde
    if (status === 'offline' || status === 'alarm') return '#EF4444'; // rosso
    return '#9CA3AF'; // grigio: mai visto / sconosciuto
}

function extractBattery(cfg: Record<string, unknown> | undefined): number | null {
    if (!cfg) return null;
    const v = cfg.battery ?? cfg.battery_level;
    if (typeof v === 'number' && Number.isFinite(v)) return v;
    return null;
}

// Nome corto della pill: l'ultimo segmento dell'alias (plant/linea/T1 → T1),
// mai oltre 8 caratteri e sempre in maiuscoletto: nei badge conta la leggibilità.

// Micro-freccia di tendenza dagli ultimi valori numerici visti.
function trendArrow(history: TagValue[]): string {
    const nums = history.filter((v): v is number => typeof v === 'number' && Number.isFinite(v));
    if (nums.length < 2) return '•';
    const a = nums[nums.length - 2];
    const b = nums[nums.length - 1];
    if (b > a) return '▲';
    if (b < a) return '▼';
    return '•';
}

// Estrae le coordinate di un poligono GeoJSON per calcolare un centro
// approssimativo (media dei vertici del primo anello). Basta per flyTo.
function firstRingCoords(boundary: Record<string, unknown> | null | undefined): [number, number][] {
    if (!boundary) return [];
    const geom = (boundary.type === 'Feature' ? boundary.geometry : boundary) as
        | { type?: string; coordinates?: unknown }
        | undefined;
    if (!geom?.coordinates) return [];
    let ring: unknown = geom.coordinates;
    // Polygon → array di anelli; MultiPolygon → array di array di anelli.
    if (geom.type === 'MultiPolygon' && Array.isArray(ring)) ring = ring[0];
    if (!Array.isArray(ring)) return [];
    const first = (ring as unknown[])[0];
    if (!Array.isArray(first)) return [];
    return first.filter(
        (c): c is [number, number] => Array.isArray(c) && typeof c[0] === 'number' && typeof c[1] === 'number',
    );
}

// Normalizza il boundary salvato (geometry O Feature) in una geometry per la Source.
function toGeometry(boundary: Record<string, unknown> | null | undefined): Record<string, unknown> | null {
    if (!boundary) return null;
    if (boundary.type === 'Feature' && boundary.geometry) return boundary.geometry as Record<string, unknown>;
    if (typeof boundary.type === 'string' && boundary.type !== 'FeatureCollection') return boundary;
    return null;
}

const fillLayer: Omit<FillLayerSpecification, 'source'> = {
    id: 'area-fill',
    type: 'fill',
    paint: {
        'fill-color': ['get', 'color'] as never,
        'fill-opacity': 0.15,
    },
};

const lineLayer: Omit<LineLayerSpecification, 'source'> = {
    id: 'area-outline',
    type: 'line',
    paint: {
        'line-color': ['get', 'color'] as never,
        'line-width': 2,
        'line-opacity': 1,
    },
};

// palette per-area: tonalità distinte su sfondo scuro
const AREA_COLORS = ['#38BDF8', '#A78BFA', '#F472B6', '#FBBF24', '#34D399', '#F87171', '#60A5FA', '#C084FC'];

interface MapPageProps {
    // Solo per i test: inietta i dati senza rete.
    gatewaysOverride?: ReturnType<typeof useGateways>['gateways'];
    areasOverride?: ReturnType<typeof useAreas>['areas'];
}

const MapPage = ({ gatewaysOverride, areasOverride }: MapPageProps) => {
    const { t } = useTranslation();
    const navigate = useNavigate();
    const { isAdmin, isGlobalAdmin, isOrgAdmin } = useAuthStore();
    // Vedo-modifica: global admin o admin della propria org. L'utente normale
    // vede la mappa in sola lettura, senza pannelli di editing.
    const canEdit = (isGlobalAdmin() || isOrgAdmin()) && isAdmin();

    // Dati in funzione dell'org selezionata nell'header dal global admin
    // (o della propria org per gli org-user): cambiando org, mappa, aree,
    // siti e pin seguono. Il filtro sito restringe ulteriormente.
    const { selectedOrgId } = useNavigationStore();
    const [isWide] = useState(() => typeof window === 'undefined' || window.matchMedia('(min-width: 640px)').matches);
    const sitesHook = useSites(selectedOrgId);
    const sites = sitesHook.sites ?? [];
    // Aree di tutti i siti dell'org (l'hook è per-sito): le uniamo qui,
    // cambiando org o sito la lista segue.
    const areaSiteIds = sites.map((st) => st.id);
    const [areas, setAreas] = useState<Area[]>([]);
    useEffect(() => {
        let cancelled = false;
        if (areasOverride) { (async () => { if (!cancelled) setAreas(areasOverride); })(); return; }
        (async () => {
            try {
                const lists = await Promise.all(areaSiteIds.map((sid) => areasApi.getAll(sid)));
                if (!cancelled) setAreas(lists.flat());
            } catch { if (!cancelled) setAreas([]); }
        })();
        return () => { cancelled = true; };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [JSON.stringify(areaSiteIds), areasOverride]);

    // Gateway: recuperati per area via query diretta quando servono (l'hook
    // useGateways richiede un area_id); il canale principale resta uno.
    const [gateways, setGateways] = useState<Gateway[]>([]);
    const [isLoadingGateways, setIsLoadingGateways] = useState(true);
    useEffect(() => {
        let cancelled = false;
        if (gatewaysOverride) { (async () => { if (!cancelled) setGateways(gatewaysOverride); })(); return; }
        (async () => {
            try {
                const all = await gatewaysApi.getAll();
                if (cancelled) return;
                // scoping: solo i gateway le cui aree appartengono ai siti dell'org
                const areaIds = new Set(areas.map((a) => a.id));
                setGateways(all.filter((g) => areaIds.has(g.area_id)));
            } finally { if (!cancelled) setIsLoadingGateways(false); }
        })();
        return () => { cancelled = true; };
    }, [areas, gatewaysOverride]);

    // Ombre iniziali di tutti i tag (valore + quality + timestamp). Le pill poi
    // vivono di realtime: stesso canale MQTT/WS delle altre pagine, niente reload.
    const [shadows, setShadows] = useState<TagShadow[]>([]);
    useEffect(() => {
        if (!selectedOrgId) return;
        let cancelled = false;
        tagsApi.getShadowBatch().then((s) => {
            if (!cancelled) setShadows(s);
        }).catch(() => { /* nessun valore: le pill mostrano '—' */ });
        return () => { cancelled = true; };
    }, [selectedOrgId]);

    // Catalogo tag dell'organizzazione: per associare gateway → tag (nome, unità).
    const [allTags, setAllTags] = useState<Tag[]>([]);
    useEffect(() => {
        let cancelled = false;
        tagsApi.getAllTags().then((tags) => {
            if (!cancelled) setAllTags(tags);
        }).catch(() => { /* nessun tag: nessuna pill */ });
        return () => { cancelled = true; };
    }, [selectedOrgId]);

    // tag_id → Tag (alias, unità, gateway di appartenenza).
    const tagsById = useMemo(() => {
        const m = new Map<number, Tag>();
        for (const tg of allTags) m.set(tg.id, tg);
        return m;
    }, [allTags]);

    // Tutti i tag_id conosciuti: il filtro del canale realtime è opzionale,
    // senza filtro arrivano tutti e la mappa non perde un aggiornamento.
    const allTagIds = useMemo(() => new Set(allTags.map((tg) => tg.id)), [allTags]);

    // Valori realtime: mappa tag_id → { value, timestamp, quality }, aggiornata
    // dal websocket senza reload pagina.
    const { values: rtValues } = useRealtime(selectedOrgId ?? undefined, allTagIds.size > 0 ? allTagIds : undefined);

    // Vista live per tag: ombra iniziale, poi il realtime la sovrascrive.
    // history tiene gli ultimi 5 valori per la freccia di tendenza.
    const tagLive = useRef<Map<number, TagLive>>(new Map());
    const [liveVersion, bumpVersion] = useState(0);
    useEffect(() => {
        let changed = false;
        // Ombre dal REST: base iniziale.
        for (const s of shadows) {
            const cur = tagLive.current.get(s.tag_id);
            if (!cur) {
                tagLive.current.set(s.tag_id, { value: s.value, quality: s.quality, ts: s.ts, history: [s.value] });
                changed = true;
            }
        }
        // Realtime: sempre più fresco delle ombre.
        for (const [tagId, v] of rtValues) {
            const cur = tagLive.current.get(tagId) ?? { value: null, quality: 0, ts: 0, history: [] };
            const history = [...cur.history, v.value].slice(-5);
            tagLive.current.set(tagId, { value: v.value, quality: v.quality, ts: v.timestamp, history });
            changed = true;
        }
        if (changed) startTransition(() => bumpVersion((n) => n + 1));
    }, [shadows, rtValues]);

    const mapRef = useRef<MapRef>(null);
    const [zoom, setZoom] = useState(DETAIL_ZOOM - 1);
    // Filtro impianto: area selezionata (null = tutte). I gateway di fuori
    // spariscono dalla mappa e dai conteggi — la vista resta quella dell'org.
    const [areaFilter, setAreaFilter] = useState<number | null>(null);
    // La mappa parte già dove ci sono i gateway dell'utente, non su un centro
    // geografico fisso: il centro del bounding box (lo zoom lo fa fitBounds
    // al load, con i dati definitivi).
    const theme = useThemeStore((s) => s.theme);
    const mapStyle = useMemo(() => basemapStyle(theme === 'dark'), [theme]);

    const initialView = useMemo(
        () => ({ longitude: CALABRIA_CENTER[0], latitude: CALABRIA_CENTER[1], zoom: 9 }),
        [],
    );
    const [selectedGwId, setSelectedGwId] = useState<number | null>(null);
    const selectedGwBase = gateways.find((g) => g.id === selectedGwId) ?? null;
    // Popup di dettaglio di un tag (dal click su una pill).
    const [openTagId, setOpenTagId] = useState<number | null>(null);
    // Popup gateway completo (dal «+N altri»).
    const [listGwId, setListGwId] = useState<number | null>(null);
    // tagLive è un ref (mutato dal realtime): il re-render lo legge solo qui,
    // dentro un tick successivo al bump di versione.
    // Snapshot del ref mutabile: copia in state quando la versione cambia,
    // così il render non legge mai .current.
    const [liveSnapshot, setLiveSnapshot] = useState<Map<number, TagLive>>(() => new Map());
    useEffect(() => {
        setLiveSnapshot(new Map(tagLive.current));
    }, [liveVersion]);
    const detailMode = zoom >= DETAIL_ZOOM;
    const openTag = openTagId !== null ? tagsById.get(openTagId) ?? null : null;
    const [openTagLive, setOpenTagLive] = useState<TagLive | null>(null);
    useEffect(() => {
        setOpenTagLive(openTagId !== null ? tagLive.current.get(openTagId) ?? null : null);
    }, [openTagId, liveVersion]);
    const listGw = gateways.find((g) => g.id === listGwId) ?? null;

    const placedGateways = useMemo(
        () => gateways.filter((g) => typeof g.latitude === 'number' && typeof g.longitude === 'number'),
        [gateways],
    );

    const geojsonAreas = useMemo(
        () => ({
            type: 'FeatureCollection' as const,
            features: areas.flatMap((area, i) => {
                const geom = toGeometry(area.boundary);
                if (!geom) return [];
                const ring = firstRingCoords(area.boundary);
                const center = ring.length
                    ? ([ring.reduce((s, c) => s + c[0], 0) / ring.length, ring.reduce((s, c) => s + c[1], 0) / ring.length] as [number, number])
                    : undefined;
                return [{
                    type: 'Feature' as const,
                    geometry: geom as unknown as GeoJSON.Geometry,
                    properties: {
                        id: area.id,
                        name: area.name,
                        color: AREA_COLORS[i % AREA_COLORS.length],
                        center_lon: center?.[0] ?? null,
                        center_lat: center?.[1] ?? null,
                    },
                }];
            }),
        }),
        [areas],
    );

    // Gateway senza coordinate: mostrati dove probabilmente sono (centro
    // dell'area, o tra i gateway già posizionati della stessa area), con un
    // pin «da confermare». Prima restavano fuori dalla mappa finché qualcuno
    // non li posizionava a mano.
    const predicted = useMemo(() => {
        const centers = new Map<number, [number, number]>();
        for (const f of geojsonAreas.features) {
            const { id, center_lon, center_lat } = f.properties;
            if (typeof center_lon === 'number' && typeof center_lat === 'number') centers.set(id as number, [center_lon, center_lat]);
        }
        return predictPositions(gateways, centers);
    }, [gateways, geojsonAreas]);
    const shownGateways = useMemo(
        () => [
            ...placedGateways.map((g) => ({ ...g, predicted: false })),
            ...gateways.flatMap((g) => { const p = predicted.get(g.id); return p ? [{ ...g, ...p, predicted: true }] : []; }),
        ],
        [placedGateways, gateways, predicted],
    );
    // Gateway dopo il filtro area: la mappa mostra solo questi.
    const visibleGateways = useMemo(
        () => (areaFilter === null ? shownGateways : shownGateways.filter((g) => g.area_id === areaFilter)),
        [shownGateways, areaFilter],
    );
    // The selected gateway where it is drawn: a provisional one has no coordinates of its own.
    const selectedGw = selectedGwBase ? (shownGateways.find((g) => g.id === selectedGwBase.id) ?? null) : null;

    // Tag di un gateway con vista live, in ordine di id.
    const liveTagsFor = (gatewayId: number, snap: Map<number, TagLive>): { tag: Tag; live: TagLive }[] => {
        const out: { tag: Tag; live: TagLive }[] = [];
        for (const tg of allTags) {
            if (tg.gateway_id !== gatewayId) continue;
            const live = snap.get(tg.id);
            if (live) out.push({ tag: tg, live });
        }
        return out;
    };

    // Editing posizione gateway (solo admin): selezione + modalità posiziona.
    const [placingForId, setPlacingForId] = useState<number | null>(null);
    const [editGwId, setEditGwId] = useState<number | null>(null);
    const editGw = gateways.find((g) => g.id === editGwId) ?? null;

    // Ricerca luogo (Nominatim) con debounce 600ms.
    const [placeQuery, setPlaceQuery] = useState('');
    const [places, setPlaces] = useState<PlaceResult[]>([]);
    const [searchingPlaces, setSearchingPlaces] = useState(false);
    useEffect(() => {
        const q = placeQuery.trim();
        if (q.length < 3) { startTransition(() => setPlaces([])); return; }
        const controller = new AbortController();
        const timer = setTimeout(() => {
            setSearchingPlaces(true);
            searchPlace(q, controller.signal)
                .then(setPlaces)
                .catch(() => setPlaces([]))
                .finally(() => setSearchingPlaces(false));
        }, 600);
        return () => { controller.abort(); clearTimeout(timer); };
    }, [placeQuery]);

    // Appena arrivano i gateway con coordinate, inquadra i loro pin: la mappa
    // si apre già "sulle cose dell'utente" senza dover cercare la Calabria.
    const fittedRef = useRef(false);
    useEffect(() => {
        if (fittedRef.current || placedGateways.length === 0) return;
        const map = mapRef.current;
        if (!map) return;
        fittedRef.current = true;
        const lons = placedGateways.map((g) => g.longitude as number);
        const lats = placedGateways.map((g) => g.latitude as number);
        map.fitBounds(
            [[Math.min(...lons), Math.min(...lats)], [Math.max(...lons), Math.max(...lats)]],
            { padding: 60, maxZoom: 12, duration: 0 },
        );
    }, [placedGateways]);

    const flyTo = (lon: number, lat: number, zoom?: number) => {
        const map = mapRef.current;
        if (!map) return;
        map.flyTo({
            center: [lon, lat],
            zoom: zoom ?? Math.max(map.getZoom(), 12),
            duration: 800,
        });
    };

    const handleSelectGateway = (id: number, lon: number, lat: number) => {
        setOpenTagId(null); // un popup alla volta: il pin apre il gateway, la pill il tag
        setSelectedGwId(id);
        if (placingForId !== null) {
            setPlacingForId(id);
            return;
        }
        // Zoom oltre la soglia dettaglio: il flyTo apre anche le pill.
        const curZoom = mapRef.current?.getZoom() ?? 9;
        flyTo(lon, lat, Math.max(curZoom, DETAIL_ZOOM + 0.5));
    };

    const handleSelectArea = (feature: MapGeoJSONFeature) => {
        const lon = feature.properties?.center_lon;
        const lat = feature.properties?.center_lat;
        if (typeof lon === 'number' && typeof lat === 'number') {
            flyTo(lon, lat, Math.max(mapRef.current?.getZoom() ?? 9, 11));
        }
    };

    // Click sulla mappa in modalità posiziona: scrive le coordinate del
    // gateway selezionato e apre il pannello coi valori pronti da salvare.
    const handleMapClick = (e: { lngLat: { lng: number; lat: number } }) => {
        if (!canEdit || placingForId === null) return;
        const id = placingForId;
        setPlacingForId(null);
        setEditGwId(id);
        setEditLat(formatCoord(e.lngLat.lat));
        setEditLon(formatCoord(e.lngLat.lng));
    };

    // Campi del pannello di editing: partono da ciò che è salvato.
    const [editLat, setEditLat] = useState('');
    const [editLon, setEditLon] = useState('');
    const [savingPos, setSavingPos] = useState(false);
    useEffect(() => {
        if (editGw) {
            setEditLat(formatCoord(editGw.latitude));
            setEditLon(formatCoord(editGw.longitude));
        }
    }, [editGwId]); // eslint-disable-line react-hooks/exhaustive-deps

    const saveGatewayPos = async (id: number, lat: number, lon: number) => {
        setSavingPos(true);
        try {
            await gatewaysApi.update(id, { latitude: lat, longitude: lon });
            // The pin moves (and stops being provisional) without a reload.
            setGateways((prev) => prev.map((g) => (g.id === id ? { ...g, latitude: lat, longitude: lon } : g)));
        } finally {
            setSavingPos(false);
        }
    };

    const handleSavePosFields = async () => {
        if (!editGw) return;
        const parsed = parseCoords(`${editLat}, ${editLon}`);
        if (!parsed) return;
        await saveGatewayPos(editGw.id, parsed.latitude, parsed.longitude);
    };

    // Drag di un marker (solo admin): aggiorna i campi del pannello e salva.
    const handleMarkerDragEnd = (gw: { id: number }, e: { lngLat: { lng: number; lat: number } }) => {
        if (!canEdit) return;
        setEditGwId(gw.id);
        setEditLat(formatCoord(e.lngLat.lat));
        setEditLon(formatCoord(e.lngLat.lng));
        // The id travels as an argument: editGwId has not updated yet here,
        // so reading it inside the save silently skipped every drag.
        void saveGatewayPos(gw.id, e.lngLat.lat, e.lngLat.lng);
    };

    if (isLoadingGateways) {
        return (
            <div className="p-8 flex items-center justify-center text-muted-foreground">
                <Loader2 className="h-6 w-6 animate-spin mr-2" /> {t('map.loading')}
            </div>
        );
    }


    return (
        <div className="space-y-4">
            <div className="flex flex-col gap-3">
                <div>
                    <h2 className="text-2xl font-semibold tracking-tight">{t('nav.map')}</h2>
                    <p className="text-sm text-muted-foreground">{t('map.subtitle')}</p>
                </div>

                {/* One toolbar: which area, which gateway, which place. On a
                    phone the two selects share a row and the search takes the next. */}
                <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">

                {/* Filtro area/impianto: restringe pin, poligoni e conteggio */}
                <select
                    className="h-10 rounded-md border bg-background px-3 text-sm sm:w-48"
                    value={areaFilter ?? ''}
                    onChange={(e) => {
                        const v = e.target.value ? parseInt(e.target.value) : null;
                        setAreaFilter(v);
                        // Selezionando un'area, la mappa ci vola sopra.
                        const pts = v === null ? placedGateways : placedGateways.filter((g) => g.area_id === v);
                        if (pts.length > 0 && mapRef.current) {
                            const lons = pts.map((g) => g.longitude as number);
                            const lats = pts.map((g) => g.latitude as number);
                            mapRef.current.fitBounds(
                                [[Math.min(...lons), Math.min(...lats)], [Math.max(...lons), Math.max(...lats)]],
                                { padding: 80, maxZoom: 13, duration: 900 },
                            );
                        }
                    }}
                >
                    <option value="">{t('map.all_areas')}</option>
                    {areas.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
                </select>

                {/* Vai al gateway: ricerca e volo sul pin, come nel Trend */}
                <select
                    className="h-10 min-w-0 rounded-md border bg-background px-3 text-sm sm:w-64"
                    value={selectedGwId ?? ''}
                    aria-label={t('map.pick_gateway')}
                    onChange={(e) => {
                        const id = e.target.value ? parseInt(e.target.value) : null;
                        if (id === null) { setSelectedGwId(null); return; }
                        const gw = gateways.find((g) => g.id === id);
                        if (gw && typeof gw.latitude === 'number' && typeof gw.longitude === 'number') {
                            handleSelectGateway(id, gw.longitude, gw.latitude);
                        } else {
                            setSelectedGwId(id);
                        }
                    }}
                >
                    <option value="">{t('map.pick_gateway')}</option>
                    {visibleGateways.map((gw) => (
                        <option key={gw.id} value={gw.id}>
                            {gw.name}{gw.connection_status === 'online' ? '' : ` — ${t('map.legend_offline').toLowerCase()}`}
                        </option>
                    ))}
                </select>

                {/* Ricerca luogo (Nominatim) */}
                <div className="relative col-span-2 w-full sm:ml-auto sm:w-72">
                    <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
                    <input
                        className="w-full h-10 pl-9 pr-8 text-sm rounded-md border bg-background"
                        placeholder={t('map.search_placeholder')}
                        value={placeQuery}
                        onChange={(e) => setPlaceQuery(e.target.value)}
                    />
                    {searchingPlaces && (
                        <Loader2 size={14} className="absolute right-2.5 top-1/2 -translate-y-1/2 animate-spin text-muted-foreground" />
                    )}
                    {places.length > 0 && (
                        <ul className="absolute z-20 mt-1 w-full rounded-md border bg-popover text-popover-foreground shadow-md max-h-60 overflow-y-auto">
                            {places.map((p) => (
                                <li key={`${p.latitude},${p.longitude}`}>
                                    <button
                                        type="button"
                                        className="w-full text-left px-3 py-2 text-xs hover:bg-muted truncate"
                                        title={p.displayName}
                                        onClick={() => {
                                            setPlaces([]);
                                            setPlaceQuery('');
                                            flyTo(p.longitude, p.latitude, 14);
                                        }}
                                    >
                                        {p.displayName}
                                    </button>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
                </div>
            </div>

            <div className="rounded-xl border bg-card overflow-hidden relative">
                <MapGL
                    ref={mapRef}
                    initialViewState={initialView}
                    mapStyle={mapStyle}
                    style={{ width: '100%', height: 'calc(100vh - 260px)', minHeight: 420 }}
                    attributionControl={{ compact: true }}
                    onClick={handleMapClick}
                    onZoom={(e) => setZoom(e.viewState.zoom)}
                    cursor={placingForId !== null ? 'crosshair' : undefined}
                    onLoad={(e) => {
                        // Al primo load inquadra esattamente i pin dell'utente.
                        if (placedGateways.length > 0) {
                            const lons = placedGateways.map((g) => g.longitude as number);
                            const lats = placedGateways.map((g) => g.latitude as number);
                            e.target.fitBounds(
                                [[Math.min(...lons), Math.min(...lats)], [Math.max(...lons), Math.max(...lats)]],
                                { padding: 60, maxZoom: 12, duration: 0 },
                            );
                        }
                    }}
                >
                    {/* One control: a wrapping div does not hide a map control, so
                        the two (desktop and phone) were both drawn. */}
                    <NavigationControl position={isWide ? 'top-right' : 'bottom-right'} visualizePitch />

                    <Source id="areas" type="geojson" data={geojsonAreas}>
                        <Layer {...fillLayer} />
                        <Layer {...lineLayer} />
                    </Source>

                    {geojsonAreas.features.map((f) => (
                        <Marker
                            key={`area-label-${f.properties.id}`}
                            longitude={f.properties.center_lon as number}
                            latitude={f.properties.center_lat as number}
                            anchor="center"
                        >
                            <button
                                type="button"
                                className="px-2 py-0.5 rounded bg-background/80 border text-xs font-semibold cursor-pointer hover:bg-background"
                                style={{ color: f.properties.color as string, borderColor: `${f.properties.color}66` }}
                                onClick={(e) => {
                                    e.stopPropagation();
                                    handleSelectArea({ properties: f.properties } as unknown as MapGeoJSONFeature);
                                }}
                            >
                                {f.properties.name}
                            </button>
                        </Marker>
                    ))}

                    {visibleGateways.map((gw) => {
                        const gwDetail = detailMode || selectedGwId === gw.id;
                        const gwTags = gwDetail ? liveTagsFor(gw.id, liveSnapshot) : [];
                        const visibleTags = gwTags.slice(0, MAX_PILLS);
                        const pillNames = shortTagNames(gwTags.map(({ tag }) => ({ id: tag.id, name: tag.alias || tag.code || `#${tag.id}` })));
                        const extra = gwTags.length - visibleTags.length;
                        return (
                            <Marker
                                key={`gw-${gw.id}-${placingForId === gw.id ? 'live' : 'static'}`}
                                longitude={gw.longitude as number}
                                latitude={gw.latitude as number}
                                anchor="bottom"
                                draggable={canEdit}
                                onDragEnd={(e) => handleMarkerDragEnd(gw, e)}
                                onClick={(e) => {
                                    e.originalEvent.stopPropagation();
                                    handleSelectGateway(gw.id, gw.longitude as number, gw.latitude as number);
                                }}
                            >
                                <div className="gw-anchor">
                                    {/* Pill tag in colonna sopra il pin, stile rack */}
                                    <div className={`gw-pills ${gwDetail ? 'gw-pills-open' : ''}`}>
                                        {gwDetail && visibleTags.map(({ tag, live }) => (
                                            <button
                                                key={tag.id}
                                                type="button"
                                                className="gw-pill"
                                                title={`${tag.alias || tag.code} = ${formatLiveValue(live.value, tag.eu_decimals)}`}
                                                onClick={(ev) => {
                                                    ev.stopPropagation();
                                                    setSelectedGwId(null);
                                                    setOpenTagId(tag.id);
                                                }}
                                            >
                                                <span
                                                    className="gw-qdot"
                                                    style={{ background: live.quality === 0 ? '#10B981' : '#EF4444' }}
                                                />
                                                <span className="gw-pill-name">{pillNames.get(tag.id)}</span>
                                                <span className="gw-pill-value">
                                                    {formatLiveValue(live.value, tag.eu_decimals)}
                                                    {tag.eu_unit ? <span className="gw-pill-unit"> {tag.eu_unit}</span> : null}
                                                </span>
                                                <span className="gw-pill-arrow">{trendArrow(live.history)}</span>
                                            </button>
                                        ))}
                                        {gwDetail && extra > 0 && (
                                            <button
                                                type="button"
                                                className="gw-pill gw-pill-more"
                                                onClick={(ev) => {
                                                    ev.stopPropagation();
                                                    setListGwId(gw.id);
                                                }}
                                            >
                                                +{extra} {t('map.others')}
                                            </button>
                                        )}
                                    </div>
                                    <button
                                        type="button"
                                        aria-label={gw.name}
                                        className={`gw-pin ${gw.predicted ? 'gw-pin-predicted' : ''} ${gw.connection_status === 'offline' ? 'gw-pin-off' : ''} ${placingForId === gw.id ? 'gw-pin-target' : ''} ${editGwId === gw.id ? 'gw-pin-editing' : ''}`}
                                        style={{ '--pin-color': statusColor(gw.connection_status) } as React.CSSProperties}
                                    />
                                </div>
                            </Marker>
                        );
                    })}

                    {selectedGw && (
                        <Popup
                            longitude={selectedGw.longitude as number}
                            latitude={selectedGw.latitude as number}
                            anchor="bottom"
                            offset={18}
                            closeOnClick={false}
                            onClose={() => setSelectedGwId(null)}
                            maxWidth="min(320px, calc(100vw - 2rem))"
                            className="map-popup"
                        >
                            <GatewayPopup
                                gateway={selectedGw}
                                tagsById={tagsById}
                                tagLive={liveSnapshot}
                                onClose={() => setSelectedGwId(null)}
                                onOpenTrend={(gid, tid) => navigate(`/trend?gateway=${gid}${tid ? `&tag=${tid}` : ''}`)}
                                onOpenTrendPens={(pens) => navigate(`/trend?${pens}`)}
                                canEdit={canEdit}
                                predicted={selectedGw.predicted}
                                onConfirm={() => {
                                    void saveGatewayPos(selectedGw.id, selectedGw.latitude as number, selectedGw.longitude as number);
                                }}
                                onPlace={() => {
                                    setPlacingForId(selectedGw.id);
                                    setSelectedGwId(null);
                                }}
                            />
                        </Popup>
                    )}

                    {/* Popup di dettaglio di un tag (click su pill) */}
                    {openTag && openTagLive && (
                        <Popup
                            longitude={(gateways.find((g) => g.id === openTag.gateway_id)?.longitude) as number}
                            latitude={(gateways.find((g) => g.id === openTag.gateway_id)?.latitude) as number}
                            anchor="bottom"
                            offset={96}
                            closeOnClick={false}
                            onClose={() => setOpenTagId(null)}
                            maxWidth="min(300px, calc(100vw - 2rem))"
                            className="map-popup"
                        >
                            <TagDetailPopup
                                tag={openTag}
                                live={openTagLive}
                                onClose={() => setOpenTagId(null)}
                                onOpenTrend={(_gid, tid) => navigate(`/trend?pens=${tid}`)}
                            />
                        </Popup>
                    )}
                </MapGL>

                {/* Legend, on the map: what the colours mean and how many of each.
                    Out of the way while a popup is open (on a phone it covered it). */}
                {!selectedGw && !openTag && <div className="pointer-events-none absolute top-3 left-3 z-10 max-w-[calc(100%-4.5rem)] flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border bg-card/90 px-3 py-1.5 text-xs text-muted-foreground shadow-sm backdrop-blur">
                    {([
                        ['online', t('map.legend_online')],
                        ['offline', t('map.legend_offline')],
                        ['never', t('map.legend_never')],
                    ] as const).map(([st, label]) => (
                        <span key={st} className="flex items-center gap-1.5">
                            <span className="size-2.5 rounded-full ring-2 ring-white dark:ring-zinc-900" style={{ background: statusColor(st) }} />
                            {label}
                            <span className="font-semibold tabular-nums text-foreground">
                                {visibleGateways.filter((g) => (st === 'never' ? !g.connection_status || (g.connection_status !== 'online' && g.connection_status !== 'offline') : g.connection_status === st)).length}
                            </span>
                        </span>
                    ))}
                    {visibleGateways.some((g) => g.predicted) && (
                        <span className="flex items-center gap-1.5">
                            <span className="size-2.5 rounded-full border-2 border-dashed border-muted-foreground" />
                            {t('map.legend_predicted')}
                            <span className="font-semibold tabular-nums text-foreground">{visibleGateways.filter((g) => g.predicted).length}</span>
                        </span>
                    )}
                    <span className="hidden sm:inline text-muted-foreground/80">· {placedGateways.length}/{gateways.length} {t('map.placed_count')}</span>
                </div>}

                {placingForId !== null && (
                    <div className="absolute top-3 left-1/2 -translate-x-1/2 z-20 px-3 py-1.5 rounded-md bg-primary text-primary-foreground text-xs font-medium shadow-md">
                        {t('map.placing_hint')}
                        <button
                            type="button"
                            className="ml-3 underline underline-offset-2"
                            onClick={() => setPlacingForId(null)}
                        >
                            {t('common.cancel')}
                        </button>
                    </div>
                )}
            </div>

            {/* Popup gateway completo (da «+N altri»): lista scrollabile dei tag */}
            {listGw && (
                <GatewayListDialog
                    gateway={listGw}
                    tagsById={tagsById}
                    tagLive={liveSnapshot}
                    onClose={() => setListGwId(null)}
                    onOpenTrend={(_gid, tid) => navigate(`/trend?pens=${tid}`)}
                />
            )}

            {/* Editing posizione gateway: solo admin */}
            {canEdit && editGw && (
                <div className="rounded-xl border bg-card p-4 space-y-3">
                    <div className="flex items-center gap-2">
                        <MapPin size={16} className="text-muted-foreground" />
                        <strong className="text-sm">{editGw.name}</strong>
                        <span className="text-xs text-muted-foreground">{t('map.edit_position')}</span>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 items-end">
                        <div className="grid gap-1">
                            <label className="text-xs text-muted-foreground" htmlFor="map-lat">{t('map.latitude')}</label>
                            <input
                                id="map-lat"
                                className="h-9 px-2 text-sm rounded-md border bg-background font-mono"
                                value={editLat}
                                onChange={(e) => setEditLat(e.target.value)}
                                placeholder="39.3592 / 39°30'N"
                            />
                        </div>
                        <div className="grid gap-1">
                            <label className="text-xs text-muted-foreground" htmlFor="map-lon">{t('map.longitude')}</label>
                            <input
                                id="map-lon"
                                className="h-9 px-2 text-sm rounded-md border bg-background font-mono"
                                value={editLon}
                                onChange={(e) => setEditLon(e.target.value)}
                                placeholder="16.5142 / 16°30'E"
                            />
                        </div>
                        <div className="flex gap-2">
                            <button
                                type="button"
                                className="h-9 px-3 rounded-md bg-primary text-primary-foreground text-xs font-medium hover:bg-primary/90 disabled:opacity-50"
                                disabled={savingPos || !parseCoords(`${editLat}, ${editLon}`)}
                                onClick={handleSavePosFields}
                            >
                                {savingPos ? t('map.saving') : t('common.save')}
                            </button>
                            <button
                                type="button"
                                className="h-9 px-3 rounded-md border text-xs hover:bg-muted"
                                onClick={() => setEditGwId(null)}
                            >
                                {t('common.close')}
                            </button>
                        </div>
                    </div>
                    <p className="text-[11px] text-muted-foreground">{t('map.coords_hint')}</p>
                </div>
            )}

            {/* Selezione gateway da posizionare (solo admin, modalità posiziona) */}
            {canEdit && placingForId === null && (
                <div className="flex items-center gap-2 text-xs text-muted-foreground px-1">
                    <span>{t('map.place_pick')}:</span>
                    <select
                        className="h-8 px-2 rounded-md border bg-background text-xs"
                        value=""
                        onChange={(e) => {
                            if (e.target.value) setPlacingForId(parseInt(e.target.value));
                        }}
                    >
                        <option value="">{t('map.place_select')}</option>
                        {gateways.map((gw) => (
                            <option key={gw.id} value={gw.id}>{gw.name}</option>
                        ))}
                    </select>
                </div>
            )}

            <style>{`
                .gw-anchor { display: flex; flex-direction: column; align-items: center; }
                .gw-pills {
                    display: flex; flex-direction: column; gap: 2px; align-items: stretch;
                    margin-bottom: 5px; max-height: 0; overflow: hidden; opacity: 0;
                    transform: translateY(6px) scale(0.96); transition: all 0.25s ease;
                }
                .gw-pills-open { max-height: 260px; opacity: 1; transform: translateY(0) scale(1); }
                .gw-pill {
                    display: flex; align-items: center; gap: 6px;
                    background: hsl(var(--card) / 0.97);
                    color: hsl(var(--card-foreground));
                    border: 1px solid hsl(var(--border));
                    border-radius: 8px;
                    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.18);
                    padding: 3px 8px;
                    font-size: 12px; line-height: 1.35;
                    cursor: pointer; white-space: nowrap;
                    backdrop-filter: blur(4px);
                    transition: border-color .15s ease, box-shadow .15s ease;
                }
                .gw-pill:hover { border-color: hsl(var(--foreground) / 0.35); box-shadow: 0 2px 8px rgba(0, 0, 0, 0.22); }
                .gw-pill-more { justify-content: center; color: hsl(var(--muted-foreground)); font-size: 11px; }
                .gw-qdot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
                .gw-pill-name { font-weight: 500; color: hsl(var(--muted-foreground)); flex-shrink: 0; }
                .gw-pill-value { font-weight: 600; font-variant-numeric: tabular-nums; margin-left: auto; }
                .gw-pill-unit { color: hsl(var(--muted-foreground)); font-weight: 400; font-size: 11px; }
                .gw-pill-arrow { color: hsl(var(--muted-foreground)); font-size: 10px; flex-shrink: 0; }
                /* The pin: a dot with a white ring, its state in the colour;
                   an offline one keeps a slow halo so it is found at a glance. */
                .gw-pin {
                    width: 18px; height: 18px; border-radius: 9999px;
                    background: var(--pin-color, #9CA3AF);
                    border: 3px solid #fff;
                    box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.25), 0 2px 6px rgba(0, 0, 0, 0.35);
                    cursor: pointer; transition: transform .15s ease;
                }
                .gw-pin:hover { transform: scale(1.15); }
                /* Provisional: where it probably is, until someone confirms it. */
                .gw-pin-predicted {
                    background: transparent; border: 2.5px dashed var(--pin-color, #9CA3AF);
                    box-shadow: 0 0 0 3px rgba(255, 255, 255, 0.85);
                }
                .gw-pin-off { animation: gw-halo 2s ease-out infinite; }
                @keyframes gw-halo {
                    0% { box-shadow: 0 0 0 1px rgba(0,0,0,.25), 0 0 0 0 rgba(239, 68, 68, 0.55); }
                    100% { box-shadow: 0 0 0 1px rgba(0,0,0,.25), 0 0 0 12px rgba(239, 68, 68, 0); }
                }
                .gw-pin-target { outline: 2px dashed hsl(var(--foreground)); outline-offset: 3px; animation: gw-pulse 1s infinite alternate; }
                .gw-pin-editing { box-shadow: 0 0 0 3px rgba(56, 189, 248, 0.6); }
                @keyframes gw-pulse { from { transform: scale(1); } to { transform: scale(1.2); } }
                .map-popup .maplibregl-popup-content {
                    background: hsl(var(--card));
                    color: hsl(var(--card-foreground));
                    border: 1px solid hsl(var(--border));
                    border-radius: 12px;
                    padding: 14px;
                    min-width: 240px;
                    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.25);
                }
                .map-popup .maplibregl-popup-tip { border-top-color: hsl(var(--card)) !important; }
                .map-popup .maplibregl-popup-close-button { color: hsl(var(--muted-foreground)); }
            `}</style>
        </div>
    );
};

interface GatewayPopupProps {
    gateway: { id: number; name: string; connection_status?: string; latitude?: number | null; longitude?: number | null; connection_config?: Record<string, unknown> };
    tagsById: Map<number, Tag>;
    tagLive: Map<number, TagLive>;
    onClose: () => void;
    onOpenTrend: (gatewayId: number, tagId?: number) => void;
    onOpenTrendPens: (query: string) => void;
    canEdit: boolean;
    onPlace: () => void;
    /** Shown where it probably is, not where someone put it. */
    predicted?: boolean;
    onConfirm?: () => void;
}

// Popup del gateway cliccato: nome, stato, batteria, valori live dei tag.
function GatewayPopup({ gateway, tagsById, tagLive, onClose, onOpenTrend, onOpenTrendPens, canEdit, onPlace, predicted, onConfirm }: GatewayPopupProps) {
    const { t } = useTranslation();
    const battery = extractBattery(gateway.connection_config);
    const tags = [...tagsById.values()].filter((tg) => tg.gateway_id === gateway.id);
    const popupNames = shortTagNames(tags.map((tg) => ({ id: tg.id, name: tg.alias || tg.code || `#${tg.id}` })));

    return (
        <div className="space-y-2 text-sm">
            <div className="flex items-center gap-2">
                <span className="w-2.5 h-2.5 rounded-full shrink-0" style={{ background: statusColor(gateway.connection_status) }} />
                <strong className="truncate">{gateway.name}</strong>
            </div>
            <div className="text-xs text-muted-foreground">
                {gateway.connection_status
                    ? t(`gatewaysPage.status.${gateway.connection_status}`, { defaultValue: gateway.connection_status })
                    : t('gatewaysPage.status.unknown')}
            </div>
            {predicted && (
                <div className="space-y-2 rounded-lg border border-dashed p-2.5 text-xs">
                    <p className="text-muted-foreground">{t('map.predicted_note')}</p>
                    {canEdit && (
                        <div className="flex gap-2">
                            <button type="button" className="h-8 px-3 rounded-md bg-primary text-primary-foreground text-xs font-medium hover:bg-primary/90" onClick={onConfirm}>
                                {t('map.confirm_here')}
                            </button>
                            <button type="button" className="inline-flex h-8 items-center gap-1 px-3 rounded-md border text-xs hover:bg-muted" onClick={onPlace}>
                                <MapPin size={12} /> {t('map.move')}
                            </button>
                        </div>
                    )}
                </div>
            )}
            {battery !== null && (
                <div className="flex items-center gap-1.5 text-xs">
                    <Battery size={13} className="text-muted-foreground" />
                    {t('map.battery')}: {battery}%
                </div>
            )}
            <div className="border-t pt-2">
                <div className="text-xs font-medium text-muted-foreground mb-1">{t('map.live_values')}</div>
                {tags.length === 0 ? (
                    <div className="text-xs text-muted-foreground">{t('map.no_tags')}</div>
                ) : (
                    <ul className="space-y-1 max-h-40 overflow-y-auto">
                        {tags.map((tg) => {
                            const live = tagLive.get(tg.id);
                            return (
                                <li key={tg.id} className="flex items-baseline justify-between gap-3 text-sm">
                                    <span className="min-w-0 truncate text-muted-foreground" title={tg.alias || tg.code}>{popupNames.get(tg.id)}</span>
                                    <span className="shrink-0 whitespace-nowrap font-semibold tabular-nums">
                                        {formatLiveValue(live?.value, tg.eu_decimals)}
                                        {tg.eu_unit && live?.value != null ? ` ${tg.eu_unit}` : ''}
                                    </span>
                                </li>
                            );
                        })}
                    </ul>
                )}
            </div>
            <div className="flex flex-wrap gap-2 pt-1">
                <button
                    type="button"
                    className="h-8 px-3 rounded-md bg-primary text-primary-foreground text-xs font-medium hover:bg-primary/90"
                    onClick={() => {
                        // Open the trend with THIS gateway's pens preloaded:
                        // ?pens=<ids> is the format the trend page understands.
                        const ids = tags.map((tg) => tg.id).join(',');
                        if (ids) onOpenTrendPens(`pens=${ids}`);
                        else onOpenTrend(gateway.id);
                        onClose();
                    }}
                >
                    {t('map.open_trend')}
                </button>
                {canEdit && !predicted && (
                    <button
                        type="button"
                        className="inline-flex h-8 items-center gap-1 px-3 rounded-md border text-xs hover:bg-muted"
                        onClick={() => { onPlace(); onClose(); }}
                    >
                        <MapPin size={12} className="inline mr-1 -mt-0.5" />
                        {t('map.place')}
                    </button>
                )}
                <button
                    type="button"
                    className="inline-flex h-8 items-center gap-1 px-3 rounded-md border text-xs hover:bg-muted"
                    onClick={onClose}
                >
                    {t('common.close')}
                </button>
            </div>
        </div>
    );
}

interface TagDetailProps {
    tag: Tag;
    live: TagLive;
    onClose: () => void;
    onOpenTrend: (gatewayId: number, tagId: number) => void;
}

// Dettaglio di un tag dal click sulla pill: valore grande, unità, aggiornamento.
function TagDetailPopup({ tag, live, onClose, onOpenTrend }: TagDetailProps) {
    const { t } = useTranslation();
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => { const id = setInterval(() => setNow(Date.now()), 5000); return () => clearInterval(id); }, []);
    // Same samples-and-range history as I3X and the Tags table, inside the popup.
    const [showHistory, setShowHistory] = useState(false);
    const ago = live.ts > 0 ? Math.max(0, Math.round((now - live.ts) / 1000)) : null;
    const agoText = (() => {
        if (ago === null) return t('map.never_updated');
        const { n, unit } = agoParts(ago);
        if (unit === 'now') return t('map.updated_now');
        if (unit === 's') return t('map.updated_ago', { s: n });
        if (unit === 'm') return t('map.updated_min_ago', { n });
        if (unit === 'h') return t('map.updated_h_ago', { n });
        return t('map.updated_d_ago', { n });
    })();
    return (
        <div className="space-y-2 text-sm">
            <div className="text-xs font-medium text-muted-foreground">{t('map.tag_detail')}</div>
            <strong className="block truncate">{tag.alias || tag.code || `#${tag.id}`}</strong>
            <div className="flex items-baseline gap-2">
                <span className="text-2xl font-semibold tabular-nums">{formatLiveValue(live.value, tag.eu_decimals)}</span>
                {tag.eu_unit && <span className="text-xs text-muted-foreground">{tag.eu_unit}</span>}
                <span
                    className="w-2 h-2 rounded-full self-center ml-1"
                    style={{ background: live.quality === 0 ? '#10B981' : '#EF4444' }}
                    title={t('map.quality', { q: live.quality })}
                />
            </div>
            <div className="text-[11px] text-muted-foreground">
                {agoText}
            </div>
            {live.quality !== 0 && (
                <div className="text-[11px] text-red-500">{t('map.offline_no_new_data')}</div>
            )}
            <div className="flex gap-2 pt-1">
                <button
                    type="button"
                    className="h-8 px-3 rounded-md bg-primary text-primary-foreground text-xs font-medium hover:bg-primary/90"
                    onClick={() => { onOpenTrend(tag.gateway_id, tag.id); onClose(); }}
                >
                    {t('map.open_trend')}
                </button>
                <button
                    type="button"
                    className="inline-flex h-8 items-center gap-1 px-3 rounded-md border text-xs hover:bg-muted"
                    onClick={() => setShowHistory((v) => !v)}
                >
                    <History size={12} className="inline mr-1 -mt-0.5" />
                    {t('map.history')}
                </button>
                <button
                    type="button"
                    className="inline-flex h-8 items-center gap-1 px-3 rounded-md border text-xs hover:bg-muted"
                    onClick={onClose}
                >
                    {t('common.close')}
                </button>
            </div>
            {showHistory && (
                <div className="pt-2 w-[280px] max-w-[72vw] overflow-hidden">
                    <TagHistoryChart tagId={tag.id} tagName={tag.alias || tag.code || `#${tag.id}`} height={120} />
                </div>
            )}
        </div>
    );
}

interface GatewayListProps {
    gateway: { id: number; name: string; connection_status?: string };
    tagsById: Map<number, Tag>;
    tagLive: Map<number, TagLive>;
    onClose: () => void;
    onOpenTrend: (gatewayId: number, tagId?: number) => void;
}

// Lista scrollabile di tutti i tag del gateway (da «+N altri»).
function GatewayListDialog({ gateway, tagsById, tagLive, onClose, onOpenTrend }: GatewayListProps) {
    const { t } = useTranslation();
    const tags = [...tagsById.values()].filter((tg) => tg.gateway_id === gateway.id);
    return (
        <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
            onClick={onClose}
        >
            <div
                className="w-full max-w-md rounded-md border border-white/10 bg-[#0a0a0a]/95 shadow-xl"
                onClick={(e) => e.stopPropagation()}
            >
                <div className="flex items-center gap-2 px-4 py-3 border-b border-white/10">
                    <span className="w-2.5 h-2.5 rounded-full" style={{ background: statusColor(gateway.connection_status) }} />
                    <strong className="truncate">{gateway.name}</strong>
                    <span className="ml-auto text-xs text-muted-foreground">{tags.length} {t('map.tags_count')}</span>
                </div>
                <ul className="max-h-80 overflow-y-auto divide-y divide-white/5">
                    {tags.map((tg) => {
                        const live = tagLive.get(tg.id);
                        return (
                            <li key={tg.id} className="flex items-center gap-2 px-4 py-2 text-sm">
                                <span
                                    className="w-1.5 h-1.5 rounded-full shrink-0"
                                    style={{ background: (live?.quality ?? 1) === 0 ? '#10B981' : '#EF4444' }}
                                />
                                <span className="truncate flex-1">{tg.alias || tg.code || `#${tg.id}`}</span>
                                <span className="text-xs font-medium tabular-nums">
                                    {formatLiveValue(live?.value, tg.eu_decimals)}
                                    {tg.eu_unit && live?.value != null ? ` ${tg.eu_unit}` : ''}
                                </span>
                                <button
                                    type="button"
                                    className="px-2 py-0.5 rounded border border-border text-[10px] hover:bg-muted"
                                    onClick={() => { onOpenTrend(gateway.id, tg.id); onClose(); }}
                                >
                                    {t('map.open_trend')}
                                </button>
                            </li>
                        );
                    })}
                    {tags.length === 0 && (
                        <li className="px-4 py-6 text-center text-xs text-muted-foreground">{t('map.no_tags')}</li>
                    )}
                </ul>
                <div className="px-4 py-3 border-t border-white/10 text-right">
                    <button
                        type="button"
                        className="px-3 py-1 rounded border border-border text-xs hover:bg-muted"
                        onClick={onClose}
                    >
                        {t('common.close')}
                    </button>
                </div>
            </div>
        </div>
    );
}

export default MapPage;
