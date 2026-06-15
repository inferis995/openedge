// Trend-specific types for AVEVA-style trend visualization

export type AggregationType = 'mean' | 'max' | 'min' | 'sum' | 'first' | 'last' | 'count' | 'median' | 'stddev';
export type TimePreset = '15m' | '1h' | '6h' | '12h' | '24h' | '7d' | '30d' | 'currentShift' | 'previousShift' | 'today' | 'yesterday' | 'custom';

export interface TrendDataPoint {
    timestamp: number;
    value: number | null;
    quality: number; // 0 = GOOD, >0 = BAD/UNCERTAIN
}

export interface TagWithHierarchy {
    id: number;
    gateway_id: number;
    code: string;
    alias?: string;
    data_type: 'BOOL' | 'INT' | 'REAL' | 'DINT' | 'STRING';
    historize: boolean;
    // Hierarchy info
    gateway_name?: string;
    area_id?: number;
    area_name?: string;
    site_id?: number;
    site_name?: string;
    org_id?: number;
    org_name?: string;
    // Scaling info
    scaling_enabled?: boolean;
    scaling_raw_min?: number;
    scaling_raw_max?: number;
    scaling_eu_min?: number;
    scaling_eu_max?: number;
    scaling_clamp?: boolean;
    eu_unit?: string;
    eu_decimals?: number;
    invert?: boolean;
}

export interface ChartConfig {
    id: string;
    title?: string;
    tagIds: number[];
    yAxisConfigs: YAxisConfig[];
    visible: boolean;
    height: number; // Grid units
}

export type LineType = 'solid' | 'dashed' | 'dotted';
export type ChartType = 'line' | 'area' | 'step' | 'bar';

export interface YAxisConfig {
    id: string;
    tagId: number;
    label?: string;
    min?: number;
    max?: number;
    autoScale: boolean;
    position: 'left' | 'right';
    color: string;
    // Per-series style settings
    lineType: LineType;
    lineWidth: number;   // 1-4
    chartType: ChartType;
    showMarkers: boolean;
    visible: boolean;
    areaOpacity: number; // 0-1, used when chartType='area'
}

export interface TimeRange {
    preset: TimePreset;
    customStart?: Date;
    customEnd?: Date;
}

export interface TrendState {
    // Charts
    charts: ChartConfig[];
    activeChartId: string | null;

    // Time
    timeRange: TimeRange;
    liveMode: boolean;
    autoRefreshInterval: number; // milliseconds

    // Aggregation
    aggregation: AggregationType;

    // UI State
    sidebarOpen: boolean;
    dataTableOpen: boolean;
    dataTableHeight: number;

    // Favorites
    favoriteTagIds: number[];
}

// API request/response types
export interface BatchHistoryRequest {
    tag_ids: number[];
    start: string;
    end: string;
    agg?: AggregationType;
    interval?: string;
}

export interface BatchHistoryResponse {
    [tagId: number]: TrendDataPoint[];
}

export interface TagHierarchyResponse {
    organizations: OrganizationHierarchy[];
}

export interface OrganizationHierarchy {
    id: number;
    name: string;
    sites: SiteHierarchy[];
}

export interface SiteHierarchy {
    id: number;
    name: string;
    areas: AreaHierarchy[];
}

export interface AreaHierarchy {
    id: number;
    name: string;
    gateways: GatewayHierarchy[];
}

export interface GatewayHierarchy {
    id: number;
    name: string;
    driver_type: string;
    tags: TagWithHierarchy[];
}

// Chart series data for ECharts
export interface ChartSeries {
    tagId: number;
    tagName: string;
    data: [number, number | null][]; // [timestamp, value]
    color: string;
    yAxisIndex: number;
    isBool: boolean;
    quality: number[];
    // Style from YAxisConfig (with safe defaults)
    lineType?: LineType;
    lineWidth?: number;
    chartType?: ChartType;
    showMarkers?: boolean;
    visible?: boolean;
    yMin?: number;
    yMax?: number;
    autoScale?: boolean;
    yPosition?: 'left' | 'right';
    areaOpacity?: number;
}

// Grid layout item
export interface TrendGridItem {
    i: string;
    x: number;
    y: number;
    w: number;
    h: number;
    minW?: number;
    minH?: number;
}

// Auto-resampling rules
export interface ResamplingRule {
    maxRangeHours: number;
    interval: string;
}

export const RESAMPLING_RULES: ResamplingRule[] = [
    { maxRangeHours: 1, interval: '10s' },
    { maxRangeHours: 6, interval: '1m' },
    { maxRangeHours: 24, interval: '5m' },
    { maxRangeHours: 168, interval: '15m' }, // 7 days
    { maxRangeHours: 720, interval: '1h' }, // 30 days
    { maxRangeHours: Infinity, interval: '6h' },
];

