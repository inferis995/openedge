import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Home, MoveLeft } from 'lucide-react';

export default function NotFoundPage() {
    const navigate = useNavigate();
    return (
        <div className="flex h-screen w-full flex-col items-center justify-center gap-6 text-center">
            <div className="space-y-2">
                <p className="text-8xl font-bold text-primary/20">404</p>
                <h1 className="text-2xl font-semibold tracking-tight">Pagina non trovata</h1>
                <p className="text-muted-foreground text-sm max-w-xs">
                    La pagina che cerchi non esiste o è stata spostata.
                </p>
            </div>
            <div className="flex gap-3">
                <Button variant="outline" onClick={() => navigate(-1)} className="gap-2">
                    <MoveLeft className="w-4 h-4" /> Indietro
                </Button>
                <Button onClick={() => navigate('/')} className="gap-2">
                    <Home className="w-4 h-4" /> Dashboard
                </Button>
            </div>
        </div>
    );
}
