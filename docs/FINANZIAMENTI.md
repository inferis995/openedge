# Bandi e finanziamenti — Campania, quadro verificato al 2026-09-05

> **Questo documento invecchia in fretta.** Ogni voce riporta la fonte. Le
> informazioni provengono da ricerca web del 2026-09-05: i siti istituzionali
> (`invitalia.it`, `regione.campania.it`, `sviluppocampania.it`, `eic.ec.europa.eu`)
> **non sono raggiungibili da questo ambiente**, quindi nessuna voce è confermata
> sulla fonte primaria. Sono un punto di partenza per la verifica, non una
> verifica. Prima di presentare qualsiasi domanda, rileggi il testo sul sito
> dell'ente.

---

## La regola che ribalta il problema

Su Smart&Start Italia — e la stessa logica vale per quasi tutti gli strumenti
agevolativi — **sono ammissibili solo le spese sostenute dopo la presentazione
della domanda**, e il piano d'impresa **deve essere avviato successivamente alla
domanda** e concluso entro 24 mesi dalla stipula del contratto.

Questo capovolge la preoccupazione di partenza. Non solo va bene presentare un
progetto non ancora completo: **è il requisito**. Un progetto già finito non è
finanziabile, perché non c'è nulla da finanziare.

Ne discendono due conseguenze pratiche, e sono le due cose più importanti di
tutto questo documento.

**1. Le 90.000 righe già scritte non sono rendicontabili.** Nessun euro speso
finora rientra nel piano. Non è una perdita: è solo che vanno raccontate in un
posto diverso.

**2. Il codice esistente si presenta come *riduzione del rischio*, non come
costo.** In una valutazione di merito, "ho già la piattaforma funzionante, con
test automatici e pipeline di rilascio verde su due modalità di deploy" è
l'argomento più forte che hai: dimostra che il team sa consegnare, e sposta il
progetto da "idea da verificare" a "tecnologia da industrializzare". Vale più di
qualsiasi cifra nel piano.

**Quindi il progetto da presentare è quello che viene DOPO**, e coincide
esattamente con ciò che oggi manca (vedi la tabella in `BUSINESS-PLAN.md` §2):

- validazione dei driver di campo su PLC reali (Siemens S7, Modbus, OPC UA);
- impianto pilota industriale e caso studio misurato;
- hardening: penetration test esterno, prove di backup/restore su dati reali;
- industrializzazione: installatore, documentazione, formazione;
- ingresso sul mercato tramite system integrator.

Questo è un piano d'impresa credibile da 200–400k€ su 24 mesi. "Scrivere lo
SCADA" non lo sarebbe — ed è un bene, perché è già scritto.

---

## Il prerequisito: iscrizione come startup innovativa

Sezione speciale del Registro delle Imprese. La richiedono o la premiano quasi
tutti gli strumenti; per il bando regionale campano è obbligatoria.

Serve almeno **uno** di questi tre requisiti:

1. spese in R&S ≥ 15% del maggior valore fra costo e valore della produzione;
2. almeno 1/3 della forza lavoro con dottorato/dottorandi, **oppure** almeno 2/3
   con laurea magistrale;
3. **titolarità di un software originario registrato** (o di una privativa
   industriale).

**Il punto 3 è la tua via.** Hai un software originale di oltre 90.000 righe di
cui sei autore: la registrazione presso il Registro Pubblico Speciale per i
Programmi per Elaboratore (SIAE) costa poche centinaia di euro e soddisfa il
requisito da sola, senza dipendere da fatturato o da assunzioni.

Porta con sé anche: accesso semplificato al Fondo di Garanzia PMI, esenzione da
diritti camerali e imposte di bollo, disciplina agevolata su perdite e lavoro.

