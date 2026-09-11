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

// Lo stato che il driver ha riportato per ultimo. Un gateway disabilitato non è
// un guasto — nessuno gli sta chiedendo niente — e mostrarlo in rosso è il modo
// più rapido per far smettere di guardare questa pagina.
function HealthBadge({ device }: { device: InventoryDevice }) {
    if (!device.enabled) {
        return (
            <Badge variant="outline" className="gap-1 text-muted-foreground">
                <PauseCircle size={12} /> Disabilitato
            </Badge>
        );
    }
    switch (device.health) {
        case 'online':
            return (
                <Badge variant="outline" className="gap-1 border-emerald-500/40 text-emerald-600">
                    <CheckCircle2 size={12} /> Online
                </Badge>
            );
        case 'offline':
            return (
                <Badge variant="outline" className="gap-1 border-red-500/40 text-red-600">
                    <AlertCircle size={12} /> Offline
                </Badge>
            );
        case 'error':
            return (
                <Badge variant="outline" className="gap-1 border-red-500/40 text-red-600">
                    <AlertCircle size={12} /> Errore
                </Badge>
            );
        default:
            return (
                <Badge variant="outline" className="gap-1 text-muted-foreground">
                    <HelpCircle size={12} /> Mai contattato
                </Badge>
            );
    }
}

const formatSeen = (iso?: string): string => {
    if (!iso) return 'mai';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return 'mai';
    return d.toLocaleString('it-IT', { dateStyle: 'short', timeStyle: 'short' });
};

export function InventoryPage() {
    const [data, setData] = useState<InventoryResponse | null>(null);
    const [loading, setLoading] = useState(true);
    const [exporting, setExporting] = useState(false);
    const [query, setQuery] = useState('');

    const load = async () => {
        setLoading(true);
        try {
            setData(await inventoryApi.get());
        } catch (err) {
            console.error('inventory', err);
            toast.error('Impossibile caricare l’inventario.');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void load();
    }, []);

    const handleExport = async () => {
        setExporting(true);
        try {
            const blob = await inventoryApi.exportCsv();
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `inventario-dispositivi-${new Date().toISOString().slice(0, 10)}.csv`;
            a.click();
            URL.revokeObjectURL(url);
        } catch (err) {
            console.error('inventory export', err);
            toast.error('Esportazione non riuscita.');
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
                    <h1 className="text-2xl font-semibold">Inventario dispositivi</h1>
                    <p className="text-sm text-muted-foreground">
                        Tutto quello che è installato: indirizzo, protocollo, stato e ultimo contatto.
                        È il documento da consegnare a un cliente o a un ispettore.
                    </p>
                </div>
                <div className="flex gap-2">
                    <Button variant="outline" onClick={() => void load()} disabled={loading} className="gap-2">
                        <RefreshCw size={16} className={loading ? 'animate-spin' : ''} /> Aggiorna
                    </Button>
                    <Button onClick={() => void handleExport()} disabled={exporting || loading} className="gap-2">
                        <Download size={16} /> {exporting ? 'Esporto…' : 'Esporta CSV'}
                    </Button>
                </div>
            </div>

            {summary && (
                <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
                    {[
                        { label: 'Dispositivi', value: summary.devices },
                        { label: 'Online', value: summary.online },
                        { label: 'Non raggiungibili', value: summary.offline },
                        { label: 'Mai contattati', value: summary.unknown },
                        { label: 'Tag totali', value: summary.tags },
                    ].map((tile) => (
                        <Card key={tile.label}>
                            <CardHeader className="pb-1">
                                <CardTitle className="text-xs font-medium text-muted-foreground">
                                    {tile.label}
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
                            placeholder="Cerca per nome, sito, indirizzo, protocollo…"
                            className="pl-8"
                        />
                    </div>
                </CardHeader>
                <CardContent className="overflow-x-auto">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                <TableHead>Gateway</TableHead>
                                <TableHead>Sito / Area</TableHead>
                                <TableHead>Protocollo</TableHead>
                                <TableHead>Indirizzo</TableHead>
                                <TableHead>Stato</TableHead>
                                <TableHead>Ultimo contatto</TableHead>
                                <TableHead className="text-right">Tag</TableHead>
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
                                        {query ? 'Nessun dispositivo corrisponde alla ricerca.' : 'Nessun dispositivo censito.'}
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
                                                ({d.historized_tags} stor.)
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
