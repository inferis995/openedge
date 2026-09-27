// Package tagsheet reads and writes the tag list of a gateway as a
// spreadsheet: CSV or Excel (.xlsx).
//
// The only import there was took the IEC 61131 declaration syntax,
// "Alias : DINT AT 40001;", pasted into a text box. That is what a TIA Portal
// or Codesys export looks like, and nothing else is: an engineer with a list of
// signals in Excel — which is where signal lists live — had to rewrite it by
// hand into a syntax the error messages then complained about.
//
// A sheet has one row per tag and a header row naming the columns. The header
// is matched loosely (case, accents, spaces and underscores do not matter) in
// Italian or English, so a sheet exported in one language imports in the
// other. Only alias, address and type are required. A column that is absent is
// left alone on an existing tag rather than reset to a default: a sheet with
// just three columns renames tags, it does not switch off their scaling.
//
// Problems are reported per row, as codes with the field they concern, so the
// web UI can say them in the user's language and point at the cell.
package tagsheet

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Field is a column the importer understands.
type Field string

// The columns, by their canonical (English) name.
const (
	FAlias     Field = "alias"
	FAddress   Field = "address"
	FDataType  Field = "data_type"
	FHistorize Field = "historize"
	FDeadband  Field = "deadband"
	FUnit      Field = "unit"
	FDecimals  Field = "decimals"
	FScaling   Field = "scaling"
	FRawMin    Field = "raw_min"
	FRawMax    Field = "raw_max"
	FEuMin     Field = "eu_min"
	FEuMax     Field = "eu_max"
	FClamp     Field = "clamp"
	FInvert    Field = "invert"
	FJSONPath  Field = "json_path"
)

// Columns is the order a sheet is written in, and the order the template
// shows.
var Columns = []Field{
	FAlias, FAddress, FDataType, FHistorize, FDeadband, FUnit, FDecimals,
	FScaling, FRawMin, FRawMax, FEuMin, FEuMax, FClamp, FInvert, FJSONPath,
}

// Headers are the column titles written out, per language. Any of them — and
// the aliases below — are accepted when reading.
var Headers = map[string]map[Field]string{
	"it": {
		FAlias: "Nome", FAddress: "Indirizzo", FDataType: "Tipo", FHistorize: "Storicizza",
		FDeadband: "Banda morta", FUnit: "Unità", FDecimals: "Decimali", FScaling: "Scalatura",
		FRawMin: "Grezzo min", FRawMax: "Grezzo max", FEuMin: "Min EU", FEuMax: "Max EU",
		FClamp: "Limita", FInvert: "Inverti", FJSONPath: "Percorso JSON",
	},
	"en": {
		FAlias: "Name", FAddress: "Address", FDataType: "Type", FHistorize: "Historize",
		FDeadband: "Deadband", FUnit: "Unit", FDecimals: "Decimals", FScaling: "Scaling",
		FRawMin: "Raw min", FRawMax: "Raw max", FEuMin: "EU min", FEuMax: "EU max",
		FClamp: "Clamp", FInvert: "Invert", FJSONPath: "JSON path",
	},
}

// aliases are further names people give these columns: the database's own
// names, and the words a signal list from a PLC project tends to use.
var aliases = map[Field][]string{
	FAlias:     {"alias", "tag", "tagname", "nome tag", "variabile", "symbol", "simbolo"},
	FAddress:   {"code", "codice", "indirizzo plc", "plc address", "register", "registro", "nodeid", "node id", "topic"},
	FDataType:  {"datatype", "data type", "tipo dato", "tipo di dato"},
	FHistorize: {"storico", "history", "storicizzazione"},
	FDeadband:  {"historize_deadband", "historize deadband", "banda"},
	FUnit:      {"eu_unit", "eu unit", "unita di misura", "um", "u.m."},
	FDecimals:  {"eu_decimals", "eu decimals"},
	FScaling:   {"scaling_enabled", "scaling enabled", "scala"},
	FRawMin:    {"scaling_raw_min", "raw_min", "raw min"},
	FRawMax:    {"scaling_raw_max", "raw_max", "raw max"},
	FEuMin:     {"scaling_eu_min", "eu_min"},
	FEuMax:     {"scaling_eu_max", "eu_max"},
	FClamp:     {"scaling_clamp", "limita al range"},
	FInvert:    {"inverti bool", "invert bool"},
	FJSONPath:  {"json_path", "json"},
}

