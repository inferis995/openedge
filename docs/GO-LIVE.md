# Messa in produzione — checklist

Cosa fare, nell'ordine, prima di dare l'accesso a un cliente. Ogni punto ha il
comando o la pagina in cui si fa e come si verifica. Per i controlli di
dettaglio dopo un rilascio vedi anche `docs/RELEASE-ACCEPTANCE.md`.

---

## 1. Segreti e accessi (bloccante)

| Cosa | Come | Verifica |
|---|---|---|
| Segreti del `.env` non di esempio | `make preflight` (o `make preflight-monitoring` se pubblichi Grafana) | esce senza errori: rifiuta segreti vuoti, segnaposto, `JWT_SECRET` < 32 caratteri, `ENCRYPTION_KEY` ≠ 32 byte |
| Password admin cambiata | Profilo → Password | accesso con la nuova; quella vecchia non funziona più |
| Nessuna password scritta in chat, email o ticket | se è successo, cambiala ora | — |
| MFA per gli admin | Profilo → Verifica in due passaggi → attiva, **salva i codici di recupero** | uscire e rientrare: chiede il codice |
| MFA obbligatoria per l'organizzazione del cliente (se la vuoi) | Organizzazioni → matita sull'organizzazione → **Verifica in due passaggi obbligatoria** | un utente senza MFA, dopo la password, vede il QR e la configura |

## 2. Rete

| Cosa | Come | Verifica |
|---|---|---|
| Solo HTTPS | VPS: `make vps-up` (Traefik + Let's Encrypt); Coolify: dominio sul servizio web-ui | `http://` reindirizza a `https://`, certificato valido |
| MQTT dall'esterno solo cifrato | porta 8883 aperta, 1883 **non** esposta su internet | `nc -vz <host> 1883` dall'esterno fallisce |
| Postgres, Redis, Grafana non esposti | nessuna porta pubblicata oltre a 80/443/8883 | `docker ps` → colonna PORTS |

## 3. Dimensione e capienza

- **Connessioni al database**: ogni gateway ha un driver con 2 connessioni.
  Servono circa `2 × gateway + 60`. Il default (`POSTGRES_MAX_CONNECTIONS=300`)
  basta fino a ~120 gateway; oltre, alzalo nel `.env` e ricrea il container
  postgres.
- **RAM**: ~30 MB per driver + 2–3 GB per il resto. 88 gateway → almeno 6 GB.
- **Disco**: dopo una settimana guarda Impostazioni → Database (spazio
  occupato) e moltiplica per il periodo di storico che vuoi tenere.

## 4. Dati dal campo

| Cosa | Come | Verifica |
|---|---|---|
| Timeout "offline" adatto al ritmo dei dispositivi | `openedge gateways configure --site <ID> --driver MQTT --set '{"stale_after_minutes":1020}' --apply` (es. logger ogni 8 h → 17 h: due invii persi + margine) | Gateway: nessuno offline senza motivo |
| Arrivano dati da tutti | dopo un giro completo di invii: Tag → filtro **Non aggiornati** | vuoto, o solo i dispositivi davvero muti |
| Posizioni sulla mappa | foglio con nome e coordinate: `openedge gateways positions file.csv --apply`, oppure confini delle aree e conferma dei pin tratteggiati | Mappa: nessun pin "da confermare" |

> Il **monitor MQTT** mostra solo il traffico in diretta: con dispositivi che
> trasmettono ogni poche ore è vuoto quasi sempre, ed è normale. Per sapere se
> i dati arrivano usa la pagina Tag, non il monitor.

## 5. Backup (bloccante)

1. Impostazioni → Backup: backup automatici **attivi**, orario e
   conservazione, chiave di cifratura `age` se i backup escono dal server,
   copia remota S3/MinIO se possibile.
2. **Prova di ripristino**, almeno una volta, su una macchina di prova:
   ```bash
   make backup-now                                   # backup a mano (host)
   make restore BACKUP=./backups/openedge_<data>.sql.gz
   ```
   Poi accedi e controlla che organizzazioni, tag e storico ci siano.
   Un backup mai ripristinato non è un backup.

## 6. Monitoraggio e avvisi

```bash
make vps-up-monitoring        # VPS
# oppure, on-prem:
docker-compose -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring up -d
```

Configura in `monitoring/alertmanager.yml` dove arrivano gli avvisi (email,
Telegram…). Gli avvisi che contano per una messa in produzione:

| Avviso | Quando |
|---|---|
| `AllGatewaysOffline` (critico) | nessun gateway dà dati entro il **proprio** timeout per 10 min |
| `GatewaysOffline` | qualche gateway offline da 30 min (la pagina Gateway dice quali) |
| `BackupStale` / `NoBackups` | ultimo backup automatico più vecchio di 36 h / nessun backup dopo il primo giorno |
| `DiskSpaceLow` / `DiskSpaceCritical` | disco sotto il 20 % / 10 % |
| `PostgresDown`, `CoreAPIDown`, `MQTTBrokerDown` | un servizio è giù |

Gli avvisi sui gateway seguono il `stale_after_minutes` di ciascuno: un logger
che trasmette ogni 8 ore non fa scattare niente tra un invio e l'altro.
`BackupStale` guarda i backup **automatici** (quelli di Impostazioni →
Backup): se usi solo lo script dal server, attiva anche quelli.

Prova un avviso: ferma un driver (`docker stop <container>`) e aspetta che
`GatewaysOffline` arrivi dove l'hai configurato.

## 6b. Previsioni (opzionale)

Attive di default con il motore statistico: non serve niente. Per il modello
Timestral (più preciso, su CPU): `make forecast-up` (on-prem),
`make forecast-up STACK=vps`, o profilo `forecast` in Coolify. Requisiti,
download, installazione offline e verifica: `docs/FORECASTING.md`, sezione
"Installare il modello Timestral". Leggi la licenza dei pesi prima di
installarlo presso un cliente grande.

## 7. Accessi del cliente

- Utenti con il ruolo giusto: sola lettura per chi guarda, admin
  dell'organizzazione solo per chi configura.
- Pagine visibili: Organizzazioni → modifica → **Pagine visibili** (es. nascondere
  turni/OEE a chi ha solo data logger).
- Logo e colore dell'organizzazione: Organizzazioni → modifica.

## 8. Aggiornamenti

```bash
make update-check     # cosa cambierebbe
make update           # snapshot → pull → riavvio → controllo salute
```

Aggiorna prima un ambiente di prova, poi la produzione. Dopo ogni
aggiornamento: login, una pagina Tag con valori, un Trend, la pagina Allarmi.

## 9. Documenti

- Informativa privacy e termini (link in basso nel menu): compilali con i dati
  del titolare.
- Se ospiti i dati per conto del cliente (multi-tenant sul tuo server): accordo
  di trattamento dati (art. 28 GDPR).
