import { useState, useEffect, useCallback } from 'react';
import { useSparkplugDeviceStore } from '@/stores/useSparkplugDeviceStore';

/**
 * Hook to determine data status based on Sparkplug B device status
 *
 * With Sparkplug B, data quality is determined by device DBIRTH/DDEATH status,
 * NOT by value timestamp. A value that doesn't change for hours is GOOD
 * if the device is still online (we received DBIRTH but not DDEATH).
 *
 * @returns Object with getDataStatus function
 */
export function useStaleData() {
    const [currentTime, setCurrentTime] = useState(Date.now());
    const getDeviceStatus = useSparkplugDeviceStore((state) => state.getDeviceStatus);

    // Update current time every second for time formatting
    useEffect(() => {
        const interval = setInterval(() => {
            setCurrentTime(Date.now());
        }, 1000);
        return () => clearInterval(interval);
    }, []);

    // Check if device is online via Sparkplug B
    const isDeviceOnline = useCallback((deviceName: string | undefined): boolean | null => {
        if (!deviceName) return null;
        const status = getDeviceStatus(deviceName);
        if (status === 'unknown') return null;
        return status === 'online';
    }, [getDeviceStatus]);

    // Calculate time ago in seconds
    const getTimeAgo = useCallback((timestamp: number | undefined): number => {
        if (!timestamp) return 0;
        return Math.floor((currentTime - timestamp) / 1000);
    }, [currentTime]);

    // Format time ago as human-readable string
    const formatTimeAgo = useCallback((timestamp: number | undefined): string => {
        const seconds = getTimeAgo(timestamp);
        if (seconds <= 0) return 'just now';
        if (seconds < 60) return `${seconds}s ago`;
        const minutes = Math.floor(seconds / 60);
        const remainingSeconds = seconds % 60;
        if (minutes < 60) return `${minutes}m ${remainingSeconds}s ago`;
        const hours = Math.floor(minutes / 60);
        const remainingMinutes = minutes % 60;
        return `${hours}h ${remainingMinutes}m ago`;
    }, [getTimeAgo]);

    return {
        isDeviceOnline,
        getTimeAgo,
        formatTimeAgo,
        currentTime,
    };
}

/**
 * Determine the data status based on Sparkplug B device status and quality
 *
 * PRODUCTION READY LOGIC:
 * 1. If listener not ready (grace period) → use quality only
 * 2. If device is OFFLINE (DDEATH received) → BAD
 * 3. If device is ONLINE (DBIRTH received) → check quality
 * 4. If device is UNKNOWN → use quality only
 */
export type DataStatus = 'good' | 'bad' | 'unknown';

export function getDataStatus(
    quality: number | undefined,
    isDeviceOnline: boolean | null = null
): DataStatus {
    // If device is explicitly offline (DDEATH received after grace period), data is BAD
    if (isDeviceOnline === false) return 'bad';

    // If device is explicitly online (DBIRTH received), check quality.
    // Quality scale: 0 = GOOD, 1 = UNCERTAIN, 2 = BAD.
    if (isDeviceOnline === true) {
        if (quality === undefined) return 'unknown';
        if (quality === 0) return 'good';
        if (quality === 1) return 'unknown'; // UNCERTAIN
        return 'bad';
    }

    // Device is unknown (no Sparkplug info or grace period) - use quality only
    // This matches MQTT Monitor behavior
    if (quality === undefined) return 'unknown';
    if (quality === 0) return 'good';
    if (quality === 1) return 'unknown'; // UNCERTAIN
    return 'bad';
}

/**
 * Get CSS classes for data status
 */
export function getStatusClasses(status: DataStatus): {
    bg: string;
    border: string;
    text: string;
} {
    switch (status) {
        case 'good':
            return {
                bg: 'bg-green-50 dark:bg-green-900/20',
                border: 'border-green-200 dark:border-green-800',
                text: 'text-green-700 dark:text-green-400',
            };
        case 'unknown':
            return {
                bg: 'bg-slate-50 dark:bg-slate-800/50',
                border: 'border-slate-300 dark:border-slate-700',
                text: 'text-slate-500 dark:text-slate-400',
            };
        case 'bad':
        default:
            return {
                bg: 'bg-red-50 dark:bg-red-900/20',
                border: 'border-red-200 dark:border-red-800',
                text: 'text-red-600 dark:text-red-400',
            };
    }
}
