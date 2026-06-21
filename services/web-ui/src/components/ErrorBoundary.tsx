import { Component, ErrorInfo, ReactNode } from 'react';
import { AlertTriangle } from 'lucide-react';
import { Button } from '@/components/ui/button';

interface Props {
    children: ReactNode;
}

interface State {
    hasError: boolean;
    error?: Error;
}

export class ErrorBoundary extends Component<Props, State> {
    state: State = { hasError: false };

    static getDerivedStateFromError(error: Error): State {
        return { hasError: true, error };
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        console.error('[ErrorBoundary]', error, info.componentStack);
    }

    render() {
        if (this.state.hasError) {
            return (
                <div className="flex h-screen w-full flex-col items-center justify-center gap-4 bg-background text-foreground">
                    <AlertTriangle className="h-12 w-12 text-destructive" />
                    <h1 className="text-xl font-semibold">Qualcosa è andato storto</h1>
                    <p className="text-sm text-muted-foreground max-w-sm text-center">
                        Si è verificato un errore imprevisto. Ricarica la pagina o contatta il supporto se il problema persiste.
                    </p>
                    {this.state.error && (
                        <pre className="text-xs bg-muted rounded p-3 max-w-lg overflow-auto text-muted-foreground">
                            {this.state.error.message}
                        </pre>
                    )}
                    <div className="flex gap-2">
                        <Button onClick={() => window.location.reload()}>Ricarica pagina</Button>
                        <Button variant="outline" onClick={() => this.setState({ hasError: false })}>Riprova</Button>
                    </div>
                    <p className="text-xs text-muted-foreground mt-2">
                        Supporto: <a href="mailto:support@openedge.io" className="underline">support@openedge.io</a>
                    </p>
                </div>
            );
        }
        return this.props.children;
    }
}
