import { useEffect, useState } from 'react';
import { Link, Navigate, useParams, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
    ArrowLeft, Building2, Key, LayoutGrid, Network, Server, ShieldCheck, UserPlus,
    Webhook as WebhookIcon, KeyRound,
} from 'lucide-react';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { SettingsCard } from '@/components/settings/SettingsCard';
import { OrgLogoField } from '@/components/OrgLogoField';
import { OrgMFAField } from '@/components/OrgMFAField';
import { OrgModulesField } from '@/components/OrgModulesField';
import OrgSettingsSections, { OrgIntegrationSection } from '@/components/OrgSettingsSections';
import { useOrganizations } from '@/hooks/useOrganizations';
import { refreshOrgAccent } from '@/hooks/useOrgTheme';
import { useOrgLogo } from '@/hooks/useOrgLogo';
import { useAuthStore } from '@/stores/useAuthStore';
import { useThemeStore } from '@/stores/useThemeStore';
import { Organization } from '@/types';

type Section = 'general' | 'security' | 'modules' | OrgIntegrationSection;

const SWATCHES = ['#e0ff00', '#ff7a00', '#2f81f7', '#22c55e', '#000000', '#ffffff'];
const HEX = /^#[0-9a-f]{6}$/i;

/**
 * Everything about one organization in one place, laid out like the system
 * settings page: the sections on the left, grouped by what they are about,
 * the open one on the right; on a phone the list is one row that scrolls.
 * The section is in the address (?tab=…), so a link opens it.
 */
