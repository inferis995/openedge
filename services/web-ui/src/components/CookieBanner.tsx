import { useState } from 'react';
import { Cookie } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

const STORAGE_KEY = 'openedge_cookie_consent';

// Storage can be blocked (private window, strict settings); the banner must
// still render and close rather than throw.
function readConsent(): string | null {
    try {
        return localStorage.getItem(STORAGE_KEY);
    } catch {
        return null;
    }
}

function saveConsent(v: string) {
    try {
        localStorage.setItem(STORAGE_KEY, v);
    } catch {
        // Closes for this visit only.
    }
}

export function CookieBanner() {
    const [visible, setVisible] = useState(() => !readConsent());
    const { t } = useTranslation();

    const accept = () => {
        saveConsent('accepted');
        setVisible(false);
    };

    const decline = () => {
        saveConsent('declined');
        setVisible(false);
    };

    if (!visible) return null;

    return (
        <div className="fixed bottom-0 left-0 right-0 z-50 border-t border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80 shadow-lg">
            <div className="mx-auto max-w-6xl flex flex-col sm:flex-row items-start sm:items-center gap-4 px-4 py-4">
                <Cookie className="h-5 w-5 shrink-0 text-muted-foreground mt-0.5" />
                <p className="text-sm text-muted-foreground flex-1">
                    {t('cookies.text')}{' '}
                    <Link to="/privacy" className="underline hover:text-foreground">{t('cookies.privacy')}</Link>
                    {' '}·{' '}
                    <Link to="/terms" className="underline hover:text-foreground">{t('cookies.terms')}</Link>
                </p>
                <div className="flex gap-2 shrink-0">
                    <Button size="sm" variant="outline" onClick={decline}>{t('cookies.decline')}</Button>
                    <Button size="sm" onClick={accept}>{t('cookies.accept')}</Button>
                </div>
            </div>
        </div>
    );
}
