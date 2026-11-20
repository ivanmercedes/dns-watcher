package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"dns-watcher/internal/config"
	"dns-watcher/internal/doh"
	"dns-watcher/internal/importer"
	"dns-watcher/internal/notifier"
	"dns-watcher/internal/storage"
)

const version = "0.2.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	appDir, err := config.GetAppDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error obteniendo directorio de datos: %v\n", err)
		return 1
	}
	cfg, err := config.LoadConfig(appDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error cargando configuración: %v\n", err)
		return 1
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("dns-watcher %s\n", version)
		return 0
	case "help", "--help", "-h":
		printUsage()
		return 0
	case "init":
		return cmdInit(args[1:], appDir, cfg)
	case "check":
		return cmdCheck(appDir, cfg)
	case "watch":
		return cmdWatch(args[1:], appDir, cfg)
	case "history":
		return cmdHistory(args[1:], appDir)
	case "status":
		return cmdStatus(appDir, cfg)
	case "domains":
		return cmdDomains(args[1:], appDir)
	case "config":
		return cmdConfig(args[1:], appDir, cfg)
	default:
		fmt.Fprintf(os.Stderr, "Comando desconocido: %s\n", args[0])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Println(`dns-watcher - Monitor de cambios de DNS (Go)

Uso:
  dns-watcher init [--file ruta.xlsx|ruta.txt]   Captura la línea base inicial
  dns-watcher check                              Revisa cambios una vez y notifica
  dns-watcher watch [--interval 300]             Monitorea continuamente
  dns-watcher history [--domain x] [--limit 50]  Muestra historial de cambios
  dns-watcher status                             Muestra estado actual
  dns-watcher domains list|import|add|remove     Gestiona dominios
  dns-watcher config show|test-notify            Ver configuración o probar alertas
  dns-watcher version                            Muestra la versión`)
}

func domainsPath(appDir string) string {
	return filepath.Join(appDir, "domains.txt")
}

func loadActiveDomains(appDir string) ([]string, error) {
	p := domainsPath(appDir)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil, nil
	}
	return importer.LoadFromFile(p)
}

func saveActiveDomains(appDir string, domains []string) error {
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	unique := make([]string, 0, len(domains))
	seen := make(map[string]bool, len(domains))
	for _, d := range domains {
		if norm, ok := importer.NormalizeDomain(d); ok && !seen[norm] {
			seen[norm] = true
			unique = append(unique, norm)
		}
	}
	sort.Strings(unique)
	content := strings.Join(unique, "\n") + "\n"
	return os.WriteFile(domainsPath(appDir), []byte(content), 0o644)
}

func cmdInit(args []string, appDir string, cfg config.AppConfig) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	filePath := fs.String("file", "", "Archivo .xlsx o .txt de dominios")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *filePath != "" {
		imported, err := importer.LoadFromFile(*filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error importando %s: %v\n", *filePath, err)
			return 1
		}
		if err := saveActiveDomains(appDir, imported); err != nil {
			fmt.Fprintf(os.Stderr, "Error guardando dominios: %v\n", err)
			return 1
		}
	}

	domains, err := loadActiveDomains(appDir)
	if err != nil || len(domains) == 0 {
		fmt.Fprintln(os.Stderr, "No hay dominios configurados. Usa: dns-watcher domains import Dominios.xlsx")
		return 1
	}

	fmt.Printf("Escaneando línea base de %d dominio(s) (%s)...\n", len(domains), strings.Join(cfg.RecordTypes, ", "))
	results := doh.QueryMany(domains, cfg.RecordTypes, cfg.DoHProvider, cfg.Workers, cfg.TimeoutSeconds, cfg.Retries)
	okCount := 0
	for _, r := range results {
		if r.OK {
			okCount++
		}
	}

	st, err := storage.Open(filepath.Join(appDir, "state.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error abriendo estado: %v\n", err)
		return 1
	}
	if _, err := st.ApplyResults(results, false); err != nil {
		fmt.Fprintf(os.Stderr, "Error guardando estado: %v\n", err)
		return 1
	}
	if _, err := config.SaveConfig(appDir, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error guardando configuración: %v\n", err)
		return 1
	}
	fmt.Printf("Línea base guardada en %s (%d consultas OK, %d fallidas).\n", filepath.Join(appDir, "state.json"), okCount, len(results)-okCount)
	return 0
}

func cmdCheck(appDir string, cfg config.AppConfig) int {
	domains, err := loadActiveDomains(appDir)
	if err != nil || len(domains) == 0 {
		fmt.Fprintln(os.Stderr, "No hay dominios configurados.")
		return 1
	}
	results := doh.QueryMany(domains, cfg.RecordTypes, cfg.DoHProvider, cfg.Workers, cfg.TimeoutSeconds, cfg.Retries)
	okCount := 0
	for _, r := range results {
		if r.OK {
			okCount++
		}
	}

	st, err := storage.Open(filepath.Join(appDir, "state.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error abriendo estado: %v\n", err)
		return 1
	}
	changes, err := st.ApplyResults(results, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error guardando estado: %v\n", err)
		return 1
	}

	if len(changes) > 0 {
		fmt.Println(notifier.FormatChangesText(changes))
		for _, nErr := range notifier.NotifyChanges(changes, cfg.Notify, appDir) {
			fmt.Fprintf(os.Stderr, "[AVISO] Error enviando notificación (%s)\n", nErr)
		}
	} else {
		fmt.Printf("Sin cambios en %d dominio(s) (%d OK, %d errores de red).\n", len(domains), okCount, len(results)-okCount)
	}
	return 0
}

