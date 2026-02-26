import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import {
    TrendState,
    ChartConfig,
    TimeRange,
    AggregationType,
    CompareMode,
    TAG_COLORS,
} from '@/types/trend';

const generateId = () => Math.random().toString(36).substring(2, 9);

interface TrendStore extends TrendState {
    // Chart actions
    addChart: (chart?: Partial<ChartConfig>) => string;
    removeChart: (chartId: string) => void;
    updateChart: (chartId: string, updates: Partial<ChartConfig>) => void;
    setActiveChart: (chartId: string | null) => void;
    addTagToChart: (chartId: string, tagId: number) => void;
    removeTagFromChart: (chartId: string, tagId: number) => void;
    clearChartTags: (chartId: string) => void;

    // Time actions
    setTimeRange: (timeRange: TimeRange) => void;
    setLiveMode: (enabled: boolean) => void;
    setAutoRefreshInterval: (interval: number) => void;

    // Aggregation actions
    setAggregation: (agg: AggregationType) => void;

    // Compare mode actions
    setCompareMode: (mode: CompareMode) => void;

    // UI actions
    toggleSidebar: () => void;
    toggleDataTable: () => void;
    setDataTableHeight: (height: number) => void;

    // Favorites actions
    addFavoriteTag: (tagId: number) => void;
    removeFavoriteTag: (tagId: number) => void;
    toggleFavoriteTag: (tagId: number) => void;

    // Utility
    getNextColor: () => string;
    resetStore: () => void;
}

const initialState: TrendState = {
    charts: [],
    activeChartId: null,
    timeRange: { preset: '1h' },
    liveMode: true,
    autoRefreshInterval: 10000,
    aggregation: 'max',
    compareMode: 'none',
    sidebarOpen: true,
    dataTableOpen: false,
    dataTableHeight: 200,
    favoriteTagIds: [],
};

// Color assignment tracking
let colorIndex = 0;

