# 🚀 Industrial Edge Middleware - Production Battle Plan
**Next Session Objective:** Finalize the application for production deployment.

## 1. 🔄 Robustezza Driver & Auto-Restart (Critico)
*   **Problema:** Se un driver crasha, muore per sempre.
*   **Soluzione:** Modificare `driver-manager` per monitorare attivamente lo stato dei container figli e riavviarli se escono inaspettatamente (Auto-Healing).
*   **File target:** `services/driver-manager/main.go`

## 2. 📝 Logging & Rotazione (Critico)
*   **Problema:** I log infiniti riempiono il disco del server in produzione.
*   **Soluzione:** Configurare la `logging options` di Docker nel `docker-compose.yml` per limitare dimensione e numero di file (es. 10MB x 3 file).
*   **File target:** `docker-compose.yml`

## 3. 🎨 Branding & Favicon (Pulizia)
*   **Problema:** Il sito usa ancora l'icona di default di Vite.
*   **Soluzione:** Sostituire `vite.svg` con il logo OpenEdge e aggiornare i meta tag HTML.
*   **File target:** `services/web-ui/index.html`, `public/`

## 4. 🔐 Sicurezza & Sessioni
*   **Problema:** Token JWT potenzialmente infiniti o mal gestiti.
*   **Soluzione:**
    *   Verificare/Impostare scadenza token (es. 8/12 ore lavorative).
    *   Implementare refresh token (opzionale) o logout forzato alla scadenza.
*   **File target:** `internal/auth/auth.go`

## 5. 📋 Audit Log (Tracciabilità)
*   **Problema:** Non sappiamo "chi ha fatto cosa".
*   **Soluzione:** Attivare il logging su `access_logs` per eventi critici:
    *   Login/Logout
    *   Modifica setpoint (scrittura Tag)
    *   Cancellazione Gateway/Device
*   **File target:** `internal/middleware/audit.go` (nuovo), `internal/handlers`

---

## 🛠️ Stato Attuale (Checkpoint)
- **Backup/Restore:** ✅ Fixato e Nucleare.
- **Modbus Driver:** ✅ Fixato indirizzo 0 e dicitura UI dinamica.
- **MQTT Monitor:** ✅ Fixato via Nginx Proxy (porta Web standard).
- **Web UI:** ✅ Fixato "Hot" e loghi superflui.

Ci vediamo domani per chiudere il cerchio! 🛡️🏭
