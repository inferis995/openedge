import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { FlaskConical, Loader2 } from 'lucide-react';
import { decodersApi, type DecoderTestResult, type PayloadEncoding } from '@/api/decoders';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';

export interface DecoderSettings {
    /** '' = no decoder: payloads are plain values or JSON. */
    decoder_preset: string;
    decoder_script: string;
    /** Minutes without data before the gateway's tags are marked bad; 0 = off. */
    stale_after_minutes: number;
}

type Mode = 'none' | 'preset' | 'custom';

const modeOf = (s: DecoderSettings): Mode =>
    s.decoder_script.trim() ? 'custom' : s.decoder_preset ? 'preset' : 'none';

/** Flattens decoded values to "path = value" rows: the paths a tag reads. */
function flatten(v: unknown, prefix = '', out: [string, string][] = []): [string, string][] {
    if (v !== null && typeof v === 'object') {
        for (const [k, child] of Object.entries(v as Record<string, unknown>)) {
            flatten(child, prefix ? `${prefix}.${k}` : k, out);
        }
    } else {
        out.push([prefix, typeof v === 'number' ? String(Math.round(v * 1e6) / 1e6) : String(v)]);
    }
    return out;
}

/**
 * The payload decoder of an MQTT gateway.
 *
 * A device that does not send JSON — a binary or base64 frame, several
 * readings in one message — is read through a decoder: a shipped preset or a
 * JavaScript function. The test panel runs it on a pasted message, so the
 * keys a tag can read, and the time of every reading, are seen before the
 * gateway relies on it.
 */
