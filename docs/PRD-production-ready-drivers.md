# PRD: Production-Ready Driver Management System

**Data:** 2025-03-09
**Versione:** 1.0
**Stato:** Draft
**Priorità:** Critica

## Executive Summary

Questo documento definisce i requisiti per rendere il sistema di gestione dei driver (S7, MODBUS_TCP, MQTT, OPC_UA) completamente production-ready. Il sistema deve gestire il ciclo di vita completo dei container driver, dalla creazione alla rimozione, con gestione degli errori, recovery automatico, e monitoraggio dello stato di salute.

---

## 1. Architettura Attuale

### 1.1 Componenti

| Componente | Responsabilità | Stato Attuale |
|------------|----------------|---------------|
| `driver-manager` | Polla DB, crea/ferma container driver | Parzialmente implementato |
| `driver-s7` | Driver Siemens S7 | Funzionante |
| `driver-modbus` | Driver Modbus TCP | Funzionante |
| `driver-mqtt` | Driver MQTT Native | Funzionante |
| `driver-opcua` | Driver OPC UA | Funzionante (write issue) |
| `core-api` | API REST, MQTT broker | Funzionante |
| `web-ui` | Frontend React | Funzionante |

### 1.2 Flusso Dati Attuale

```
┌─────────┐      ┌──────────────┐      ┌─────────────────┐
│ Web UI  │ ───> │   core-api   │ ───> │   PostgreSQL    │
└─────────┘      └──────────────┘      └─────────────────┘
                       │
                       ▼
                  ┌─────────┐      ┌──────────────────────────────┐
                  │ Mosquitto│      │    Docker Containers         │
                  │  Broker  │      │ ┌────┬────┬────┬────┐       │
                  └─────────┘      │ │ S7 │MOD │MQTT│OPC │       │
                                   │ └────┴────┴────┴────┘       │
                                   └──────────────────────────────┘
                                                ▲
                                                │
                                   ┌─────────────────────────────┴──────┐
                                   │        driver-manager             │
                                   │  (poll DB, manage containers)     │
                                   └──────────────────────────────────┘
```

---

## 2. Problemi Identificati e Soluzioni

### 2.1 MODBUS Driver Non Creato (RISOLTO)

**Problema:** In un'installazione pulita, il container del driver MODBUS non viene creato.

**Root Cause:**
- Il driver-manager non implementava il pull delle immagini Docker
- Se l'immagine non esiste localmente, il ContainerCreate fallisce silenziosamente

**Soluzione Implementata:**
```go
// Prima: solo log
log.Printf("Pulling image %s...", imageName)

// Dopo: pull effettivo
reader, err := m.dockerClient.ImagePull(m.ctx, imageName, types.ImagePullOptions{})
if err != nil {
    return fmt.Errorf("failed to pull image: %w", imageName, err)
}
```

### 2.2 Zero-Based Addressing Non Salvato (RISOLTO)

**Problema:** Il campo `zero_based` non viene salvato quando si crea un gateway MODBUS.

**Root Cause:**
- Il frontend non includeva `zero_based` nel CreateGatewayDto
- Inconsistenza tra default DB (TRUE) e backend (FALSE)

**Soluzione Implementata:**
- Aggiunto `zero_based?: boolean` a CreateGatewayDto
- Creata migrazione per correggere default DB a FALSE

### 2.3 Cloud MQTT Write → OPC UA Fails (IN CORSO - FIX IMPLEMENTATO)

**Problema:** I comandi di write dal cloud broker falliscono con `StatusBadTypeMismatch`.

**Investigazione:**
- Server riporta tipo: Boolean
- Provato: Boolean, SByte, Int16 - tutti falliscono
- UA Expert funziona con lo stesso nodo

**Ipotesi:**
1. Server richiede encoding specifico
2. Namespace personalizzato (ns=2)
3. Libreria gopcua incompatibile

**Fix Implementato (2025-03-09):**
Implementato multi-strategy write con fallback. Il client ora prova多种 tipi in ordine:
1. Boolean nativo (bool)
2. Int16 (0/1)
3. Int32 (0/1)
4. SByte (0/1)
5. Byte (0/1)
6. UInt16 (0/1)
7. UInt32 (0/1)

