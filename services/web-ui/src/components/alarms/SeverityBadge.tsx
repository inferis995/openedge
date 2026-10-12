import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

/**
 * An alarm's severity, the same everywhere (alarms, dashboard, trend):
 * ISA-101 priorities — critical solid red, high red, medium amber, low blue,
 * info grey. One word, capitalised, in the user's language.
 */
export function SeverityBadge({ severity, className }: { severity?: string | null; className?: string }) {
    const { t } = useTranslation();
    const s = (severity || '').toLowerCase();
    const label = t(`alarmsPage.severity_${s}`, { defaultValue: severity || '—' });
    const text = label.charAt(0).toUpperCase() + label.slice(1);
    if (s === 'critical') {
        return <Badge className={cn('border-transparent bg-red-600 text-white hover:bg-red-600', className)}>{text}</Badge>;
    }
    const variant = s === 'high' ? 'alarm'
        : s === 'medium' || s === 'warning' ? 'warning'
            : s === 'low' ? 'info'
                : 'muted';
    return <Badge variant={variant} className={className}>{text}</Badge>;
}

/** An alarm event's state: active (needs someone), acknowledged, cleared. */
export function AlarmStatusBadge({ status, className }: { status: string; className?: string }) {
    const { t } = useTranslation();
    if (status === 'ACTIVE') {
        return <Badge variant="alarm" className={className}><span className="size-1.5 rounded-full bg-current animate-pulse" />{t('alarmsPage.status_active')}</Badge>;
    }
    if (status === 'ACKNOWLEDGED') return <Badge variant="warning" className={className}>{t('alarmsPage.status_acknowledged')}</Badge>;
    if (status === 'CLEARED') return <Badge variant="success" className={className}>{t('alarmsPage.status_cleared')}</Badge>;
    return <Badge variant="muted" className={className}>{status}</Badge>;
}
