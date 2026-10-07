import { useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { ImagePlus, Trash2 } from 'lucide-react';
import { isAxiosError } from 'axios';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { organizationsApi } from '@/api/organizations';
import { orgLogoKey, useOrgLogo } from '@/hooks/useOrgLogo';

const MAX_BYTES = 512 * 1024;

/** The organization's logo: a preview, a new one chosen from a file, or none. */
export function OrgLogoField({ orgId }: { orgId: number }) {
    const { t } = useTranslation();
    const qc = useQueryClient();
    const { data: logo } = useOrgLogo(orgId);
    const input = useRef<HTMLInputElement>(null);
    const [busy, setBusy] = useState(false);

    const run = async (action: () => Promise<void>, ok: string) => {
        setBusy(true);
        try {
            await action();
            await qc.invalidateQueries({ queryKey: orgLogoKey(orgId) });
            toast.success(ok);
        } catch (e) {
            const msg = isAxiosError(e) ? (e.response?.data as { error?: string } | undefined)?.error : undefined;
            toast.error(msg || t('organizationsPage.logo_failed'));
        } finally {
            setBusy(false);
        }
    };

    const onFile = (f: File | undefined) => {
        if (!f) return;
        if (f.size > MAX_BYTES) { toast.error(t('organizationsPage.logo_too_big')); return; }
        void run(() => organizationsApi.setLogo(orgId, f), t('organizationsPage.logo_saved'));
    };

    return (
        <div className="space-y-1.5">
            <Label>{t('organizationsPage.logo')}</Label>
            <div className="flex items-center gap-3">
                <div className="h-14 w-28 rounded border bg-muted/40 flex items-center justify-center overflow-hidden shrink-0">
                    {logo
                        ? <img src={logo} alt="" className="max-h-full max-w-full object-contain" />
                        : <span className="text-[11px] text-muted-foreground">{t('organizationsPage.logo_none')}</span>}
                </div>
                <div className="flex flex-wrap gap-2">
                    <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => input.current?.click()}>
                        <ImagePlus className="w-4 h-4 mr-1" /> {logo ? t('organizationsPage.logo_change') : t('organizationsPage.logo_upload')}
                    </Button>
                    {logo && (
                        <Button type="button" size="sm" variant="ghost" disabled={busy} className="text-destructive"
                            onClick={() => void run(() => organizationsApi.deleteLogo(orgId), t('organizationsPage.logo_removed'))}>
                            <Trash2 className="w-4 h-4 mr-1" /> {t('organizationsPage.logo_remove')}
                        </Button>
                    )}
                </div>
                <input ref={input} type="file" accept="image/png,image/jpeg,image/webp" className="hidden"
                    onChange={e => { onFile(e.target.files?.[0]); e.target.value = ''; }} />
            </div>
            <p className="text-xs text-muted-foreground">{t('organizationsPage.logo_hint')}</p>
        </div>
    );
}