export default function MqttDecoderSection({ value, onChange }: {
    value: DecoderSettings;
    onChange: (next: DecoderSettings) => void;
}) {
    const { t, i18n } = useTranslation();
    const { data: presets = [] } = useQuery({ queryKey: ['decoder-presets'], queryFn: decodersApi.presets, staleTime: Infinity });
    const mode = modeOf(value);
    const [topic, setTopic] = useState('');
    const [payload, setPayload] = useState('');
    const [encoding, setEncoding] = useState<PayloadEncoding>('text');
    const [result, setResult] = useState<DecoderTestResult | null>(null);
    const [testing, setTesting] = useState(false);

    const setMode = (m: Mode) => {
        if (m === 'none') onChange({ ...value, decoder_preset: '', decoder_script: '' });
        else if (m === 'preset') onChange({ ...value, decoder_preset: value.decoder_preset || presets[0]?.id || '', decoder_script: '' });
        else onChange({ ...value, decoder_script: value.decoder_script || presets.find(p => p.id === value.decoder_preset)?.script || 'function decode(msg) {\n  // msg.text, msg.bytes (Uint8Array), msg.topic\n  return [{ ts: Date.now(), values: {} }];\n}\n' });
    };

    const runTest = async () => {
        setTesting(true);
        setResult(null);
        try {
            setResult(await decodersApi.test({
                preset: mode === 'preset' ? value.decoder_preset : undefined,
                script: mode === 'custom' ? value.decoder_script : undefined,
                payload, payload_encoding: encoding, topic,
            }));
        } catch (e) {
            const ax = e as { response?: { data?: { error?: string } }; message?: string };
            setResult({ samples: [], error: ax.response?.data?.error ?? ax.message ?? 'error' });
        } finally {
            setTesting(false);
        }
    };

    const rows = useMemo(() => (result?.samples ?? []).map(s => ({
        time: new Date(s.ts).toLocaleString(i18n.language), values: flatten(s.values),
    })), [result, i18n.language]);

    return (
        <div className="space-y-3 border rounded-md p-3 bg-muted/30">
            <div>
                <p className="text-sm font-semibold">{t('decoder.title')}</p>
                <p className="text-[11px] text-muted-foreground">{t('decoder.hint')}</p>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div className="grid gap-1">
                    <Label className="text-xs">{t('decoder.mode')}</Label>
                    <Select value={mode} onValueChange={(v) => setMode(v as Mode)}>
                        <SelectTrigger><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value="none">{t('decoder.mode_none')}</SelectItem>
                            <SelectItem value="preset" disabled={presets.length === 0}>{t('decoder.mode_preset')}</SelectItem>
                            <SelectItem value="custom">{t('decoder.mode_custom')}</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
                {mode === 'preset' && (
                    <div className="grid gap-1">
                        <Label className="text-xs">{t('decoder.preset')}</Label>
                        <Select value={value.decoder_preset} onValueChange={(v) => onChange({ ...value, decoder_preset: v })}>
                            <SelectTrigger><SelectValue /></SelectTrigger>
                            <SelectContent>
                                {presets.map(p => <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>)}
                            </SelectContent>
                        </Select>
                    </div>
                )}
            </div>

            {mode === 'preset' && (
                <Button type="button" variant="outline" size="sm" onClick={() => setMode('custom')}>{t('decoder.customize')}</Button>
            )}

            {mode === 'custom' && (
                <div className="grid gap-1">
                    <Label className="text-xs">{t('decoder.script')}</Label>
                    <Textarea className="font-mono text-xs min-h-[220px]" spellCheck={false}
                        value={value.decoder_script}
                        onChange={(e) => onChange({ ...value, decoder_script: e.target.value })} />
                    <p className="text-[11px] text-muted-foreground">{t('decoder.script_hint')}</p>
                </div>
            )}

            {mode !== 'none' && (
                <div className="grid gap-1 sm:max-w-xs">
                    <Label className="text-xs">{t('decoder.stale_after')}</Label>
                    <Input type="number" min={0} value={value.stale_after_minutes}
                        onChange={(e) => {
                            const n = parseFloat(e.target.value);
                            onChange({ ...value, stale_after_minutes: Number.isFinite(n) && n >= 0 ? n : 0 });
                        }} />
                    <p className="text-[11px] text-muted-foreground">{t('decoder.stale_after_hint')}</p>
                </div>
            )}

            {mode !== 'none' && (
                <div className="space-y-2 border-t pt-3">
                    <p className="text-xs font-semibold flex items-center gap-1.5"><FlaskConical size={14} /> {t('decoder.test_title')}</p>
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                        <Input className="sm:col-span-2 text-xs" placeholder={t('decoder.test_topic')} value={topic} onChange={(e) => setTopic(e.target.value)} />
                        <Select value={encoding} onValueChange={(v) => setEncoding(v as PayloadEncoding)}>
                            <SelectTrigger className="text-xs"><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value="text">{t('decoder.enc_text')}</SelectItem>
                                <SelectItem value="hex">{t('decoder.enc_hex')}</SelectItem>
                                <SelectItem value="base64">{t('decoder.enc_base64')}</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>
                    <Textarea className="font-mono text-xs min-h-[80px]" spellCheck={false}
                        placeholder={t('decoder.test_payload')} value={payload} onChange={(e) => setPayload(e.target.value)} />
                    <Button type="button" size="sm" className="gap-1" disabled={testing || !payload.trim()} onClick={runTest}>
                        {testing ? <Loader2 size={14} className="animate-spin" /> : <FlaskConical size={14} />} {t('decoder.test_run')}
                    </Button>
                    {result?.error && <p role="alert" className="text-xs text-destructive break-words">{result.error}</p>}
                    {result && !result.error && rows.length === 0 && <p className="text-xs text-muted-foreground">{t('decoder.test_empty')}</p>}
                    {rows.length > 0 && (
                        <div className="max-h-72 overflow-auto rounded border bg-background text-xs">
                            {rows.map((r, i) => (
                                <div key={i} className="border-b last:border-b-0 p-2">
                                    <p className="font-semibold mb-1">{t('decoder.reading_at', { time: r.time })}</p>
                                    <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 font-mono">
                                        {r.values.map(([k, v]) => (
                                            <div key={k} className="contents">
                                                <span className="text-muted-foreground">{k}</span>
                                                <span className="break-all">{v}</span>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                    {result?.logs && result.logs.length > 0 && (
                        <pre className="text-[11px] bg-muted rounded p-2 overflow-auto max-h-32">{result.logs.join('\n')}</pre>
                    )}
                    <p className="text-[11px] text-muted-foreground">{t('decoder.tag_hint')}</p>
                </div>
            )}
        </div>
    );
}
