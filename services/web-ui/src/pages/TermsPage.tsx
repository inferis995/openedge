import { ArrowLeft } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/button';

export default function TermsPage() {
    const navigate = useNavigate();
    return (
        <div className="min-h-screen bg-background text-foreground">
            <div className="mx-auto max-w-3xl px-6 py-12">
                <Button variant="ghost" size="sm" className="mb-8 gap-2" onClick={() => navigate(-1)}>
                    <ArrowLeft className="h-4 w-4" /> Indietro
                </Button>
                <h1 className="text-3xl font-bold mb-2">Termini di Servizio</h1>
                <p className="text-sm text-muted-foreground mb-8">Ultimo aggiornamento: Giugno 2026</p>

                <div className="prose prose-sm dark:prose-invert max-w-none space-y-6 text-sm leading-relaxed text-foreground/80">
                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">1. Accettazione dei termini</h2>
                        <p>Utilizzando la piattaforma OpenEdge accetti i presenti Termini di Servizio. Se non accetti, non utilizzare il servizio.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">2. Descrizione del servizio</h2>
                        <p>OpenEdge è una piattaforma IIoT industriale per il monitoraggio OT, la gestione di asset edge e la supervisione SCADA. Include controlli automatici sulla postura di sicurezza del proprio impianto; non fornisce, né sostituisce, gli adempimenti previsti dalla direttiva NIS2 o dalla norma IEC 62443, che restano in capo al Cliente. Il servizio è disponibile in modalità on-premises e SaaS.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">3. Licenza d'uso</h2>
                        <p>Viene concessa una licenza limitata, non esclusiva e non trasferibile per l'utilizzo della piattaforma nei limiti del piano sottoscritto. È vietata la sublicenza, la reverse engineering e la distribuzione non autorizzata.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">4. Responsabilità dell'utente</h2>
                        <ul className="list-disc list-inside space-y-1 mt-2">
                            <li>Mantenere riservate le credenziali di accesso</li>
                            <li>Non utilizzare la piattaforma per scopi illeciti</li>
                            <li>Rispettare le normative applicabili (NIS2, GDPR, ecc.)</li>
                            <li>Segnalare tempestivamente qualsiasi accesso non autorizzato</li>
                        </ul>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">5. Limitazione di responsabilità</h2>
                        <p>OpenEdge non è responsabile per danni indiretti, perdita di dati o interruzioni del servizio causate da eventi fuori dal suo controllo. La responsabilità massima è limitata al corrispettivo pagato negli ultimi 12 mesi.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">6. SLA e disponibilità</h2>
                        <p>Per i piani Business e Enterprise è garantito un uptime del 99,9% mensile (escluse manutenzioni programmate comunicate con 48h di anticipo). I piani Free e Pro non hanno SLA garantito.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">7. Modifiche al servizio</h2>
                        <p>OpenEdge si riserva il diritto di modificare il servizio con preavviso di 30 giorni per cambiamenti sostanziali. Le modifiche minori possono essere applicate senza preavviso.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">8. Risoluzione</h2>
                        <p>Entrambe le parti possono risolvere il contratto con preavviso di 30 giorni. In caso di violazione dei presenti termini, OpenEdge può sospendere l'accesso con effetto immediato.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">9. Legge applicabile</h2>
                        <p>I presenti termini sono regolati dalla legge italiana. Per qualsiasi controversia è competente il Foro di residenza del Titolare.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">10. Contatti</h2>
                        <p>Per informazioni: <a href="mailto:support@openedge.io" className="underline">support@openedge.io</a></p>
                    </section>
                </div>
            </div>
        </div>
    );
}
