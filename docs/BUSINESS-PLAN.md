# OpenEdge — Business Plan (bozza di lavoro)

> **Stato**: bozza. Le sezioni marcate `[DA COMPILARE]` richiedono dati che solo il
> fondatore possiede (residenza fiscale, forma societaria, contatti industriali).
> Ogni numero in questo documento è etichettato come **misurato**, **ipotesi** o
> **da verificare**. Un business plan che confonde le tre cose non supera una
> valutazione di merito Invitalia, e non serve nemmeno a chi lo scrive.
>
> Ultimo aggiornamento: 2026-09-05.

---

## 1. Che cosa è OpenEdge

Piattaforma software per la supervisione e l'acquisizione dati di impianti
industriali (SCADA / IIoT), multi-tenant, installabile sia in fabbrica
(on-prem) sia in cloud. Raccoglie i dati dai PLC e dai dispositivi di campo,
li storicizza, li mostra su sinottici web, genera allarmi e li notifica.

Non è un prototipo: è un sistema completo, con test automatici e pipeline di
rilascio verde. È però un sistema **non ancora collegato a un impianto reale**.
Le due affermazioni convivono, e la seconda è quella che determina sia il valore
di mercato sia il punteggio nei bandi.

---

## 2. Stato reale: cosa è dimostrato e cosa no

Questa tabella è il cuore del documento. Serve a te per sapere dove sei, e serve
a un valutatore per capire che sai dove sei.

| Elemento | Stato | Evidenza |
|---|---|---|
| Backend applicativo | **Dimostrato** | 55.664 righe Go (esclusi i test), 50 handler di dominio |
| Copertura di test | **Dimostrato** | 13.997 righe di test Go, CI verde su 8 job |
| Interfaccia web | **Dimostrato** | 34.794 righe TypeScript/TSX |
| Driver di campo | **Scritti, non provati sul campo** | `driver-modbus`, `driver-opcua`, `driver-s7`, `driver-mqtt`, `driver-lorawan` |
| Deploy on-prem | **Dimostrato in CI** | suite e2e completa sullo stack Docker |
| Deploy cloud | **Dimostrato in CI** | job `e2e-vps` sull'overlay Traefik + Let's Encrypt |
| Multi-tenancy | **Dimostrato** | isolamento per `org_id` su tutte le query, test dedicati |
| Sicurezza broker MQTT | **Dimostrato** | identità per-organizzazione read-only, anonimo disabilitato |
| Automazione via AI (CLI + MCP + skill) | **Dimostrato** | CLI completa, server MCP con 39 tool, skill `openedge` e `openedge-ops` |
| **Collegamento a un PLC reale** | **MAI FATTO** | — |
| **Impianto in produzione** | **NESSUNO** | — |
| **Clienti paganti** | **ZERO** | — |
| **Certificazioni / audit di terzi** | **NESSUNA** | — |

Le ultime quattro righe sono, insieme, l'unica cosa che separa questo progetto
da un progetto finanziabile e vendibile. Tutto il resto è già fatto.

---

## 3. Il problema

Una PMI manifatturiera italiana che vuole vedere i dati dei propri impianti ha
oggi tre strade, tutte con un costo che non è solo la licenza:

1. **Suite del costruttore del PLC** (Siemens WinCC, Rockwell FactoryTalk).
   Funziona, ma lega l'impianto a un fornitore, costa in licenze per postazione
   e per tag, e non è pensata per essere multi-sito o multi-cliente.
2. **Piattaforma commerciale indipendente** (Ignition, AVEVA, ThingWorx).
   Tecnicamente ottima, ma il modello di licenza e il costo di ingresso sono
   tarati su impianti grandi.
3. **Stack open-source assemblato** (Node-RED + InfluxDB + Grafana, o
   ThingsBoard). Costo di licenza zero, costo di integrazione e manutenzione a
   carico di qualcuno — di solito un system integrator che deve rimontare lo
   stesso puzzle a ogni cliente.

Chi resta scoperto è la fascia in mezzo: l'azienda con 1–5 stabilimenti che
vuole un sistema completo, che non vuole restare legata a un costruttore, e che
non ha un reparto IT per tenere in piedi uno stack assemblato a mano.

**[DA VERIFICARE]** La dimensione di questo segmento in Italia va quantificata
con dati ISTAT sul manifatturiero per classe dimensionale prima di scrivere un
numero in un bando. Non inventarne uno.

---

## 4. Prodotto e differenziazione

Tre cose distinguono OpenEdge dalle alternative sopra. La terza è quella che, se
dimostrata su un impianto vero, diventa un argomento di vendita che nessun
concorrente ha oggi.

**a. Stesso software on-prem e in cloud.** Non due prodotti. La stessa immagine
Docker, con un overlay diverso. Per il cliente questo significa: si parte in
fabbrica, si passa al cloud quando serve, senza migrazione. Entrambi i percorsi
sono eseguiti dalla pipeline a ogni commit.