const OrgSettingsPage = () => {
    const { t } = useTranslation();
    const { id } = useParams();
    const orgId = Number(id);
    const { organizations, isLoading } = useOrganizations();
    const { isAdmin } = useAuthStore();
    const org = organizations.find((o) => o.id === orgId);

    const groups: { key: string; items: { value: Section; icon: typeof Server }[] }[] = [
        { key: 'orgSettings.group_org', items: [
            { value: 'general', icon: Building2 },
            { value: 'modules', icon: LayoutGrid },
        ] },
        { key: 'orgSettings.group_access', items: [
            { value: 'security', icon: ShieldCheck },
            { value: 'sso', icon: KeyRound },
            { value: 'invites', icon: UserPlus },
            { value: 'apikeys', icon: Key },
        ] },
        { key: 'orgSettings.group_integrations', items: [
            { value: 'edge', icon: Server },
            { value: 'mqtt', icon: Network },
            { value: 'webhooks', icon: WebhookIcon },
        ] },
    ];
    const all = groups.flatMap((g) => g.items.map((i) => i.value));
    const [params, setParams] = useSearchParams();
    const requested = params.get('tab') as Section | null;
    const tab: Section = requested && all.includes(requested) ? requested : 'general';
    const setTab = (v: string) => setParams((p) => { p.set('tab', v); return p; }, { replace: true });

    // On a phone the section list is one scrolling row: bring the open one into view.
    useEffect(() => {
        document.querySelector('nav [role="tab"][data-state="active"]')?.scrollIntoView({ block: 'nearest', inline: 'center' });
    }, [tab, isLoading]);

    if (!isAdmin()) return <Navigate to="/" replace />;
    if (isLoading) {
        return <div className="p-8 text-center text-muted-foreground">{t('organizationsPage.loading')}</div>;
    }
    if (!org) {
        return (
            <div className="space-y-4 p-8 text-center">
                <p className="text-muted-foreground">{t('orgSettings.not_found')}</p>
                <Button variant="outline" asChild><Link to="/organizations">{t('orgSettings.back')}</Link></Button>
            </div>
        );
    }

    return (
        <div className="space-y-6">
            {/* Page header: where you are, back to the list */}
            <div className="space-y-3">
                <Link to="/organizations"
                    className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
                    <ArrowLeft className="size-4" /> {t('orgSettings.back')}
                </Link>
                <div className="flex items-center gap-3">
                    <OrgMark org={org} />
                    <div className="min-w-0">
                        <h1 className="truncate text-2xl font-semibold tracking-tight">{org.name}</h1>
                        <p className="text-sm text-muted-foreground">{t('orgSettings.title')}</p>
                    </div>
                </div>
            </div>

            <Tabs value={tab} onValueChange={setTab} orientation="vertical"
                className="grid gap-6 lg:grid-cols-[230px_minmax(0,1fr)] lg:items-start">
                <nav className="-mx-4 min-w-0 px-4 lg:sticky lg:top-4 lg:mx-0 lg:px-0" aria-label={t('orgSettings.title')}>
                    <TabsList className="flex h-auto w-full flex-row items-stretch justify-start gap-1 overflow-x-auto rounded-none bg-transparent p-0 sm:justify-start lg:flex-col lg:overflow-visible [scrollbar-width:none]">
                        {groups.map((group, gi) => (
                            <div key={group.key} className="contents">
                                <p className={`hidden px-3 pb-1 text-xs font-medium text-muted-foreground lg:block ${gi === 0 ? '' : 'pt-5'}`}>{t(group.key)}</p>
                                {group.items.map(({ value, icon: Icon }) => (
                                    <TabsTrigger key={value} value={value}
                                        className="shrink-0 justify-start gap-2.5 rounded-lg border border-transparent px-3 py-2 text-sm font-medium text-muted-foreground shadow-none hover:bg-muted hover:text-foreground data-[state=active]:border-border data-[state=active]:bg-card data-[state=active]:text-foreground data-[state=active]:shadow-xs lg:w-full">
                                        <Icon className="size-4 shrink-0" />
                                        {t(`orgSettings.${value}`)}
                                    </TabsTrigger>
                                ))}
                            </div>
                        ))}
                    </TabsList>
                </nav>

                <div className="min-w-0 max-w-4xl space-y-6">
                    {/* The open section, named and described */}
                    <div className="border-b pb-4">
                        <h2 className="text-lg font-semibold">{t(`orgSettings.${tab}`)}</h2>
                        <p className="text-sm text-muted-foreground">{t(`orgSettings.desc_${tab}`)}</p>
                    </div>

                    {tab === 'general' && <GeneralSection key={org.id} org={org} />}

                    {tab === 'modules' && (
                        <SettingsCard title={t('orgSettings.modules_card_title')} description={t('orgSettings.modules_card')}
                            footer={<SavedNote />}>
                            <OrgModulesField orgId={org.id} />
                        </SettingsCard>
                    )}

                    {tab === 'security' && (
                        <SettingsCard title={t('orgSettings.mfa_title')} footer={<SavedNote />}>
                            <OrgMFAField key={org.id} orgId={org.id} initial={!!org.mfa_required} />
                        </SettingsCard>
                    )}

                    {tab !== 'general' && tab !== 'modules' && tab !== 'security' && (
                        <OrgSettingsSections key={tab} org={org} section={tab} />
                    )}
                </div>
            </Tabs>
        </div>
    );
};

/** The organization's logo if it has one, its initial in its colour otherwise. */
function OrgMark({ org }: { org: Organization }) {
    const { data: logo } = useOrgLogo(org.id);
    if (logo) {
        return (
            <div className="flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-lg border bg-card p-1">
                <img src={logo} alt="" className="max-h-full max-w-full object-contain" />
            </div>
        );
    }
    return (
        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg border bg-muted text-lg font-semibold"
            style={org.theme_color ? { boxShadow: `inset 0 -3px 0 ${org.theme_color}` } : undefined}>
            {org.name.trim().charAt(0).toUpperCase() || <Building2 className="size-5" />}
        </div>
    );
}

function SavedNote() {
    const { t } = useTranslation();
    return <p className="text-xs text-muted-foreground sm:mr-auto">{t('orgSettings.saved_instantly')}</p>;
}

