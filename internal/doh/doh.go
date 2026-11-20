package doh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var endpoints = map[string]string{
	"cloudflare": "https://cloudflare-dns.com/dns-query",
	"google":     "https://dns.google/resolve",
}

var typeCodes = map[string]int{
	"A":     1,
	"NS":    2,
	"CNAME": 5,
	"SOA":   6,
	"MX":    15,
	"TXT":   16,
	"AAAA":  28,
}

type Result struct {
	Domain     string
	RecordType string
	Records    []string
	Status     int
	OK         bool
	Error      string
}

type dohResponse struct {
	Status int `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

func NormalizeRecordValue(recordType, data string) string {
	val := strings.TrimSpace(data)
	switch recordType {
	case "CNAME", "NS", "MX", "SOA":
		val = strings.ToLower(strings.TrimSuffix(val, "."))
	case "TXT":
		if len(val) >= 2 && strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
			val = val[1 : len(val)-1]
		}
	}
	return val
}

func QueryDoH(client *http.Client, domain, recordType, provider string, retries int) Result {
	providers := []string{provider}
	for _, p := range []string{"cloudflare", "google"} {
		if p != provider {
			providers = append(providers, p)
		}
	}

	var lastErr string
	for attempt := 0; attempt <= retries; attempt++ {
		p := providers[min(attempt, len(providers)-1)]
		base, ok := endpoints[p]
		if !ok {
			base = endpoints["cloudflare"]
		}
		u := fmt.Sprintf("%s?name=%s&type=%s", base, url.QueryEscape(domain), url.QueryEscape(recordType))
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		req.Header.Set("Accept", "application/dns-json")
		req.Header.Set("User-Agent", "dns-watcher/0.1")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err.Error()
			if attempt < retries {
				time.Sleep(time.Duration(250*(attempt+1)) * time.Millisecond)
			}
			continue
		}

		var payload dohResponse
		decErr := json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if decErr != nil {
			lastErr = decErr.Error()
			continue
		}
		if payload.Status != 0 && payload.Status != 3 {
			lastErr = fmt.Sprintf("DNS RCODE %d", payload.Status)
			continue
		}

		expectedType := typeCodes[recordType]
		seen := make(map[string]bool)
		var values []string
		for _, ans := range payload.Answer {
			if expectedType != 0 && ans.Type != expectedType {
				continue
			}
			norm := NormalizeRecordValue(recordType, ans.Data)
			if norm != "" && !seen[norm] {
				seen[norm] = true
				values = append(values, norm)
			}
		}
		sort.Strings(values)
		return Result{
			Domain:     domain,
			RecordType: recordType,
			Records:    values,
			Status:     payload.Status,
			OK:         true,
		}
	}

	return Result{
		Domain:     domain,
		RecordType: recordType,
		Status:     -1,
		OK:         false,
		Error:      lastErr,
	}
}

func QueryMany(domains, recordTypes []string, provider string, workers, timeoutSec, retries int) []Result {
	type task struct {
		domain     string
		recordType string
	}
	var tasks []task
	for _, d := range domains {
		for _, rt := range recordTypes {
			tasks = append(tasks, task{domain: d, recordType: strings.ToUpper(rt)})
		}
	}
	if len(tasks) == 0 {
		return nil
	}
	if workers <= 0 {
		workers = 20
	}
	if workers > len(tasks) {
		workers = len(tasks)
	}

	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	results := make([]Result, len(tasks))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i, t := range tasks {
		wg.Add(1)
		go func(idx int, tk task) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = QueryDoH(client, tk.domain, tk.recordType, provider, retries)
		}(i, t)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		if results[i].Domain == results[j].Domain {
			return results[i].RecordType < results[j].RecordType
		}
		return results[i].Domain < results[j].Domain
	})
	return results
}