// DataTypes are the types a tag may have — the CHECK constraint on
// tags.data_type.
var DataTypes = []string{"BOOL", "INT", "DINT", "REAL", "STRING"}

// Limits the database imposes on the text columns.
const (
	MaxAlias   = 100
	MaxAddress = 500
	// MaxRows bounds one import. A gateway with more tags than this is not a
	// gateway, and a file with more rows than this is a mistake.
	MaxRows = 20000
)

// Problem is one thing wrong with a row, or with the sheet when Line is 0.
type Problem struct {
	Line  int    `json:"line"`
	Field Field  `json:"field,omitempty"`
	Code  string `json:"code"`
	// Value is what the cell held, for the message.
	Value string `json:"value,omitempty"`
}

// Problem codes. The web UI has a sentence for each.
const (
	CodeRequired       = "required"
	CodeBadType        = "bad_type"
	CodeBadNumber      = "bad_number"
	CodeBadBool        = "bad_bool"
	CodeTooLong        = "too_long"
	CodeDuplicate      = "duplicate_address"
	CodeScalingRange   = "scaling_range"
	CodeNegative       = "negative"
	CodeDecimalsRange  = "decimals_range"
	CodeUnknownColumn  = "unknown_column"
	CodeMissingColumn  = "missing_column"
	CodeNoRows         = "no_rows"
	CodeTooManyRows    = "too_many_rows"
	CodeUnreadableFile = "unreadable_file"
)

// Row is one tag read from a sheet. The optional fields are nil when the
// column is absent or the cell is empty.
type Row struct {
	Line      int    `json:"line"`
	Alias     string `json:"alias"`
	Address   string `json:"address"`
	DataType  string `json:"data_type"`
	Historize *bool  `json:"historize,omitempty"`
	Deadband  *float64
	Unit      *string
	Decimals  *int
	Scaling   *bool
	RawMin    *float64
	RawMax    *float64
	EuMin     *float64
	EuMax     *float64
	Clamp     *bool
	Invert    *bool
	JSONPath  *string
}

// Sheet is what a file contained.
type Sheet struct {
	Rows []Row
	// Problems holds sheet-level problems (Line 0) and row problems.
	Problems []Problem
	// Present lists the columns the sheet has.
	Present map[Field]bool
}

// RowProblems returns the problems of one line.
func (s *Sheet) RowProblems(line int) []Problem {
	var out []Problem
	for _, p := range s.Problems {
		if p.Line == line {
			out = append(out, p)
		}
	}
	return out
}

// Blocking reports whether anything prevents the import. An unknown column is
// only a warning: it is ignored, and the user is told so.
func (s *Sheet) Blocking() bool {
	for _, p := range s.Problems {
		if p.Code != CodeUnknownColumn {
			return true
		}
	}
	return false
}

// Parse reads a CSV or XLSX file. The format is taken from the content, not
// the name: an .xlsx is a ZIP archive and starts with "PK".
func Parse(data []byte) *Sheet {
	var records [][]string
	var err error
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		records, err = readXLSX(data)
	} else {
		records, err = readCSV(data)
	}
	if err != nil {
		return &Sheet{Problems: []Problem{{Code: CodeUnreadableFile, Value: err.Error()}}}
	}
	return fromRecords(records)
}

// findHeader returns the index of the header row, or -1 for a sheet with
// nothing in it.
//
// It is the first row that names at least two columns this understands, within
// the first few. Sheets made by hand often have a title, a date or blank rows
// above the table. Failing that, the first non-blank row, so that its columns
// are reported as unknown or missing.
func findHeader(records [][]string) int {
	for i, r := range records {
		if i >= 10 {
			break
		}
		known := 0
		for _, c := range r {
			if _, ok := FieldFor(c); ok {
				known++
			}
		}
		if known >= 2 {
			return i
		}
	}
	for i, r := range records {
		if !blank(r) {
			return i
		}
	}
	return -1
}

