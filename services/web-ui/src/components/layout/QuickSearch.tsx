import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
    CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList,
} from '@/components/ui/command';
import { navSections, scorePage, QUICK_SEARCH_EVENT as OPEN_EVENT } from '@/components/layout/navigation';
import { useAuthStore } from '@/stores/useAuthStore';

/**
 * Ctrl+K (Cmd+K on a Mac): type the name of a page, or what you want to do,
 * and go there.
 *
 * Thirty pages do not fit in anyone's head. Each entry is searchable by its
 * label, its section and its one-line explanation, in the language the user
 * is reading, so "turni", "shift" and "orari di lavoro" all find the shifts.
 */
export default function QuickSearch() {
    const [open, setOpen] = useState(false);
    const navigate = useNavigate();
    const { t } = useTranslation();
    const { isAdmin, isGlobalAdmin, isOrgScoped } = useAuthStore();

    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
                e.preventDefault();
                setOpen((o) => !o);
            }
        };
        const onOpen = () => setOpen(true);
        window.addEventListener('keydown', onKey);
        window.addEventListener(OPEN_EVENT, onOpen);
        return () => {
            window.removeEventListener('keydown', onKey);
            window.removeEventListener(OPEN_EVENT, onOpen);
        };
    }, []);

    const sections = navSections({
        isAdmin: isAdmin(),
        isGlobalAdmin: isGlobalAdmin(),
        isOrgScoped: isOrgScoped(),
    });

    return (
        <CommandDialog open={open} onOpenChange={setOpen} filter={scorePage}>
            <CommandInput placeholder={t('nav.search_input')} />
            <CommandList>
                <CommandEmpty>{t('nav.search_empty')}</CommandEmpty>
                {sections.map((section) => (
                    <CommandGroup key={section.key} heading={t(section.key)}>
                        {section.items.map((item) => {
                            const label = t(item.key);
                            const hint = item.hint ? t(item.hint) : '';
                            return (
                                <CommandItem
                                    key={item.path}
                                    value={label}
                                    keywords={[hint, t(section.key), item.path]}
                                    onSelect={() => {
                                        setOpen(false);
                                        navigate(item.path);
                                    }}
                                    className="flex items-start gap-3 py-2"
                                >
                                    <item.icon size={16} className="mt-0.5 shrink-0 text-muted-foreground" />
                                    <div className="min-w-0">
                                        <div className="font-medium">{label}</div>
                                        {hint && <div className="text-xs text-muted-foreground truncate">{hint}</div>}
                                    </div>
                                </CommandItem>
                            );
                        })}
                    </CommandGroup>
                ))}
            </CommandList>
        </CommandDialog>
    );
}
