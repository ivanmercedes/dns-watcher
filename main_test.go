package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"dns-watcher/internal/config"
	"dns-watcher/internal/doh"
	"dns-watcher/internal/importer"
	"dns-watcher/internal/notifier"
	"dns-watcher/internal/storage"
)

func createTestXLSX(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create xlsx: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	files := map[string]string{
		"xl/sharedStrings.xml": `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>Domain Name</t></si>
  <si><t>EXAMPLE.COM</t></si>
  <si><t>sub.example.org</t></si>
  <si><t>xn--casadeespaa-beb.com</t></si>
</sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c></row>
    <row r="2"><c r="A2" t="s"><v>1</v></c></row>
    <row r="3"><c r="A3" t="s"><v>2</v></c></row>
    <row r="4"><c r="A4" t="s"><v>3</v></c></row>
  </sheetData>
</worksheet>`,
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

func TestImportXLSX(t *testing.T) {
	xlsxPath := filepath.Join(t.TempDir(), "test_domains.xlsx")
	createTestXLSX(t, xlsxPath)

	domains, err := importer.LoadFromFile(xlsxPath)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}
	if len(domains) != 3 {
		t.Fatalf("expected 3 domains, got %d (%v)", len(domains), domains)
	}
	if !slices.Contains(domains, "example.com") {
		t.Errorf("expected example.com in imported domains")
	}
	if !slices.Contains(domains, "xn--casadeespaa-beb.com") {
		t.Errorf("expected punycode domain in imported domains")
	}
}

func TestNormalizeRecordValue(t *testing.T) {
	if got := doh.NormalizeRecordValue("CNAME", "Target.Example.COM."); got != "target.example.com" {
		t.Errorf("unexpected CNAME normalization: %q", got)
	}
	if got := doh.NormalizeRecordValue("TXT", `"v=spf1 ~all"`); got != "v=spf1 ~all" {
		t.Errorf("unexpected TXT normalization: %q", got)
	}
}

func TestStorageBaselineAndDiff(t *testing.T) {
	dir := t.TempDir()
	st, err := storage.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	baseline := []doh.Result{
		{Domain: "example.com", RecordType: "A", Records: []string{"1.1.1.1"}, Status: 0, OK: true},
	}
	changes, err := st.ApplyResults(baseline, false)
	if err != nil || len(changes) != 0 {
		t.Fatalf("expected 0 changes on init, got %d (err=%v)", len(changes), err)
	}

	// Transient failure should not trigger false positive
	failed := []doh.Result{{Domain: "example.com", RecordType: "A", Status: -1, OK: false, Error: "timeout"}}
	changes, err = st.ApplyResults(failed, true)
	if err != nil || len(changes) != 0 {
		t.Fatalf("expected 0 changes on failed query, got %d", len(changes))
	}

	// Real change
	updated := []doh.Result{{Domain: "example.com", RecordType: "A", Records: []string{"8.8.8.8"}, Status: 0, OK: true}}
	changes, err = st.ApplyResults(updated, true)
	if err != nil || len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d (err=%v)", len(changes), err)
	}

	errs := notifier.NotifyChanges(changes, config.NotifyConfig{LogFile: true}, dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected notify errors: %v", errs)
	}
	if _, err := os.Stat(filepath.Join(dir, "changes.log")); err != nil {
		t.Fatalf("expected changes.log to exist: %v", err)
	}
}
