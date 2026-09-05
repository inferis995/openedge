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
| Server OPC UA (essere letti da MES/ERP) | **ASSENTE** | `internal/opcua` è solo client: OpenEdge legge, non si fa leggere |
| Ridondanza / failover | **ASSENTE** | nessuna traccia nel codice |
| Store-and-forward sul gateway | **ASSENTE** | i driver riconnettono, ma il client MQTT usa il MemoryStore: i campioni prodotti a link caduto si perdono al riavvio |
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
2. **Piattaforma commerciale indipendente** (Ignition, AVEVA, ThingWorx e —
   soprattutto in Italia — **Movicon.NExT**). Tecnicamente ottime, ma il modello
   di licenza e il costo di ingresso sono tarati su impianti grandi.
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

### Il concorrente che incontrerai davvero: Movicon.NExT

Un elenco di concorrenti che nomina Siemens, Rockwell e AVEVA e non nomina
Movicon descrive il mercato mondiale, non quello in cui venderai. In una PMI
manifatturiera italiana, la piattaforma che è già installata — o che
l'integratore propone per abitudine — con ogni probabilità è quella.

**Che cos'è.** SCADA/HMI di Progea, azienda di Modena, **acquisita da Emerson
nell'ottobre 2020**. Piattaforma client/server costruita sul modello
informativo OPC UA, che copre supervisione, HMI, allarmi, ricette, historian,
scheduler, logiche e analisi MES.

**Come si vende, ed è il punto che ti riguarda.** Le licenze runtime sono
**modulari sul numero di tag** dichiarati nel progetto, da 50 fino a 100.000, e
le funzioni si acquistano separatamente. Runtime in versione Server, Client o
Client/Server, di tipo PRO o LT; l'Editor per sviluppare è un'opzione a parte —
un distributore listava l'Editor V4 a circa €3.000 IVA inclusa. I prezzi non
sono pubblici: si passa dal commerciale.

**Dove OpenEdge perde, e va detto prima di sentirselo dire in una trattativa:**

- venticinque anni di installato, driver certificati e collaudati su migliaia
  di impianti; OpenEdge su zero;
- una rete di integratori italiani già formata sul prodotto, che non ha nessuna
  ragione di imparare il tuo;
- Emerson dietro, cioè un fornitore che non fallisce — argomento decisivo per
  chi compra un sistema che deve durare quindici anni;
- assistenza in italiano, con un numero da chiamare.

**Dove c'è spazio, e discende dal loro stesso modello:**

- **il conteggio dei tag.** Un impianto con 20.000 tag paga in proporzione. Un
  prezzo per sito a tag illimitati non è una furbizia commerciale, è una
  differenza strutturale, e si spiega in una frase a chi ha appena ricevuto un
  preventivo scalato sui tag;
- **la multi-tenancy.** Una licenza governa un'installazione. Un integratore che
  serve venti clienti ne compra venti; con OpenEdge ne installa una e li tiene
  isolati per `org_id`. È l'argomento di margine per il canale, e nessuno dei
  concorrenti citati lo offre nella fascia PMI;
- **la stessa immagine on-prem e in cloud**, con entrambi i percorsi eseguiti
  dalla pipeline a ogni commit;
- **l'operabilità da agente AI** (CLI, server MCP, skill). Su questo il
  confronto non esiste: nessuno dei prodotti sopra ha un equivalente.

**[DA VERIFICARE]** Se Movicon.NExT giri anche su Linux o sia di fatto legato a
Windows/.NET. È una verifica che vale la pena fare bene: se è vincolato a
Windows, il costo di licenza del sistema operativo su ogni gateway di campo
diventa un tuo argomento, e non piccolo, su un cliente con dieci gateway.

**Come NON posizionarsi.** Non contro Movicon sulle funzioni: perderesti, e
giustamente. La domanda a cui rispondi meglio di loro è un'altra — *ho più
stabilimenti, o servo più clienti, e non voglio moltiplicare licenze e
installazioni.*

### E il confronto vero: Movicon Connext

Movicon.NExT è lo SCADA. **Connext è l'altro prodotto della stessa famiglia, e
sta esattamente sullo strato di OpenEdge**: server OPC UA, I/O data server,
gateway e motore di connettività IIoT, con dentro Gateway, Historian, Alarms &
Condition, IIoT e **ridondanza**. Porta il dato dal campo verso SCADA, Plant
Analytics o ERP, e lo registra su qualunque database o sul cloud. **Gira anche
su Linux**, per sistemi embedded e IoT.

È il confronto che conta, e va fatto onestamente in tutte e due le direzioni.

