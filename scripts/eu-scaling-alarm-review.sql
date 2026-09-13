-- Ricognizione: quali allarmi cambiano comportamento con la conversione EU
-- spostata nel driver (vedi docs/EU_SCALING.md).
--
-- SOLA LETTURA. Non modifica niente. Da eseguire PRIMA di aggiornare un
-- impianto che ha tag con lo scaling attivo:
--
--     psql "$DATABASE_URL" -f scripts/eu-scaling-alarm-review.sql
--
-- COSA È CAMBIATO
--
-- Gli allarmi sono valutati dentro il driver. Prima confrontavano la soglia —
-- che l'operatore digita nelle unità che vede, cioè ingegneristiche — con il
-- valore GREZZO letto dal device. Adesso la confrontano con il valore
-- convertito. Su un trasmettitore 0..27648 → 0..100 bar, una soglia di 80 bar
-- era confrontata con i conteggi: superata alla prima lettura e da lì sempre.
--
-- La colonna "grezzo_equivalente" dice a quale valore grezzo corrisponde la
-- soglia: è il punto in cui l'allarme interverrà d'ora in poi. La colonna
-- "soglia_vs_campo_grezzo" dice dove cadeva la soglia rispetto al campo dei
-- conteggi, che è ciò con cui veniva confrontata prima.

\pset border 2
\echo ''
\echo '=== 1. Allarmi a soglia su tag con scaling attivo ==='
\echo ''

WITH scaled AS (
    SELECT d.id            AS def_id,
           t.id            AS tag_id,
           t.alias,
           d.alarm_type,
           d.severity,
           d.threshold,
           t.eu_unit,
           t.scaling_raw_min  AS raw_min,
           t.scaling_raw_max  AS raw_max,
           t.scaling_eu_min   AS eu_min,
           t.scaling_eu_max   AS eu_max,
           -- Il valore grezzo che corrisponde alla soglia. NULL quando il
           -- campo ingegneristico è nullo: lì la conversione è indefinita.
           CASE WHEN (t.scaling_eu_max - t.scaling_eu_min) <> 0
                THEN (d.threshold - t.scaling_eu_min)
                     / (t.scaling_eu_max - t.scaling_eu_min)
                     * (t.scaling_raw_max - t.scaling_raw_min) + t.scaling_raw_min
           END AS raw_equivalent
      FROM alarm_definitions d
      JOIN tags t ON t.id = d.tag_id
     WHERE d.enabled
       AND t.scaling_enabled
       AND d.threshold IS NOT NULL
       AND d.alarm_type IN ('high', 'high_high', 'low', 'low_low')
       -- Un campo grezzo nullo non viene convertito da scaling.Apply, quindi
       -- per quei tag non cambia niente.
       AND (t.scaling_raw_max - t.scaling_raw_min) <> 0
       -- Campo ingegneristico nullo: la conversione e' indefinita e Apply
       -- restituisce il valore invariato, quindi niente cambia.
       AND (t.scaling_eu_max - t.scaling_eu_min) <> 0
)
SELECT s.def_id                                        AS "allarme",
       s.tag_id                                        AS "tag",
       s.alias                                         AS "nome",
       s.alarm_type                                    AS "tipo",
       s.severity                                      AS "gravita",
       s.threshold || COALESCE(' ' || NULLIF(s.eu_unit, ''), '') AS "soglia",
       -- Dove interveniva e dove interverra, come percentuale del campo di
       -- misura. E' il confronto che conta: la soglia e' la stessa, cambia
       -- solo il numero con cui viene confrontata.
       -- Solo quando la soglia cadeva DENTRO il campo grezzo: fuori, il
       -- vecchio confronto aveva esito fisso e la percentuale non significa
       -- niente (verrebbe negativa o sopra il 100, contraddicendo il verdetto).
       CASE WHEN s.threshold BETWEEN least(s.raw_min, s.raw_max) AND greatest(s.raw_min, s.raw_max)
            THEN round((100 * (s.threshold - s.raw_min) / (s.raw_max - s.raw_min))::numeric, 1)
       END                                             AS "interveniva a % campo",
       round((100 * (s.threshold - s.eu_min)  / (s.eu_max  - s.eu_min))::numeric, 1)  AS "interverra a % campo",
       round(s.raw_equivalent::numeric, 1)             AS "grezzo_equivalente",
       s.raw_min || '..' || s.raw_max                  AS "campo_grezzo",
       CASE
         -- Fuori dal campo grezzo il vecchio confronto aveva esito fisso.
         WHEN s.alarm_type IN ('high','high_high') AND s.threshold < least(s.raw_min, s.raw_max)
              THEN 'era SEMPRE attivo -> si spegne'
         WHEN s.alarm_type IN ('high','high_high') AND s.threshold > greatest(s.raw_min, s.raw_max)
              THEN 'non scattava MAI -> puo scattare'
         WHEN s.alarm_type IN ('low','low_low') AND s.threshold > greatest(s.raw_min, s.raw_max)
              THEN 'era SEMPRE attivo -> si spegne'
         WHEN s.alarm_type IN ('low','low_low') AND s.threshold < least(s.raw_min, s.raw_max)
              THEN 'non scattava MAI -> puo scattare'
         -- Dentro il campo: la soglia effettiva si sposta. Sotto il 5% o sopra
         -- il 95% del campo il vecchio comportamento era di fatto fisso.
         WHEN (100 * (s.threshold - s.raw_min) / (s.raw_max - s.raw_min)) < 5
              THEN CASE WHEN s.alarm_type IN ('high','high_high')
                        THEN 'interveniva quasi sempre -> si calma'
                        ELSE 'interveniva quasi mai -> comincia a scattare' END
         WHEN (100 * (s.threshold - s.raw_min) / (s.raw_max - s.raw_min)) > 95
              THEN CASE WHEN s.alarm_type IN ('high','high_high')
                        THEN 'interveniva quasi mai -> comincia a scattare'
                        ELSE 'interveniva quasi sempre -> si calma' END
         ELSE 'la soglia effettiva si sposta'
       END                                             AS "cosa cambia",
       CASE WHEN e.status IS NULL THEN '' ELSE e.status END AS "stato adesso"
  FROM scaled s
  LEFT JOIN LATERAL (
      SELECT status FROM alarm_events
       WHERE definition_id = s.def_id
         AND status IN ('ACTIVE', 'ACKNOWLEDGED')
       ORDER BY trigger_time DESC
       LIMIT 1
  ) e ON true
 ORDER BY (e.status IS NOT NULL) DESC, s.severity, s.tag_id;