func fromRecords(records [][]string) *Sheet {
	s := &Sheet{Present: map[Field]bool{}}

	head := findHeader(records)
	if head < 0 {
		s.Problems = append(s.Problems, Problem{Code: CodeNoRows})
		return s
	}

	cols := s.mapColumns(records[head])
	if !s.Present[FAlias] || !s.Present[FAddress] || !s.Present[FDataType] {
		return s
	}

	seen := map[string]int{}
	for i := head + 1; i < len(records); i++ {
		rec := records[i]
		if blank(rec) {
			continue
		}
		line := i + 1 // 1-based, as a spreadsheet numbers its rows
		if len(s.Rows) >= MaxRows {
			s.Problems = append(s.Problems, Problem{Line: line, Code: CodeTooManyRows, Value: strconv.Itoa(MaxRows)})
			break
		}
		row := Row{Line: line}
		cell := map[Field]string{}
		for j, f := range cols {
			if f != "" && j < len(rec) {
				cell[f] = strings.TrimSpace(rec[j])
			}
		}
		s.readRow(&row, cell)

		if row.Address != "" {
			if first, dup := seen[row.Address]; dup {
				s.Problems = append(s.Problems, Problem{
					Line: line, Field: FAddress, Code: CodeDuplicate, Value: strconv.Itoa(first),
				})
			} else {
				seen[row.Address] = line
			}
		}
		s.Rows = append(s.Rows, row)
	}
	if len(s.Rows) == 0 && !s.Blocking() {
		s.Problems = append(s.Problems, Problem{Code: CodeNoRows})
	}
	return s
}

// mapColumns matches the header's titles to fields, noting unknown columns
// and missing required ones.
func (s *Sheet) mapColumns(header []string) []Field {
	cols := make([]Field, len(header))
	for i, title := range header {
		f, ok := FieldFor(title)
		if !ok {
			if strings.TrimSpace(title) != "" {
				s.Problems = append(s.Problems, Problem{Code: CodeUnknownColumn, Value: title})
			}
			continue
		}
		cols[i] = f
		s.Present[f] = true
	}
	for _, f := range []Field{FAlias, FAddress, FDataType} {
		if !s.Present[f] {
			s.Problems = append(s.Problems, Problem{Field: f, Code: CodeMissingColumn})
		}
	}
	return cols
}

func (s *Sheet) readRow(row *Row, cell map[Field]string) {
	s.readRequired(row, cell)
	s.readOptional(row, cell)
}

func (s *Sheet) add(line int, f Field, code, value string) {
	s.Problems = append(s.Problems, Problem{Line: line, Field: f, Code: code, Value: value})
}

func (s *Sheet) readRequired(row *Row, cell map[Field]string) {
	add := func(f Field, code, value string) { s.add(row.Line, f, code, value) }

	row.Alias = cell[FAlias]
	row.Address = cell[FAddress]
	row.DataType = strings.ToUpper(cell[FDataType])
	if row.Alias == "" {
		add(FAlias, CodeRequired, "")
	} else if len([]rune(row.Alias)) > MaxAlias {
		add(FAlias, CodeTooLong, strconv.Itoa(MaxAlias))
	}
	if row.Address == "" {
		add(FAddress, CodeRequired, "")
	} else if len([]rune(row.Address)) > MaxAddress {
		add(FAddress, CodeTooLong, strconv.Itoa(MaxAddress))
	}
	switch {
	case row.DataType == "":
		add(FDataType, CodeRequired, "")
	case !validType(row.DataType):
		add(FDataType, CodeBadType, cell[FDataType])
	}
}

