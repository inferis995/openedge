import { useEffect, useMemo, useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { AlertCircle, CheckCircle2, Download, HelpCircle, PauseCircle, RefreshCw, Search } from 'lucide-react';
import { inventoryApi, InventoryDevice, InventoryResponse } from '@/api/inventory';
import { toast } from 'sonner';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { startLoad } from '@/lib/startLoad';

// Lo stato che il driver ha riportato per ultimo. Un gateway disabilitato non è
// un guasto — nessuno gli sta chiedendo niente — e mostrarlo in rosso è il modo
// più rapido per far smettere di guardare questa pagina.
function HealthBadge({ device }: { device: InventoryDevice }) {
    const { t } = useTranslation();
    if (!device.enabled) {
        return (
            <Badge variant="outline" className="gap-1 text-muted-foreground">
                <PauseCircle size={12} /> {t('inventoryPage.health_disabled')}
            </Badge>
        );
    }
    switch (device.health) {
        case 'online':
            return (
                <Badge variant="outline" className="gap-1 border-emerald-500/40 text-emerald-600">
                    <CheckCircle2 size={12} /> {t('inventoryPage.health_online')}
                </Badge>
            );
        case 'offline':
            return (
                <Badge variant="outline" className="gap-1 border-red-500/40 text-red-600">
                    <AlertCircle size={12} /> {t('inventoryPage.health_offline')}
                </Badge>
            );
        case 'error':
            return (
                <Badge variant="outline" className="gap-1 border-red-500/40 text-red-600">
                    <AlertCircle size={12} /> {t('inventoryPage.health_error')}
                </Badge>
            );
        default:
            return (
                <Badge variant="outline" className="gap-1 text-muted-foreground">
                    <HelpCircle size={12} /> {t('inventoryPage.health_unknown')}
                </Badge>
            );
    }
}

const formatSeen = (iso?: string): string => {
    if (!iso) return i18n.t('inventoryPage.never');
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return i18n.t('inventoryPage.never');
    return d.toLocaleString(i18n.language, { dateStyle: 'short', timeStyle: 'short' });
};

export function InventoryPage() {
    const { t } = useTranslation();
    const [data, setData] = useState<InventoryResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [exporting, setExporting] = useState(false);
    const [query, setQuery] = useState('');

    // Fetch without flipping the loading flag first: on mount it is already true.
    const fetchInventory = async () => {
        try {
            setData(await inventoryApi.get());
        } catch (err) {
            console.error('inventory', err);
            toast.error(t('inventoryPage.load_failed'));
        } finally {
            setLoading(false);
        }
    };

    const load = () => {
        setLoading(true);
        return fetchInventory();
    };

    useEffect(() => {
        startLoad(fetchInventory);
    }, []);

    const handleExport = async () => {
        setExporting(true);
        try {
            const blob = await inventoryApi.exportCsv();
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `${t('inventoryPage.export_filename')}-${new Date().toISOString().slice(0, 10)}.csv`;
            a.click();
            URL.revokeObjectURL(url);
        } catch (err) {
            console.error('inventory export', err);
            toast.error(t('inventoryPage.export_failed'));
        } finally {
            setExporting(false);
        }
    };

    const devices = useMemo(() => {
        const all = data?.devices ?? [];
        const q = query.trim().toLowerCase();
        if (!q) return all;
        return all.filter((d) =>
            [d.name, d.site, d.area, d.organization, d.protocol, d.endpoint]
                .some((field) => (field ?? '').toLowerCase().includes(q)),
        );
    }, [data, query]);

    const summary = data?.summary;

    return (
        <div className="space-y-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                    <h1 className="text-2xl font-semibold">{t('inventoryPage.title')}</h1>
                    <p className="text-sm text-muted-foreground">
                        {t('inventoryPage.subtitle')}
                    </p>
                </div>
                <div className="flex gap-2">
                    <Button variant="outline" onClick={() => void load()} disabled={loading} className="gap-2">
                        <RefreshCw size={16} className={loading ? 'animate-spin' : ''} /> {t('inventoryPage.refresh')}
                    </Button>
                    <Button onClick={() => void handleExport()} disabled={exporting || loading} className="gap-2">
                        <Download size={16} /> {exporting ? t('inventoryPage.exporting') : t('inventoryPage.export_csv')}
                    </Button>
                </div>
            </div>

            {summary && (
                <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
                    {[
                        { label: 'inventoryPage.tile_devices', value: summary.devices },
                        { label: 'inventoryPage.tile_online', value: summary.online },
                        { label: 'inventoryPage.tile_offline', value: summary.offline },
                        { label: 'inventoryPage.tile_unknown', value: summary.unknown },
                        { label: 'inventoryPage.tile_tags', value: summary.tags },
                    ].map((tile) => (
                        <Card key={tile.label}>
                            <CardHeader className="pb-1">
                                <CardTitle className="text-xs font-medium text-muted-foreground">
                                    {t(tile.label)}
                                </CardTitle>
                            </CardHeader>
                            <CardContent>
                                <p className="font-mono text-2xl font-semibold">{tile.value}</p>
                            </CardContent>
                        </Card>
                    ))}
                </div>
            )}

            <Card>
                <CardHeader className="pb-3">
                    <div className="relative max-w-sm">
                        <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" />
                        <Input
                            value={query}
                            onChange={(e) => setQuery(e.target.value)}
                            placeholder={t('inventoryPage.search_placeholder')}
                            className="pl-8"
                        />
                    </div>
                </CardHeader>
                <CardContent className="overflow-x-auto">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>{t('common.gateway')}</TableHead>
                                <TableHead>{t('inventoryPage.col_site_area')}</TableHead>
                                <TableHead>{t('inventoryPage.col_protocol')}</TableHead>
                                <TableHead>{t('inventoryPage.col_address')}</TableHead>
                                <TableHead>{t('inventoryPage.col_status')}</TableHead>
                                <TableHead>{t('inventoryPage.col_last_seen')}</TableHead>
                                <TableHead className="text-right">{t('inventoryPage.col_tags')}</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {loading && (
                                Array.from({ length: 5 }).map((_, i) => (
                                    <TableRow key={i}>
                                        {Array.from({ length: 7 }).map((__, j) => (
                                            <TableCell key={j}><Skeleton className="h-4 w-full" /></TableCell>
                                        ))}
                                    </TableRow>
                                ))
                            )}
                            {!loading && devices.length === 0 && (
                                <TableRow>
                                    <TableCell colSpan={7} className="py-8 text-center text-sm text-muted-foreground">
                                        {query ? t('inventoryPage.no_match') : t('inventoryPage.empty')}
                                    </TableCell>
                                </TableRow>
                            )}
                            {!loading && devices.map((d) => (
                                <TableRow key={d.gateway_id}>
                                    <TableCell className="font-medium">{d.name}</TableCell>
                                    <TableCell className="text-sm text-muted-foreground">
                                        {d.site} / {d.area}
                                    </TableCell>
                                    <TableCell><Badge variant="outline" className="text-xs">{d.protocol}</Badge></TableCell>
                                    <TableCell className="font-mono text-xs">{d.endpoint}</TableCell>
                                    <TableCell><HealthBadge device={d} /></TableCell>
                                    <TableCell className="text-sm text-muted-foreground">{formatSeen(d.health_seen_at)}</TableCell>
                                    <TableCell className="text-right font-mono text-sm">
                                        {d.tags}
                                        {d.historized_tags > 0 && (
                                            <span className="ml-1 text-xs text-muted-foreground">
                                                {t('inventoryPage.historized', { count: d.historized_tags })}
                                            </span>
                                        )}
                                    </TableCell>
                                </TableRow>
                            ))}
                        </TableBody>
                    </Table>
                </CardContent>
            </Card>
        </div>
    );
}

export default InventoryPage;