**Dove OpenEdge è più largo.** Connext è headless: acquisisce e smista, ma per
vedere qualcosa serve Movicon.NExT o un altro SCADA sopra. OpenEdge è tutto lo
stack — acquisizione, storico, allarmi, notifiche, sinottici e interfaccia web —
in un prodotto solo, multi-tenant. Per una PMI che vuole un sistema e non due
licenze da comporre, è una differenza concreta.

**Dove OpenEdge è indietro, e sono due cose serie:**

- **Non è un server OPC UA.** `internal/opcua` è solo client: OpenEdge legge dai
  PLC, ma nessun MES, ERP o SCADA di terzi può leggere da OpenEdge in OPC UA. In
  una fabbrica che ha già un sistema gestionale, questa è spesso la prima
  domanda del reparto IT. Le vie d'uscita esistono — REST, Sparkplug B su MQTT,
  l'API i3X — ma non sono ciò che chiedono, e "abbiamo un'API REST" non risponde
  a "esponi OPC UA?".
- **Non c'è ridondanza.** Nessuna: né failover, né standby, né registrazione
  ridondata. Connext ce l'ha e la mette in prima pagina, perché in un impianto
  serio è un requisito di gara, non una funzione desiderabile.

### La loro descrizione, voce per voce, contro il codice

Connext si presenta così: *"server OPC UA, I/O data server, gateway e motore di
connettività IIoT... gateway di rete, historian e data logger, ridondanza e
protocolli IIoT client, con massima sicurezza e prestazioni."* Presa alla
lettera e confrontata con il repository:

| Quello che dichiara Connext | OpenEdge, verificato |
|---|---|
| Server OPC UA | **No.** `internal/opcua` espone solo `NewClient` |
| I/O data server | **Sì.** Tag engine con driver Modbus, S7, OPC UA, MQTT, LoRaWAN, Redis |
| Gateway | **Sì.** `driver-manager` avvia i container driver; MQTT e Sparkplug B |
| Motore di connettività IIoT | **Parziale.** MQTT e Sparkplug B nativi, export InfluxDB v2. Niente Azure IoT Hub, AWS IoT, Kafka |
| Historian e data logger | **Sì, ma centralizzati.** `engine-historian` + TimescaleDB stanno al centro, non sul gateway |
| Ridondanza | **No.** Nessuna occorrenza nel codice |
| Massima sicurezza | **Forte.** JWT, RBAC, isolamento per `org_id`, identità MQTT per organizzazione, TLS |
| Massime prestazioni | **Non misurata.** Nessun impianto, quindi nessun numero da mostrare |

### La terza lacuna, che è la più seria delle tre

L'historian centralizzato porta con sé una conseguenza che nessuno scopre finché
la linea non cade: **non c'è store-and-forward**.

Verificato: i driver riconnettono con backoff — `SetAutoReconnect`,
`SetConnectRetry` — ma il client MQTT non imposta nessuno store persistente,
quindi resta il MemoryStore di default. I campioni prodotti mentre il broker è
irraggiungibile vivono solo in memoria e **si perdono al riavvio del processo**.
Non c'è un buffer su disco che sopravviva a un riavvio o a un'interruzione
lunga.

Per un prodotto che si vende come gateway questo è il difetto che fa perdere la
fiducia: un buco nello storico è la cosa che il capo reparto nota per primo, e
l'unica per cui non esistono attenuanti. Connext mette historian e data logger
sul gateway proprio per questo.

**Che cosa farne.** Sono le tre voci di sviluppo con il ritorno commerciale più
alto dopo il pilota, e vanno nel piano di Smart&Start — che finanzia esattamente
questo, l'industrializzazione di una tecnologia esistente. Non nel senso di
"aggiungiamo funzioni": nel senso che senza server OPC UA, senza
ridondanza e senza store-and-forward ci sono gare a cui non puoi nemmeno
presentarti, e questo va scritto nel piano d'impresa invece che scoperto alla
prima trattativa persa.

Fra le tre, **lo store-and-forward viene prima**: è la meno costosa da
realizzare, è quella che un cliente verifica staccando un cavo, ed è l'unica che
senza di essa rende discutibili i dati di tutte le altre.

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

### Dove cercarlo, in Campania

Il **Distretto Aerospaziale Campano** riunisce oltre 170 soggetti, di cui più di
130 PMI subfornitrici, attorno a committenti come Leonardo, Magnaghi, DEMA e
Atitech. La Campania è prima in Italia per addetti del settore aerospaziale.

Quelle PMI subfornitrici sono il profilo 2 di questo elenco quasi alla lettera:
20-250 addetti, macchine con PLC, standard di qualità e tracciabilità imposti
dal committente, nessuna supervisione centralizzata.

