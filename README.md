# DNS Watcher (Go)

Monitor de cambios de DNS escrito en **Go** (binario único sin dependencias externas). Compatible con **Windows 10/11**, **macOS** y **Linux**.

## Qué hace

- Consulta registros `A`, `AAAA`, `CNAME`, `MX`, `NS` y `TXT` de forma concurrente vía DNS-over-HTTPS (Cloudflare con respaldo de Google).
- Guarda la línea base e historial en `state.json` (sin CGO, 100% portable).
- Ignora errores transitorios de red para evitar falsos positivos.
- Avisa cuando detecta cambios mediante:
  - **Notificación nativa de Windows (Toast)**
  - **Consola y archivo `changes.log`**
  - **Discord / Slack / Telegram / Webhook genérico**
  - **Correo electrónico (SMTP)**

---

## Instalación en Windows (`irm`)

Ejecuta en **PowerShell**:

```powershell
irm https://raw.githubusercontent.com/ivanmercedes/dns-watcher/main/install.ps1 | iex
```

Para instalar **y programar revisión automática cada 15 minutos** en el Programador de Tareas de Windows:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/ivanmercedes/dns-watcher/main/install.ps1))) -Schedule -IntervalMinutes 15
```

---

## Instalación en Linux / macOS (`curl`)

```bash
curl -fsSL https://raw.githubusercontent.com/ivanmercedes/dns-watcher/main/install.sh | bash
```

---

## Uso rápido

```powershell
# 1. Importa tu archivo .xlsx o .txt de dominios
dns-watcher domains import "C:\ruta\a\Dominios.xlsx"

# 2. Captura la línea base inicial (sin alertar)
dns-watcher init

# 3. Revisa cambios una vez o monitorea en bucle
dns-watcher check
dns-watcher watch --interval 300

# Ver estado, historial o probar notificaciones
dns-watcher status
dns-watcher history
dns-watcher config test-notify
```

## Releases automáticos

Cada tag `v*` (por ejemplo `v0.2.0`) ejecuta GitHub Actions (`.github/workflows/release.yml`), corre `go test -v ./...`, compila binarios para Windows (`amd64`, `386`), Linux (`amd64`, `arm64`) y macOS (`amd64`, `arm64`) y publica el Release automáticamente.
