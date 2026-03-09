# PRD: Gateway & MQTT Write Fixes

**Data:** 2024-03-09
**Priorità:** Alta
**Stato:** In Analisi

## Problemi Identificati

### 1. MODBUS Driver Non Creato Quando Si Crea Gateway
**Descrizione:** Quando si crea un nuovo gateway con driver_type "MODBUS_TCP", il container del driver MODBUS non viene creato automaticamente.

**Root Cause Analisys:**
- Il driver-manager polla il database ogni 10 secondi
- Quando un gateway viene creato con `enabled = true`, il driver-manager DOVREBBE creare il container
- Possibili cause:
  - Il gateway viene creato con `enabled = false` di default?
  - Il driver-manager non è in esecuzione?
  - Il poll interval di 10 secondi crea un ritardo percepito come "non funziona"?

**Soluzione Proposta:**
- Verificare che i gateway vengano creati con `enabled = true` di default
- Aggiungere un trigger MQTT per notificare immediatamente il driver-manager quando un gateway viene creato
- Ridurre il poll interval o aggiungere una sincronizzazione immediata

---

### 2. Zero-Based Addressing Non Salvato in Creazione Gateway
**Descrizione:** Quando si crea un gateway MODBUS con zero_based=true, il valore non viene salvato. Bisogna modificare il gateway dopo la creazione per impostarlo correttamente.

**Root Cause Analisys:**
```typescript
// services/web-ui/src/types/index.ts - riga 66-84
export interface CreateGatewayDto {
    area_id: number;
    name: string;
    driver_type: 'S7' | 'MODBUS_TCP' | 'MQTT' | 'OPC_UA';
    // ... altri campi ...
    scan_rate_ms: number;
    enabled: boolean;
    org_id?: number;
    // MANCA zero_based!!!
}
```

Il frontend gestisce `zero_based` correttamente nel form (Switch UI, stato locale), ma il campo NON è definito nel DTO!

**Soluzione Proposta:**
Aggiungere `zero_based?: boolean;` all'interfaccia `CreateGatewayDto`

---

### 3. Cloud MQTT Write a OPC UA Fails (StatusBadTypeMismatch) - FIX IMPLEMENTATO
**Descrizione:** I comandi di write dal cloud broker al PLC OPC UA falliscono con errore `StatusBadTypeMismatch (0x80740000)`.

**Root Cause Analisys:**
- Server OPC UA riporta tipo: Boolean (NodeID: i=1)
- Valore corrente sul server: TypeIDBoolean, Value=false
- Abbiamo provato:
  - Boolean (Go bool) → FALLITO
  - SByte (int8, value 1) → FALLITO
  - Int16 (int16, value 1) → FALLITO
- Tutti falliscono con: "The value supplied for the attribute is not of the same type as the attribute's value"
- UA Expert riesce a scrivere lo stesso nodo

**Ipotesi:**
1. Il server potrebbe richiedere un encoding specifico del valore che non stiamo usando
2. Potrebbe essere un problema specifico del namespace (ns=2) - namespace personalizzato
3. Potrebbe servire leggere il valore attuale e ri-scriverlo con lo stesso encoding
4. Il gopcua library potrebbe avere un bug con questo server specifico

**Soluzione Implementata (2025-03-09):**
Implementato multi-strategy write con fallback in `internal/opcua/client.go`:
- Prova多种 tipi in ordine: Boolean, Int16, Int32, SByte, Byte, UInt16, UInt32
- Ogni tentativo è loggato con dettagli per diagnosi
- Si ferma al primo successo
- Se tutti falliscono, ritorna l'ultimo errore

**Prossimi Passi:**
1. Testare con gateway 138 (tag: hmi_cfg_enablevalvola_1)
2. Verificare nei log quale tipo il server accetta
3. Ottimizzare il codice per usare solo il tipo corretto

---

## Piano di Implementazione

### Fase 1: Fix Zero-Based (Priorità ALTA - Bloccante)
1. [ ] Aggiungere `zero_based?: boolean;` a `CreateGatewayDto`
2. [ ] Testare creazione gateway MODBUS con zero_based=true
3. [ ] Verificare che il valore venga salvato correttamente nel DB

### Fase 2: Debug MODBUS Driver Creation (Priorità MEDIA)
1. [ ] Verificare cosa succede quando si crea un gateway:
   - Controllare il valore di `enabled` nel DB dopo creazione
   - Controllare i log del driver-manager
   - Verificare che il container venga creato
2. [ ] Se necessario, fix per trigger immediato del driver-manager

### Fase 3: Cloud MQTT Write (Priorità ALTA - Complesso)
1. [ ] Aggiungere ulteriore debug logging per confrontare con UA Expert
2. [ ] Provare soluzioni alternative per il tipo di dato
3. [ ] Testare con altri nodi OPC UA per vedere se è specifico a ns=2;i=3

---

## Note Aggiuntive

**Utente:** Ha confermato che UA Expert riesce a scrivere sul nodo
**Ambiente:** OpenPLC server su 192.168.1.231:4849
**Tag problematico:** hmi_cfg_enablevalvola_1 (ID 138, ns=2;i=3)