Ogni tentativo è loggato con dettagli per identificare quale tipo il server accetta.

**Prossimi Passi:**
1. Testare il nuovo codice con gateway 138
2. Verificare nei log quale tipo funziona
3. Ottimizzare per usare solo quello corretto

---

## 3. Requisiti Production-Ready

### 3.1 Gestione Container Driver

#### 3.1.1 Pull Immagini

| Requisito | Descrizione | Priorità |
|-----------|-------------|----------|
| Pull automatico | Scaricare immagine se non esiste localmente | ALTA |
| Timeout pull | Timeout configurabile per pull immagini | MEDIA |
| Retry pull | Retry con backoff esponenziale in caso di fallimento | MEDIA |
| Log progress | Log del progresso del pull | BASSA |

**Implementazione:**
```go
const (
    imagePullTimeout = 5 * time.Minute
    maxPullRetries   = 3
)

func (m *Manager) pullImageWithRetry(imageName string) error {
    for i := 0; i < maxPullRetries; i++ {
        reader, err := m.dockerClient.ImagePull(m.ctx, imageName, types.ImagePullOptions{})
        if err == nil {
            // Read pull output to completion
            defer reader.Close()
            io.Copy(io.Discard, reader) // Wait for pull to complete
            return nil
        }

        if i < maxPullRetries-1 {
            backoff := time.Duration(1<<uint(i)) * time.Second
            log.Printf("Pull failed (attempt %d/%d), retrying in %v: %v",
                i+1, maxPullRetries, backoff, err)
            time.Sleep(backoff)
        }
    }
    return fmt.Errorf("failed to pull image after %d retries", maxPullRetries)
}
```

#### 3.1.2 Creazione Container

| Requisito | Descrizione | Priorità |
|-----------|-------------|----------|
| Cleanup pre-creazione | Rimuovere container esistente con lo stesso nome | ALTA |
| Network verifica | Verifica esistenza rete Docker | ALTA |
| DNS resolution | Resolve DB_HOST e MQTT_HOST a IP | MEDIA |
| Resource limits | Limiti CPU/memoria configurabili | MEDIA |
| Health check | Health check per ogni container | ALTA |

**Implementazione:**
```go
func (m *Manager) startGatewayContainer(gateway models.Gateway) error {
    // 1. Verify network exists
    if err := m.ensureNetwork(); err != nil {
        return fmt.Errorf("network verification failed: %w", err)
    }

    // 2. Pull image
    if err := m.pullImageWithRetry(imageName); err != nil {
        return fmt.Errorf("image pull failed: %w", err)
    }

    // 3. Resolve hostnames to IPs
    dbHost := m.resolveHostname(getEnv("DB_HOST", "postgres"))
    mqttHost := m.resolveHostname(getEnv("MQTT_HOST", "mosquitto"))

    // 4. Create container with resource limits
    hostConfig := &container.HostConfig{
        RestartPolicy: container.RestartPolicy{Name: "always"},
        Resources: container.Resources{
            Limits: container.Resources{
                NanoCPUs:  1000000000, // 1 CPU
                MemoryBytes: 256 * 1024 * 1024, // 256MB
            },
        },
        NetworkMode: container.NetworkMode(dockerNetworkName),
    }

    // 5. Create and start
    // ... existing code ...
}
```

#### 3.1.3 Gestione Errori

| Requisito | Descrizione | Priorità |
|-----------|-------------|----------|
| Log dettagliato | Log con causa errore e contesto | ALTA |
| Notifica errori | Pubblica stato errore su MQTT | MEDIA |
| Graceful degradation | Sistema continua con altri driver | ALTA |
| Max retry counter | Limitare tentativi di riavvio | MEDIA |