/** Name and colour, saved together; the logo is saved as soon as it is chosen. */
function GeneralSection({ org }: { org: Organization }) {
    const { t } = useTranslation();
    const { update, isUpdating } = useOrganizations();
    const [name, setName] = useState(org.name);
    const [color, setColor] = useState(org.theme_color || '');
    const [hex, setHex] = useState(org.theme_color || '');

    const dirty = name.trim() !== org.name || color !== (org.theme_color || '');
    const valid = name.trim() !== '' && (color === '' || HEX.test(color));
    const pick = (c: string) => { setColor(c); setHex(c); };

    const save = async () => {
        if (!dirty || !valid) return;
        await update({ id: org.id, data: { name: name.trim(), theme_color: color } });
        // The new colour applies at once everywhere (and lands in the cache).
        refreshOrgAccent(org.id, useThemeStore.getState().theme === 'dark');
    };
    const reset = () => { setName(org.name); pick(org.theme_color || ''); };

    return (
        <div className="space-y-6">
            <SettingsCard
                title={t('orgSettings.identity_title')}
                description={t('orgSettings.identity_desc')}
                footer={<>
                    {dirty && <p className="text-xs text-muted-foreground sm:mr-auto">{t('orgSettings.unsaved')}</p>}
                    {dirty && <Button variant="outline" onClick={reset}>{t('common.cancel')}</Button>}
                    <Button onClick={() => void save()} disabled={!dirty || !valid || isUpdating}>
                        {t('orgSettings.save_changes')}
                    </Button>
                </>}
            >
                <div className="space-y-6">
                    <div className="space-y-1.5 sm:max-w-md">
                        <Label htmlFor="org-name">{t('common.name')}</Label>
                        <Input id="org-name" value={name} onChange={(e) => setName(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && void save()} />
                    </div>

                    <div className="space-y-2">
                        <Label>{t('organizationsPage.accent_title')}</Label>
                        <div className="flex flex-wrap items-center gap-1.5 sm:gap-2">
                            <button type="button" onClick={() => pick('')}
                                className={`h-8 sm:h-9 rounded-full border px-3 text-xs font-medium transition-colors ${color === '' ? 'border-foreground bg-muted' : 'hover:bg-muted'}`}>
                                {t('orgSettings.color_default')}
                            </button>
                            {SWATCHES.map((c) => (
                                <button key={c} type="button" title={c} aria-label={c} onClick={() => pick(c)}
                                    className={`size-8 sm:size-9 shrink-0 rounded-full border-2 transition-transform ${color.toLowerCase() === c ? 'scale-110 border-foreground' : 'border-border'}`}
                                    style={{ backgroundColor: c }} />
                            ))}
                        </div>
                        <div className="flex items-center gap-2">
                            <Input type="color" aria-label={t('orgSettings.color_custom')}
                                className="h-10 w-14 shrink-0 cursor-pointer p-1"
                                value={HEX.test(color) ? color : '#e0ff00'}
                                onChange={(e) => pick(e.target.value)} />
                            <Input value={hex} placeholder="#e0ff00" maxLength={7} spellCheck={false}
                                aria-label={t('orgSettings.color_custom')}
                                className={`w-32 font-mono ${hex !== '' && !HEX.test(hex) ? 'border-destructive' : ''}`}
                                onChange={(e) => {
                                    const v = e.target.value.trim();
                                    setHex(v);
                                    if (v === '' || HEX.test(v)) setColor(v.toLowerCase());
                                }} />
                        </div>
                        <p className="text-xs text-muted-foreground">{t('organizationsPage.accent_help')}</p>
                    </div>
                </div>
            </SettingsCard>

            <SettingsCard title={t('organizationsPage.logo')} description={t('organizationsPage.logo_hint')}
                footer={<SavedNote />}>
                <OrgLogoField orgId={org.id} />
            </SettingsCard>
        </div>
    );
}

export default OrgSettingsPage;
