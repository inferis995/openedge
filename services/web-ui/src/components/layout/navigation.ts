// The one list of places in the application.
//
// The sidebar, the breadcrumb in the header and the quick search (Ctrl+K) all
// read it. They used to keep their own: the sidebar had 29 items in one flat
// list with labels half in Italian and half in English, and the breadcrumb
// spelled a page by capitalising its URL ("Udt", "Oee-profiles").
import type { LucideIcon } from 'lucide-react';
import {
    Activity, Bell, Boxes, Building2, ChefHat, ClipboardList, Clock, Cpu, Factory,
    FileText, Gauge, History, LayoutDashboard, LayoutTemplate, Layers, Lock, MapPin,
    Network, PackageOpen, Plug, Radio, Rocket, Server, Settings, Shield, Tags, Target,
    TrendingUp, Users, Wrench,
} from 'lucide-react';

export interface NavItem {
    /** i18n key of the label, under nav.* */
    key: string;
    path: string;
    icon: LucideIcon;
    /** i18n key of a one-line explanation, shown in the quick search. */
    hint?: string;
}

export interface NavSection {
    /** i18n key of the heading, under nav.section.* */
    key: string;
    items: NavItem[];
}

export interface NavAccess {
    isAdmin: boolean;
    isGlobalAdmin: boolean;
    isOrgScoped: boolean;
}

/**
 * The sections a user sees, in order.
 *
 * Grouped by what the person is doing rather than by how the backend is laid
 * out: first the plant is described (Impianto), then it is run (Operatività),
 * then looked back on (Analisi). Administration comes last and only for admins.
 */
export function navSections(access: NavAccess): NavSection[] {
    const organization: NavItem[] = access.isGlobalAdmin
        ? [{ key: 'nav.organizations', path: '/organizations', icon: Building2, hint: 'nav.hint.organizations' }]
        : access.isOrgScoped && access.isAdmin
            ? [{ key: 'nav.my_organization', path: '/organizations', icon: Building2, hint: 'nav.hint.organizations' }]
            : [];

    const sections: NavSection[] = [
        {
            key: 'nav.section.overview',
            items: [
                { key: 'nav.dashboard', path: '/', icon: LayoutDashboard, hint: 'nav.hint.dashboard' },
                ...(access.isAdmin
                    ? [{ key: 'nav.setup', path: '/setup', icon: Rocket, hint: 'nav.hint.setup' }]
                    : []),
            ],
        },
        {
            key: 'nav.section.plant',
            items: [
                ...organization,
                { key: 'nav.sites', path: '/sites', icon: Factory, hint: 'nav.hint.sites' },
                { key: 'nav.areas', path: '/areas', icon: MapPin, hint: 'nav.hint.areas' },
                { key: 'nav.gateways', path: '/gateways', icon: Cpu, hint: 'nav.hint.gateways' },
                { key: 'nav.tags', path: '/tags', icon: Tags, hint: 'nav.hint.tags' },
                { key: 'nav.inventory', path: '/inventory', icon: ClipboardList, hint: 'nav.hint.inventory' },
                { key: 'nav.udt_types', path: '/udt/types', icon: Boxes, hint: 'nav.hint.udt_types' },
                { key: 'nav.udt_instances', path: '/udt/instances', icon: Layers, hint: 'nav.hint.udt_instances' },
            ],
        },
        {
            key: 'nav.section.operations',
            items: [
                { key: 'nav.synoptics', path: '/synoptics', icon: LayoutTemplate, hint: 'nav.hint.synoptics' },
                { key: 'nav.alarms', path: '/alarms', icon: Bell, hint: 'nav.hint.alarms' },
                { key: 'nav.recipes', path: '/recipes', icon: ChefHat, hint: 'nav.hint.recipes' },
                { key: 'nav.shifts', path: '/shifts', icon: Clock, hint: 'nav.hint.shifts' },
                { key: 'nav.maintenance', path: '/maintenance', icon: Wrench, hint: 'nav.hint.maintenance' },
            ],
        },
        {
            key: 'nav.section.analysis',
            items: [
                { key: 'nav.trend', path: '/trend', icon: TrendingUp, hint: 'nav.hint.trend' },
                { key: 'nav.historian', path: '/history', icon: History, hint: 'nav.hint.historian' },
                { key: 'nav.reports', path: '/reports', icon: FileText, hint: 'nav.hint.reports' },
                { key: 'nav.kpis', path: '/kpis', icon: Target, hint: 'nav.hint.kpis' },
                { key: 'nav.oee_profiles', path: '/oee-profiles', icon: Gauge, hint: 'nav.hint.oee_profiles' },
            ],
        },
        {
            key: 'nav.section.integrations',
            items: [
                { key: 'nav.i3x', path: '/i3x', icon: Network, hint: 'nav.hint.i3x' },
                { key: 'nav.connected_apps', path: '/connected-apps', icon: Plug, hint: 'nav.hint.connected_apps' },
            ],
        },
    ];

    if (access.isAdmin) {
        sections.push({
            key: 'nav.section.admin',
            items: [
                ...(access.isGlobalAdmin
                    ? [
                        { key: 'nav.system', path: '/system', icon: Settings, hint: 'nav.hint.system' },
                        { key: 'nav.infrastructure', path: '/infrastructure', icon: Server, hint: 'nav.hint.infrastructure' },
                        { key: 'nav.mqtt_monitor', path: '/mqtt-monitor', icon: Radio, hint: 'nav.hint.mqtt_monitor' },
                        { key: 'nav.diagnostics', path: '/diagnostics', icon: Activity, hint: 'nav.hint.diagnostics' },
                        { key: 'nav.security', path: '/security', icon: Lock, hint: 'nav.hint.security' },
                        { key: 'nav.audit_log', path: '/audit', icon: Shield, hint: 'nav.hint.audit_log' },
                        { key: 'nav.fleet', path: '/fleet', icon: Network, hint: 'nav.hint.fleet' },
                        { key: 'nav.releases', path: '/releases', icon: PackageOpen, hint: 'nav.hint.releases' },
                    ]
                    : []),
                { key: 'nav.users', path: '/users', icon: Users, hint: 'nav.hint.users' },
            ],
        });
    }

    return sections.filter((s) => s.items.length > 0);
}

