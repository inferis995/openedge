/**
 * Coordinate parsing per la mappa GIS.
 *
 * Il campo lat/lon deve accettare ciò che un operatore incolla: «39,3592»
 * (virgola decimale italiana), «39.3592, 16.5142» (coppia Google Maps),
 * «40°30'12"N 16°30'0"E» (gradi-minuti-secondi) o «N 39.3 E 16.5».
 * La funzione ritorna null invece di lanzare: un input sbagliato non deve
 * rompere la pagina, solo non aggiornare il campo.
 */

export interface ParsedCoords {
    latitude: number;
    longitude: number;
}

/** Converte «39° 30' 15.5" N» in 39.504305. Accetta anche i gradi decimali con °. */
function parseDms(part: string): number | null {
    const m = part.match(
        /^\s*(\d+(?:[.,]\d+)?)\s*(?:°|º)?\s*(?:(\d+(?:[.,]\d+)?)\s*['′])?\s*(?:(\d+(?:[.,]\d+)?)\s*(?:"|″|''|”))?\s*$/,
    );
    if (!m) return null;
    const deg = parseFloat(m[1].replace(',', '.'));
    const min = m[2] ? parseFloat(m[2].replace(',', '.')) : 0;
    const sec = m[3] ? parseFloat(m[3].replace(',', '.')) : 0;
    if (!Number.isFinite(deg)) return null;
    return deg + min / 60 + sec / 3600;
}

/** Applica il segno dalla lettera N/S/E/W, se presente. */
function applyHemisphere(value: number, hemi: string): number {
    const h = hemi.trim().toUpperCase();
    if (h === 'S' || h === 'O' || h === 'W') return -Math.abs(value);
    return Math.abs(value); // N / E (o assente): valore positivo
}

/**
 * Parser tollerante di una coppia di coordinate.
 * Accetta separatore virgola, punto e virgola o spazio; gradi decimali o DMS;
 * emisfemi N/S/E/W (e O per Ovest, uso italiano). Ritorna null se non riconosce
 * due valori plausibili.
 */
export function parseCoords(input: string): ParsedCoords | null {
    if (!input) return null;
    let s = input.trim();
    if (!s) return null;

    // Un solo numero su riga: è una latitudine senza longitudine — non basta.
    // Coppia «lat, lon» / «lat; lon» / «lat lon».
    // Se c'è una virgola tra le due parti, divide lì; altrimenti sull'ultimo
    // spazio che separa due numeri (o su DMS: tra N e E).
    const hemiSplit = s.match(/^(.*?[NSEWO])\s+(.*[NSEWO]?)\s*$/i);
    if (hemiSplit && /[NSEWO]/i.test(hemiSplit[2])) {
        s = `${hemiSplit[1]} ; ${hemiSplit[2]}`;
    }

    const parts = s
        .split(/\s*[;,]\s*|\s+(?=[NSEWOnsewo-]|\d)/)
        .map((p) => p.trim())
        .filter(Boolean);
    if (parts.length < 2) return null;

    const latMatch = parts[0].match(/^([NSEWOnsewo])?\s*(.+?)\s*([NSEWOnsewo])?$/);
    const lonMatch = parts[1].match(/^([NSEWOnsewo])?\s*(.+?)\s*([NSEWOnsewo])?$/);
    if (!latMatch || !lonMatch) return null;

    const latVal = parseDms(latMatch[2]);
    const lonVal = parseDms(lonMatch[2]);
    if (latVal === null || lonVal === null) return null;

    const latHemi = (latMatch[1] || latMatch[3] || '').trim();
    const lonHemi = (lonMatch[1] || lonMatch[3] || '').trim();

    const latitude = applyHemisphere(latVal, latHemi || 'N');
    const longitude = applyHemisphere(lonVal, lonHemi || 'E');

    //Fuori dal plausibile: l'operatore ha scambiato l'ordine o incollato altro.
    if (Math.abs(latitude) > 90 || Math.abs(longitude) > 180) return null;
    return { latitude, longitude };
}

/** Formatta una coordinata per la casella di testo: gradi decimali, 6 cifre. */
export function formatCoord(v: number | null | undefined): string {
    if (v === null || v === undefined || !Number.isFinite(v)) return '';
    return v.toFixed(6);
}

/** Risultato di una ricerca luogo su Nominatim. */
export interface PlaceResult {
    displayName: string;
    latitude: number;
    longitude: number;
}

/**
 * Ricerca luogo su Nominatim (OpenStreetMap), gratuito e senza API key.
 * Il header From identifica l'app, come chiede la usage policy di Nominatim.
 */
export async function searchPlace(query: string, signal?: AbortSignal): Promise<PlaceResult[]> {
    const url = `https://nominatim.openstreetmap.org/search?format=json&limit=5&q=${encodeURIComponent(query)}`;
    const res = await fetch(url, {
        signal,
        headers: { Accept: 'application/json', From: 'openedge-web-ui' },
    });
    if (!res.ok) throw new Error(`nominatim ${res.status}`);
    const rows: { display_name: string; lat: string; lon: string }[] = await res.json();
    return rows.map((r) => ({
        displayName: r.display_name,
        latitude: parseFloat(r.lat),
        longitude: parseFloat(r.lon),
    }));
}