func cmdWatch(args []string, appDir string, cfg config.AppConfig) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	interval := fs.Int("interval", 0, "Segundos entre revisiones")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	sec := *interval
	if sec <= 0 {
		sec = cfg.IntervalSeconds
	}
	fmt.Printf("Iniciando monitoreo continuo cada %ds (Ctrl+C para detener)...\n", sec)
	for {
		cmdCheck(appDir, cfg)
		time.Sleep(time.Duration(sec) * time.Second)
	}
}

func cmdHistory(args []string, appDir string) int {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	domain := fs.String("domain", "", "Filtrar por dominio")
	limit := fs.Int("limit", 50, "Máximo de registros")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	st, err := storage.Open(filepath.Join(appDir, "state.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error abriendo estado: %v\n", err)
		return 1
	}
	items := st.GetHistory(*domain, *limit)
	if len(items) == 0 {
		fmt.Println("No hay cambios registrados aún.")
		return 0
	}
	fmt.Println(notifier.FormatChangesText(items))
	return 0
}

func cmdStatus(appDir string, cfg config.AppConfig) int {
	domains, _ := loadActiveDomains(appDir)
	st, err := storage.Open(filepath.Join(appDir, "state.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error abriendo estado: %v\n", err)
		return 1
	}
	lastCheck := st.State.LastCheck
	if lastCheck == "" {
		lastCheck = "never"
	}
	fmt.Printf("DNS Watcher v%s\n", version)
	fmt.Printf("Directorio de datos : %s\n", appDir)
	fmt.Printf("Dominios en lista   : %d\n", len(domains))
	fmt.Printf("Tipos de registro   : %s\n", strings.Join(cfg.RecordTypes, ", "))
	fmt.Printf("Dominios en estado  : %d\n", st.UniqueDomainsCount())
	fmt.Printf("Registros guardados : %d\n", len(st.State.Snapshots))
	fmt.Printf("Cambios históricos  : %d\n", len(st.State.History))
	fmt.Printf("Última revisión UTC : %s\n", lastCheck)
	return 0
}

func cmdDomains(args []string, appDir string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Uso: dns-watcher domains list|import|add|remove")
		return 1
	}
	current, _ := loadActiveDomains(appDir)
	switch args[0] {
	case "list":
		for _, d := range current {
			fmt.Println(d)
		}
		fmt.Printf("Total: %d dominio(s)\n", len(current))
		return 0
	case "import":
		fs := flag.NewFlagSet("import", flag.ContinueOnError)
		merge := fs.Bool("merge", false, "Combinar con dominios existentes")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "Uso: dns-watcher domains import [--merge] <archivo.xlsx|txt>")
			return 1
		}
		imported, err := importer.LoadFromFile(fs.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error importando: %v\n", err)
			return 1
		}
		finalList := imported
		if *merge {
			finalList = append(current, imported...)
		}
		if err := saveActiveDomains(appDir, finalList); err != nil {
			fmt.Fprintf(os.Stderr, "Error guardando: %v\n", err)
			return 1
		}
		updated, _ := loadActiveDomains(appDir)
		fmt.Printf("Importados %d dominio(s). Total activo: %d en %s\n", len(imported), len(updated), domainsPath(appDir))
		return 0
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Uso: dns-watcher domains add <dominio>")
			return 1
		}
		dom, ok := importer.NormalizeDomain(args[1])
		if !ok {
			fmt.Fprintf(os.Stderr, "Dominio inválido: %s\n", args[1])
			return 1
		}
		if err := saveActiveDomains(appDir, append(current, dom)); err != nil {
			fmt.Fprintf(os.Stderr, "Error guardando: %v\n", err)
			return 1
		}
		fmt.Printf("Agregado %s.\n", dom)
		return 0
	case "remove":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Uso: dns-watcher domains remove <dominio>")
			return 1
		}
		target := strings.ToLower(strings.TrimSpace(args[1]))
		filtered := slices.DeleteFunc(append([]string{}, current...), func(s string) bool { return s == target })
		if err := saveActiveDomains(appDir, filtered); err != nil {
			fmt.Fprintf(os.Stderr, "Error guardando: %v\n", err)
			return 1
		}
		fmt.Printf("Eliminado %s. Total: %d\n", target, len(filtered))
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Subcomando desconocido: %s\n", args[0])
		return 1
	}
}

func cmdConfig(args []string, appDir string, cfg config.AppConfig) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Uso: dns-watcher config show|test-notify")
		return 1
	}
	switch args[0] {
	case "show":
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(data))
		return 0
	case "test-notify":
		sample := []storage.Change{{
			Domain:     "ejemplo.com",
			RecordType: "A",
			OldRecords: []string{"1.1.1.1"},
			NewRecords: []string{"2.2.2.2"},
			DetectedAt: "test",
		}}
		errs := notifier.NotifyChanges(sample, cfg.Notify, appDir)
		if len(errs) > 0 {
			fmt.Fprintf(os.Stderr, "Errores en prueba de notificación: %v\n", errs)
			return 1
		}
		fmt.Println("Notificación de prueba enviada.")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Subcomando desconocido: %s\n", args[0])
		return 1
	}
}
