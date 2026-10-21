import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { Switch } from '@/components/ui/switch';
import { organizationsApi } from '@/api/organizations';
import { showApiError } from '@/lib/api-error-handler';

/**
 * Two-step verification required for everyone in the organization. Saved as
 * it is switched: users without it set it up at their next sign-in (the login
 * page walks them through it), so nobody is locked out.
 */
export function OrgMFAField({ orgId, initial }: { orgId: number; initial: boolean }) {
    const { t } = useTranslation();
    const qc = useQueryClient();
    const [on, setOn] = useState(initial);
    const [busy, setBusy] = useState(false);
    const change = async (v: boolean) => {
        setBusy(true);
        try {
            await organizationsApi.setMFARequired(orgId, v);
            setOn(v);
            await qc.invalidateQueries({ queryKey: ['organizations'] });
            toast.success(v ? t('organizationsPage.mfa_required_on') : t('organizationsPage.mfa_required_off'));
        } catch (e) {
            showApiError(e);
        } finally {
            setBusy(false);
        }
    };
    return (
        <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
                <p className="text-sm font-medium">{t('organizationsPage.mfa_required')}</p>
                <p className="text-sm text-muted-foreground">{t('organizationsPage.mfa_required_hint')}</p>
            </div>
            <Switch checked={on} disabled={busy} onCheckedChange={(v) => void change(v)} aria-label={t('organizationsPage.mfa_required')} className="mt-0.5 shrink-0" />
        </div>
    );
}
