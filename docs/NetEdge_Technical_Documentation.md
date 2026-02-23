# NetEdge Professional - Documentazione Tecnica

## 1. Panoramica del Progetto
**NetEdge** è una piattaforma professionale di Industrial Edge Computing progettata per l'acquisizione, la normalizzazione e la visualizzazione di dati in tempo reale da dispositivi industriali (PLC, sensori, macchinari).

Il sistema adotta un'architettura a **microservizi containerizzati**, garantendo scalabilità, isolamento dei processi e robustezza. È progettato per essere agnostico rispetto all'hardware e supportare protocolli multipli (attualmente Modbus TCP, predisposto per S7/OPC-UA).

---

## 2. Architettura Tecnologica

Il sistema è composto dai seguenti moduli dockerizzati che comunicano tramite bus di messaggi e API.

### Stack Tecnologico
| Componente | Tecnologia | Ruolo |
|------------|------------|-------|
| **Core API** | Go (Golang) + Gin | Backend centrale, REST API, gestione configurazioni e business logic. |
| **Data Broker** | MQTT (Mosquitto) | Bus di comunicazione Real-Time ad alte prestazioni. Pub/Sub. |
| **Driver Manager** | Go (Golang) | Orchestratore intelligente dei driver. Monitora DB e Docker per avviare/fermare i driver on-demand. |
| **Drivers** | Go (Golang) | Container effimeri e indipendenti. Un container per ogni Gateway fisico. |
| **Database** | PostgreSQL | Persistenza dati relazionali (configurazioni, utenti, gerarchie). |
| **Cache/Bus** | Redis | (Opzionale/Legacy) Cache rapida e pub/sub interno. |
| **Web UI** | React + TypeScript | Frontend moderno, SPA, TailwindCSS, Shadcn/UI, Recharts. |

---

## 3. Flusso di Funzionamento (Data Flow)

### 3.1 Acquisizione Dati
1.  L'utente configura un **Gateway** (es. "PLC Linea A") tramite la Web UI.
2.  Il **Core API** salva la configurazione su PostgreSQL.
3.  Il **Driver Manager** rileva la nuova configurazione e avvia un container Docker dedicato (`driver-modbus-X`).
4.  Il **Driver** si connette al dispositivo fisico e inizia il polling.
    *   *Quality Check*: Se il dispositivo non risponde, il driver pubblica immediatamente dati con Quality=BAD.

### 3.2 Distribuzione Real-Time
1.  Il Driver normalizza i dati letti e li pubblica su MQTT nel topic: `data/{org}/{site}/{area}/{gateway}/{tag}`.
2.  I messaggi sono JSON ottimizzati: `{"v": 123.45, "ts": 177000123, "q": 1}`.
3.  Il Broker smista i messaggi a tutti i sottoscrittori (Frontend, Historian, Alarms Engine).

### 3.3 Gestione "Zero-Ghosting"
*   Quando un Gateway viene eliminato/disattivato:
    1.  Il sistema invia messaggi "retained clear" per rimuovere i dati vecchi dal broker.
    2.  Il container del driver viene terminato e rimosso.
    3.  L'interfaccia si pulisce immediatamente.

---

## 4. Scalabilità e Prestazioni

*   **1000+ Tag**: Gestibili senza alcuno stress.
*   **Parallelismo**: Ogni PLC ha il suo processo. Il blocco di un PLC non rallenta gli altri.
*   **Efficienza**: I driver scritti in Go utilizzano pochissima RAM (<20MB ciascuno).
*   **UI Ottimizzata**: Il frontend utilizza WebSocket su MQTT per ricevere solo i dati necessari alla vista corrente.

---

## 5. Roadmap e Miglioramenti Futuri

Per portare il prodotto da "Prototipo Avanzato" a "Leader di Mercato", si suggeriscono i seguenti step:

### breve Termine (Stabilizzazione)
- [x] **Gestione Disconnessioni**: Implementato Quality BAD su timeout.
- [x] **UI Professionale**: Rebranding NetEdge e Sidebar collassabile.
- [ ] **Modifica Tag a Caldo**: Permettere di modificare indirizzi tag senza riavviare l'intero driver (Hot Reload).
- [ ] **Storico Avanzato**: Implementare un Historian dedicato (es. TimescaleDB o InfluxDB) per query veloci su dati vecchi.

### Medio Termine (Funzionalità)
- [ ] **Alarm Engine**: Motore complesso per allarmi (es. "Se Temp > 100 per 5 min").
- [ ] **Nuovi Protocolli**: Siemens S7, OPC-UA, Ethernet/IP.
- [ ] **Role Based Access Control (RBAC)**: Utenti con permessi diversi (Visualizzatore, Tecnico, Admin).

### Lungo Termine (Enterprise)
- [ ] **Cloud Sync**: Sincronizzazione dati verso cloud (AWS IoT / Azure IoT Hub).
- [ ] **Edge AI**: Eseguire modelli ML locali per manutenzione predittiva sui dati acquisiti.
- [ ] **Dashboard Builder**: Drag & drop widget per creare dashboard personalizzate dall'utente.
