import { useEffect, useRef, useCallback } from 'react';
import type { ECharts } from 'echarts';

interface ChartInstanceEntry {
    instance: ECharts;
    group: string;
}

// Global registry for chart instances
const chartRegistry = new Map<string, ChartInstanceEntry>();
const syncGroups = new Map<string, Set<string>>();

/**
 * Hook to manage cross-chart cursor synchronization
 */
export const useChartSync = (chartId: string, syncGroup: string = 'default') => {
    const instanceRef = useRef<ECharts | null>(null);

    // Register chart instance
    const registerInstance = useCallback((instance: ECharts | null) => {
        if (instance) {
            instanceRef.current = instance;
            instance.group = syncGroup;

            // Add to registry
            chartRegistry.set(chartId, { instance, group: syncGroup });

            // Add to sync group
            if (!syncGroups.has(syncGroup)) {
                syncGroups.set(syncGroup, new Set());
            }
            syncGroups.get(syncGroup)!.add(chartId);

            // Connect all charts in the same group
            connectChartsInGroup(syncGroup);
        } else {
            // Unregister
            chartRegistry.delete(chartId);
            const group = syncGroups.get(syncGroup);
            if (group) {
                group.delete(chartId);
                if (group.size === 0) {
                    syncGroups.delete(syncGroup);
                }
            }
            instanceRef.current = null;
        }
    }, [chartId, syncGroup]);

    // Cleanup on unmount
    useEffect(() => {
        return () => {
            if (instanceRef.current) {
                registerInstance(null);
            }
        };
    }, [registerInstance]);

    return {
        registerInstance,
        chartId,
    };
};

/**
 * Connect all charts in a sync group
 */
const connectChartsInGroup = (groupName: string) => {
    const group = syncGroups.get(groupName);
    if (!group) return;

    const instances: ECharts[] = [];
    group.forEach(id => {
        const entry = chartRegistry.get(id);
        if (entry && entry.group === groupName) {
            instances.push(entry.instance);
        }
    });

    // ECharts 'connect' function links charts for synchronized actions
    if (instances.length > 1) {
        // Import echarts dynamically to use connect
        import('echarts').then(echarts => {
            echarts.connect(instances);
        });
    }
};

/**
 * Get all chart instances in a sync group
 */
export const getChartsInGroup = (groupName: string): ECharts[] => {
    const group = syncGroups.get(groupName);
    if (!group) return [];

    const instances: ECharts[] = [];
    group.forEach(id => {
        const entry = chartRegistry.get(id);
        if (entry && entry.group === groupName) {
            instances.push(entry.instance);
        }
    });

    return instances;
};

/**
 * Dispatch action to all charts in a group
 */
export const dispatchToGroup = (groupName: string, action: any) => {
    const instances = getChartsInGroup(groupName);
    instances.forEach(instance => {
        instance.dispatchAction(action);
    });
};

/**
 * Hook for synchronized highlight
 */
export const useSyncHighlight = (chartId: string, syncGroup: string = 'default') => {
    const { registerInstance } = useChartSync(chartId, syncGroup);

    const highlightPoint = useCallback((dataIndex: number, seriesIndex?: number) => {
        const group = syncGroups.get(syncGroup);
        if (!group) return;

        group.forEach(id => {
            const entry = chartRegistry.get(id);
            if (entry) {
                entry.instance.dispatchAction({
                    type: 'showTip',
                    seriesIndex: seriesIndex ?? 0,
                    dataIndex,
                });
            }
        });
    }, [syncGroup]);

    const hideHighlight = useCallback(() => {
        const group = syncGroups.get(syncGroup);
        if (!group) return;

        group.forEach(id => {
            const entry = chartRegistry.get(id);
            if (entry) {
                entry.instance.dispatchAction({
                    type: 'hideTip',
                });
            }
        });
    }, [syncGroup]);

    return {
        registerInstance,
        highlightPoint,
        hideHighlight,
    };
};

/**
 * Hook for synchronized data zoom
 */
export const useSyncZoom = (chartId: string, syncGroup: string = 'default') => {
    const { registerInstance } = useChartSync(chartId, syncGroup);

    const zoomToRange = useCallback((start: number, end: number) => {
        const group = syncGroups.get(syncGroup);
        if (!group) return;

        group.forEach(id => {
            const entry = chartRegistry.get(id);
            if (entry) {
                entry.instance.dispatchAction({
                    type: 'dataZoom',
                    start,
                    end,
                });
            }
        });
    }, [syncGroup]);

    const resetZoom = useCallback(() => {
        const group = syncGroups.get(syncGroup);
        if (!group) return;

        group.forEach(id => {
            const entry = chartRegistry.get(id);
            if (entry) {
                entry.instance.dispatchAction({
                    type: 'restore',
                });
            }
        });
    }, [syncGroup]);

    return {
        registerInstance,
        zoomToRange,
        resetZoom,
    };
};

export default useChartSync;