**b. Multi-tenancy nativa.** Un system integrator può servire N clienti da una
sola installazione, con isolamento dei dati garantito a livello di query. Questo
trasforma l'integratore da cliente a canale di vendita.

**c. Operabilità da agente AI.** OpenEdge espone le sue funzioni via CLI
e via server MCP (39 tool), con due skill pronte (`openedge` per l'uso,
`openedge-ops` per l'esercizio). Un assistente AI può quindi diagnosticare un
allarme, interrogare lo storico, verificare lo stato dei gateway. Non è una
funzione cosmetica: è l'unica risposta credibile al fatto che una PMI non ha un
turnista che guarda i sinottici alle tre di notte.

Il punto (c) è anche l'angolo di innovazione più difendibile in un bando: non
"abbiamo fatto uno SCADA", ma "abbiamo fatto uno SCADA che un agente può
operare". Quella è una frase che regge una valutazione di merito, purché sia
supportata da un impianto reale.

---

## 5. Cliente ideale (ICP)

In ordine di facilità di chiusura, non di dimensione:

1. **System integrator / quadrista** con 5–30 impianti già installati presso i
   propri clienti. Compra una volta, rivende N volte. È il canale, non il
   cliente finale. Ha già le relazioni, i PLC in casa e il personale tecnico.
2. **PMI manifatturiera 20–250 addetti, 1–3 stabilimenti**, che ha già dei PLC
   ma nessuna supervisione centralizzata. È il cliente finale che paga di più
   per il minor sforzo di vendita — ma serve una referenza per entrare.
3. **Impianti tecnologici distribuiti** (depurazione, teleriscaldamento,
   fotovoltaico su più siti) dove il valore della multi-tenancy e del cloud è
   immediato. Ciclo di vendita più lungo, spesso pubblico.

**Primo cliente da cercare: il numero 1.** Un integratore che accetti di fare un
impianto pilota gratuito o quasi in cambio della licenza a vita. Serve una
persona, non un mercato.

**[DA COMPILARE]** Elenca qui i nomi che conosci già: integratori, quadristi,
ex colleghi, aziende della tua zona. Questa lista vale più delle prossime tre
sezioni messe insieme.

---

## 6. Modello di ricavo — ipotesi da validare

Tutti i numeri di questa sezione sono **ipotesi**. Diventano dati solo dopo il
primo cliente che paga.

| Voce | Prezzo ipotizzato | Note |
|---|---|---|
| Abbonamento on-prem, per sito | €3.500 / anno | tag illimitati, aggiornamenti inclusi |
| Licenza perpetua on-prem, per sito | €8.000 + 20%/anno manutenzione | per clienti che rifiutano l'abbonamento (frequente nel manifatturiero) |
| SaaS multi-tenant, per sito | €150 / mese | rivolto agli integratori, sconti a volume |
| Messa in opera per impianto | €6.000 – €12.000 | una tantum, a giornate |
| Sviluppo driver dedicato | €4.000 – €10.000 | per protocolli non coperti |

Il ricavo da servizi finanzia i primi due anni; il ricavo ricorrente è ciò che
crea valore d'impresa. Vanno tenuti separati nel conto economico, sempre.

### Scenario a tre anni (ipotesi, non previsione)

| | Anno 1 | Anno 2 | Anno 3 |
|---|---|---|---|
| Siti attivi a fine anno | 3 | 12 | 40 |
| Ricavo ricorrente | €7.000 | €42.000 | €140.000 |
| Ricavo da servizi | €15.000 | €60.000 | €120.000 |
| **Ricavo totale** | **€22.000** | **€102.000** | **€260.000** |

Assunzioni sottostanti, da rendere esplicite in qualsiasi domanda di
finanziamento: (i) due dei tre pilota del primo anno diventano paganti; (ii) un
integratore porta almeno 6 siti nel secondo anno; (iii) il tasso di abbandono
annuo è sotto il 10%. Se una delle tre salta, salta lo scenario — ed è meglio
scriverlo tu che sentirselo dire in commissione.

---

## 7. Go-to-market

**Fase 0 — adesso, senza spendere.** Un impianto pilota. Un PLC vero, un
gateway, tre mesi di dati storicizzati, uno screenshot dei sinottici con dati
reali. Costo stimato in hardware: €800–2.000 (un S7-1200 o un Modbus RTU usato,
un mini PC). Questo è il singolo investimento con il ritorno più alto in tutto
il piano: sblocca contemporaneamente la vendita, la valutazione dell'azienda e
il punteggio nei bandi.

**Fase 1 — referenza.** Trasformare il pilota in un caso studio con numeri:
quanti tag, quale frequenza di campionamento, quale problema ha risolto,
quanto tempo di fermo ha evitato. Senza numeri non è un caso studio, è una
brochure.

**Fase 2 — canale.** Portare il caso studio a 10 integratori. Offrire la
multi-tenancy come argomento di margine per loro, non come funzione tecnica.

**Fase 3 — visibilità.** Fiere di settore (SPS Italia a Parma è quella giusta
per questo mercato), associazioni di categoria territoriali, ANIE Automazione.
Da fare solo dopo la Fase 1: presentarsi a una fiera senza una referenza è
denaro speso per imparare che serviva una referenza.

---

## 8. Roadmap 12 mesi, legata a ciò che sblocca

Ordinata per dipendenza, non per interesse tecnico.

| Mese | Obiettivo | Sblocca |
|---|---|---|
| 1 | Costituzione società + iscrizione sezione speciale startup innovative | Praticamente ogni bando (vedi `FINANZIAMENTI.md`) |
| 1–2 | Impianto pilota con PLC reale, dati storicizzati | Vendita, valutazione, punteggio nei bandi |
| 2–3 | Caso studio con numeri misurati | Fase canale |
| 3–4 | Domanda Smart&Start Italia (sportello, nessuna scadenza) | €250k–400k a tasso zero |
| 4–6 | Primo cliente pagante | Trasforma la valutazione da "asset" a "azienda" |
| 6–9 | Hardening: penetration test esterno, backup/restore provato su dati veri | Requisito per clienti industriali seri |
| 9–12 | Secondo e terzo sito, primo integratore a contratto | Ricorrente sopra i €20k |

Il miglioramento del codice che stai già facendo si inserisce nel mese 6–9. Fino
ad allora, ogni ora spesa sul codice invece che sul pilota rimanda la sola cosa
che cambia il valore del progetto.

---

## 9. Fabbisogno e impiego dei fondi

Ipotesi di progetto su 24 mesi, dimensionato per stare nella fascia bassa di
Smart&Start Italia.

| Voce | Importo | Note |
|---|---|---|
| Compenso fondatore (24 mesi) | €70.000 | |
| Uno sviluppatore dal mese 6 (18 mesi) | €70.000 | lordo azienda |
| Laboratorio: PLC, gateway, sensori, banco prova | €12.000 | |
| Infrastruttura cloud e strumenti (24 mesi) | €12.000 | |
| Costituzione, notaio, commercialista, legale | €10.000 | |
| Penetration test e audit di sicurezza esterni | €15.000 | |
| Fiere, marketing, materiale commerciale | €20.000 | |
| Consulenza per la domanda di finanziamento | €8.000 | opzionale ma consigliata |
| **Totale** | **€217.000** | |

**[DA VERIFICARE]** Smart&Start finanzia progetti a partire da una soglia minima
(comunemente indicata in €100.000): confermare la soglia sul bando vigente prima
di dimensionare il progetto.

---

## 10. Team

**[DA COMPILARE]** Fondatore unico. Un business plan con un solo nome viene
penalizzato in quasi tutte le valutazioni di merito. Due contromisure, entrambe
oneste e realizzabili prima della domanda:

- un secondo socio tecnico o commerciale, anche di minoranza;
- lettere di intenti da un integratore o da un impianto pilota, che dimostrano
  che il progetto ha già un interlocutore industriale.

La seconda è più veloce e pesa quasi quanto la prima.

---

## 11. Rischi, dichiarati

| Rischio | Perché è reale | Mitigazione |
|---|---|---|
| I driver non funzionano sul campo | Non sono mai stati collegati a un PLC | È esattamente ciò che il pilota verifica. Va fatto prima di vendere, non dopo |
| Mercato conservativo | L'automazione industriale compra da chi conosce da vent'anni | Entrare tramite integratori, non in diretta |
| Concorrente open-source gratuito | ThingsBoard e simili costano zero in licenza | Il costo vero è l'integrazione: il confronto va fatto sul costo totale, non sulla licenza |
| Fondatore unico | Rischio di continuità, penalizzato nei bandi | Vedi sezione 10 |
| Responsabilità su impianti industriali | Un allarme perso ha conseguenze fisiche | Assicurazione RC professionale, limitazioni contrattuali, e mai vendere il sistema come funzione di sicurezza (un SCADA non sostituisce un sistema di sicurezza certificato) |

L'ultimo punto non è formale: non promettere mai che OpenEdge svolga funzioni di
sicurezza macchina. Non è certificato per farlo e non lo sarà a breve.

---

## 12. Cosa manca a questo documento

Dati che nessuno può ricostruire dal codice:

- Regione e comune di residenza fiscale — determina quali bandi regionali sono
  accessibili e se rientri nelle 8 regioni del Mezzogiorno (dove Smart&Start
  eroga il 30% a fondo perduto invece che il solo tasso zero).
- Età anagrafica e composizione dei soci — determina l'accesso a ON e a diverse
  misure regionali.
- Se esiste già una società e da quanto — il limite dei 60 mesi vale per quasi
  tutti gli strumenti nazionali.
- I contatti industriali già disponibili (sezione 5).
- Se hai già speso ore/denaro sul progetto e in che forma — alcune spese
  pregresse sono rendicontabili, altre no, e la data di costituzione conta.

---

*Documento di lavoro interno. Non è consulenza finanziaria né legale: prima di
presentare una domanda, i requisiti vanno riletti sul testo del bando vigente.*