**Implementazione:**
```go
func (m *Manager) syncGateways() error {
    // ... existing query code ...

    for _, gateway := range gateways {
        if gateway.Enabled {
            if !exists || !state.Running {
                if err := m.startGatewayContainer(gateway); err != nil {
                    // Log detailed error
                    log.Printf("[ERROR] Failed to start container for gateway %d (%s, type: %s): %v",
                        gateway.ID, gateway.Name, gateway.DriverType, err)

                    // Publish error status to MQTT
                    m.publishGatewayStatus(gateway.ID, "error", err.Error())

                    // Track retry count
                    if state != nil {
                        state.ConsecutiveErrors++
                        if state.ConsecutiveErrors > maxStartRetries {
                            log.Printf("[ERROR] Gateway %d exceeded max retries, marking as failed", gateway.ID)
                            state.Running = false
                        }
                    }
                    continue
                }
                // Reset error count on success
                if state != nil {
                    state.ConsecutiveErrors = 0
                }
            }
        }
    }
    return nil
}
```

### 3.2 Driver-Specific Requirements

#### 3.2.1 MODBUS TCP Driver

| Requisito | Descrizione | Priorità |
|-----------|-------------|----------|
| Zero-based addressing | Supporto indirizzamento 0-based | ALTA |
| Byte order | Configurabile (Big/Little Endian) | MEDIA |
| Word order | Configurabile (Low/High Word First) | MEDIA |
| Register types | Coil, Discrete, Input, Holding | ALTA |
| Timeout | Timeout configurabile per richieste | MEDIA |

**Variabili Ambiente:**
```bash
GATEWAY_ID=<id>
MODBUS_ZERO_BASED=<true|false>
MODBUS_BYTE_ORDER=<big|little>
MODBUS_WORD_ORDER=<low|high>
MODBUS_REQUEST_TIMEOUT=30s
```

#### 3.2.2 OPC UA Driver

| Requisito | Descrizione | Priorità |
|-----------|-------------|----------|
| Write support | Scrittura valori al server | ALTA |
| Type conversion | Conversione corretta tipi dati | ALTA |
| Authentication | Auth anonima, username/password, certificati | ALTA |
| Encryption | Supporto TLS/SSL | MEDIA |
| Browse | Navigazione albero nodi | MEDIA |
| Subscription | Subscriptions per changes | MEDIA |

**Variabili Ambiente:**
```bash
GATEWAY_ID=<id>
OPCUA_AUTH_MODE=<anonymous|username|certificate>
OPCUA_SECURITY_POLICY=<None|Basic128Rsa15|Basic256>
OPCUA_REQUEST_TIMEOUT=10s
OPCUA_SESSION_TIMEOUT=60s
```

#### 3.2.3 S7 Driver

| Requisito | Descrizione | Priorità |
|-----------|-------------|----------|
| Rack/Slot | Configurazione rack e slot | ALTA |
| PDU types | Supporto PDU tipi 1-20 | MEDIA |
| Multi-block | Lettura multi-block per efficienza | BASSA |

#### 3.2.4 MQTT Native Driver

| Requisito | Descrizione | Priorità |
|-----------|-------------|----------|
| Topic pattern | Pattern topic configurabile | ALTA |
| QoS support | Supporto QoS 0,1,2 | MEDIA |
| Retain | Gestione messaggi retained | MEDIA |
| Last Will | LWT per disconnessione | MEDIA |

### 3.3 Monitoraggio e Observabilità

#### 3.3.1 Health Check

| Metrica | Descrizione | Frequenza |
|---------|-------------|-----------|
| Container running | Container in esecuzione | 10s |
| DB reachable | Database accessibile | 30s |
| MQTT connected | MQTT broker connesso | 10s |
| PLC connected | PLC accessibile | Ogni scan |
| Tag count | Numero tag monitorati | Ogni scan |

**Implementazione Health Check:**
```go
func (d *Driver) publishHealth() {
    health := HealthStatus{
        GatewayID:    d.gatewayID,
        Status:       "online",
        ContainerID:  d.containerID,
        PLCConnected: d.isConnected(),
        TagCount:     len(d.tags),
        LastError:    d.lastError,
        Timestamp:    time.Now().UnixMilli(),
    }

    topic := fmt.Sprintf("sys/health/%d", d.gatewayID)
    d.mqtt.PublishWithQoS(topic, health, 1, true) // retained
}
```

#### 3.3.2 Logging

