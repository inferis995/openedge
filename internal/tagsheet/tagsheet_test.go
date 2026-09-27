package tagsheet

import (
	"reflect"
	"strings"
	"testing"
)

func codes(s *Sheet) []string {
	var out []string
	for _, p := range s.Problems {
		out = append(out, p.Code+":"+string(p.Field))
	}
	return out
}

func TestAnItalianExcelCSVImports(t *testing.T) {
	// What Excel in an Italian locale saves: BOM, semicolons, decimal commas,
	// accented header, "sì".
	csv := "\xef\xbb\xbfNome;Indirizzo;Tipo;Storicizza;Banda morta;Unità\n" +
		"Temperatura;DB1.DBD0;real;sì;0,5;°C\n" +
		"\n" +
		"Motore;M10.0;BOOL;no;;\n"
	s := Parse([]byte(csv))
	if s.Blocking() {
		t.Fatalf("problems: %v", codes(s))
	}
	if len(s.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(s.Rows))
	}
	r := s.Rows[0]
	if r.Alias != "Temperatura" || r.Address != "DB1.DBD0" || r.DataType != "REAL" {
		t.Errorf("row 1 = %+v", r)
	}
	if r.Historize == nil || !*r.Historize || r.Deadband == nil || *r.Deadband != 0.5 || r.Unit == nil || *r.Unit != "°C" {
		t.Errorf("row 1 optional fields: historize=%v deadband=%v unit=%v", r.Historize, r.Deadband, r.Unit)
	}
	if s.Rows[1].Line != 4 {
		t.Errorf("the second tag is on spreadsheet line %d, want 4 (blank line counted)", s.Rows[1].Line)
	}
	// An empty cell is "not given", not zero: an update must leave it alone.
	if s.Rows[1].Deadband != nil {
		t.Errorf("an empty deadband cell read as %v", *s.Rows[1].Deadband)
	}
}

func TestAnEnglishCommaCSVWithATitleRowImports(t *testing.T) {
	csv := "Signal list line 1,,\n,,\ntag,Address,Data Type,Scaling,Raw min,Raw max,EU min,EU max\n" +
		"Pressure,40001,INT,yes,0,27648,0,10\n"
	s := Parse([]byte(csv))
	if s.Blocking() {
		t.Fatalf("problems: %v", codes(s))
	}
	r := s.Rows[0]
	if r.Line != 4 || r.Scaling == nil || !*r.Scaling || *r.RawMax != 27648 || *r.EuMax != 10 {
		t.Errorf("row = %+v", r)
	}
	if s.Present[FHistorize] {
		t.Error("a column that is not in the sheet is reported as present")
	}
}

func TestEveryProblemIsReportedOnItsRow(t *testing.T) {
	csv := "Name;Address;Type;Deadband;Decimals;Scaling;Raw min;Raw max;Historize\n" +
		";DB1.DBD0;REAL;;;;;;\n" + // line 2: no name
		"A;;WORD;;;;;;\n" + // line 3: no address, bad type
		"B;DB1.DBD4;REAL;-1;12;;;;maybe\n" + // line 4: negative deadband, decimals, bad bool
		"C;DB1.DBD4;REAL;abc;;;;;\n" + // line 5: duplicate address, bad number
		"D;DB1.DBD8;REAL;;;yes;5;5;\n" // line 6: scaling min == max
	s := Parse([]byte(csv))
	want := map[int][]string{
		2: {"required:alias"},
		3: {"required:address", "bad_type:data_type"},
		4: {"negative:deadband", "decimals_range:decimals", "bad_bool:historize"},
		5: {"bad_number:deadband", "duplicate_address:address"},
		6: {"scaling_range:raw_max"},
	}
	for line, w := range want {
		var got []string
		for _, p := range s.RowProblems(line) {
			got = append(got, p.Code+":"+string(p.Field))
		}
		if !sameSet(got, w) {
			t.Errorf("line %d: problems %v, want %v", line, got, w)
		}
	}
	if !s.Blocking() {
		t.Error("a sheet with errors is not blocking")
	}
}

