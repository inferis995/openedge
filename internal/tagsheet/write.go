package tagsheet

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Tag is a tag as written to a sheet.
type Tag struct {
	Alias     string
	Address   string
	DataType  string
	Historize bool
	Deadband  float64
	Unit      string
	Decimals  int
	Scaling   bool
	RawMin    float64
	RawMax    float64
	EuMin     float64
	EuMax     float64
	Clamp     bool
	Invert    bool
	JSONPath  string
}

// lang returns a language the sheet can be written in.
func lang(l string) string {
	if _, ok := Headers[l]; ok {
		return l
	}
	return "en"
}

func yesNo(l string, b bool) string {
	switch {
	case l == "it" && b:
		return "sì"
	case l == "it":
		return "no"
	case b:
		return "yes"
	default:
		return "no"
	}
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (t *Tag) cells(l string) []string {
	return []string{
		t.Alias, t.Address, t.DataType, yesNo(l, t.Historize), num(t.Deadband), t.Unit,
		strconv.Itoa(t.Decimals), yesNo(l, t.Scaling), num(t.RawMin), num(t.RawMax),
		num(t.EuMin), num(t.EuMax), yesNo(l, t.Clamp), yesNo(l, t.Invert), t.JSONPath,
	}
}

func header(l string) []string {
	h := make([]string, len(Columns))
	for i, f := range Columns {
		h[i] = Headers[l][f]
	}
	return h
}

// WriteCSV writes tags as CSV. The separator is ";" and there is a BOM: that
// is what Excel needs to open the file in columns, with accents, in an Italian
// locale. The importer reads either separator.
func WriteCSV(l string, tags []Tag) ([]byte, error) {
	l = lang(l)
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	if err := w.Write(header(l)); err != nil {
		return nil, err
	}
	for i := range tags {
		if err := w.Write(tags[i].cells(l)); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

const sheetTags = "Tag"

// WriteXLSX writes tags as an Excel workbook: the tags on the first sheet, and
// on a second one what every column means and accepts, so the file explains
// itself to whoever fills it in.
func WriteXLSX(l string, tags []Tag) ([]byte, error) {
	l = lang(l)
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", sheetTags); err != nil {
		return nil, err
	}
	rows := [][]string{header(l)}
	for i := range tags {
		rows = append(rows, tags[i].cells(l))
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		vals := make([]interface{}, len(r))
		for j, v := range r {
			vals[j] = v
		}
		if rowErr := f.SetSheetRow(sheetTags, cell, &vals); rowErr != nil {
			return nil, rowErr
		}
	}

	bold, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E7E6E6"}},
	})
	if err != nil {
		return nil, err
	}
	last, _ := excelize.CoordinatesToCellName(len(Columns), 1)
	_ = f.SetCellStyle(sheetTags, "A1", last, bold)
	_ = f.SetPanes(sheetTags, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	for i := range Columns {
		col, _ := excelize.ColumnNumberToName(i + 1)
		width := 14.0
		if i < 2 {
			width = 28
		}
		_ = f.SetColWidth(sheetTags, col, col, width)
	}

	// The type column offers the valid types as a drop-down, so the most common
	// mistake — "FLOAT", "WORD" — cannot be typed in the first place.
	typeCol, _ := excelize.ColumnNumberToName(3)
	dv := excelize.NewDataValidation(true)
	dv.Sqref = fmt.Sprintf("%s2:%s%d", typeCol, typeCol, MaxRows+1)
	if dvErr := dv.SetDropList(DataTypes); dvErr == nil {
		_ = f.AddDataValidation(sheetTags, dv)
	}

	help := helpSheet[l]
	if _, sheetErr := f.NewSheet(help.name); sheetErr != nil {
		return nil, sheetErr
	}
	for i, r := range help.rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		vals := []interface{}{r[0], r[1]}
		if rowErr := f.SetSheetRow(help.name, cell, &vals); rowErr != nil {
			return nil, rowErr
		}
	}
	_ = f.SetCellStyle(help.name, "A1", "B1", bold)
	_ = f.SetColWidth(help.name, "A", "A", 18)
	_ = f.SetColWidth(help.name, "B", "B", 110)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Examples returns template rows whose addresses are in the format the
// gateway's driver reads: a template with S7 addresses is no help on a Modbus
// gateway, and a wrong format is only discovered when the driver fails to read.
func Examples(l, driverType string) []Tag {
	it := lang(l) == "it"
	name := func(itName, enName string) string {
		if it {
			return itName
		}
		return enName
	}
	type ex struct{ addr, typ string }
	var a [4]ex
	switch strings.ToUpper(driverType) {
	case "MODBUS_TCP":
		a = [4]ex{{"40001", "REAL"}, {"00001", "BOOL"}, {"40003", "INT"}, {"40004", "DINT"}}
	case "OPC_UA":
		a = [4]ex{{"ns=2;s=Line1.Temperature", "REAL"}, {"ns=2;s=Line1.MotorRunning", "BOOL"},
			{"ns=2;s=Line1.Pressure", "INT"}, {"ns=2;s=Line1.Counter", "DINT"}}
	case "MQTT":
		a = [4]ex{{"plant/line1/temperature", "REAL"}, {"plant/line1/motor", "BOOL"},
			{"plant/line1/pressure", "INT"}, {"plant/line1/counter", "DINT"}}
	default: // S7
		a = [4]ex{{"DB1.DBD0", "REAL"}, {"M10.0", "BOOL"}, {"DB1.DBW4", "INT"}, {"DB1.DBD8", "DINT"}}
	}
	return []Tag{
		{Alias: name("Temperatura_Forno", "Oven_Temperature"), Address: a[0].addr, DataType: a[0].typ, Historize: true,
			Deadband: 0.5, Unit: "°C", Decimals: 1, RawMin: 0, RawMax: 100, EuMin: 0, EuMax: 100, Clamp: true},
		{Alias: name("Motore_In_Marcia", "Motor_Running"), Address: a[1].addr, DataType: a[1].typ, Historize: true,
			RawMax: 100, EuMax: 100, Clamp: true},
		{Alias: name("Pressione_Linea", "Line_Pressure"), Address: a[2].addr, DataType: a[2].typ, Historize: true,
			Deadband: 0.1, Unit: "bar", Decimals: 2, Scaling: true, RawMin: 0, RawMax: 27648, EuMin: 0, EuMax: 10, Clamp: true},
		{Alias: name("Pezzi_Prodotti", "Parts_Produced"), Address: a[3].addr, DataType: a[3].typ,
			Historize: true, RawMax: 100, EuMax: 100, Clamp: true},
	}
}

var helpSheet = map[string]struct {
	name string
	rows [][2]string
}{
	"it": {name: "Istruzioni", rows: [][2]string{
		{"Colonna", "Cosa contiene"},
		{"Nome", "Obbligatorio. Nome del tag come lo vedranno operatori e sinottici (max 100 caratteri)."},
		{"Indirizzo", "Obbligatorio. Indirizzo nel PLC: S7 «DB1.DBD0», «DB1.DBX0.0», «M10.0», «MW20»; Modbus «40001» (holding), «30001» (input), «00001» (coil), «10001» (ingresso digitale), «40001.3» (bit di un registro); OPC UA «ns=2;s=…»; MQTT il topic. Un tag con lo stesso indirizzo sul gateway viene aggiornato, non duplicato."},
		{"Tipo", "Obbligatorio. BOOL, INT, DINT, REAL o STRING."},
		{"Storicizza", "sì / no. Salva i valori nello storico."},
		{"Banda morta", "Variazione minima per salvare un nuovo valore (0 = ogni variazione). Si può scrivere 0,5 o 0.5."},
		{"Unità", "Unità di misura mostrata accanto al valore (°C, bar, pz/h…)."},
		{"Decimali", "Cifre decimali mostrate (0–10)."},
		{"Scalatura", "sì / no. Converte il valore grezzo del PLC in unità ingegneristiche con i quattro limiti seguenti."},
		{"Grezzo min / max", "Intervallo del valore grezzo (es. 0 – 27648 per un ingresso analogico S7). Min e max non possono coincidere."},
		{"Min EU / Max EU", "Intervallo corrispondente in unità ingegneristiche (es. 0 – 10 bar)."},
		{"Limita", "sì / no. Tiene il valore scalato dentro l'intervallo EU."},
		{"Inverti", "sì / no. Solo per BOOL: legge 1 come 0 e viceversa."},
		{"Percorso JSON", "Solo MQTT: campo da leggere se il messaggio è JSON (es. data.temp)."},
		{"", ""},
		{"Righe vuote", "Vengono ignorate. Le colonne che mancano non cambiano i tag esistenti."},
		{"Import", "Prima di salvare vedi un'anteprima: cosa viene creato, cosa aggiornato e gli errori riga per riga. Se c'è anche un solo errore non viene salvato niente."},
	}},
	"en": {name: "Instructions", rows: [][2]string{
		{"Column", "What it holds"},
		{"Name", "Required. The tag's name as operators and synoptics will see it (max 100 characters)."},
		{"Address", "Required. Address in the PLC: S7 “DB1.DBD0”, “DB1.DBX0.0”, “M10.0”, “MW20”; Modbus “40001” (holding), “30001” (input), “00001” (coil), “10001” (discrete input), “40001.3” (a bit of a register); OPC UA “ns=2;s=…”; MQTT the topic. A tag with the same address on the gateway is updated, not duplicated."},
		{"Type", "Required. BOOL, INT, DINT, REAL or STRING."},
		{"Historize", "yes / no. Store the values in the history."},
		{"Deadband", "Minimum change before a new value is stored (0 = every change)."},
		{"Unit", "Unit shown next to the value (°C, bar, parts/h…)."},
		{"Decimals", "Decimal places shown (0–10)."},
		{"Scaling", "yes / no. Converts the PLC's raw value to engineering units with the four limits below."},
		{"Raw min / max", "Range of the raw value (e.g. 0 – 27648 for an S7 analog input). Min and max cannot be equal."},
		{"EU min / EU max", "Matching range in engineering units (e.g. 0 – 10 bar)."},
		{"Clamp", "yes / no. Keeps the scaled value inside the EU range."},
		{"Invert", "yes / no. BOOL only: reads 1 as 0 and the other way round."},
		{"JSON path", "MQTT only: field to read when the message is JSON (e.g. data.temp)."},
		{"", ""},
		{"Empty rows", "Ignored. Missing columns leave existing tags unchanged."},
		{"Import", "Before saving you see a preview: what is created, what is updated, and the errors row by row. If there is a single error nothing is saved."},
	}},
}

// readXLSX reads the sheet that holds the tags: the one called "Tag"/"Tags" if
// there is one, else the first.
func readXLSX(data []byte) ([][]string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("excel: %w", err)
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("excel: no sheets")
	}
	name := sheets[0]
	for _, s := range sheets {
		if k := strings.ToLower(s); k == "tag" || k == "tags" {
			name = s
			break
		}
	}
	return f.GetRows(name)
}
