import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
    AlertTriangle, CheckCircle2, FileSpreadsheet, FileText, Loader2, Upload,
} from 'lucide-react';
import {
    Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import { tagsApi, type TagSheetImportResult, type TagSheetProblem, type TagSheetRow } from '@/api/tags';
import { showApiError } from '@/lib/api-error-handler';
import { saveBlob } from '@/lib/download';
import { cn } from '@/lib/utils';

interface Props {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    gatewayId: number;
    driverType?: string;
}

type Step = 'pick' | 'preview';

/**
 * Importing tags: an Excel or CSV sheet, previewed before anything is written.
 *
 * The file goes to the server twice. The first time only to ask what would
 * happen — which rows create a tag, which update one and what changes, which
 * are wrong and why, in the user's language and on the spreadsheet's own line
 * numbers. The second time, when the user agrees, to write it, all or nothing.
 *
 * The declaration-text import is kept, in the second tab, for people pasting
 * from TIA Portal or Codesys.
 */
export default function TagImportDialog({ open, onOpenChange, gatewayId, driverType }: Props) {
    const { t, i18n } = useTranslation();
    const queryClient = useQueryClient();
    const inputRef = useRef<HTMLInputElement>(null);

    const [step, setStep] = useState<Step>('pick');
    const [file, setFile] = useState<File | null>(null);
    const [preview, setPreview] = useState<TagSheetImportResult | null>(null);
    const [busy, setBusy] = useState(false);
    const [onlyProblems, setOnlyProblems] = useState(false);
    const [dragging, setDragging] = useState(false);

    // Declaration text (the old import)
    const [text, setText] = useState('');
    const [historize, setHistorize] = useState(false);
    const [textResult, setTextResult] = useState<{ created: number; updated: number; errors?: string[] } | null>(null);

    const lang = i18n.language?.startsWith('it') ? 'it' : 'en';

    const reset = () => {
        setStep('pick');
        setFile(null);
        setPreview(null);
        setOnlyProblems(false);
        setTextResult(null);
    };

    const close = (v: boolean) => {
        if (!v) reset();
        onOpenChange(v);
    };

    const refreshTags = () => queryClient.invalidateQueries({ queryKey: ['tags'] });

    const analyse = async (f: File) => {
        setFile(f);
        setBusy(true);
        try {
            const res = await tagsApi.importSheet(gatewayId, f, false);
            setPreview(res);
            setOnlyProblems(res.invalid > 0);
            setStep('preview');
        } catch (e) {
            showApiError(e, t('tagImport.read_failed'));
        } finally {
            setBusy(false);
        }
    };

    const apply = async () => {
        if (!file) return;
        setBusy(true);
        try {
            const res = await tagsApi.importSheet(gatewayId, file, true);
            if (res.applied) {
                toast.success(t('tagImport.done', { created: res.created, updated: res.updated }));
                await refreshTags();
                close(false);
            } else {
                // Something changed between the preview and now (another user,
                // another tab). Show the new state instead of pretending.
                setPreview(res);
                toast.warning(t('tagImport.changed_since_preview'));
            }
        } catch (e) {
            showApiError(e, t('tagImport.apply_failed'));
        } finally {
            setBusy(false);
        }
    };

    const download = async (format: 'xlsx' | 'csv') => {
        try {
            const { blob, filename } = await tagsApi.downloadSheet(gatewayId, format, lang, true);
            saveBlob(blob, filename);
        } catch (e) {
            showApiError(e, t('tagImport.template_failed'));
        }
    };

    const importText = async () => {
        setBusy(true);
        setTextResult(null);
        try {
            const res = await tagsApi.importTags(gatewayId, text, historize);
            setTextResult(res);
            if (res.created > 0 || res.updated > 0) {
                await refreshTags();
                toast.success(t('tagImport.done', { created: res.created, updated: res.updated }));
            }
        } catch (e) {
            showApiError(e, t('tagImport.apply_failed'));
        } finally {
            setBusy(false);
        }
    };

    const problemText = (p: TagSheetProblem) =>
        t(`tagImport.problem.${p.code}`, {
            field: p.field ? t(`tagImport.field.${p.field}`) : '',
            value: p.value ?? '',
            defaultValue: p.code,
        });

    const sheetProblems = preview?.problems ?? [];
    const blockingSheet = sheetProblems.some((p) => p.code !== 'unknown_column');
    const rows = (preview?.rows ?? []).filter((r) => !onlyProblems || r.action === 'invalid');
    const toWrite = (preview?.created ?? 0) + (preview?.updated ?? 0);
    const canApply = !!preview && preview.invalid === 0 && !blockingSheet && toWrite > 0;

    return (
        <Dialog open={open} onOpenChange={close}>
            <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>{t('tagImport.title')}</DialogTitle>
                    <DialogDescription>{t('tagImport.subtitle')}</DialogDescription>
                </DialogHeader>

                <Tabs defaultValue="file">
                    <TabsList>
                        <TabsTrigger value="file" className="gap-2"><FileSpreadsheet size={14} /> {t('tagImport.tab_file')}</TabsTrigger>
                        <TabsTrigger value="text" className="gap-2"><FileText size={14} /> {t('tagImport.tab_text')}</TabsTrigger>
                    </TabsList>

                    <TabsContent value="file" className="space-y-4 pt-2">
                        {step === 'pick' && (
                            <>
                                <div
                                    role="button"
                                    tabIndex={0}
                                    onClick={() => inputRef.current?.click()}
                                    onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') inputRef.current?.click(); }}
                                    onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
                                    onDragLeave={() => setDragging(false)}
                                    onDrop={(e) => {
                                        e.preventDefault();
                                        setDragging(false);
                                        const f = e.dataTransfer.files?.[0];
                                        if (f) void analyse(f);
                                    }}
                                    className={cn(
                                        'border-2 border-dashed rounded-lg p-8 flex flex-col items-center gap-3 text-center cursor-pointer transition-colors',
                                        dragging ? 'border-primary bg-primary/5' : 'border-border hover:bg-muted/40',
                                    )}
                                >
                                    {busy ? <Loader2 className="animate-spin text-muted-foreground" /> : <Upload className="text-muted-foreground" />}
                                    <div>
                                        <p className="font-medium">{busy ? t('tagImport.reading') : t('tagImport.drop')}</p>
                                        <p className="text-sm text-muted-foreground">{t('tagImport.drop_hint')}</p>
                                    </div>
                                    <input
                                        ref={inputRef}
                                        type="file"
                                        accept=".xlsx,.csv,.txt,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
                                        className="hidden"
                                        onChange={(e) => {
                                            const f = e.target.files?.[0];
                                            e.target.value = '';
                                            if (f) void analyse(f);
                                        }}
                                    />
                                </div>

                                <div className="rounded-md bg-muted/50 p-3 text-sm space-y-2">
                                    <p className="font-medium">{t('tagImport.template_title')}</p>
                                    <p className="text-muted-foreground">
                                        {t('tagImport.template_desc', { driver: driverType ?? '' })}
                                    </p>
                                    <div className="flex flex-wrap gap-2">
                                        <Button size="sm" variant="outline" className="gap-2" onClick={() => void download('xlsx')}>
                                            <FileSpreadsheet size={14} /> {t('tagImport.template_xlsx')}
                                        </Button>
                                        <Button size="sm" variant="ghost" className="gap-2" onClick={() => void download('csv')}>
                                            <FileText size={14} /> {t('tagImport.template_csv')}
                                        </Button>
                                    </div>
                                </div>
                            </>
                        )}

                        {step === 'preview' && preview && (
                            <>
                                <div className="flex flex-wrap items-center gap-2 text-sm">
                                    <span className="text-muted-foreground mr-1">{file?.name}</span>
                                    <Badge className="bg-emerald-600 hover:bg-emerald-600">{t('tagImport.count_create', { count: preview.created })}</Badge>
                                    <Badge className="bg-blue-600 hover:bg-blue-600">{t('tagImport.count_update', { count: preview.updated })}</Badge>
                                    <Badge variant="secondary">{t('tagImport.count_unchanged', { count: preview.unchanged })}</Badge>
                                    {preview.invalid > 0 && (
                                        <Badge variant="destructive">{t('tagImport.count_invalid', { count: preview.invalid })}</Badge>
                                    )}
                                </div>

                                {sheetProblems.length > 0 && (
                                    <div className={cn(
                                        'rounded-md p-3 text-sm border',
                                        blockingSheet ? 'bg-destructive/10 border-destructive/30' : 'bg-amber-50 border-amber-200 text-amber-900 dark:bg-amber-950/30 dark:text-amber-200',
                                    )}>
                                        <ul className="space-y-1">
                                            {sheetProblems.map((p, i) => (
                                                <li key={i} className="flex gap-2"><AlertTriangle size={14} className="mt-0.5 shrink-0" /> {problemText(p)}</li>
                                            ))}
                                        </ul>
                                    </div>
                                )}

                                {preview.invalid > 0 ? (
                                    <p className="text-sm text-destructive font-medium">{t('tagImport.fix_first', { count: preview.invalid })}</p>
                                ) : !blockingSheet && toWrite === 0 ? (
                                    <p className="text-sm text-muted-foreground">{t('tagImport.nothing_to_do')}</p>
                                ) : null}

                                {preview.rows.length > 0 && (
                                    <>
                                        {preview.invalid > 0 && (
                                            <div className="flex items-center gap-2">
                                                <Switch id="only-problems" checked={onlyProblems} onCheckedChange={setOnlyProblems} />
                                                <Label htmlFor="only-problems">{t('tagImport.only_problems')}</Label>
                                            </div>
                                        )}
                                        <div className="border rounded-md max-h-[45vh] overflow-auto">
                                            <Table>
                                                <TableHeader>
                                                    <TableRow>
                                                        <TableHead className="w-16">{t('tagImport.col_line')}</TableHead>
                                                        <TableHead>{t('tagImport.field.alias')}</TableHead>
                                                        <TableHead>{t('tagImport.field.address')}</TableHead>
                                                        <TableHead className="w-20">{t('tagImport.field.data_type')}</TableHead>
                                                        <TableHead className="w-32">{t('tagImport.col_action')}</TableHead>
                                                        <TableHead>{t('tagImport.col_detail')}</TableHead>
                                                    </TableRow>
                                                </TableHeader>
                                                <TableBody>
                                                    {rows.map((r) => (
                                                        <PreviewRow key={r.line} row={r} problemText={problemText} />
                                                    ))}
                                                </TableBody>
                                            </Table>
                                        </div>
                                    </>
                                )}

                                <DialogFooter className="gap-2">
                                    <Button variant="outline" onClick={reset} disabled={busy}>{t('tagImport.other_file')}</Button>
                                    <Button onClick={() => void apply()} disabled={!canApply || busy} className="gap-2">
                                        {busy && <Loader2 size={14} className="animate-spin" />}
                                        {t('tagImport.apply', { count: toWrite })}
                                    </Button>
                                </DialogFooter>
                            </>
                        )}
                    </TabsContent>

                    <TabsContent value="text" className="space-y-4 pt-2">
                        <p className="text-sm text-muted-foreground">
                            {t('tagImport.text_desc')} <code className="bg-muted px-1 rounded">Alias : DataType AT Address;</code>
                        </p>
                        <p className="text-xs text-muted-foreground">{t('tagImport.text_types')}</p>
                        <textarea
                            className="w-full h-56 p-3 font-mono text-sm border rounded-md bg-background"
                            placeholder={driverType === 'S7'
                                ? 'Totalizzatore_1 : BOOL AT DB1.DBX0.0;\nPortata_Misuratore : INT AT DB1.DBW2;\nHMI_PortataTotale : REAL AT DB1.DBD8;'
                                : 'HMI_CFG_HBeg_1 : DINT AT 42095;\nHMI_PortataTotEM1 : REAL AT 42131;\nDO_Valvola_1 : BOOL AT 00001;'}
                            value={text}
                            onChange={(e) => setText(e.target.value)}
                        />
                        <div className="flex items-center gap-2">
                            <Switch id="historize-text" checked={historize} onCheckedChange={setHistorize} />
                            <Label htmlFor="historize-text">{t('tagImport.text_historize')}</Label>
                        </div>
                        {textResult && (
                            <div className={cn(
                                'p-3 rounded-md text-sm border',
                                textResult.errors?.length ? 'bg-destructive/10 border-destructive/30' : 'bg-emerald-50 border-emerald-200 dark:bg-emerald-950/30',
                            )}>
                                {textResult.errors?.length ? (
                                    <>
                                        <p className="font-medium">{t('tagImport.text_nothing')}</p>
                                        <ul className="list-disc list-inside max-h-32 overflow-auto mt-1">
                                            {textResult.errors.map((err, i) => <li key={i}>{err}</li>)}
                                        </ul>
                                    </>
                                ) : (
                                    <p>{t('tagImport.done', { created: textResult.created, updated: textResult.updated })}</p>
                                )}
                            </div>
                        )}
                        <DialogFooter>
                            <Button onClick={() => void importText()} disabled={busy || !text.trim()}>
                                {busy ? t('tagImport.importing') : t('common.import')}
                            </Button>
                        </DialogFooter>
                    </TabsContent>
                </Tabs>
            </DialogContent>
        </Dialog>
    );
}

function PreviewRow({ row, problemText }: { row: TagSheetRow; problemText: (p: TagSheetProblem) => string }) {
    const { t } = useTranslation();
    const style: Record<TagSheetRow['action'], string> = {
        create: 'text-emerald-700 dark:text-emerald-400',
        update: 'text-blue-700 dark:text-blue-400',
        unchanged: 'text-muted-foreground',
        invalid: 'text-destructive',
    };
    return (
        <TableRow className={row.action === 'invalid' ? 'bg-destructive/5' : undefined}>
            <TableCell className="tabular-nums text-muted-foreground">{row.line}</TableCell>
            <TableCell className="font-medium">{row.alias || '—'}</TableCell>
            <TableCell className="font-mono text-xs">{row.address || '—'}</TableCell>
            <TableCell className="font-mono text-xs">{row.data_type || '—'}</TableCell>
            <TableCell className={cn('text-sm font-medium', style[row.action])}>
                <span className="inline-flex items-center gap-1">
                    {row.action === 'invalid' ? <AlertTriangle size={13} /> : row.action !== 'unchanged' && <CheckCircle2 size={13} />}
                    {t(`tagImport.action.${row.action}`)}
                </span>
            </TableCell>
            <TableCell className="text-xs">
                {row.problems?.length
                    ? row.problems.map((p, i) => <div key={i} className="text-destructive">{problemText(p)}</div>)
                    : row.changes?.length
                        ? t('tagImport.changes', { fields: row.changes.map((c) => t(`tagImport.field.${c}`)).join(', ') })
                        : null}
            </TableCell>
        </TableRow>
    );
}