Fonte: [Regione Campania](https://www.regione.campania.it/it/printable/campania-start-up-innovativa)

---

# A. Strumenti che accettano l'idea, senza società già costituita

## A1. Smart&Start Italia — Invitalia ⭐ *il tuo strumento principale*

| | |
|---|---|
| **Cosa dà** | Piano d'impresa fino a €1.500.000 |
| **In Campania** | **30% a fondo perduto** + 70% a tasso zero — la Campania è fra le 8 regioni del Mezzogiorno. Fuori dal Mezzogiorno il fondo perduto non c'è |
| **Chi può** | Startup innovative iscritte da meno di 60 mesi **oppure team di persone fisiche** che si impegnano a costituire la società se la domanda è accolta |
| **Garanzie** | Nessuna garanzia personale |
| **Scadenze** | **Nessuna: sportello**, domanda in qualsiasi momento |
| **Tempi** | ~60 giorni fra verifica formale e valutazione di merito |
| **Durata piano** | Avvio dopo la domanda, conclusione entro 24 mesi dal contratto |
| **Piattaforma** | Nuova gestione domande dal 3 novembre 2025 |

**Spese ammissibili** (verificate):

- immobilizzazioni materiali: impianti, macchinari, attrezzature tecnologiche o
  tecnico-scientifiche, **nuovi di fabbrica** — qui rientrano PLC, gateway,
  banco prova, server;
- immobilizzazioni immateriali: brevetti, marchi, licenze, certificazioni,
  know-how e conoscenze tecniche anche non brevettate;
- consulenze e servizi esterni: **massimo 20%** del totale ammissibile — qui
  rientrano penetration test, consulenza legale, consulenza per la domanda;
- capitale circolante: **massimo 20%** delle spese precedenti.

**[DA VERIFICARE]** Se e come rientri il costo del personale (tuo e di un
eventuale assunto): è la voce più pesante del piano in §9 del business plan e
va confermata sul testo vigente prima di dimensionare il progetto.

**Perché è il primo della lista**: fondo perduto al 30% grazie alla Campania,
sportello sempre aperto (nessuna corsa alla scadenza), nessuna garanzia
personale, e aperto anche prima di costituire la società — quindi puoi
prepararlo *adesso*, in parallelo alla registrazione SIAE.

Fonti: [Invitalia FAQ](https://www.invitalia.it/incentivi-e-strumenti/smartstart-italia/faq) ·
[MIMIT](https://www.mimit.gov.it/it/incentivi/sostegno-alle-startup-innovative-smart-start-italia) ·
[incentivimpresa.it — spese ammissibili](https://www.incentivimpresa.it/spese-ammissibili-smartstart-italia-la-guida-completa-per-il-tuo-piano-di-investimento/) ·
[SprintX](https://sprintx.it/blog/smart-start/)

---

## A2. Resto al Sud 2.0 — Invitalia *(solo se hai meno di 35 anni)*

| | |
|---|---|
| **Cosa dà** | **Contributo interamente a fondo perduto** — il precedente 50% di mutuo bancario non esiste più |
| **Dotazione** | €356,4 milioni (PN Giovani, Donne e Lavoro FSE+ 2021-2027) |
| **Territori** | Abruzzo, Basilicata, Calabria, **Campania**, Molise, Puglia, Sardegna, Sicilia |
| **Chi può** | Da 18 a 34 anni non compiuti |
| **Scadenze** | Sportello aperto dalle 12:00 del 15 ottobre 2025, prosegue nel 2026 |
| **Storia** | Sostituisce il vecchio "Resto al Sud", chiuso definitivamente il 14 ottobre 2025 |

Importi più contenuti di Smart&Start e taglio orientato all'avvio di attività
autonoma più che al progetto di ricerca industriale. **Ma è denaro a fondo
perduto al 100%**: se rientri nel requisito anagrafico va valutato seriamente, e
va confrontato con Smart&Start, non sommato d'ufficio (verifica le regole di
cumulo).

Fonti: [Invitalia — Resto al Sud 2.0](https://www.invitalia.it/incentivi-e-strumenti/resto-al-sud-20) ·
[incentivimpresa.it](https://www.incentivimpresa.it/resto-al-sud-2026-contributi-fondo-perduto-mezzogiorno/) ·
[SprintX](https://sprintx.it/blog/resto-al-sud/)

---

## A3. StartCup Campania / Premio Nazionale per l'Innovazione *(gratuito)*

Business plan competition regionale promossa dagli atenei campani — Federico II,
Vanvitelli, Parthenope, Suor Orsola Benincasa, Sannio, Salerno, L'Orientale — con
Sviluppo Campania. Percorso di formazione e accompagnamento che porta alla
stesura di un business plan valutato da esperti indipendenti. I vincitori
regionali accedono al Premio Nazionale per l'Innovazione.

**È esattamente il formato che chiedevi: si presenta un'idea, non un prodotto
finito.** Non dà capitale significativo, ma dà tre cose che oggi ti mancano più
del capitale: un business plan validato da terzi, visibilità, e contatti
nell'ecosistema campano — inclusi possibili primi clienti industriali.

L'edizione 2026 è stata presentata il **29 aprile 2026** alla Federico II, quindi
**la finestra 2026 è con ogni probabilità già chiusa**; l'edizione 2027 dovrebbe
aprirsi in primavera.

**[DA VERIFICARE]**, e vale la pena farlo subito: se la partecipazione sia
aperta anche a chi non proviene dagli atenei campani. Molte Start Cup hanno una
categoria aperta a chiunque. Se sì, metti in calendario la primavera 2027.

Fonti: [Unina — Start Cup Campania 2026](https://www.unina.it/it/w/start-cup-campania-2026) ·
[Sviluppo Campania](https://www.sviluppocampania.it/2026/04/20/le-competenze-per-fare-impresa-un-evento-per-inaugurare-ledizione-2026-della-business-plan-competition-regionale/)

---

## A4. EIC Accelerator — Commissione Europea *(opzione gratuita)*

| | |
|---|---|
| **Cosa dà** | Grant fino a €2.500.000 + equity da €1M a €10M |
| **Stage 1 (proposta breve)** | **Sempre aperto**, esito in 4–6 settimane |
| **Stage 2 (proposta completa)** | Solo su cut-off. 2026: 7 gen, 4 mar, 6 mag, 8 lug, 2 set, **4 nov** |

Lo Stage 1 è gratuito e si presenta quando vuoi: è una proposta breve, cioè
proprio un'idea. Anche un rifiuto costa poco e insegna molto. Detto questo,
l'EIC seleziona una percentuale bassissima e premia la validazione di mercato —
**non costruirci sopra il piano finanziario**.

Fonti: [EIC](https://eic.ec.europa.eu) ·
[Zabala](https://www.zabala.eu/news/eic-accelerator-2026-updates/) ·
[Innovation Manager](https://innovation-manager.com/eic-accelerator-2026-key-application-dates-announced/)

---

# B. Strumenti che richiedono la società già iscritta

## B1. Campania Start-up — PR FESR Campania 2021-2027 / Sviluppo Campania

| | |
|---|---|
| **Cosa fa** | Sostegno alla creazione e al consolidamento di startup innovative e spin-off della ricerca |
| **Dotazione** | Risorse a valere sul PR FESR 21-27; le comunicazioni regionali parlano di **30 milioni** complessivi per lo sviluppo di prodotti e servizi, con una linea da 15 milioni sulla creazione di nuove imprese ad alta innovazione |
| **Chi può** | Micro e piccole imprese costituite da **non più di 48 mesi** dalla data di pubblicazione dell'avviso **e già iscritte alla sezione speciale startup innovative** |
| **Vincolo tematico** | Il progetto deve ricadere nelle **aree di specializzazione RIS3 Campania** |
| **Fondo perduto** | I bandi FESR Campania si collocano tipicamente fra il **30% e il 65%**, spesso in combinazione con tasso zero |
| **Scadenze** | Finestra riportata come aperta fino a nuove comunicazioni |

**[DA VERIFICARE], ed è la verifica decisiva**: che l'automazione industriale /
IIoT rientri nelle aree RIS3 della Campania. Le RIS3 campane storicamente
includono aerospazio, trasporti, biotecnologie, energia/ambiente, beni culturali
e **materiali avanzati/manifattura**: la piattaforma di supervisione industriale
dovrebbe rientrare, ma va letta la classificazione ufficiale e scritta la
domanda nel linguaggio di quell'area. È il tipo di dettaglio che fa scartare una
domanda buona.

Questo è lo strumento con la percentuale di contributo potenzialmente più alta
di tutto l'elenco, ma arriva **dopo** l'iscrizione come startup innovativa.

Fonti: [Regione Campania — 30 milioni per le startup](https://www.regione.campania.it/regione/it/news/primo-piano/startup-innovative-dalla-regione-campania-30-milioni-di-euro-per-lo-sviluppo-di-prodotti-e-servizi) ·
[PR FESR Campania 21-27 — bandi](https://prfesr2127.regione.campania.it/category/opportunita-e-bandi/) ·
[Sviluppo Campania — bandi e agevolazioni](https://www.sviluppocampania.it/bandi-e-agevolazioni/) ·
[Avviso Campania Start-up](https://porfesr.regione.campania.it/it/news/primo-piano/avviso-campania-start-up)

---

## B2. Fondo per la Crescita Sostenibile — MIMIT *(più avanti)*

Ricerca industriale e sviluppo sperimentale, sportelli tematici (tecnologie
critiche ed emergenti STEP, elettronica innovativa, economia circolare). Importi
maggiori, ma strutturato per partenariati e per soggetti con capacità di
rendicontazione. Da riprendere dopo il primo anno, magari in cordata con un
integratore o con un dipartimento universitario campano.

Fonte: [MIMIT](https://www.mimit.gov.it)

---

## Non applicabile: Voucher 3i

Fino a €4.000 per brevetto. Il software in quanto tale non è brevettabile in
Italia se non come parte di un'invenzione tecnica. La tutela pertinente nel tuo
caso è il diritto d'autore — la registrazione SIAE di cui sopra, che ti serve
comunque. Non contarci come finanziamento.

---

## Contesto che è cambiato e conviene sapere

**Transizione 5.0 non esiste più.** Dal 1° gennaio 2026 il credito d'imposta è
decaduto: la Legge di Bilancio 2026 lo ha sostituito con un **iperammortamento**
sui beni strumentali tecnologicamente avanzati, che **non richiede più il vincolo
di efficientamento energetico**.

Perché ti riguarda commercialmente: sotto il 5.0 il credito richiedeva di
misurare il risparmio energetico, e quindi richiedeva un software di
monitoraggio. Era un argomento di vendita che si vendeva da solo. **Non c'è
più.** Resta il requisito 4.0 di interconnessione della macchina al sistema di
fabbrica, che OpenEdge soddisfa — **[DA VERIFICARE]** nel testo attuativo
dell'iperammortamento 2026, perché cambia il pitch commerciale.

Fonti: [PMI.it](https://www.pmi.it/impresa/normativa/487154/transizione-5-0-scadenze-incentivi-raccordo-2026.html) ·
[orki.green](https://orki.green/it/articolo/transizione-5-0)

---

## Ordine consigliato, con le dipendenze esplicite

| # | Azione | Dipende da | Tempi indicativi |
|---|---|---|---|
| 1 | **Registrazione SIAE del software** | nulla | settimane, poche centinaia di € |
| 2 | **Stesura del piano d'impresa** (parti da `BUSINESS-PLAN.md`) | nulla — si può fare in parallelo a 1 | settimane |
| 3 | **Domanda Smart&Start** come team di aspiranti imprenditori | 1 e 2 | sportello, ~60 gg di istruttoria |
| 4 | **Costituzione società + sezione speciale startup innovative** | 1 (e 3, se accolta) | settimane |
| 5 | **Campania Start-up (FESR)** | 4 | finestra aperta, da verificare |
| 6 | **Resto al Sud 2.0**, se hai meno di 35 anni | — | sportello aperto |
| 7 | **EIC Accelerator Stage 1** | 2 | sempre aperto, esito 4–6 settimane |
| 8 | **StartCup Campania** | 2 | edizione 2027, primavera |

I punti 1 e 2 dipendono solo da te e non hanno scadenze: **inizia da lì, oggi**.

**E l'impianto pilota?** Non è un passo burocratico e non compare in questa
tabella, ma è ciò che decide il colloquio di merito di Smart&Start e la
valutazione di Campania Start-up. Costa €800–2.000 di hardware. Va fatto in
parallelo al punto 2, non dopo il punto 3 — perché, ricorda, le spese sostenute
prima della domanda non sono rendicontabili: quei due mila euro li perdi come
contributo e li guadagni come credibilità. È uno scambio che conviene.
