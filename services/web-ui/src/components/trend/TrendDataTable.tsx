import React, { useMemo } from 'react';
import { useReactTable, getCoreRowModel, flexRender, ColumnDef } from '@tanstack/react-table';
import { Button } from '@/components/ui/button';
import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area';
import { Download, X, GripVertical } from 'lucide-react';
import { TrendDataPoint, TagWithHierarchy, TAG_COLORS } from '@/types/trend';
import { toast } from 'sonner';

interface TrendDataTableProps {
    data: Map<number, TrendDataPoint[]>;
    tags: TagWithHierarchy[];
    selectedTagIds: number[];
    onClose?: () => void;
    height?: number;
}

interface TableRow {
    timestamp: number;
    date: string;
    time: string;
    [key: string]: number | string | null;
}

export const TrendDataTable: React.FC<TrendDataTableProps> = ({
    data,
    tags,
    selectedTagIds,
    onClose,
    height = 200,
}) => {
    const selectedTags = useMemo(() => {
        return selectedTagIds
            .map(id => tags.find(t => t.id === id))
            .filter(Boolean) as TagWithHierarchy[];
    }, [selectedTagIds, tags]);

    // Build table data with forward-fill for RBE gaps
    // Strategy: Collect all data, sort by time, then forward-fill each tag independently
    const tableData = useMemo(() => {
        // Collect ALL data points with their tag info
        interface DataPoint {
            timestamp: number;
            tagKey: string;
            value: any;
            quality: number;
        }

        const allPoints: DataPoint[] = [];

        selectedTagIds.forEach(tagId => {
            const tagData = data.get(tagId);
            if (!tagData) return;

            const tag = tags.find(t => t.id === tagId);
            const tagKey = tag?.alias || tag?.code || `Tag_${tagId}`;

            tagData.forEach(point => {
                allPoints.push({
                    timestamp: point.timestamp,
                    tagKey,
                    value: point.quality === 0 ? point.value : null,
                    quality: point.quality,
                });
            });
        });

        // Sort all points by timestamp
        allPoints.sort((a, b) => a.timestamp - b.timestamp);

        // Get unique timestamps
        const uniqueTimestamps = [...new Set(allPoints.map(p => p.timestamp))].sort((a, b) => a - b);

        // Build rows with forward-fill
        const lastKnownValues: Record<string, { value: any; quality: number }> = {};
        const rows: TableRow[] = [];

        uniqueTimestamps.forEach(ts => {
            const date = new Date(ts);
            const row: TableRow = {
                timestamp: ts,
                date: date.toLocaleDateString('it-IT'),
                time: date.toLocaleTimeString('it-IT', {
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit',
                }),
            };

            // Get all points at this timestamp
            const pointsAtTs = allPoints.filter(p => p.timestamp === ts);
            pointsAtTs.forEach(p => {
                row[p.tagKey] = p.value;
                row[`${p.tagKey}_quality`] = p.quality;
                // Unconditionally update last known value, even if it's null (offline),
                // so forward-fill propagates the N/A state instead of holding onto stale TRUE/FALSE
                lastKnownValues[p.tagKey] = { value: p.value, quality: p.quality };
            });

            // Forward-fill missing tags
            selectedTags.forEach(tag => {
                const key = tag.alias || tag.code;
                if (row[key] === undefined && lastKnownValues[key]) {
                    row[key] = lastKnownValues[key].value;
                    row[`${key}_quality`] = lastKnownValues[key].quality;
                }
            });

            rows.push(row);
        });

        // Return most recent first
        return rows.reverse().slice(0, 500);
    }, [data, selectedTagIds, tags, selectedTags]);

    // Define columns
    const columns = useMemo<ColumnDef<TableRow>[]>(() => {
        const cols: ColumnDef<TableRow>[] = [
            {
                accessorKey: 'date',
                header: 'Date',
                size: 80,
                cell: ({ getValue }) => (
                    <span className="text-xs text-muted-foreground">{getValue() as string}</span>
                ),
            },
            {
                accessorKey: 'time',
                header: 'Time',
                size: 80,
                cell: ({ getValue }) => (
                    <span className="text-xs text-muted-foreground font-mono">{getValue() as string}</span>
                ),
            },
        ];

        selectedTags.forEach((tag, index) => {
            const key = tag.alias || tag.code;
            cols.push({
                id: key,
                header: () => (
                    <div className="flex items-center gap-1.5">
                        <div
                            className="w-2.5 h-2.5 rounded-full"
                            style={{ backgroundColor: TAG_COLORS[index % TAG_COLORS.length] }}
                        />
                        <span className="text-xs font-medium">{key}</span>
                        <span className="text-[10px] text-muted-foreground">({tag.data_type})</span>
                    </div>
                ),
                accessorFn: (row) => row[key],
                cell: ({ row: tableRow }) => {
                    const value = tableRow.original[key];
                    const quality = tableRow.original[`${key}_quality`];
                    const isGood = quality === 0;
                    const isBool = tag.data_type === 'BOOL';

                    if (value === null || value === undefined) {
                        return (
                            <span className="text-xs font-mono text-destructive bg-destructive/10 px-1.5 py-0.5 rounded">
                                N/A
                            </span>
                        );
                    }

                    let displayValue: string;
                    let isTrue = false;

                    if (isBool) {
                        let numValue: number;
                        if (typeof value === 'boolean') {
                            numValue = value ? 1 : 0;
                        } else if (typeof value === 'string') {
                            numValue =
                                value.toLowerCase() === 'true' || value === '1' ? 1 : 0;
                        } else {
                            numValue = Number(value);
                        }
                        isTrue = numValue >= 0.5;
                        displayValue = isTrue ? 'TRUE' : 'FALSE';

                        return (
                            <span
                                className={`text-xs font-mono font-bold px-2 py-0.5 rounded ${isTrue
                                    ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
                                    : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
                                    }`}
                            >
                                {displayValue}
                            </span>
                        );
                    }

                    if (Number.isInteger(value)) {
                        displayValue = String(value);
                    } else {
                        displayValue = Number(value).toFixed(3);
                    }

                    return (
                        <span className={`text-xs font-mono ${isGood ? 'text-foreground' : 'text-destructive'}`}>
                            {displayValue}
                        </span>
                    );
                },
            });
        });

        return cols;
    }, [selectedTags]);

    const table = useReactTable({
        data: tableData,
        columns,
        getCoreRowModel: getCoreRowModel(),
    });

    const handleExportCSV = () => {
        if (tableData.length === 0 || selectedTags.length === 0) {
            toast.error('No data to export');
            return;
        }

        const headers = ['Timestamp', 'Date', 'Time', ...selectedTags.map(t => t.alias || t.code)];
        const rows = tableData.map(row => {
            const values = selectedTags.map(tag => {
                const key = tag.alias || tag.code;
                const value = row[key];
                return value ?? '';
            });
            return [
                new Date(row.timestamp).toISOString(),
                row.date,
                row.time,
                ...values,
            ];
        });

        const csvContent = [headers.join(','), ...rows.map(r => r.join(','))].join('\n');
        const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `trend_export_${new Date().toISOString().split('T')[0]}.csv`;
        link.click();
        URL.revokeObjectURL(url);
        toast.success('CSV exported successfully');
    };

    if (selectedTags.length === 0) {
        return (
            <div className="bg-card border-t p-4 text-center text-muted-foreground text-sm">
                No tags selected
            </div>
        );
    }

    return (
        <div
            className="bg-card border-t flex flex-col"
            style={{ height }}
        >
            {/* Header */}
            <div className="flex items-center justify-between px-3 py-1.5 border-b bg-muted/50">
                <div className="flex items-center gap-2">
                    <GripVertical className="w-3 h-3 text-muted-foreground cursor-ns-resize" />
                    <span className="text-xs font-semibold text-foreground">
                        Data Table ({tableData.length} rows)
                    </span>
                </div>
                <div className="flex items-center gap-1">
                    <Button
                        onClick={handleExportCSV}
                        variant="outline"
                        size="sm"
                        className="h-6 text-xs gap-1"
                        disabled={tableData.length === 0}
                    >
                        <Download className="w-3 h-3" />
                        Export CSV
                    </Button>
                    {onClose && (
                        <Button
                            onClick={onClose}
                            variant="ghost"
                            size="icon"
                            className="h-6 w-6"
                        >
                            <X className="w-3 h-3" />
                        </Button>
                    )}
                </div>
            </div>

            {/* Table */}
            <ScrollArea className="flex-1">
                <table className="w-full text-left">
                    <thead className="bg-muted/50 sticky top-0">
                        {table.getHeaderGroups().map(headerGroup => (
                            <tr key={headerGroup.id}>
                                {headerGroup.headers.map(header => (
                                    <th
                                        key={header.id}
                                        className="px-3 py-1.5 border-b font-medium text-foreground"
                                        style={{ width: header.getSize() }}
                                    >
                                        {header.isPlaceholder
                                            ? null
                                            : flexRender(
                                                header.column.columnDef.header,
                                                header.getContext()
                                            )}
                                    </th>
                                ))}
                            </tr>
                        ))}
                    </thead>
                    <tbody>
                        {table.getRowModel().rows.map(row => (
                            <tr
                                key={row.id}
                                className="hover:bg-muted/30 border-b border-border"
                            >
                                {row.getVisibleCells().map(cell => (
                                    <td key={cell.id} className="px-3 py-1">
                                        {flexRender(
                                            cell.column.columnDef.cell,
                                            cell.getContext()
                                        )}
                                    </td>
                                ))}
                            </tr>
                        ))}
                    </tbody>
                </table>
                <ScrollBar orientation="horizontal" />
            </ScrollArea>
        </div>
    );
};

export default TrendDataTable;