/**
 * Whether a menu entry is where the user is.
 *
 * An exact match left the menu blank on every detail page — /synoptics/4,
 * /udt/types/2 — so the user could not see which part of the application they
 * were in. The dashboard is the exception: every path starts with "/".
 */
export function isActivePath(itemPath: string, pathname: string): boolean {
    if (itemPath === '/') return pathname === '/';
    return pathname === itemPath || pathname.startsWith(itemPath + '/');
}

/** The entry that owns a path, for the breadcrumb and the page title. */
export function findNavItem(sections: NavSection[], pathname: string): NavItem | undefined {
    let best: NavItem | undefined;
    for (const s of sections) {
        for (const item of s.items) {
            if (isActivePath(item.path, pathname) && (!best || item.path.length > best.path.length)) {
                best = item;
            }
        }
    }
    return best;
}

export const QUICK_SEARCH_EVENT = 'openedge:quick-search';

/** Opens the quick search (Ctrl+K) from anywhere — the sidebar button, a help text. */
export function openQuickSearch() {
    window.dispatchEvent(new Event(QUICK_SEARCH_EVENT));
}

const fold = (s: string) => s.normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase();

/**
 * Ranks a page against what the user typed in the quick search.
 *
 * cmdk's own score accepts letters scattered anywhere, so "turni" put
 * "Tipi (UDT)" first (T-ipi U-DT … R-iutilizzabili …). Here every word typed
 * has to appear whole in the label or its explanation, and a hit in the label
 * outranks one in the explanation. Accents are ignored: "attivita" finds
 * "attività".
 */
export function scorePage(label: string, search: string, keywords: string[] = []): number {
    const words = fold(search).split(/\s+/).filter(Boolean);
    if (words.length === 0) return 1;
    const l = fold(label);
    const rest = fold(keywords.join(' '));
    let score = 0;
    for (const w of words) {
        if (l.startsWith(w)) score += 1;
        else if (l.split(/[\s()]+/).some((part) => part.startsWith(w))) score += 0.9;
        else if (l.includes(w)) score += 0.7;
        else if (rest.includes(w)) score += 0.4;
        else return 0;
    }
    return score / words.length;
}