\echo ''
\echo '=== 2. Allarmi booleani su tag con invert: lo stato si ribalta ==='
\echo ''

-- invert non veniva applicato da nessuna parte sul percorso di lettura, quindi
-- questi allarmi vedevano il segnale non invertito. Adesso lo vedono invertito:
-- quelli attivi si spengono e viceversa. Non c'è niente da ricalcolare, ma
-- vanno guardati.
SELECT d.id AS "allarme", t.id AS "tag", t.alias AS "nome",
       d.alarm_type AS "tipo", d.severity AS "gravita",
       COALESCE(e.status, '') AS "stato adesso",
       'il segnale si ribalta -> lo stato si inverte' AS "cosa cambia"
  FROM alarm_definitions d
  JOIN tags t ON t.id = d.tag_id
  LEFT JOIN LATERAL (
      SELECT status FROM alarm_events
       WHERE definition_id = d.id AND status IN ('ACTIVE','ACKNOWLEDGED')
       ORDER BY trigger_time DESC LIMIT 1
  ) e ON true
 WHERE d.enabled AND t.scaling_enabled AND t.invert
   AND d.alarm_type IN ('bool_true', 'bool_false')
 ORDER BY t.id;

\echo ''
\echo '=== 3. Riepilogo ==='
\echo ''

SELECT count(*) FILTER (WHERE t.scaling_enabled) AS "tag scalati",
       count(*) FILTER (WHERE t.scaling_enabled
                          AND ((t.scaling_raw_max - t.scaling_raw_min) = 0
                            OR (t.scaling_eu_max  - t.scaling_eu_min)  = 0)) AS "di cui campo nullo (non convertiti)",
       -- Gli stessi filtri della sezione 1, o le due cifre non coincidono e
       -- non ci si puo fidare di nessuna delle due.
       (SELECT count(*) FROM alarm_definitions d JOIN tags t2 ON t2.id = d.tag_id
         WHERE d.enabled AND t2.scaling_enabled AND d.threshold IS NOT NULL
           AND d.alarm_type IN ('high','high_high','low','low_low')
           AND (t2.scaling_raw_max - t2.scaling_raw_min) <> 0
           AND (t2.scaling_eu_max  - t2.scaling_eu_min)  <> 0) AS "allarmi a soglia da rivedere",
       (SELECT count(*) FROM alarm_definitions d JOIN tags t3 ON t3.id = d.tag_id
         WHERE d.enabled AND t3.scaling_enabled AND t3.invert
           AND d.alarm_type IN ('bool_true','bool_false')) AS "allarmi booleani che si ribaltano"
  FROM tags t;
