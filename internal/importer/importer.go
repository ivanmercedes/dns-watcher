package importer

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var domainRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9-]{1,63})+$`)

var headerWords = map[string]bool{
	"domain":      true,
	"domain name": true,
	"dominio":     true,
	"dominios":    true,
}

func NormalizeDomain(raw string) (string, bool) {
	cleaned := strings.TrimSpace(strings.ToLower(raw))
	cleaned = strings.TrimSuffix(cleaned, ".")
	if cleaned == "" || strings.HasPrefix(cleaned, "#") || headerWords[cleaned] {
		return "", false
	}
	if !domainRE.MatchString(cleaned) {
		return "", false
	}
	return cleaned, true
}

func ParseDomainLines(content string) []string {
	return dedupeDomains(strings.Split(content, "\n"))
}

func LoadFromTxt(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDomainLines(string(data)), nil
}

func LoadFromFile(path string) ([]string, error) {
	if strings.EqualFold(filepath.Ext(path), ".xlsx") {
		return LoadFromXLSX(path)
	}
	return LoadFromTxt(path)
}

func LoadFromXLSX(path string) ([]string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	files := make(map[string]*zip.File, len(r.File))
	for _, f := range r.File {
		files[f.Name] = f
	}

	sharedStrings, err := readSharedStrings(files["xl/sharedStrings.xml"])
	if err != nil {
		return nil, err
	}

	sheetFile := files["xl/worksheets/sheet1.xml"]
	if sheetFile == nil {
		var names []string
		for name := range files {
			if strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			return nil, errors.New("no worksheet found inside .xlsx archive")
		}
		sheetFile = files[names[0]]
	}

	rc, err := sheetFile.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	type cellXML struct {
		Type   string `xml:"t,attr"`
		Val    string `xml:"v"`
		Inline struct {
			Text string `xml:"t"`
		} `xml:"is"`
	}
	type rowXML struct {
		Cells []cellXML `xml:"c"`
	}
	type worksheetXML struct {
		Rows []rowXML `xml:"sheetData>row"`
	}

	rawXML, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var ws worksheetXML
	if err := xml.Unmarshal(rawXML, &ws); err != nil {
		return nil, err
	}

	var candidates []string
	for _, row := range ws.Rows {
		for _, c := range row.Cells {
			switch c.Type {
			case "s":
				idx, convErr := strconv.Atoi(strings.TrimSpace(c.Val))
				if convErr == nil && idx >= 0 && idx < len(sharedStrings) {
					candidates = append(candidates, sharedStrings[idx])
				}
			case "inlineStr":
				if c.Inline.Text != "" {
					candidates = append(candidates, c.Inline.Text)
				}
			default:
				if c.Val != "" {
					candidates = append(candidates, c.Val)
				}
			}
		}
	}
	return dedupeDomains(candidates), nil
}

func readSharedStrings(f *zip.File) ([]string, error) {
	if f == nil {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	type siXML struct {
		T []string `xml:"t"`
		R []struct {
			T string `xml:"t"`
		} `xml:"r"`
	}
	type sstXML struct {
		Items []siXML `xml:"si"`
	}
	var sst sstXML
	if err := xml.Unmarshal(data, &sst); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(sst.Items))
	for _, item := range sst.Items {
		if len(item.T) > 0 {
			out = append(out, strings.Join(item.T, ""))
			continue
		}
		var sb strings.Builder
		for _, run := range item.R {
			sb.WriteString(run.T)
		}
		out = append(out, sb.String())
	}
	return out, nil
}

func dedupeDomains(items []string) []string {
	seen := make(map[string]bool, len(items))
	var out []string
	for _, item := range items {
		if dom, ok := NormalizeDomain(item); ok && !seen[dom] {
			seen[dom] = true
			out = append(out, dom)
		}
	}
	return out
}
