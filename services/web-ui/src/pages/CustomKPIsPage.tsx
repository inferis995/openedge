import { useState, useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Target, Plus, Pencil, Trash2 } from 'lucide-react';

import {
    customKPIsApi, CustomKPI, CreateCustomKPIDto, AggregationType,
    AGGREGATION_LABELS, WINDOW_PRESETS,
} from '@/api/customKPIs';
import { tagsApi } from '@/api/tags';
import { showApiError, showApiSuccess } from '@/lib/api-error-handler';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog';

const CustomKPIsPage = () => {
    const qc = useQueryClient();
    const { data: kpis = [], isLoading } = useQuery({
        queryKey: ['custom-kpis'],
        queryFn: customKPIsApi.list,
    });
    const { data: tags = [] } = useQuery({
        queryKey: ['tags-with-hierarchy'],
        queryFn: tagsApi.getAllWithHierarchy,
        staleTime: 60_000,
    });

    const tagById = useMemo(() => {
        const m: Record<number, { alias?: string; code: string }> = {};
        tags.forEach((t) => { m[t.id] = { alias: t.alias, code: t.code }; });
        return m;
    }, [tags]);

    // Editor state.
    const [editorOpen, setEditorOpen] = useState(false);
    const [editingId, setEditingId] = useState<number | null>(null);
    const [name, setName] = useState('');
    const [tagID, setTagID] = useState<number | ''>('');
    const [aggregation, setAggregation] = useState<AggregationType>('avg');
    const [windowMinutes, setWindowMinutes] = useState<number>(1440);
    const [unit, setUnit] = useState('');
    const [multiplier, setMultiplier] = useState<string>('1');
    const [goodWhen, setGoodWhen] = useState<'up' | 'down'>('up');
    const [target, setTarget] = useState<string>('');
    const [active, setActive] = useState(true);

    const openCreate = () => {
        setEditingId(null);
        setName(''); setTagID(''); setAggregation('avg');
        setWindowMinutes(1440); setUnit(''); setMultiplier('1');
        setGoodWhen('up'); setTarget(''); setActive(true);
        setEditorOpen(true);
    };

    const openEdit = (k: CustomKPI) => {
        setEditingId(k.id);
        setName(k.name); setTagID(k.tag_id); setAggregation(k.aggregation);
        setWindowMinutes(k.window_minutes); setUnit(k.unit ?? '');
        setMultiplier(String(k.multiplier ?? 1));
        setGoodWhen(k.good_when); setTarget(k.target_value != null ? String(k.target_value) : '');
        setActive(k.active);
        setEditorOpen(true);
    };

    const saveMutation = useMutation({
        mutationFn: async () => {
            if (tagID === '') throw new Error('Seleziona un tag');
            const data: CreateCustomKPIDto = {
                name: name.trim(),
                tag_id: Number(tagID),
                aggregation,
                window_minutes: windowMinutes,
                unit: unit.trim(),
                multiplier: parseFloat(multiplier) || 1,
                good_when: goodWhen,
                target_value: target.trim() === '' ? null : parseFloat(target),
                active,
            };
            if (editingId !== null) await customKPIsApi.update(editingId, data);
            else await customKPIsApi.create(data);
        },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['custom-kpis'] });
            showApiSuccess('KPI salvato');
            setEditorOpen(false);
        },
        onError: (e) => showApiError(e, 'Salvataggio fallito'),
    });

    const deleteMutation = useMutation({
        mutationFn: (id: number) => customKPIsApi.delete(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['custom-kpis'] });
            showApiSuccess('KPI eliminato');
        },
        onError: (e) => showApiError(e, 'Eliminazione fallita'),
    });

    return (
        <div className="space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <h2 className="text-2xl font-bold tracking-tight flex items-center gap-2">
                        <Target size={22} /> KPI di Produzione
                    </h2>
                    <p className="text-muted-foreground">
                        Definisci metriche custom (pezzi/h, kWh per turno, OEE, ...) basate sui
                        tuoi tag. Appaiono automaticamente nella dashboard accanto ai KPI di sistema.
                    </p>
                </div>
                <Button onClick={openCreate} className="gap-2">
                    <Plus size={16} /> Nuovo KPI
                </Button>
            </div>

            <div className="rounded-md border bg-card">
                <Table>
                    <TableHeader>
                        <TableRow>
                            <TableHead>Nome</TableHead>
                            <TableHead>Tag</TableHead>
                            <TableHead>Aggregazione</TableHead>
                            <TableHead>Finestra</TableHead>
                            <TableHead>Target</TableHead>
                            <TableHead>Stato</TableHead>
                            <TableHead className="text-right">Azioni</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {isLoading ? (
                            <TableRow><TableCell colSpan={7} className="h-20 text-center">Caricamento…</TableCell></TableRow>
                        ) : kpis.length === 0 ? (
                            <TableRow><TableCell colSpan={7} className="h-20 text-center text-muted-foreground">
                                Nessun KPI custom. Crea il primo per vedere metriche di produzione sulla dashboard.
                            </TableCell></TableRow>
                        ) : kpis.map((k) => {
                            const tag = tagById[k.tag_id];
                            return (
                                <TableRow key={k.id}>
                                    <TableCell className="font-semibold">{k.name}</TableCell>
                                    <TableCell className="font-mono text-xs">{tag?.alias ?? tag?.code ?? `#${k.tag_id}`}</TableCell>
                                    <TableCell className="text-xs">{AGGREGATION_LABELS[k.aggregation]}</TableCell>
                                    <TableCell className="text-xs">
                                        {k.window_minutes >= 60
                                            ? `${(k.window_minutes / 60).toFixed(k.window_minutes % 60 ? 1 : 0)}h`
                                            : `${k.window_minutes}m`}
                                    </TableCell>
                                    <TableCell className="text-xs font-mono">
                                        {k.target_value != null
                                            ? `${k.good_when === 'down' ? '≤' : '≥'} ${k.target_value}${k.unit ?? ''}`
                                            : '—'}
                                    </TableCell>
                                    <TableCell>
                                        {k.active
                                            ? <Badge className="bg-emerald-500/10 text-emerald-500 border-none">Attivo</Badge>
                                            : <Badge className="bg-slate-500/10 text-slate-400 border-none">Disattivato</Badge>}
                                    </TableCell>
                                    <TableCell className="text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <Button variant="ghost" size="icon" title="Modifica"
                                                className="h-10 sm:h-8 w-10 sm:w-8 text-blue-500"
                                                onClick={() => openEdit(k)}>
                                                <Pencil size={16} />
                                            </Button>
                                            <Button variant="ghost" size="icon" title="Elimina"
                                                className="h-10 sm:h-8 w-10 sm:w-8 text-red-500"
                                                onClick={() => { if (confirm(`Eliminare KPI "${k.name}"?`)) deleteMutation.mutate(k.id); }}>
                                                <Trash2 size={16} />
                                            </Button>
                                        </div>
                                    </TableCell>
                                </TableRow>
                            );
                        })}
                    </TableBody>
                </Table>
            </div>

            <Dialog open={editorOpen} onOpenChange={setEditorOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>{editingId !== null ? 'Modifica KPI' : 'Nuovo KPI di produzione'}</DialogTitle>
                        <DialogDescription>
                            Scegli un tag + un'aggregazione + una finestra temporale. Il valore appare
                            sulla dashboard con freccia trend e (se imposti il target) colore verde/rosso.
                        </DialogDescription>
                    </DialogHeader>
                    <div className="grid gap-3 py-2">
                        <div className="grid gap-1">
                            <Label htmlFor="ck-name">Nome KPI</Label>
                            <Input id="ck-name" value={name} onChange={(e) => setName(e.target.value)}
                                placeholder="es. Pezzi prodotti turno" />
                        </div>
                        <div className="grid gap-1">
                            <Label htmlFor="ck-tag">Tag sorgente</Label>
                            <select id="ck-tag" value={tagID} onChange={(e) => setTagID(e.target.value === '' ? '' : Number(e.target.value))}
                                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
                                <option value="">— seleziona —</option>
                                {tags.map((t) => (
                                    <option key={t.id} value={t.id}>
                                        {t.alias ?? t.code} ({t.gateway_name ?? 'gw?'})
                                    </option>
                                ))}
                            </select>
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                            <div className="grid gap-1">
                                <Label htmlFor="ck-agg">Aggregazione</Label>
                                <select id="ck-agg" value={aggregation} onChange={(e) => setAggregation(e.target.value as AggregationType)}
                                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
                                    {Object.entries(AGGREGATION_LABELS).map(([k, v]) => (
                                        <option key={k} value={k}>{v}</option>
                                    ))}
                                </select>
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="ck-win">Finestra</Label>
                                <select id="ck-win" value={windowMinutes} onChange={(e) => setWindowMinutes(Number(e.target.value))}
                                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
                                    {WINDOW_PRESETS.map((p) => (
                                        <option key={p.value} value={p.value}>{p.label}</option>
                                    ))}
                                </select>
                            </div>
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                            <div className="grid gap-1">
                                <Label htmlFor="ck-unit">Unità</Label>
                                <Input id="ck-unit" value={unit} onChange={(e) => setUnit(e.target.value)}
                                    placeholder="es. pz, kWh, °C" />
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="ck-mult">Moltiplicatore</Label>
                                <Input id="ck-mult" type="number" step="0.001" value={multiplier}
                                    onChange={(e) => setMultiplier(e.target.value)}
                                    placeholder="1" />
                            </div>
                            <div className="grid gap-1">
                                <Label htmlFor="ck-good">Buono se</Label>
                                <select id="ck-good" value={goodWhen} onChange={(e) => setGoodWhen(e.target.value as 'up' | 'down')}
                                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm">
                                    <option value="up">↑ aumenta</option>
                                    <option value="down">↓ diminuisce</option>
                                </select>
                            </div>
                        </div>
                        <div className="grid gap-1">
                            <Label htmlFor="ck-target">Target (opzionale)</Label>
                            <div className="flex items-center gap-2">
                                <span className="text-sm text-muted-foreground font-mono w-5 text-right">
                                    {goodWhen === 'down' ? '≤' : '≥'}
                                </span>
                                <Input id="ck-target" type="number" step="0.01" value={target}
                                    onChange={(e) => setTarget(e.target.value)}
                                    placeholder="es. 500" />
                                {unit && <span className="text-sm text-muted-foreground">{unit}</span>}
                            </div>
                            <p className="text-xs text-muted-foreground">Vuoto = nessun target, colore neutro.</p>
                        </div>
                        <div className="flex items-center gap-3 pt-1">
                            <Switch checked={active} onCheckedChange={setActive} />
                            <Label>KPI attivo (visibile in dashboard)</Label>
                        </div>
                    </div>
                    <DialogFooter>
                        <Button onClick={() => saveMutation.mutate()}
                            disabled={!name.trim() || tagID === '' || saveMutation.isPending}>
                            {saveMutation.isPending ? 'Salvataggio…' : (editingId !== null ? 'Salva' : 'Crea')}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
};

export default CustomKPIsPage;