// Color palette for tags
export const TAG_COLORS = [
    '#3b82f6', // Blue
    '#ef4444', // Red
    '#22c55e', // Green
    '#f59e0b', // Amber
    '#8b5cf6', // Violet
    '#06b6d4', // Cyan
    '#ec4899', // Pink
    '#84cc16', // Lime
    '#f97316', // Orange
    '#6366f1', // Indigo
    '#14b8a6', // Teal
    '#f43f5e', // Rose
    '#a855f7', // Purple
    '#eab308', // Yellow
    '#22d3ee', // Light Cyan
];

// Shift definitions (can be customized)
export interface ShiftConfig {
    name: string;
    startHour: number;
    endHour: number;
}

export const DEFAULT_SHIFTS: ShiftConfig[] = [
    { name: 'Morning', startHour: 6, endHour: 14 },
    { name: 'Afternoon', startHour: 14, endHour: 22 },
    { name: 'Night', startHour: 22, endHour: 6 },
];

// Helper function to get interval for time range
export function getAutoInterval(start: Date | string | number, end: Date | string | number): string {
    // Ensure inputs are Date objects
    const startDate = start instanceof Date ? start : new Date(start);
    const endDate = end instanceof Date ? end : new Date(end);

    // Validate dates
    if (isNaN(startDate.getTime()) || isNaN(endDate.getTime())) {
        console.warn('Invalid date passed to getAutoInterval', { start, end });
        return '1m'; // Default fallback
    }

    const rangeHours = (endDate.getTime() - startDate.getTime()) / (1000 * 60 * 60);

    for (const rule of RESAMPLING_RULES) {
        if (rangeHours <= rule.maxRangeHours) {
            return rule.interval;
        }
    }

    return '6h';
}

// Helper to calculate time range from preset
export function calculateTimeRange(preset: TimePreset, now?: Date): { start: Date; end: Date } {
    const refNow = now || new Date();

    switch (preset) {
        case '15m':
            return { start: new Date(refNow.getTime() - 15 * 60 * 1000), end: refNow };
        case '1h':
            return { start: new Date(refNow.getTime() - 60 * 60 * 1000), end: refNow };
        case '6h':
            return { start: new Date(refNow.getTime() - 6 * 60 * 60 * 1000), end: refNow };
        case '12h':
            return { start: new Date(refNow.getTime() - 12 * 60 * 60 * 1000), end: refNow };
        case '24h':
            return { start: new Date(refNow.getTime() - 24 * 60 * 60 * 1000), end: refNow };
        case '7d':
            return { start: new Date(refNow.getTime() - 7 * 24 * 60 * 60 * 1000), end: refNow };
        case '30d':
            return { start: new Date(refNow.getTime() - 30 * 24 * 60 * 60 * 1000), end: refNow };
        case 'today':
            return {
                start: new Date(refNow.getFullYear(), refNow.getMonth(), refNow.getDate(), 0, 0, 0),
                end: refNow
            };
        case 'yesterday': {
            const yesterdayStart = new Date(refNow.getFullYear(), refNow.getMonth(), refNow.getDate() - 1, 0, 0, 0);
            const yesterdayEnd = new Date(refNow.getFullYear(), refNow.getMonth(), refNow.getDate() - 1, 23, 59, 59);
            return { start: yesterdayStart, end: yesterdayEnd };
        }
        case 'currentShift': {
            const currentHour = refNow.getHours();
            for (const shift of DEFAULT_SHIFTS) {
                if (shift.startHour <= shift.endHour) {
                    if (currentHour >= shift.startHour && currentHour < shift.endHour) {
                        return {
                            start: new Date(refNow.getFullYear(), refNow.getMonth(), refNow.getDate(), shift.startHour, 0, 0),
                            end: refNow
                        };
                    }
                } else {
                    // Night shift crosses midnight
                    if (currentHour >= shift.startHour || currentHour < shift.endHour) {
                        const shiftStart = currentHour >= shift.startHour
                            ? new Date(refNow.getFullYear(), refNow.getMonth(), refNow.getDate(), shift.startHour, 0, 0)
                            : new Date(refNow.getFullYear(), refNow.getMonth(), refNow.getDate() - 1, shift.startHour, 0, 0);
                        return { start: shiftStart, end: refNow };
                    }
                }
            }
            return { start: new Date(refNow.getTime() - 8 * 60 * 60 * 1000), end: refNow };
        }
        case 'previousShift': {
            const prevShiftEnd = new Date(refNow.getTime() - 8 * 60 * 60 * 1000);
            const prevShiftStart = new Date(prevShiftEnd.getTime() - 8 * 60 * 60 * 1000);
            return { start: prevShiftStart, end: prevShiftEnd };
        }
        default:
            return { start: new Date(refNow.getTime() - 60 * 60 * 1000), end: refNow };
    }
}
