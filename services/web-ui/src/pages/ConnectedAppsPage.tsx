import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plug, Trash2, AlertTriangle, ShieldCheck } from 'lucide-react';

import { oauthApi, OAuthAuthorization, describeScope } from '@/api/oauth';
import { showApiError, showApiSuccess } from '@/lib/api-error-handler';

import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import {
    Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog';

/**
 * The other half of the consent screen.
 *
 * A user approves a client once, in a browser window that then closes. Without
 * this page that decision is unreviewable and unrevocable — the grant lives in
 * a table nobody can see, and taking it back means a SQL statement.
 */
const ConnectedAppsPage = () => {
    const queryClient = useQueryClient();
    const [toRevoke, setToRevoke] = useState<OAuthAuthorization | null>(null);

    const { data, isLoading } = useQuery({
        queryKey: ['oauth-authorizations'],
        queryFn: oauthApi.listAuthorizations,
    });
    const items: OAuthAuthorization[] = data?.items ?? [];

    const revokeMutation = useMutation({
        mutationFn: (clientID: string) => oauthApi.revokeAuthorization(clientID),
        onSuccess: (res) => {
            // Quote the API's own caveat rather than saying "revocato" and
            // leaving the user to discover the hour for themselves.
            showApiSuccess(
                `Accesso revocato. ${res.note}`,
            );
            setToRevoke(null);
            queryClient.invalidateQueries({ queryKey: ['oauth-authorizations'] });
        },
        onError: (e) => showApiError(e, 'Revoca fallita'),
    });

    const fmt = (iso: string) =>
        new Date(iso).toLocaleString('it-IT', { dateStyle: 'medium', timeStyle: 'short' });

    return (
        <div className="p-6 space-y-6">
            <div>
                <h1 className="text-2xl font-semibold flex items-center gap-2">
                    <Plug size={22} /> App collegate
                </h1>
                <p className="text-sm text-muted-foreground mt-1 max-w-2xl">
                    Applicazioni a cui hai dato accesso a OpenEdge per tuo conto. Ognuna
                    agisce con la tua identità, limitata a quello che avevi approvato.
                </p>
            </div>

            <Table>
                <TableHeader>
                    <TableRow>
                        <TableHead>Applicazione</TableHead>
                        <TableHead>Permessi</TableHead>
                        <TableHead>Autorizzata il</TableHead>
                        <TableHead>Ultimo accesso</TableHead>
                        <TableHead className="w-16" />
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {isLoading && (
                        <TableRow>
                            <TableCell colSpan={5} className="text-muted-foreground">Caricamento…</TableCell>
                        </TableRow>
                    )}
                    {!isLoading && items.length === 0 && (
                        <TableRow>
                            <TableCell colSpan={5} className="text-muted-foreground py-10 text-center">
                                <ShieldCheck size={20} className="inline mr-2 opacity-60" />
                                Nessuna applicazione collegata. Quando ne autorizzi una,
                                compare qui e puoi toglierle l'accesso da questa pagina.
                            </TableCell>
                        </TableRow>
                    )}
                    {items.map((a) => (
                        <TableRow key={a.client_id}>
                            <TableCell>
                                <div className="font-medium">{a.client_name}</div>
                                <code className="text-xs text-muted-foreground">{a.client_id.slice(0, 12)}…</code>
                            </TableCell>
                            <TableCell>
                                <div className="flex flex-col gap-1">
                                    {describeScope(a.scope).map((s) => (
                                        <Badge key={s} variant="outline" className="w-fit font-normal">{s}</Badge>
                                    ))}
                                </div>
                            </TableCell>
                            <TableCell className="text-muted-foreground">{fmt(a.authorized_at)}</TableCell>
                            <TableCell className="text-muted-foreground">{fmt(a.last_issued_at)}</TableCell>
                            <TableCell>
                                <div className="flex justify-end">
                                    <Button variant="ghost" size="icon" onClick={() => setToRevoke(a)}>
                                        <Trash2 size={15} className="text-destructive" />
                                    </Button>
                                </div>
                            </TableCell>
                        </TableRow>
                    ))}
                </TableBody>
            </Table>

            <Dialog open={!!toRevoke} onOpenChange={(o) => !o && setToRevoke(null)}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <AlertTriangle size={18} className="text-destructive" />
                            Togliere l'accesso a «{toRevoke?.client_name}»?
                        </DialogTitle>
                        <DialogDescription asChild>
                            <div className="space-y-2">
                                <p>
                                    L'applicazione non potrà più rinnovare il proprio accesso e
                                    dovrà chiederti di nuovo l'autorizzazione.
                                </p>
                                <p>
                                    Il token che ha già in mano resta valido fino alla scadenza,
                                    al massimo un'ora: è firmato e non può essere richiamato.
                                    Se l'applicazione è compromessa, cambia anche la password.
                                </p>
                            </div>
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setToRevoke(null)}>Annulla</Button>
                        <Button variant="destructive" disabled={revokeMutation.isPending}
                            onClick={() => toRevoke && revokeMutation.mutate(toRevoke.client_id)}>
                            Togli l'accesso
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
};

export default ConnectedAppsPage;