func (s *Sheet) readOptional(row *Row, cell map[Field]string) {
	add := func(f Field, code, value string) { s.add(row.Line, f, code, value) }

	boolean := func(f Field) *bool {
		v, present := cell[f]
		if !present || v == "" {
			return nil
		}
		b, ok := ParseBool(v)
		if !ok {
			add(f, CodeBadBool, v)
			return nil
		}
		return &b
	}
	number := func(f Field) *float64 {
		v, present := cell[f]
		if !present || v == "" {
			return nil
		}
		n, ok := ParseNumber(v)
		if !ok {
			add(f, CodeBadNumber, v)
			return nil
		}
		return &n
	}
	text := func(f Field) *string {
		v, present := cell[f]
		if !present {
			return nil
		}
		return &v
	}

	row.Historize = boolean(FHistorize)
	row.Deadband = number(FDeadband)
	if row.Deadband != nil && *row.Deadband < 0 {
		add(FDeadband, CodeNegative, cell[FDeadband])
	}
	row.Unit = text(FUnit)
	if d := number(FDecimals); d != nil {
		if *d < 0 || *d > 10 || *d != float64(int(*d)) {
			add(FDecimals, CodeDecimalsRange, cell[FDecimals])
		} else {
			n := int(*d)
			row.Decimals = &n
		}
	}
	row.Scaling = boolean(FScaling)
	row.RawMin = number(FRawMin)
	row.RawMax = number(FRawMax)
	row.EuMin = number(FEuMin)
	row.EuMax = number(FEuMax)
	row.Clamp = boolean(FClamp)
	row.Invert = boolean(FInvert)
	row.JSONPath = text(FJSONPath)

	// Scaling divides by (raw max − raw min). Equal bounds are a division by
	// zero at ingestion; the tag would read NaN forever.
	if row.Scaling != nil && *row.Scaling && row.RawMin != nil && row.RawMax != nil && *row.RawMin == *row.RawMax {
		add(FRawMax, CodeScalingRange, cell[FRawMax])
	}
}

// FieldFor matches a column title to a field.
func FieldFor(title string) (Field, bool) {
	k := key(title)
	if k == "" {
		return "", false
	}
	for _, f := range Columns {
		if key(string(f)) == k {
			return f, true
		}
		for _, lang := range Headers {
			if key(lang[f]) == k {
				return f, true
			}
		}
		for _, a := range aliases[f] {
			if key(a) == k {
				return f, true
			}
		}
	}
	return "", false
}

// key folds a title for comparison: lower case, no accents, no spaces,
// underscores, dots or hyphens.
func key(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(strings.TrimSpace(s))) {
		if unicode.Is(unicode.Mn, r) || r == ' ' || r == '_' || r == '-' || r == '.' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ParseBool reads a yes/no cell as people fill them in.
func ParseBool(v string) (bool, bool) {
	switch key(v) {
	case "1", "true", "yes", "y", "si", "s", "x", "on", "vero":
		return true, true
	case "0", "false", "no", "n", "off", "falso":
		return false, true
	}
	return false, false
}

// ParseNumber reads a number with either decimal separator. An Italian Excel
// writes "0,5"; a CSV from anywhere else writes "0.5". A cell with both, as in
// "1.234,5", is read the Italian way.
func ParseNumber(v string) (float64, bool) {
	v = strings.ReplaceAll(strings.TrimSpace(v), " ", "")
	if strings.Contains(v, ",") {
		if strings.Contains(v, ".") {
			v = strings.ReplaceAll(v, ".", "")
		}
		v = strings.ReplaceAll(v, ",", ".")
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func validType(t string) bool {
	for _, d := range DataTypes {
		if d == t {
			return true
		}
	}
	return false
}

func blank(r []string) bool {
	for _, c := range r {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// readCSV reads a CSV whatever its separator. Excel in Italian writes ";",
// most other tools ",", and a copy from a table often gives tabs; the one that
// splits the header into the most columns wins.
func readCSV(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // Excel's UTF-8 BOM
	firstLine := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		firstLine = data[:i]
	}
	sep := ','
	best := 0
	for _, c := range []rune{';', ',', '\t'} {
		if n := bytes.Count(firstLine, []byte(string(c))); n > best {
			best, sep = n, c
		}
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = sep
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var out [][]string
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv: %w", err)
		}
		// encoding/csv skips blank lines. Keep them as empty records, or every
		// row after one is reported on the wrong spreadsheet line.
		line, _ := r.FieldPos(0)
		for len(out) < line-1 {
			out = append(out, nil)
		}
		out = append(out, rec)
	}
	return out, nil
}
