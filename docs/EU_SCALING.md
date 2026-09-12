# Conversione in unità ingegneristiche (EU scaling)

## Dov'è il confine

La conversione avviene **nel driver, una volta sola, sul valore in uscita**.
Da quel punto in poi tutto il prodotto parla unità ingegneristiche: il topic
`data/#`, l'historian, gli allarmi, InfluxDB, il WebSocket, la sincronizzazione
cloud.

```
PLC ──raw──> driver ──scaling.Apply()──> EU ──> MQTT ──> tutti i consumatori
                         │
                         └─ allarmi (stessa conversione, stesso valore)
```

Il percorso di scrittura va nella direzione opposta e non è cambiato:
`scaling.Reverse` converte EU → raw prima che il comando raggiunga il device
(`internal/handlers/write_scaling.go`, ricette, i3X).

## Com'era prima

La conversione esisteva in un punto solo: `handleDataUpdate` in core-api, che
alimenta Redis, il WebSocket, InfluxDB e la shadow. L'historian è un
sottoscrittore MQTT indipendente e riceveva il valore grezzo, quindi:

- il **manometro** di un tag scalato mostrava 25,3 bar;
- il **trend dello stesso tag** mostrava 6912;
- una **soglia di allarme** digitata in bar veniva confrontata con i conteggi
  grezzi — su un trasmettitore 0..27648 qualunque soglia sotto il fondo scala
  risultava superata alla prima lettura;
- la **banda morta** `historize_deadband`, anch'essa in unità ingegneristiche
  nella maschera, era confrontata con i grezzi;
- in modalità **Sparkplug** non c'era conversione da nessuna parte.

Il commento su `models.Tag.ScalingEnabled` descriveva già il comportamento
voluto — *"conversion applied at ingestion... the stored/broadcast value is
already in EU"* — che il codice non realizzava.

## Cosa cambia in un impianto già in esercizio

Solo per i tag con `scaling_enabled = true`. Chi lavora in conteggi grezzi non
è toccato: `scaling.Apply` con la configurazione disabilitata restituisce il
valore invariato.

1. **Le righe già in `tag_history` restano grezze.** Il trend di un tag scalato
   mostra un gradino nel momento dell'aggiornamento. Non è un errore ed è
   inevitabile con qualunque correzione: i due tratti sono in unità diverse.
   La conversione delle righe storiche è deliberatamente **non** automatica —
   è una riscrittura irreversibile di una hypertable. Chi la vuole:

   ```sql
   -- Verificare PRIMA su una finestra breve. Non reversibile.
   UPDATE tag_history h
      SET value = (h.value - t.scaling_raw_min)
                  / NULLIF(t.scaling_raw_max - t.scaling_raw_min, 0)
                  * (t.scaling_eu_max - t.scaling_eu_min) + t.scaling_eu_min
     FROM tags t
    WHERE t.id = h.tag_id
      AND t.scaling_enabled
      AND h.time < '<istante dell aggiornamento>';
   ```

2. **Le soglie di allarme cominciano a funzionare come erano state scritte.**
   Un allarme che era permanentemente attivo perché confrontato con i grezzi si
   spegne; uno che non scattava mai comincia a scattare. **Vanno riviste le
   definizioni di allarme sui tag scalati prima di aggiornare un impianto.**

3. **La banda morta comincia a valere in unità ingegneristiche.** Una banda di
   0,5 su un tag 0..27648 → 0..100 filtrava prima variazioni di mezzo
   conteggio (cioè nulla); adesso filtra mezzo bar.

## L'aggiornamento graduale

I container dei driver scendono via OTA, core-api con lo stack: per la durata
di un aggiornamento sullo stesso broker convivono driver nuovi e vecchi. Il
payload porta perciò `"eu": true` quando la conversione è già stata fatta, e
core-api converte **solo** i payload che non la portano
(`services/core-api/main.go`, `handleDataUpdate`).

Un valore in bar e lo stesso numero in conteggi grezzi sono entrambi letture
plausibili: senza quella bandiera non c'è modo di distinguerli, e l'errore è
silenzioso in entrambe le direzioni. Il ramo di compatibilità in core-api si
può togliere quando in campo non resta nessun driver vecchio.

## Cosa impedisce che si rompa di nuovo

`internal/scaling/drivers_test.go` legge il sorgente dei driver e verifica che:

- ogni driver carichi i tag tramite `models.DriverTagColumns` e
  `models.ScanDriverTag` (il difetto originale è nato così: le colonne sono
  state aggiunte alla tabella e le sei `SELECT` scritte a mano no);
- ogni percorso di pubblicazione converta, **esattamente una volta**;
- chi converte metta la bandiera sul payload;
- nessuno si dichiari una copia privata del payload.
