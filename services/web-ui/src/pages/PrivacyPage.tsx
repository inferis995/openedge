import { ArrowLeft } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/button';

export default function PrivacyPage() {
    const navigate = useNavigate();
    return (
        <div className="min-h-screen bg-background text-foreground">
            <div className="mx-auto max-w-3xl px-6 py-12">
                <Button variant="ghost" size="sm" className="mb-8 gap-2" onClick={() => navigate(-1)}>
                    <ArrowLeft className="h-4 w-4" /> Indietro
                </Button>
                <h1 className="text-3xl font-bold mb-2">Privacy Policy</h1>
                <p className="text-sm text-muted-foreground mb-8">Ultimo aggiornamento: Giugno 2026</p>

                <div className="prose prose-sm dark:prose-invert max-w-none space-y-6 text-sm leading-relaxed text-foreground/80">
                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">1. Titolare del trattamento</h2>
                        <p>Il Titolare del trattamento dei dati personali raccolti tramite la piattaforma OpenEdge è il soggetto che ha sottoscritto il contratto di licenza o di servizio. Per informazioni: <a href="mailto:privacy@openedge.io" className="underline">privacy@openedge.io</a></p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">2. Dati raccolti</h2>
                        <p>La piattaforma raccoglie esclusivamente i dati necessari al funzionamento del servizio:</p>
                        <ul className="list-disc list-inside space-y-1 mt-2">
                            <li>Dati di accesso: username, email, password cifrata (bcrypt)</li>
                            <li>Dati operativi: valori tag, allarmi, log di sistema</li>
                            <li>Dati di utilizzo: log di audit delle azioni utente</li>
                        </ul>
                        <p className="mt-2">Non vengono raccolti dati di profilazione, tracking pubblicitario o cookie di terze parti.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">3. Cookie</h2>
                        <p>Utilizziamo esclusivamente cookie tecnici necessari al funzionamento della piattaforma (sessione di autenticazione). Nessun cookie di profilazione o marketing.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">4. Base giuridica del trattamento</h2>
                        <p>Il trattamento avviene sulla base dell'esecuzione del contratto di servizio (Art. 6, par. 1, lett. b GDPR) e del legittimo interesse alla sicurezza del sistema.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">5. Conservazione dei dati</h2>
                        <p>I dati storici (valori tag, eventi) sono conservati per il periodo definito nella configurazione della piattaforma. I log di audit sono conservati per 12 mesi. I dati di accesso vengono eliminati alla cancellazione dell'account.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">6. Diritti dell'interessato</h2>
                        <p>In conformità al GDPR (Reg. UE 2016/679), hai il diritto di accedere, rettificare, cancellare i tuoi dati, opporti al trattamento e richiedere la portabilità. Richieste a: <a href="mailto:privacy@openedge.io" className="underline">privacy@openedge.io</a></p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">7. Sicurezza</h2>
                        <p>I dati sono protetti da cifratura in transito (TLS 1.3), password hash bcrypt, JWT con scadenza 24h, audit log di tutte le azioni e controllo accessi RBAC.</p>
                    </section>

                    <section>
                        <h2 className="text-lg font-semibold text-foreground mb-2">8. Contatti</h2>
                        <p>Per qualsiasi questione relativa alla privacy: <a href="mailto:privacy@openedge.io" className="underline">privacy@openedge.io</a></p>
                    </section>
                </div>
            </div>
        </div>
    );
}