| Livello | Contenuto | Esempio |
|---------|----------|---------|
| ERROR | Fallimenti critici | Container start failed |
| WARN | Recovery, retry | Reconnecting to PLC |
| INFO | Operazioni normali | Gateway created, started |
| DEBUG | Dettagli operazioni | Tag read, write |

**Formato Log:**
```
[timestamp] [component] [gateway_id] message
```

Esempio:
```
2025-03-09T10:15:30.123Z [driver-modbus] [5] Starting container for gateway 5 (type: MODBUS_TCP)
2025-03-09T10:15:32.456Z [driver-modbus] [5] Successfully connected to 192.168.1.100:502
2025-03-09T10:15:33.789Z [driver-modbus] [5] Read 15 tags successfully
```

### 3.4 Configurazione

#### 3.4.1 Variabili Environment

| Variabile | Default | Descrizione |
|-----------|---------|-------------|
| DB_HOST | postgres | Database host |
| DB_PORT | 5432 | Database port |
| DB_USER | industrial_user | Database user |
| DB_PASSWORD | industrial_pass | Database password |
| DB_NAME | industrial_edge | Database name |
| MQTT_HOST | mosquitto | MQTT broker host |
| MQTT_PORT | 1883 | MQTT broker port |
| POLL_INTERVAL | 10s | Gateway poll interval |
| IMAGE_PULL_TIMEOUT | 5m | Image pull timeout |

#### 3.4.2 Database Schema

```sql
CREATE TABLE gateways (
    id SERIAL PRIMARY KEY,
    area_id INT NOT NULL REFERENCES areas(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    driver_type VARCHAR(20) NOT NULL
        CHECK (driver_type IN ('S7', 'MODBUS_TCP', 'MQTT', 'OPC_UA')),
    connection_config JSONB NOT NULL,
    scan_rate_ms INT DEFAULT 1000,
    enabled BOOLEAN DEFAULT TRUE,
    zero_based BOOLEAN DEFAULT FALSE, -- Per MODBUS
    created_at TIMESTAMPTZ DEFAULT NOW(),
    -- Opc UA specific
    opcua_auth_mode VARCHAR(20) DEFAULT 'Anonymous',
    opcua_security_policy VARCHAR(50) DEFAULT 'None',
    -- Resource limits (optional)
    max_cpu INT DEFAULT 1, -- CPU cores (in nanoseconds: 1e9 = 1 core)
    max_memory_mb INT DEFAULT 256
);

CREATE INDEX idx_gateways_area ON gateways(area_id);
CREATE INDEX idx_gateways_enabled ON gateways(enabled) WHERE enabled = TRUE;
```

### 3.5 Deployment

#### 3.5.1 Docker Compose Production

```yaml
version: '3.8'

services:
  driver-manager:
    image: industrial-driver-manager:latest
    container_name: industrial-driver-manager
    restart: unless-stopped
    environment:
      - DB_HOST=industrial-postgres
      - DB_PORT=5432
      - DB_USER=industrial_user
      - DB_PASSWORD=${DB_PASSWORD}
      - DB_NAME=industrial_edge
      - MQTT_HOST=industrial-mosquitto
      - MQTT_PORT=1883
      - POLL_INTERVAL=10s
      - IMAGE_PULL_TIMEOUT=5m
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks:
      - industrial-network
    depends_on:
      - postgres
      - mosquitto
    healthcheck:
      test: ["CMD", "pgrep", "driver-manager"]
      interval: 30s
      timeout: 10s
      retries: 3

networks:
  industrial-network:
    external: true
```

#### 3.5.2 K8s Deployment (Futuro)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: driver-manager
spec:
  replicas: 1
  selector:
    matchLabels:
      app: driver-manager
  template:
    metadata:
      labels:
        app: driver-manager
    spec:
      containers:
      - name: driver-manager
        image: industrial-driver-manager:latest
        env:
        - name: DB_HOST
          value: postgres-service
        # ... other env vars
        volumeMounts:
        - name: docker-socket
          mountPath: /var/run/docker.sock
      volumes:
      - name: docker-socket
        hostPath:
          path: /var/run/docker.sock