func TestAMissingRequiredColumnIsNamed(t *testing.T) {
	s := Parse([]byte("Name,Type,Colore\nA,REAL,rosso\n"))
	got := codes(s)
	if !sameSet(got, []string{"unknown_column:", "missing_column:address"}) {
		t.Errorf("problems = %v", got)
	}
}

func TestAnUnknownColumnAloneDoesNotBlock(t *testing.T) {
	s := Parse([]byte("Name,Address,Type,Commento\nA,40001,INT,x\n"))
	if s.Blocking() {
		t.Errorf("an extra column blocked the import: %v", codes(s))
	}
}

func TestAWrittenSheetReadsBackTheSame(t *testing.T) {
	tags := Examples("it", "S7")
	tags = append(tags, Tag{Alias: "Nome; con, separatori \"e virgolette\"", Address: "DB2.DBX0.1",
		DataType: "BOOL", Invert: true, JSONPath: "a.b"})
	for _, lang := range []string{"it", "en"} {
		for name, write := range map[string]func(string, []Tag) ([]byte, error){"csv": WriteCSV, "xlsx": WriteXLSX} {
			data, err := write(lang, tags)
			if err != nil {
				t.Fatalf("%s/%s: %v", lang, name, err)
			}
			s := Parse(data)
			if s.Blocking() || len(s.Rows) != len(tags) {
				t.Fatalf("%s/%s: %d rows, problems %v", lang, name, len(s.Rows), codes(s))
			}
			for i := range s.Rows {
				if got := back(&s.Rows[i]); !reflect.DeepEqual(got, tags[i]) {
					t.Errorf("%s/%s row %d:\n got %+v\nwant %+v", lang, name, i, got, tags[i])
				}
			}
		}
	}
}

func TestTemplatesUseTheirDriversAddressFormat(t *testing.T) {
	for driver, want := range map[string]string{"S7": "DB1.", "MODBUS_TCP": "4000", "OPC_UA": "ns=2;", "MQTT": "plant/"} {
		if a := Examples("en", driver)[0].Address; !strings.HasPrefix(a, want) {
			t.Errorf("%s template address %q", driver, a)
		}
	}
}

func TestNumbersInEitherNotation(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
	}{{"0,5", 0.5}, {"0.5", 0.5}, {"1.234,5", 1234.5}, {"-3", -3}, {" 27648 ", 27648}} {
		if got, ok := ParseNumber(c.in); !ok || got != c.want {
			t.Errorf("ParseNumber(%q) = %v, %v", c.in, got, ok)
		}
	}
	if _, ok := ParseNumber("1,2,3"); ok {
		t.Error("1,2,3 read as a number")
	}
}

func back(r *Row) Tag {
	t := Tag{Alias: r.Alias, Address: r.Address, DataType: r.DataType}
	if r.Historize != nil {
		t.Historize = *r.Historize
	}
	if r.Deadband != nil {
		t.Deadband = *r.Deadband
	}
	if r.Unit != nil {
		t.Unit = *r.Unit
	}
	if r.Decimals != nil {
		t.Decimals = *r.Decimals
	}
	if r.Scaling != nil {
		t.Scaling = *r.Scaling
	}
	for _, p := range []struct {
		dst *float64
		src *float64
	}{{&t.RawMin, r.RawMin}, {&t.RawMax, r.RawMax}, {&t.EuMin, r.EuMin}, {&t.EuMax, r.EuMax}} {
		if p.src != nil {
			*p.dst = *p.src
		}
	}
	if r.Clamp != nil {
		t.Clamp = *r.Clamp
	}
	if r.Invert != nil {
		t.Invert = *r.Invert
	}
	if r.JSONPath != nil {
		t.JSONPath = *r.JSONPath
	}
	return t
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}
