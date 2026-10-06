// MiDo data logger (protocol version 3) — UP/IS data frames and AN announce
// frames, base64 over MQTT, big-endian.
//
// Field layout from the decoder MiDo ships with the device. What this adds to
// it: every reading carries its own time (an UP frame holds hours of
// quarter-hourly readings), and status and alarm bytes come out as one
// true/false value per bit, so each can be a tag with its own alarm.
//
// Keys a tag can read (its JSON path):
//   in0 … in31            value of input N, at the reading's time
//   in7_allarme           the input's alarm byte
//   in7_allarmi.<bit>     leakage, blocco_contatore, flusso_inverso,
//                         stato_ingresso, sopra_soglia, sotto_soglia
//   stato_modbus          Modbus instruments status word, per reading
//   batteria              volt, at the time the frame was sent
//   rssi, sqi, seriale, versione
//   stato                 device status byte
//   stato_bit.<bit>       allarme_stato, allarme_leakage, allarme_soglia,
//                         portate_min_max_non_valide, portate_non_valide,
//                         lettura_non_valida, batteria_bassa, reset_anomalia
// Which input is which (Ing1, AN1, …) is in the device's AN frame: decoded
// on the test page, it lists every input with its id.

var STATO_BITS = ["allarme_stato", "allarme_leakage", "allarme_soglia",
  "portate_min_max_non_valide", "portate_non_valide", "lettura_non_valida",
  "batteria_bassa", "reset_anomalia"];
var ALLARME_BITS = ["leakage", "blocco_contatore", "flusso_inverso",
  "stato_ingresso", "sopra_soglia", "sotto_soglia"];

function bits(byteValue, names) {
  var out = {};
  for (var i = 0; i < names.length; i++) out[names[i]] = (byteValue & (1 << i)) !== 0;
  return out;
}

function view(msg) {
  var bin = atob(msg.text.trim());
  var buf = new ArrayBuffer(bin.length);
  var u8 = new Uint8Array(buf);
  for (var i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i);
  return new DataView(buf);
}

// Header common to UP and AN frames: 13 bytes.
function header(v) {
  return {
    seriale: v.getUint32(0, false),
    versione: v.getUint8(4),
    batteria: (v.getUint8(5) + 125) / 100,
    ts: v.getUint32(6, false) * 1000,
    sqi: v.getUint8(10),
    rssi: v.getInt8(11),
    stato: v.getUint8(12)
  };
}

function deviceSample(h) {
  return {
    ts: h.ts,
    values: {
      seriale: h.seriale, versione: h.versione, batteria: h.batteria,
      sqi: h.sqi, rssi: h.rssi, stato: h.stato, stato_bit: bits(h.stato, STATO_BITS)
    }
  };
}

function readValue(v, o, tipo) {
  switch (tipo) {
    case 0: return [v.getUint8(o), 1];
    case 1: return [v.getInt8(o), 1];
    case 2: return [v.getUint16(o, false), 2];
    case 3: return [v.getInt16(o, false), 2];
    case 4: return [v.getUint32(o, false), 4];
    case 5: return [v.getInt32(o, false), 4];
    case 6: return [v.getFloat32(o, false), 4];
    case 7: return [v.getUint32(o, false) * v.getFloat32(o + 4, false), 8]; // pulses × weight
  }
  throw new Error("unknown data type " + tipo);
}

function decodeUP(v) {
  var h = header(v);
  var samples = [deviceSample(h)];
  var o = 13;
  var nLetture = v.getUint8(o); o += 1;
  for (var i = 0; i < nLetture; i++) {
    var s = { ts: v.getUint32(o, false) * 1000, values: {} };
    o += 4;
    var ning = v.getUint8(o); o += 1;
    s.values.stato_modbus = v.getUint16(o, false); o += 2;
    for (var j = 0; j < ning; j++) {
      var b = v.getUint8(o); o += 1;
      var id = b & 0x1F, tipo = (b >> 5) & 0x07;
      var allarme = v.getUint8(o); o += 1;
      var r = readValue(v, o, tipo); o += r[1];
      s.values["in" + id] = r[0];
      s.values["in" + id + "_allarme"] = allarme;
      s.values["in" + id + "_allarmi"] = bits(allarme, ALLARME_BITS);
    }
    samples.push(s);
  }
  if (o !== v.byteLength) throw new Error("UP frame: " + (v.byteLength - o) + " bytes left over");
  return samples;
}

function text(v, o, n) {
  var s = "";
  for (var i = 0; i < n; i++) { var c = v.getUint8(o + i); if (c !== 0) s += String.fromCharCode(c); }
  return s.trim();
}

var TIPI = ["uint8", "int8", "uint16", "int16", "uint32", "int32", "float32", "uint32×float32"];

function decodeAN(v) {
  var h = header(v);
  var s = deviceSample(h);
  var o = 13;
  s.values.imei = text(v, o, 17); o += 17;
  s.values.iccid = text(v, o, 23); o += 23;
  s.values.freq_com = v.getUint16(o, false); o += 2;
  var nStrumenti = v.getUint8(o); o += 1;
  var strumenti = [];
  for (var i = 0; i < nStrumenti; i++) {
    strumenti.push({ id: v.getUint8(o), modbus: v.getUint8(o + 1), etichetta: text(v, o + 2, 9) });
    o += 11;
  }
  var nIngressi = v.getUint8(o); o += 1;
  var ingressi = [];
  for (var k = 0; k < nIngressi; k++) {
    var b = v.getUint8(o);
    ingressi.push({
      chiave: "in" + (b & 0x1F), etichetta: text(v, o + 6, 8), tipo: TIPI[(b >> 5) & 0x07],
      unita: v.getUint8(o + 1), tipo_ingresso: v.getUint8(o + 2),
      freq_memorizzazione: v.getUint16(o + 3, false), strumento: v.getUint8(o + 5)
    });
    o += 14; // type/id, unit, input type, 2 frequency, instrument, 8 label
  }
  if (o !== v.byteLength) throw new Error("AN frame: " + (v.byteLength - o) + " bytes left over");
  s.values.strumenti = strumenti;
  s.values.ingressi = ingressi;
  return [s];
}

function decode(msg) {
  var v = view(msg);
  var t = msg.topic.toLowerCase();
  if (/(^|\/)an$/.test(t)) return decodeAN(v);
  if (/(^|\/)(up|is)$/.test(t)) return decodeUP(v);
  // Topic does not say: the frame's own structure does — an UP frame is
  // consumed exactly by its reading blocks, an AN frame by its descriptors.
  try { return decodeUP(v); } catch (e) { return decodeAN(v); }
}