```

---

## 4. Test Plan

### 4.1 Unit Tests

| Test | Descrizione |
|------|-------------|
| Image pull | Verifica pull immagine con retry |
| Container create | Verifica creazione container |
| Container cleanup | Verifica rimozione container esistente |
| Network setup | Verifica creazione rete Docker |

### 4.2 Integration Tests

| Test | Descrizione |
|------|-------------|
| Gateway creation | Creazione gateway → container creato |
| Gateway update | Modifica gateway → container aggiornato |
| Gateway delete | Eliminazione gateway → container rimosso |
| Gateway disable | Disabilita gateway → container fermato |
| Multiple gateways | Più gateway simultanei |

### 4.3 Scenario Tests

| Scenario | Steps |
|----------|-------|
| Fresh install | 1. Installa sistema 2. Crea gateway MODBUS 3. Verifica container creato |
| Image not present | 1. Rimuovi immagine locale 2. Crea gateway 3. Verifica pull automatico |
| Driver crash | 1. Uccidi container driver 2. Verifica riavvio automatico |
| DB connection loss | 1. Ferma DB 2. Verifica reconnection 3. Verifica ripresa sync |

---

## 5. Performance Considerazioni

### 5.1 Resource Limits

| Driver | CPU Min | CPU Max | Memoria Max |
|--------|---------|--------|-------------|
| S7 | 0.1 | 0.5 | 128MB |
| MODBUS_TCP | 0.05 | 0.25 | 64MB |
| MQTT | 0.05 | 0.25 | 64MB |
| OPC_UA | 0.1 | 1.0 | 256MB |

### 5.2 Scalabilità

| Componente | Max Gateway | Max Tag Total |
|-------------|-------------|---------------|
| Driver Manager | 100 | 10,000 |
| S7 Driver | 1 | 1,000 |
| MODBUS Driver | 1 | 500 |
| OPC_UA Driver | 1 | 2,000 |
| MQTT Driver | 1 | 5,000 (topic-based) |

---

## 6. Security Considerazioni

### 6.1 Docker Socket

⚠️ **CRITICO**: L'accesso a `/var/run/docker.sock` è privilegiato.

**Mitigazioni:**
1. Read-only mount dove possibile
2. Limitare a container driver-manager solo
3. Usare Docker context appropriato

### 6.2 Credentials

| Credential | Storage | Encryption |
|------------|---------|------------|
| DB Password | Environment variable | TLS |
| MQTT Password | Environment variable | TLS |
| OPC UA Cert | Volume mount | File permissions |

---

## 7. Rollout Plan

### Fase 1: Fix Immediati (Week 1)
- [x] Fix pull immagine driver
- [x] Fix zero_based salvataggio
- [ ] Fix OPC UA write issue

### Fase 2: Robustezza (Week 2)
- [ ] Implementare retry con backoff
- [ ] Migliorare logging errori
- [ ] Aggiungere health check per container

### Fase 3: Produzione (Week 3)
- [ ] Implementare resource limits
- [ ] Configurazione variabili ambiente
- [ ] Documentazione deployment

### Fase 4: Monitoring (Week 4)
- [ ] Dashboard stato gateway
- [ ] Alert per errori
- [ ] Metriche performance

---

## 8. Appendice

### A. Codici Errore

| Codice | Descrizione |
|--------|-------------|
| ERR_DRIVER_IMAGE_PULL | Fallimento pull immagine |
| ERR_DRIVER_CREATE | Fallimento creazione container |
| ERR_DRIVER_START | Fallimento avvio container |
| ERR_DB_QUERY | Fallimento query database |
| ERR_DB_CONNECT | Fallimento connessione database |

### B. MQTT Topics

| Topic | QoS | Retain | Descrizione |
|-------|-----|--------|-------------|
| sys/health/{gateway_id} | 1 | Yes | Stato salute gateway |
| sys/command/reload/{gateway_id} | 0 | No | Comando reload config |
| sys/command/write/{gateway_id} | 0 | No | Comando write tag |
| sys/error/{gateway_id} | 1 | No | Errori gateway |

### C. Container Naming Convention

```
{driver-type}-{gateway-id}

Esempi:
- driver-s7-1
- driver-modbus-2
- driver-mqtt-3
- driver-opcua-4
```