E c'è un secondo motivo, che riguarda i finanziamenti. *"Factory 4.0 per
l'aeronautica e lo spazio"* è una traiettoria tecnologica esplicita della RIS3
Campania (vedi `FINANZIAMENTI.md`). Un pilota in un subfornitore aeronautico
campano vale quindi due volte: è la referenza commerciale che manca, ed è la
prova che il progetto sta dentro il dominio su cui la Regione concentra più
risorse. Nessun altro pilota fa entrambe le cose.

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
| 1 | Registrazione SIAE del software | Requisito per l'iscrizione come startup innovativa |
| 1–2 | Impianto pilota con PLC reale, dati storicizzati | Vendita, valutazione, punteggio nei bandi |
| 2 | Caso studio con numeri misurati | Colloquio di merito, fase canale |
| 2–3 | Domanda Smart&Start **come team di aspiranti imprenditori** | In Campania: 30% a fondo perduto + 70% a tasso zero |
| 3–4 | Costituzione società + iscrizione sezione speciale startup innovative | Bando regionale, Fondo di Garanzia PMI, agevolazioni fiscali |
| 4–5 | Domanda Campania Start-up (PR FESR) | Fondo perduto FESR, tipicamente 30–65% |
| 5–7 | Primo cliente pagante | Trasforma la valutazione da "asset" a "azienda" |
| 6–9 | Hardening: penetration test esterno, backup/restore provato su dati veri | Requisito per clienti industriali seri |
| 9–12 | Secondo e terzo sito, primo integratore a contratto | Ricorrente sopra i €20k |

La società arriva dopo la domanda Smart&Start, non prima: lo strumento accetta
un team di persone fisiche che si impegna a costituirla se la domanda è accolta,
e così i costi di costituzione cadono *dopo* la domanda, dove sono
potenzialmente ammissibili. Se preferisci costituire subito per altre ragioni
puoi farlo — perdi solo quella copertura.

Il miglioramento del codice che stai già facendo si inserisce nel mese 6–9. Fino
ad allora, ogni ora spesa sul codice invece che sul pilota rimanda la sola cosa
che cambia il valore del progetto.

---

## 9. Fabbisogno e impiego dei fondi

Ipotesi di progetto su 24 mesi, dimensionato per stare nella fascia bassa di
Smart&Start Italia.

> **Regola che vincola il piano**: sono ammissibili solo le spese sostenute
> **dopo** la presentazione della domanda, e il piano deve essere avviato dopo la
> domanda. Le 90.000 righe già scritte non sono quindi rendicontabili — e non
> devono comparire in questa tabella. Il loro posto è la valutazione di merito,
> dove valgono come riduzione del rischio: la tecnologia esiste già, il progetto
> finanzia la sua validazione sul campo e la sua industrializzazione, non la sua
> scrittura. Vedi `FINANZIAMENTI.md`, prima sezione.

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

Su Smart&Start, essendo la Campania fra le 8 regioni del Mezzogiorno, la
copertura si articola in **30% a fondo perduto e 70% a tasso zero**: su €217.000
significa circa €65.000 non restituiti e €152.000 da restituire senza interessi.

Entrambe le verifiche aperte sono state chiuse, e la tabella regge:

- **la fascia ammessa è €100.000 – €1.500.000**: €217.000 sta sopra la soglia
  minima e lontano dal tetto;
- **il costo del personale è ammissibile** — personale dipendente e
  collaboratori a qualsiasi titolo, nella misura in cui sono impiegati
  funzionalmente nel piano. Era la voce più pesante qui sopra, €140.000 su
  €217.000, ed è coperta.

Le spese vanno sostenute nei 24 mesi successivi alla firma del contratto, e il
personale va pagato prima di rendicontarlo — quindi serve cassa per anticipare,
che è la ragione per cui la voce capitale circolante non è decorativa.

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

- ~~Regione~~ — **Campania**, quindi Mezzogiorno: Smart&Start eroga il 30% a
  fondo perduto, ed è accessibile il canale regionale PR FESR / Sviluppo
  Campania, dove **Manifattura 4.0 è un'area RIS3 esplicita**. Resta da
  precisare il comune, per i bandi camerali e provinciali.
- Età anagrafica e composizione dei soci — determina l'accesso a **Resto al Sud
  2.0** (under 35, contributo interamente a fondo perduto) e a ON.
- Se esiste già una società e da quanto — il limite dei 60 mesi vale per quasi
  tutti gli strumenti nazionali.
- I contatti industriali già disponibili (sezione 5).
- Se hai già speso ore/denaro sul progetto e in che forma — alcune spese
  pregresse sono rendicontabili, altre no, e la data di costituzione conta.

---

*Documento di lavoro interno. Non è consulenza finanziaria né legale: prima di
presentare una domanda, i requisiti vanno riletti sul testo del bando vigente.*
