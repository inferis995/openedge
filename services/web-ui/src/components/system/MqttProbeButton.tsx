import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { CheckCircle2, Loader2, PlugZap, XCircle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { testMqttBroker, type MqttProbeResult } from '@/api/system';
import { showApiError } from '@/lib/api-error-handler';

interface Props {
    target: 'cloud' | 'external';
    host: string;
    port: number;
    username?: string;
    password?: string;
}

/**
 * "Test connection" for a broker, before saving it.
 *
 * The only way to know whether a cloud broker's settings were right used to be
 * to save them and read the historian's log. This connects the way the
 * platform will and says what went wrong in words: wrong host, port closed,
 * TLS, password.
 */
export default function MqttProbeButton({ target, host, port, username, password }: Props) {
    const { t } = useTranslation();
    const [busy, setBusy] = useState(false);
    const [result, setResult] = useState<MqttProbeResult | null>(null);

    const run = async () => {
        setBusy(true);
        setResult(null);
        try {
            setResult(await testMqttBroker({ target, host: host.trim(), port, username, password }));
        } catch (e) {
            showApiError(e);
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="space-y-2">
            <Button type="button" variant="outline" size="sm" className="gap-2" onClick={() => void run()} disabled={busy || !host.trim()}>
                {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <PlugZap className="h-3.5 w-3.5" />}
                {t('mqttSettings.test')}
            </Button>
            {result && (
                <div className={`text-xs rounded-md border p-2.5 space-y-1 ${result.ok
                    ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                    : 'border-destructive/30 bg-destructive/10 text-destructive'}`}>
                    <p className="font-medium flex items-center gap-1.5">
                        {result.ok ? <CheckCircle2 className="h-3.5 w-3.5" /> : <XCircle className="h-3.5 w-3.5" />}
                        {result.ok
                            ? t('mqttSettings.ok', { ms: result.latency_ms, transport: result.scheme === 'ssl' ? 'TLS' : 'TCP' })
                            : t(`mqttSettings.fail_${result.code ?? 'error'}`, { host, port, defaultValue: t('mqttSettings.fail_error') })}
                    </p>
                    {!result.ok && result.message && (
                        <p className="font-mono text-[10px] opacity-80 break-all">{result.message}</p>
                    )}
                    {!result.ok && result.code === 'tls' && port !== 8883 && (
                        <p>{t('mqttSettings.tls_hint')}</p>
                    )}
                </div>
            )}
        </div>
    );
}
