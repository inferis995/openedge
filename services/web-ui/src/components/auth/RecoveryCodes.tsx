import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Copy, Download, Check } from 'lucide-react';
import { Button } from '@/components/ui/button';

/**
 * One-time recovery codes, shown once: each replaces the authenticator for
 * one login. Copy them or save them as a file before leaving this screen.
 */
export function RecoveryCodes({ codes }: { codes: string[] }) {
    const { t } = useTranslation();
    const [copied, setCopied] = useState(false);
    const text = codes.join('\n');
    const copy = async () => {
        try {
            await navigator.clipboard.writeText(text);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        } catch { /* clipboard unavailable: the codes are on screen */ }
    };
    const download = () => {
        const blob = new Blob([`OpenEdge — ${t('mfa.recovery_title')}\n\n${text}\n`], { type: 'text/plain' });
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = 'openedge-recovery-codes.txt';
        a.click();
        URL.revokeObjectURL(a.href);
    };
    return (
        <div className="space-y-3">
            <div>
                <p className="text-sm font-medium">{t('mfa.recovery_title')}</p>
                <p className="text-xs text-muted-foreground">{t('mfa.recovery_desc')}</p>
            </div>
            <ul className="grid grid-cols-2 gap-2 rounded-lg border bg-muted/40 p-3 font-mono text-sm">
                {codes.map((c) => <li key={c} className="select-all text-center">{c}</li>)}
            </ul>
            <div className="flex gap-2">
                <Button type="button" variant="outline" size="sm" onClick={() => void copy()}>
                    {copied ? <Check size={14} /> : <Copy size={14} />} {copied ? t('mfa.copied') : t('mfa.copy')}
                </Button>
                <Button type="button" variant="outline" size="sm" onClick={download}>
                    <Download size={14} /> {t('mfa.download')}
                </Button>
            </div>
        </div>
    );
}
