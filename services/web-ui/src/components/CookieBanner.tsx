import { useState, useEffect } from 'react';
import { Cookie } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Link } from 'react-router-dom';

const STORAGE_KEY = 'openedge_cookie_consent';

export function CookieBanner() {
    const [visible, setVisible] = useState(false);

    useEffect(() => {
        if (!localStorage.getItem(STORAGE_KEY)) {
            setVisible(true);
        }
    }, []);

    const accept = () => {
        localStorage.setItem(STORAGE_KEY, 'accepted');
        setVisible(false);
    };

    const decline = () => {
        localStorage.setItem(STORAGE_KEY, 'declined');
        setVisible(false);
    };

    if (!visible) return null;

    return (
        <div className="fixed bottom-0 left-0 right-0 z-50 border-t border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80 shadow-lg">
            <div className="mx-auto max-w-6xl flex flex-col sm:flex-row items-start sm:items-center gap-4 px-4 py-4">
                <Cookie className="h-5 w-5 shrink-0 text-muted-foreground mt-0.5" />
                <p className="text-sm text-muted-foreground flex-1">
                    Utilizziamo cookie tecnici necessari al funzionamento della piattaforma. Nessun cookie di profilazione o marketing.{' '}
                    <Link to="/privacy" className="underline hover:text-foreground">Privacy Policy</Link>
                    {' '}·{' '}
                    <Link to="/terms" className="underline hover:text-foreground">Termini di Servizio</Link>
                </p>
                <div className="flex gap-2 shrink-0">
                    <Button size="sm" variant="outline" onClick={decline}>Rifiuta</Button>
                    <Button size="sm" onClick={accept}>Accetta</Button>
                </div>
            </div>
        </div>
    );
}