export const useTrendStore = create<TrendStore>()(
    persist(
        (set, get) => ({
            ...initialState,

            addChart: (chart?: Partial<ChartConfig>) => {
                const id = chart?.id || generateId();
                const newChart: ChartConfig = {
                    id,
                    title: chart?.title || `Chart ${(get().charts.length + 1)}`,
                    tagIds: chart?.tagIds || [],
                    yAxisConfigs: chart?.yAxisConfigs || [],
                    visible: chart?.visible ?? true,
                    height: chart?.height || 4,
                };

                set((state) => ({
                    charts: [...state.charts, newChart],
                    activeChartId: id,
                }));

                return id;
            },

            removeChart: (chartId: string) => {
                set((state) => {
                    const newCharts = state.charts.filter((c) => c.id !== chartId);
                    return {
                        charts: newCharts,
                        activeChartId: state.activeChartId === chartId
                            ? (newCharts[0]?.id || null)
                            : state.activeChartId,
                    };
                });
            },

            updateChart: (chartId: string, updates: Partial<ChartConfig>) => {
                set((state) => ({
                    charts: state.charts.map((c) =>
                        c.id === chartId ? { ...c, ...updates } : c
                    ),
                }));
            },

            setActiveChart: (chartId: string | null) => {
                set({ activeChartId: chartId });
            },

            addTagToChart: (chartId: string, tagId: number) => {
                set((state) => {
                    const chart = state.charts.find((c) => c.id === chartId);
                    if (!chart || chart.tagIds.includes(tagId)) return state;

                    const color = get().getNextColor();
                    const newYAxisConfig = {
                        id: generateId(),
                        tagId,
                        autoScale: true,
                        position: (chart.yAxisConfigs.length % 2 === 0 ? 'left' : 'right') as 'left' | 'right',
                        color,
                    };

                    return {
                        charts: state.charts.map((c) =>
                            c.id === chartId
                                ? {
                                    ...c,
                                    tagIds: [...c.tagIds, tagId],
                                    yAxisConfigs: [...c.yAxisConfigs, newYAxisConfig],
                                }
                                : c
                        ),
                    };
                });
            },

            removeTagFromChart: (chartId: string, tagId: number) => {
                set((state) => ({
                    charts: state.charts.map((c) =>
                        c.id === chartId
                            ? {
                                ...c,
                                tagIds: c.tagIds.filter((id) => id !== tagId),
                                yAxisConfigs: c.yAxisConfigs.filter((y) => y.tagId !== tagId),
                            }
                            : c
                    ),
                }));
            },

            clearChartTags: (chartId: string) => {
                set((state) => ({
                    charts: state.charts.map((c) =>
                        c.id === chartId
                            ? { ...c, tagIds: [], yAxisConfigs: [] }
                            : c
                    ),
                }));
            },

            setTimeRange: (timeRange: TimeRange) => {
                set({ timeRange });
            },

            setLiveMode: (enabled: boolean) => {
                set({ liveMode: enabled });
            },

            setAutoRefreshInterval: (interval: number) => {
                set({ autoRefreshInterval: interval });
            },

            setAggregation: (agg: AggregationType) => {
                set({ aggregation: agg });
            },

            setCompareMode: (mode: CompareMode) => {
                set({ compareMode: mode });
            },

            toggleSidebar: () => {
                set((state) => ({ sidebarOpen: !state.sidebarOpen }));
            },

            toggleDataTable: () => {
                set((state) => ({ dataTableOpen: !state.dataTableOpen }));
            },

            setDataTableHeight: (height: number) => {
                set({ dataTableHeight: height });
            },

            addFavoriteTag: (tagId: number) => {
                set((state) => ({
                    favoriteTagIds: state.favoriteTagIds.includes(tagId)
                        ? state.favoriteTagIds
                        : [...state.favoriteTagIds, tagId],
                }));
            },

            removeFavoriteTag: (tagId: number) => {
                set((state) => ({
                    favoriteTagIds: state.favoriteTagIds.filter((id) => id !== tagId),
                }));
            },

            toggleFavoriteTag: (tagId: number) => {
                set((state) => ({
                    favoriteTagIds: state.favoriteTagIds.includes(tagId)
                        ? state.favoriteTagIds.filter((id) => id !== tagId)
                        : [...state.favoriteTagIds, tagId],
                }));
            },

            getNextColor: () => {
                const color = TAG_COLORS[colorIndex % TAG_COLORS.length];
                colorIndex++;
                return color;
            },

            resetStore: () => {
                colorIndex = 0;
                set(initialState);
            },
        }),
        {
            name: 'ralph-trend-store',
            partialize: (state) => ({
                charts: state.charts,
                timeRange: state.timeRange,
                aggregation: state.aggregation,
                sidebarOpen: state.sidebarOpen,
                dataTableOpen: state.dataTableOpen,
                dataTableHeight: state.dataTableHeight,
                favoriteTagIds: state.favoriteTagIds,
            }),
            // Reviver to convert date strings back to Date objects
            merge: (persistedState: unknown, currentState) => {
                const persisted = persistedState as Partial<TrendState> | null;
                if (!persisted) return currentState;

                // Convert date strings to Date objects in timeRange
                if (persisted.timeRange?.customStart && typeof persisted.timeRange.customStart === 'string') {
                    persisted.timeRange.customStart = new Date(persisted.timeRange.customStart);
                }
                if (persisted.timeRange?.customEnd && typeof persisted.timeRange.customEnd === 'string') {
                    persisted.timeRange.customEnd = new Date(persisted.timeRange.customEnd);
                }

                return {
                    ...currentState,
                    ...persisted,
                };
            },
        }
    )
);

// Reset color index when store is hydrated
if (typeof window !== 'undefined') {
    const stored = localStorage.getItem('ralph-trend-store');
    if (stored) {
        try {
            const data = JSON.parse(stored);
            if (data.state?.charts) {
                // Count total yAxisConfigs to set colorIndex
                const totalAxes = data.state.charts.reduce(
                    (sum: number, c: ChartConfig) => sum + (c.yAxisConfigs?.length || 0),
                    0
                );
                colorIndex = totalAxes;
            }
        } catch {
            // Ignore parse errors
        }
    }
}
