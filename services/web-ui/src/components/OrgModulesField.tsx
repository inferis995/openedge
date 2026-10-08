import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { Label } from '@/components/ui/label';
import { Checkbox } from '@/components/ui/checkbox';
import { organizationsApi } from '@/api/organizations';
import { orgModulesKey } from '@/hooks/useOrgModules';

/** Module keys use URL style (connected-apps); nav labels use underscores. */
const moduleNavKey: Record<string, string> = {
    oee: 'oee_profiles',
    udt: 'udt_types',
    'connected-apps': 'connected_apps',
};

/** Which pages the organization may use: one checkbox per module. */
export function OrgModulesField({ orgId }: { orgId: number }) {
    const { t } = useTranslation();
    const qc = useQueryClient();
    const [busy, setBusy] = useState(false);
    const { data: modules } = useQuery({
        queryKey: orgModulesKey(orgId),
        queryFn: () => organizationsApi.getModules(orgId),
        retry: false,
    });

    const toggle = async (key: string, on: boolean) => {
        if (!modules) return;
        setBusy(true);
        try {
            const disabled = modules
                .filter((m) => (m.key === key ? !on : !m.enabled))
                .map((m) => m.key);
            await organizationsApi.setModules(orgId, disabled);
            await qc.invalidateQueries({ queryKey: orgModulesKey(orgId) });
            toast.success(t('organizationsPage.modules_saved'));
        } catch {
            toast.error(t('organizationsPage.modules_failed'));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="space-y-1.5">
            <Label>{t('organizationsPage.modules')}</Label>
            <div className="grid grid-cols-2 gap-1.5 rounded border p-3">
                {(modules ?? []).map((m) => (
                    <label key={m.key} className="flex items-center gap-2 text-sm cursor-pointer">
                        <Checkbox
                            checked={m.enabled}
                            disabled={busy}
                            onCheckedChange={(v) => void toggle(m.key, v === true)}
                        />
                        {t(`nav.${moduleNavKey[m.key] ?? m.key}`, { defaultValue: m.key })}
                    </label>
                ))}
            </div>
        </div>
    );
}
