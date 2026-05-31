import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Download, FileText, BellRing, ShieldCheck } from 'lucide-react';

import api from '@/api/client';
import { tagsApi } from '@/api/tags';
import { useAuthStore } from '@/stores/useAuthStore';
import { showApiError } from '@/lib/api-error-handler';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

// Default ISO-8601 helpers used by the date inputs. We work in the
// operator's LOCAL timezone in the UI; the API converts back to UTC.
const toLocalInput = (d: Date) => {
    const pad = (n: number) => `${n}`.padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
};
const fromLocalInput = (s: string): string => new Date(s).toISOString();

const defaultEnd = () => toLocalInput(new Date());
const defaultStart = () => {
    const d = new Date();
    d.setHours(d.getHours() - 24);
    return toLocalInput(d);
};

interface ExportCardProps {
    icon: React.ReactNode;
    title: string;
    description: string;
    onExport: () => void;
    extraControls?: React.ReactNode;
    busy?: boolean;
    disabled?: boolean;
}

const ExportCard = ({ icon, title, description, onExport, extraControls, busy, disabled }: ExportCardProps) => (
    <div className="rounded-md border bg-card p-4 space-y-3">
        <div className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">{icon}{title}</div>
        <p className="text-sm">{description}</p>
        {extraControls}
        <Button onClick={onExport} disabled={busy || disabled} className="gap-2">
            <Download size={16} /> {busy ? 'Preparing...' : 'Download CSV'}
        </Button>
    </div>
);

const ReportsPage = () => {
    const { isGlobalAdmin } = useAuthStore();

    const [start, setStart] = useState(defaultStart());
    const [end, setEnd] = useState(defaultEnd());
    const [selectedTags, setSelectedTags] = useState<number[]>([]);
    const [busy, setBusy] = useState<string | null>(null);

    const { data: tags = [] } = useQuery({
        queryKey: ['tags-with-hierarchy'],
        queryFn: tagsApi.getAllWithHierarchy,
        staleTime: 60_000,
    });

    const range = useMemo(() => {
        try {
            return { start: fromLocalInput(start), end: fromLocalInput(end) };
        } catch {
            return null;
        }
    }, [start, end]);

    // Triggers a browser download for a CSV endpoint, attaching the
    // Authorization header axios already injects. We use the axios
    // client directly (rather than building a URL with the token in a
    // query string) so the JWT never leaks into browser history.
    const download = async (path: string, filename: string, params: Record<string, string>) => {
        if (!range) {
            showApiError(new Error('Invalid date range'), 'Invalid date range');
            return;
        }
        setBusy(path);
        try {
            const res = await api.get(path, {
                params: { ...params, start: range.start, end: range.end },
                responseType: 'blob',
            });
            const url = window.URL.createObjectURL(new Blob([res.data], { type: 'text/csv' }));
            const a = document.createElement('a');
            a.href = url;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            a.remove();
            window.URL.revokeObjectURL(url);
        } catch (e) {
            showApiError(e, 'Export failed');
        } finally {
            setBusy(null);
        }
    };

    const toggleTag = (id: number) =>
        setSelectedTags((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));

    return (
        <div className="space-y-6">
            <div>
                <h2 className="text-2xl font-bold tracking-tight flex items-center gap-2">
                    <FileText size={22} /> Reports
                </h2>
                <p className="text-muted-foreground">
                    Export historian, alarms and audit data as CSV. Files open directly in Excel /
                    LibreOffice and import cleanly into BI tools.
                </p>
            </div>

            <div className="rounded-md border bg-card p-4 grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="grid gap-1">
                    <Label htmlFor="rep-start">Start (local time)</Label>
                    <Input id="rep-start" type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} />
                </div>
                <div className="grid gap-1">
                    <Label htmlFor="rep-end">End (local time)</Label>
                    <Input id="rep-end" type="datetime-local" value={end} onChange={(e) => setEnd(e.target.value)} />
                </div>
                <p className="text-xs text-muted-foreground md:col-span-2">
                    Max range: 90 days. The API converts the times to UTC before querying.
                </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">

                <ExportCard
                    icon={<FileText size={16} />}
                    title="Tag history"
                    description="Raw historian samples. Filter by selecting one or more tags below."
                    busy={busy === '/reports/history.csv'}
                    onExport={() => download('/reports/history.csv',
                        `history-${Date.now()}.csv`,
                        selectedTags.length ? { tag_ids: selectedTags.join(',') } : {})}
                    extraControls={
                        <div className="text-xs text-muted-foreground">
                            {selectedTags.length === 0
                                ? 'All tags will be included (large export possible).'
                                : `${selectedTags.length} tag${selectedTags.length === 1 ? '' : 's'} selected.`}
                        </div>
                    }
                />

                <ExportCard
                    icon={<BellRing size={16} />}
                    title="Alarm events"
                    description="Every alarm trigger / clear within the range with severity, value and message."
                    busy={busy === '/reports/alarms.csv'}
                    onExport={() => download('/reports/alarms.csv', `alarms-${Date.now()}.csv`, {})}
                />

                {isGlobalAdmin() && (
                    <ExportCard
                        icon={<ShieldCheck size={16} />}
                        title="Audit log"
                        description="System-wide actions (logins, tag writes, recipe loads, ...). Global admin only."
                        busy={busy === '/reports/audit.csv'}
                        onExport={() => download('/reports/audit.csv', `audit-${Date.now()}.csv`, {})}
                    />
                )}

            </div>

            <div className="rounded-md border bg-card p-4">
                <div className="flex items-center justify-between mb-3">
                    <h3 className="font-semibold">Tag filter for history export</h3>
                    {selectedTags.length > 0 && (
                        <Button variant="outline" size="sm" onClick={() => setSelectedTags([])}>Clear</Button>
                    )}
                </div>
                <div className="max-h-72 overflow-auto border rounded-md divide-y">
                    {tags.length === 0 ? (
                        <p className="p-4 text-sm text-muted-foreground">No tags loaded.</p>
                    ) : tags.map((t) => (
                        <label key={t.id} className="flex items-center gap-3 px-3 py-2 hover:bg-muted/30 cursor-pointer">
                            <input
                                type="checkbox"
                                className="accent-primary"
                                checked={selectedTags.includes(t.id)}
                                onChange={() => toggleTag(t.id)}
                            />
                            <span className="font-mono text-xs flex-1 truncate">{t.alias ?? t.code}</span>
                            <span className="text-xs text-muted-foreground">{t.gateway_name}</span>
                            <span className="text-xs text-muted-foreground">{t.data_type}</span>
                        </label>
                    ))}
                </div>
            </div>
        </div>
    );
};

export default ReportsPage;
